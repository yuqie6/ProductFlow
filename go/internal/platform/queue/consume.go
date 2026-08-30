package queue

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

func gormFrom(pool *pgxpool.Pool) (*gorm.DB, error) {
	return pfdb.OpenGorm(pool)
}

func ClaimForConsumption(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID string, leaseSeconds int) (string, bool, error) {
	if leaseSeconds <= 0 {
		leaseSeconds = DefaultConsumerLeaseSeconds
	}
	gdb, err := gormFrom(pool)
	if err != nil {
		return "", false, err
	}
	now := time.Now().UTC()
	token := clockid.New()
	res := gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Where("id = ? AND aggregate_id = ? AND status = ? AND lease_token IS NULL", dispatchID, aggregateID, StatusSent).
		Updates(map[string]any{
			"lease_token":      token,
			"lease_expires_at": now.Add(time.Duration(leaseSeconds) * time.Second),
			"updated_at":       now,
		})
	if res.Error != nil {
		return "", false, res.Error
	}
	if res.RowsAffected != 1 {
		return "", false, nil
	}
	return token, true, nil
}

func MarkConsumed(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID, leaseToken string) (bool, error) {
	gdb, err := gormFrom(pool)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	res := gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Where("id = ? AND aggregate_id = ? AND status = ? AND lease_token = ?", dispatchID, aggregateID, StatusSent, leaseToken).
		Updates(map[string]any{
			"status":           StatusConsumed,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"consumed_at":      now,
			"updated_at":       now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func MarkFailed(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID, leaseToken, errMsg string, maxAttempts, backoffSeconds int) (bool, error) {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	if backoffSeconds <= 0 {
		backoffSeconds = DefaultBackoffSeconds
	}
	gdb, err := gormFrom(pool)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	ok := false
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row schema.AsyncDispatches
		takeErr := tx.Where("id = ? AND aggregate_id = ? AND status = ? AND lease_token = ?", dispatchID, aggregateID, StatusSent, leaseToken).Take(&row).Error
		if errors.Is(takeErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if takeErr != nil {
			return takeErr
		}
		updates := map[string]any{
			"lease_token":      nil,
			"lease_expires_at": nil,
			"sent_at":          nil,
			"consumed_at":      nil,
			"last_error":       errMsg,
			"updated_at":       now,
		}
		if row.Attempts >= maxAttempts {
			updates["status"] = StatusDead
		} else {
			updates["status"] = StatusPending
			updates["available_at"] = now.Add(time.Duration(backoffSeconds) * time.Second)
		}
		res := tx.Model(&schema.AsyncDispatches{}).
			Where("id = ? AND aggregate_id = ? AND status = ? AND lease_token = ?", dispatchID, aggregateID, StatusSent, leaseToken).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		ok = res.RowsAffected == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return ok, nil
}

func Consume(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID string, actors map[string]ActorFunc) error {
	gdb, err := gormFrom(pool)
	if err != nil {
		return err
	}
	var row schema.AsyncDispatches
	err = gdb.WithContext(ctx).Where("id = ?", dispatchID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.AggregateID != aggregateID || row.Status != StatusSent {
		return nil
	}
	token, ok, err := ClaimForConsumption(ctx, pool, dispatchID, aggregateID, DefaultConsumerLeaseSeconds)
	if err != nil || !ok {
		return err
	}
	fn := actors[row.ActorName]
	if fn == nil {
		_, _ = MarkFailed(ctx, pool, dispatchID, aggregateID, token, "unknown async dispatch actor: "+row.ActorName, DefaultMaxAttempts, DefaultBackoffSeconds)
		return nil
	}
	if err := fn(ctx, aggregateID); err != nil {
		if errors.Is(err, ErrBusy) || errors.Is(err, ErrLater) {
			delay := time.Duration(DefaultBusyRetrySeconds) * time.Second
			if errors.Is(err, ErrLater) {
				delay = time.Duration(DefaultLaterRetrySeconds) * time.Second
			}
			_, _ = ReleaseForRetry(ctx, pool, dispatchID, aggregateID, token, delay)
			return nil
		}
		_, _ = MarkFailed(ctx, pool, dispatchID, aggregateID, token, err.Error(), DefaultMaxAttempts, DefaultBackoffSeconds)
		return err
	}
	_, err = MarkConsumed(ctx, pool, dispatchID, aggregateID, token)
	return err
}

func ReleaseForRetry(ctx context.Context, pool *pgxpool.Pool, dispatchID, aggregateID, leaseToken string, delay time.Duration) (bool, error) {
	if delay < 0 {
		delay = 0
	}
	gdb, err := gormFrom(pool)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	res := gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Where("id = ? AND aggregate_id = ? AND status = ? AND lease_token = ?", dispatchID, aggregateID, StatusSent, leaseToken).
		Updates(map[string]any{
			"status":           StatusPending,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"sent_at":          nil,
			"consumed_at":      nil,
			"available_at":     now.Add(delay),
			"updated_at":       now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
