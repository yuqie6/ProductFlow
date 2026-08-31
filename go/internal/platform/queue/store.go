package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// Stage 按 delivery_key 幂等写入 PENDING。已存在且身份一致则复用；CONSUMED 会重置为 PENDING。
// 同一 key 绑到不同 actor/aggregate/payload 返回 409。
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
			if err := tx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", existing.ID).Updates(map[string]any{
				"payload_json": string(payloadJSON),
				"updated_at":   now,
			}).Error; err != nil {
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
				if err := tx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", existing.ID).Updates(map[string]any{
					"available_at": availableAt.UTC(),
					"updated_at":   now,
				}).Error; err != nil {
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
	row := schema.AsyncDispatches{
		ID: id, DeliveryKey: deliveryKey, ActorName: actorName, AggregateID: aggregateID,
		PayloadJSON: payloadPtr(payloadJSON), Status: StatusPending, AvailableAt: avail, Attempts: 0,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
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
		return Dispatch{}, err
	}
	return fromSchema(row), nil
}

// RestageIfIdle 仅在无 PENDING/SENT/DEAD 行时重新入队。返回是否新建或重置了信封。
func RestageIfIdle(ctx context.Context, tx *gorm.DB, actorName, aggregateID string, payload any) (bool, error) {
	key := DeliveryKey(actorName, aggregateID)
	existing, err := loadByDeliveryKey(ctx, tx, key)
	if err != nil {
		return false, err
	}
	if existing != nil && (existing.Status == StatusPending || existing.Status == StatusSent || existing.Status == StatusDead) {
		return false, nil
	}
	if _, err := Requeue(ctx, tx, key, actorName, aggregateID, payload, nil, false); err != nil {
		return false, err
	}
	return true, nil
}

// StageForActor 用 [DeliveryKey] 调用 [Stage]；delay>0 时设置 available_at。
func StageForActor(ctx context.Context, tx *gorm.DB, actorName, aggregateID string, delay time.Duration) (Dispatch, error) {
	var availableAt *time.Time
	if delay > 0 {
		t := time.Now().UTC().Add(delay)
		availableAt = &t
	}
	return Stage(ctx, tx, DeliveryKey(actorName, aggregateID), actorName, aggregateID, nil, availableAt)
}

// Requeue 把已有信封拉回 PENDING。SENT 且 lease 仍有效时，除非 allowActiveLease，否则原样返回。
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
		if err := tx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"payload_json": string(payloadJSON),
			"updated_at":   now,
		}).Error; err != nil {
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
	var row schema.AsyncDispatches
	err := tx.WithContext(ctx).Where("delivery_key = ?", deliveryKey).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d := fromSchema(row)
	return &d, nil
}

// resetPending 把已有信封拉回 PENDING：清 lease、sent_at、consumed_at、last_error。
// resetAttempts 为 true 时把 attempts 归零（用户重试）；dispatcher 对账不要归零，否则死信封会无限复活。
func resetPending(ctx context.Context, tx *gorm.DB, id string, availableAt, now time.Time, resetAttempts bool) (Dispatch, error) {
	updates := map[string]any{
		"status":           StatusPending,
		"available_at":     availableAt,
		"lease_token":      nil,
		"lease_expires_at": nil,
		"last_error":       nil,
		"sent_at":          nil,
		"consumed_at":      nil,
		"updated_at":       now,
	}
	if resetAttempts {
		updates["attempts"] = 0
	}
	if err := tx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return Dispatch{}, err
	}
	var row schema.AsyncDispatches
	if err := tx.WithContext(ctx).Where("id = ?", id).Take(&row).Error; err != nil {
		return Dispatch{}, err
	}
	return fromSchema(row), nil
}

func fromSchema(row schema.AsyncDispatches) Dispatch {
	d := Dispatch{
		ID: row.ID, DeliveryKey: row.DeliveryKey, ActorName: row.ActorName, AggregateID: row.AggregateID,
		Status: row.Status, AvailableAt: row.AvailableAt, LeaseToken: row.LeaseToken, LeaseExpiresAt: row.LeaseExpiresAt,
		Attempts: row.Attempts, LastError: row.LastError, SentAt: row.SentAt, ConsumedAt: row.ConsumedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.PayloadJSON != nil {
		d.PayloadJSON = []byte(*row.PayloadJSON)
	}
	return d
}

func payloadPtr(raw []byte) *string {
	if len(raw) == 0 {
		return nil
	}
	s := string(raw)
	return &s
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

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
