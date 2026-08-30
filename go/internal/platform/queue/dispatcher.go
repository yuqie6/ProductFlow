package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func RunDispatcherOnce(ctx context.Context, pool *pgxpool.Pool, enqueue EnqueueFunc, limit int) (Summary, error) {
	if limit < 1 {
		limit = DefaultClaimLimit
	}
	gdb, err := gormFrom(pool)
	if err != nil {
		return Summary{}, err
	}
	now := time.Now().UTC()
	var summary Summary
	err = tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		expired, err := reconcileExpiredLeases(ctx, dbTx, now)
		if err != nil {
			return err
		}
		stale, err := reconcileStaleSent(ctx, dbTx, now, time.Duration(DefaultSentReconcileAfter)*time.Second, DefaultMaxAttempts)
		if err != nil {
			return err
		}
		summary.Reconciled = expired + stale
		return nil
	})
	if err != nil {
		return Summary{}, err
	}

	claimed, err := claimPending(ctx, gdb, now, limit, DefaultLeaseSeconds)
	if err != nil {
		return Summary{}, err
	}
	summary.Pending = len(claimed)
	for _, dispatch := range claimed {
		if sendClaimed(ctx, gdb, dispatch, enqueue, now) {
			summary.Sent++
		}
	}
	var dead int64
	if err := gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("status = ?", StatusDead).Count(&dead).Error; err != nil {
		return Summary{}, err
	}
	summary.Dead = int(dead)
	return summary, nil
}

func reconcileExpiredLeases(ctx context.Context, dbTx *gorm.DB, now time.Time) (int, error) {
	res := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", StatusPending, now).
		Updates(map[string]any{
			"lease_token":      nil,
			"lease_expires_at": nil,
			"available_at":     now,
			"updated_at":       now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func reconcileStaleSent(ctx context.Context, dbTx *gorm.DB, now time.Time, sentAfter time.Duration, maxAttempts int) (int, error) {
	cutoff := now.Add(-sentAfter)
	var items []schema.AsyncDispatches
	if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Clauses(pfdb.ForUpdate()).
		Where("status = ? AND sent_at IS NOT NULL AND sent_at <= ?", StatusSent, cutoff).
		Where("lease_token IS NULL OR (lease_expires_at IS NOT NULL AND lease_expires_at <= ?)", now).
		Find(&items).Error; err != nil {
		return 0, err
	}
	for _, item := range items {
		nextStatus := StatusPending
		avail := item.AvailableAt
		if item.Attempts >= maxAttempts {
			nextStatus = StatusDead
		} else if !avail.After(now) {
			avail = now
		}
		if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", item.ID).Updates(map[string]any{
			"status":           nextStatus,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"sent_at":          nil,
			"available_at":     avail,
			"updated_at":       now,
		}).Error; err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func claimPending(ctx context.Context, gdb *gorm.DB, now time.Time, limit, leaseSeconds int) ([]Dispatch, error) {
	var claimed []Dispatch
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		var rows []schema.AsyncDispatches
		if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Clauses(pfdb.SkipLocked()).
			Where("status = ? AND available_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", StatusPending, now, now).
			Order("available_at ASC, id ASC").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return err
		}
		leaseUntil := now.Add(time.Duration(leaseSeconds) * time.Second)
		for _, row := range rows {
			token := clockid.New()
			if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", row.ID).Updates(map[string]any{
				"lease_token":      token,
				"lease_expires_at": leaseUntil,
				"attempts":         row.Attempts + 1,
				"updated_at":       now,
			}).Error; err != nil {
				return err
			}
			d := fromSchema(row)
			d.LeaseToken = &token
			d.LeaseExpiresAt = &leaseUntil
			d.Attempts = row.Attempts + 1
			d.UpdatedAt = now
			claimed = append(claimed, d)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func sendClaimed(ctx context.Context, gdb *gorm.DB, dispatch Dispatch, enqueue EnqueueFunc, now time.Time) bool {
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		return dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", dispatch.ID).Updates(map[string]any{
			"status":           StatusSent,
			"sent_at":          now,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"last_error":       nil,
			"updated_at":       now,
		}).Error
	})
	if err != nil {
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
		_ = gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).
			Where("id = ? AND status = ? AND lease_token IS NULL", dispatch.ID, StatusSent).
			Updates(map[string]any{"last_error": msg, "updated_at": now}).Error
		return false
	}
	return true
}
