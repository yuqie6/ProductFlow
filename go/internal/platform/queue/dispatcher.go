package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// RunDispatcherOnce 对账过期 lease / 陈旧 SENT，再 claim PENDING：先标 SENT 再 enqueue。
func RunDispatcherOnce(ctx context.Context, pool *pgxpool.Pool, enqueue EnqueueFunc, limit int) (Summary, error) {
	if limit < 1 {
		limit = DefaultClaimLimit
	}
	now := time.Now().UTC()
	var summary Summary
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	expired, err := reconcileExpiredLeases(ctx, tx, now)
	if err != nil {
		return Summary{}, err
	}
	stale, err := reconcileStaleSent(ctx, tx, now, time.Duration(DefaultSentReconcileAfter)*time.Second, DefaultMaxAttempts)
	if err != nil {
		return Summary{}, err
	}
	summary.Reconciled = expired + stale
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, err
	}

	claimed, err := claimPending(ctx, pool, now, limit, DefaultLeaseSeconds)
	if err != nil {
		return Summary{}, err
	}
	summary.Pending = len(claimed)
	for _, dispatch := range claimed {
		if sendClaimed(ctx, pool, dispatch, enqueue, now) {
			summary.Sent++
		}
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_dispatches WHERE status = 'dead'`).Scan(&summary.Dead); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func reconcileExpiredLeases(ctx context.Context, tx pgx.Tx, now time.Time) (int, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE async_dispatches SET
			lease_token = NULL,
			lease_expires_at = NULL,
			available_at = $1,
			updated_at = $1
		WHERE status = 'pending'
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at <= $1
	`, now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func reconcileStaleSent(ctx context.Context, tx pgx.Tx, now time.Time, sentAfter time.Duration, maxAttempts int) (int, error) {
	cutoff := now.Add(-sentAfter)
	rows, err := tx.Query(ctx, `
		SELECT id, attempts, available_at
		FROM async_dispatches
		WHERE status = 'sent'
		  AND sent_at IS NOT NULL
		  AND sent_at <= $1
		  AND (lease_token IS NULL OR (lease_expires_at IS NOT NULL AND lease_expires_at <= $2))
		FOR UPDATE
	`, cutoff, now)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type stale struct {
		id          string
		attempts    int
		availableAt time.Time
	}
	var items []stale
	for rows.Next() {
		var item stale
		if err := rows.Scan(&item.id, &item.attempts, &item.availableAt); err != nil {
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, item := range items {
		nextStatus := StatusPending
		avail := item.availableAt
		if item.attempts >= maxAttempts {
			nextStatus = StatusDead
		} else if !avail.After(now) {
			avail = now
		}
		if _, err := tx.Exec(ctx, `
			UPDATE async_dispatches SET
				status = $2,
				lease_token = NULL,
				lease_expires_at = NULL,
				sent_at = NULL,
				available_at = $3,
				updated_at = $4
			WHERE id = $1
		`, item.id, nextStatus, avail, now); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func claimPending(ctx context.Context, pool *pgxpool.Pool, now time.Time, limit, leaseSeconds int) ([]Dispatch, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT id, delivery_key, actor_name, aggregate_id, payload_json, status,
		       available_at, lease_token, lease_expires_at, attempts, last_error,
		       sent_at, consumed_at, created_at, updated_at
		FROM async_dispatches
		WHERE status = 'pending'
		  AND available_at <= $1
		  AND (lease_expires_at IS NULL OR lease_expires_at <= $1)
		ORDER BY available_at ASC, id ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, now, limit)
	if err != nil {
		return nil, err
	}
	var claimed []Dispatch
	for rows.Next() {
		var row Dispatch
		if err := rows.Scan(
			&row.ID, &row.DeliveryKey, &row.ActorName, &row.AggregateID, &row.PayloadJSON, &row.Status,
			&row.AvailableAt, &row.LeaseToken, &row.LeaseExpiresAt, &row.Attempts, &row.LastError,
			&row.SentAt, &row.ConsumedAt, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		claimed = append(claimed, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	leaseUntil := now.Add(time.Duration(leaseSeconds) * time.Second)
	for i := range claimed {
		token := clockid.New()
		claimed[i].LeaseToken = &token
		claimed[i].LeaseExpiresAt = &leaseUntil
		claimed[i].Attempts++
		claimed[i].UpdatedAt = now
		if _, err := tx.Exec(ctx, `
			UPDATE async_dispatches SET
				lease_token = $2, lease_expires_at = $3, attempts = $4, updated_at = $5
			WHERE id = $1
		`, claimed[i].ID, token, leaseUntil, claimed[i].Attempts, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return claimed, nil
}

// 先把行写成 SENT 再碰 broker。enqueue 失败保持 SENT，由过期对账决定是否重投。
func sendClaimed(ctx context.Context, pool *pgxpool.Pool, dispatch Dispatch, enqueue EnqueueFunc, now time.Time) bool {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		UPDATE async_dispatches SET
			status = 'sent',
			sent_at = $2,
			lease_token = NULL,
			lease_expires_at = NULL,
			last_error = NULL,
			updated_at = $2
		WHERE id = $1
	`, dispatch.ID, now)
	if err != nil {
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		return false
	}
	if enqueue == nil {
		return true
	}
	if err := enqueue(dispatch.ID, dispatch.AggregateID); err != nil {
		msg := err.Error()
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		_, _ = pool.Exec(ctx, `
			UPDATE async_dispatches SET last_error = $2, updated_at = $3
			WHERE id = $1 AND status = 'sent' AND lease_token IS NULL
		`, dispatch.ID, msg, now)
		return false
	}
	return true
}
