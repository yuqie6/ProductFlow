package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	sqldb "database/sql"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

// Stage 创建或返回耐久投递行，不 commit。调用方拥有事务。
func Stage(ctx context.Context, tx *gorm.DB, deliveryKey, actorName, aggregateID string, payload any, availableAt *time.Time) (Dispatch, error) {
	existing, err := loadByDeliveryKey(ctx, tx, deliveryKey)
	if err != nil {
		return Dispatch{}, err
	}
	now := time.Now().UTC()
	var payloadJSON []byte
	if payload != nil {
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return Dispatch{}, err
		}
	}
	if existing != nil {
		if err := validateIdentity(*existing, actorName, aggregateID, payloadJSON); err != nil {
			return Dispatch{}, err
		}
		if len(payloadJSON) > 0 && len(existing.PayloadJSON) == 0 {
			existing.PayloadJSON = payloadJSON
			if _, err := pfdb.Exec(ctx, tx, `
				UPDATE async_dispatches SET payload_json = $2, updated_at = $3 WHERE id = $1
			`, existing.ID, payloadJSON, now); err != nil {
				return Dispatch{}, err
			}
		}
		if existing.Status == StatusConsumed {
			avail := now
			if availableAt != nil {
				avail = availableAt.UTC()
			} else if !existing.AvailableAt.IsZero() {
				avail = existing.AvailableAt
			}
			row, err := resetPending(ctx, tx, existing.ID, avail, now, true)
			if err != nil {
				return Dispatch{}, err
			}
			return row, nil
		}
		if existing.Status == StatusSent && availableAt != nil && existing.LeaseToken != nil {
			if existing.LeaseExpiresAt == nil || existing.LeaseExpiresAt.After(now) {
				_, err := pfdb.Exec(ctx, tx, `
					UPDATE async_dispatches SET available_at = $2, updated_at = $3 WHERE id = $1
				`, existing.ID, availableAt.UTC(), now)
				if err != nil {
					return Dispatch{}, err
				}
				existing.AvailableAt = availableAt.UTC()
				existing.UpdatedAt = now
			}
		}
		return *existing, nil
	}

	avail := now
	if availableAt != nil {
		avail = availableAt.UTC()
	}
	id := clockid.New()
	row := Dispatch{
		ID: id, DeliveryKey: deliveryKey, ActorName: actorName, AggregateID: aggregateID,
		PayloadJSON: payloadJSON, Status: StatusPending, AvailableAt: avail, Attempts: 0,
	}
	err = pfdb.QueryRow(ctx, tx, `
		INSERT INTO async_dispatches (
			id, delivery_key, actor_name, aggregate_id, payload_json, status,
			available_at, attempts, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'pending', $6, 0, $7, $7)
		RETURNING created_at, updated_at
	`, id, deliveryKey, actorName, aggregateID, nullableJSON(payloadJSON), avail, now).Scan(&row.CreatedAt, &row.UpdatedAt)
	if uniqueViolation(err) {
		existing, loadErr := loadByDeliveryKey(ctx, tx, deliveryKey)
		if loadErr != nil {
			return Dispatch{}, loadErr
		}
		if existing == nil {
			return Dispatch{}, err
		}
		return *existing, nil
	}
	if err != nil {
		return Dispatch{}, err
	}
	return row, nil
}

// RestageIfIdle 把缺失 / consumed / dead 信封补回 PENDING。
// pending、sent 视为已在投递路径上，不改行、不计数。
func RestageIfIdle(ctx context.Context, tx *gorm.DB, actorName, aggregateID string, payload any) (bool, error) {
	key := DeliveryKey(actorName, aggregateID)
	existing, err := loadByDeliveryKey(ctx, tx, key)
	if err != nil {
		return false, err
	}
	if existing != nil && (existing.Status == StatusPending || existing.Status == StatusSent) {
		return false, nil
	}
	if _, err := Requeue(ctx, tx, key, actorName, aggregateID, payload, nil, false); err != nil {
		return false, err
	}
	return true, nil
}

// StageForActor 按 actor+aggregate 暂存 PENDING 行，不 commit。
func StageForActor(ctx context.Context, tx *gorm.DB, actorName, aggregateID string, delay time.Duration) (Dispatch, error) {
	var availableAt *time.Time
	if delay > 0 {
		t := time.Now().UTC().Add(delay)
		availableAt = &t
	}
	return Stage(ctx, tx, DeliveryKey(actorName, aggregateID), actorName, aggregateID, nil, availableAt)
}

