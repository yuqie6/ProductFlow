package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// ClaimForConsumption 从 SENT 抢消费 lease。抢不到说明另一 worker 正在跑或行已不是 SENT。
func ClaimForConsumption(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID string, leaseSeconds int) (string, bool, error) {
	if leaseSeconds <= 0 {
		leaseSeconds = DefaultConsumerLeaseSeconds
	}
	now := time.Now().UTC()
	token := clockid.New()
	tag, err := pool.Exec(ctx, `
		UPDATE async_dispatches SET
			lease_token = $3,
			lease_expires_at = $4,
			updated_at = $5
		WHERE id = $1
		  AND aggregate_id = $2
		  AND status = 'sent'
		  AND lease_token IS NULL
	`, dispatchID, aggregateID, token, now.Add(time.Duration(leaseSeconds)*time.Second), now)
	if err != nil {
		return "", false, err
	}
	if tag.RowsAffected() != 1 {
		return "", false, nil
	}
	return token, true, nil
}

// MarkConsumed 把 SENT 标 CONSUMED。SENT 只表示已交给 broker。
func MarkConsumed(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID, leaseToken string) (bool, error) {
	now := time.Now().UTC()
	tag, err := pool.Exec(ctx, `
		UPDATE async_dispatches SET
			status = 'consumed',
			lease_token = NULL,
			lease_expires_at = NULL,
			consumed_at = $4,
			updated_at = $4
		WHERE id = $1
		  AND aggregate_id = $2
		  AND status = 'sent'
		  AND lease_token = $3
	`, dispatchID, aggregateID, leaseToken, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// MarkFailed 目标失败后释放消费 lease，走有界重试或死信。
func MarkFailed(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID, leaseToken, errMsg string, maxAttempts, backoffSeconds int) (bool, error) {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	if backoffSeconds <= 0 {
		backoffSeconds = DefaultBackoffSeconds
	}
	now := time.Now().UTC()
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	var attempts int
	err := pool.QueryRow(ctx, `
		SELECT attempts FROM async_dispatches
		WHERE id = $1 AND aggregate_id = $2 AND status = 'sent' AND lease_token = $3
	`, dispatchID, aggregateID, leaseToken).Scan(&attempts)
	if err != nil {
		return false, err
	}
	nextStatus := StatusPending
	var availableAt any
	if attempts >= maxAttempts {
		nextStatus = StatusDead
		availableAt = nil
	} else {
		availableAt = now.Add(time.Duration(backoffSeconds) * time.Second)
	}
	tag, err := pool.Exec(ctx, `
		UPDATE async_dispatches SET
			status = $4,
			lease_token = NULL,
			lease_expires_at = NULL,
			sent_at = NULL,
			consumed_at = NULL,
			last_error = $5,
			available_at = COALESCE($6, available_at),
			updated_at = $7
		WHERE id = $1 AND aggregate_id = $2 AND status = 'sent' AND lease_token = $3
	`, dispatchID, aggregateID, leaseToken, nextStatus, errMsg, availableAt, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Consume 领取 SENT 行、跑 actor、成功才 CONSUMED。claim 失败安静退出。
func Consume(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID string, actors map[string]ActorFunc) error {
	var actorName string
	var status string
	var storedAggregate string
	err := pool.QueryRow(ctx, `
		SELECT actor_name, status, aggregate_id FROM async_dispatches WHERE id = $1
	`, dispatchID).Scan(&actorName, &status, &storedAggregate)
	if err != nil {
		return nil
	}
	if storedAggregate != aggregateID || status != StatusSent {
		return nil
	}
	token, ok, err := ClaimForConsumption(ctx, pool, dispatchID, aggregateID, DefaultConsumerLeaseSeconds)
	if err != nil || !ok {
		return err
	}
	fn := actors[actorName]
	if fn == nil {
		_, _ = MarkFailed(ctx, pool, dispatchID, aggregateID, token, "unknown async dispatch actor: "+actorName, DefaultMaxAttempts, DefaultBackoffSeconds)
		return nil
	}
	if err := fn(ctx, aggregateID); err != nil {
		_, _ = MarkFailed(ctx, pool, dispatchID, aggregateID, token, err.Error(), DefaultMaxAttempts, DefaultBackoffSeconds)
		return err
	}
	_, err = MarkConsumed(ctx, pool, dispatchID, aggregateID, token)
	return err
}
