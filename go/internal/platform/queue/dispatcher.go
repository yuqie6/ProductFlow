package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// RunDispatcherOnce 对账过期 lease / 陈旧 SENT，再 claim PENDING：先标 SENT 再 enqueue。
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
	if err := pfdb.QueryRow(ctx, gdb, `SELECT COUNT(*) FROM async_dispatches WHERE status = 'dead'`).Scan(&summary.Dead); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func reconcileExpiredLeases(ctx context.Context, dbTx *gorm.DB, now time.Time) (int, error) {
	n, err := pfdb.Exec(ctx, dbTx, `
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
	return int(n), nil
}

func reconcileStaleSent(ctx context.Context, dbTx *gorm.DB, now time.Time, sentAfter time.Duration, maxAttempts int) (int, error) {
	cutoff := now.Add(-sentAfter)
	rows, err := pfdb.Query(ctx, dbTx, `
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
		if _, err := pfdb.Exec(ctx, dbTx, `
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

func claimPending(ctx context.Context, gdb *gorm.DB, now time.Time, limit, leaseSeconds int) ([]Dispatch, error) {
	var claimed []Dispatch
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		rows, err := pfdb.Query(ctx, dbTx, `
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
			return err
		}
		for rows.Next() {
			var row Dispatch
			if err := rows.Scan(
				&row.ID, &row.DeliveryKey, &row.ActorName, &row.AggregateID, &row.PayloadJSON, &row.Status,
				&row.AvailableAt, &row.LeaseToken, &row.LeaseExpiresAt, &row.Attempts, &row.LastError,
				&row.SentAt, &row.ConsumedAt, &row.CreatedAt, &row.UpdatedAt,
			); err != nil {
				rows.Close()
				return err
			}
			claimed = append(claimed, row)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		leaseUntil := now.Add(time.Duration(leaseSeconds) * time.Second)
		for i := range claimed {
			token := clockid.New()
			claimed[i].LeaseToken = &token
			claimed[i].LeaseExpiresAt = &leaseUntil
			claimed[i].Attempts++
			claimed[i].UpdatedAt = now
			if _, err := pfdb.Exec(ctx, dbTx, `
				UPDATE async_dispatches SET
					lease_token = $2, lease_expires_at = $3, attempts = $4, updated_at = $5
				WHERE id = $1
			`, claimed[i].ID, token, leaseUntil, claimed[i].Attempts, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// 先把行写成 SENT 再碰 broker。enqueue 失败保持 SENT，由过期对账决定是否重投。
func sendClaimed(ctx context.Context, gdb *gorm.DB, dispatch Dispatch, enqueue EnqueueFunc, now time.Time) bool {
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, dbTx, `
			UPDATE async_dispatches SET
				status = 'sent',
				sent_at = $2,
				lease_token = NULL,
				lease_expires_at = NULL,
				last_error = NULL,
				updated_at = $2
			WHERE id = $1
		`, dispatch.ID, now)
		return err
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
		_, _ = pfdb.Exec(ctx, gdb, `
			UPDATE async_dispatches SET last_error = $2, updated_at = $3
			WHERE id = $1 AND status = 'sent' AND lease_token IS NULL
		`, dispatch.ID, msg, now)
		return false
	}
	return true
}