// Requeue 把已有行重置为 pending，或新建。SENT 且仍持有消费 lease 时默认不抢。
func Requeue(ctx context.Context, tx *gorm.DB, deliveryKey, actorName, aggregateID string, payload any, availableAt *time.Time, allowActiveLease bool) (Dispatch, error) {
	existing, err := loadByDeliveryKey(ctx, tx, deliveryKey)
	if err != nil {
		return Dispatch{}, err
	}
	if existing == nil {
		return Stage(ctx, tx, deliveryKey, actorName, aggregateID, payload, availableAt)
	}
	var payloadJSON []byte
	if payload != nil {
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return Dispatch{}, err
		}
	}
	if err := validateIdentity(*existing, actorName, aggregateID, payloadJSON); err != nil {
		return Dispatch{}, err
	}
	now := time.Now().UTC()
	if len(payloadJSON) > 0 && len(existing.PayloadJSON) == 0 {
		if _, err := pfdb.Exec(ctx, tx, `UPDATE async_dispatches SET payload_json = $2, updated_at = $3 WHERE id = $1`, existing.ID, payloadJSON, now); err != nil {
			return Dispatch{}, err
		}
	}
	leaseActive := existing.Status == StatusSent && existing.LeaseToken != nil &&
		(existing.LeaseExpiresAt == nil || existing.LeaseExpiresAt.After(now))
	if leaseActive && !allowActiveLease {
		return *existing, nil
	}
	if existing.Status == StatusPending {
		return *existing, nil
	}
	avail := now
	if availableAt != nil {
		avail = availableAt.UTC()
	}
	return resetPending(ctx, tx, existing.ID, avail, now, true)
}

func loadByDeliveryKey(ctx context.Context, tx *gorm.DB, deliveryKey string) (*Dispatch, error) {
	row, err := scanDispatch(ctx, tx, `
		SELECT id, delivery_key, actor_name, aggregate_id, payload_json, status,
		       available_at, lease_token, lease_expires_at, attempts, last_error,
		       sent_at, consumed_at, created_at, updated_at
		FROM async_dispatches WHERE delivery_key = $1
	`, deliveryKey)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func scanDispatch(ctx context.Context, q *gorm.DB, sql string, args ...any) (Dispatch, error) {
	var row Dispatch
	err := pfdb.QueryRow(ctx, q, sql, args...).Scan(
		&row.ID, &row.DeliveryKey, &row.ActorName, &row.AggregateID, &row.PayloadJSON, &row.Status,
		&row.AvailableAt, &row.LeaseToken, &row.LeaseExpiresAt, &row.Attempts, &row.LastError,
		&row.SentAt, &row.ConsumedAt, &row.CreatedAt, &row.UpdatedAt,
	)
	return row, err
}

func resetPending(ctx context.Context, tx *gorm.DB, id string, availableAt, now time.Time, resetAttempts bool) (Dispatch, error) {
	attemptsSQL := "attempts"
	if resetAttempts {
		attemptsSQL = "0"
	}
	row, err := scanDispatch(ctx, tx, `
		UPDATE async_dispatches SET
			status = 'pending',
			available_at = $2,
			lease_token = NULL,
			lease_expires_at = NULL,
			attempts = `+attemptsSQL+`,
			last_error = NULL,
			sent_at = NULL,
			consumed_at = NULL,
			updated_at = $3
		WHERE id = $1
		RETURNING id, delivery_key, actor_name, aggregate_id, payload_json, status,
		          available_at, lease_token, lease_expires_at, attempts, last_error,
		          sent_at, consumed_at, created_at, updated_at
	`, id, availableAt, now)
	return row, err
}

func validateIdentity(existing Dispatch, actorName, aggregateID string, payloadJSON []byte) error {
	if existing.ActorName != actorName || existing.AggregateID != aggregateID {
		return apperr.Conflict("同一 delivery key 不能复用到不同的异步目标")
	}
	if len(payloadJSON) > 0 && len(existing.PayloadJSON) > 0 && !payloadEqual(existing.PayloadJSON, payloadJSON) {
		return apperr.Conflict("同一 delivery key 不能复用到不同的异步 payload")
	}
	return nil
}

func payloadEqual(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	la, _ := json.Marshal(left)
	lb, _ := json.Marshal(right)
	return bytes.Equal(la, lb)
}

func nullableJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
