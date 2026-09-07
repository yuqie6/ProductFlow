// Package quota 实现商家商业额度账本（MP-C B0）。
//
// 与平台调用事实分离：agent_model_invocations.usage_source 仍是 provider/estimated/unavailable
// 操作事实；本包只管商家被收取的服务额度（available/reserved/事件），不另造平台成本账。
//
// B0 账本已交付；B1–B3 已接图会话 / Graph / Agent 主入口。
// B4 余额 HTTP：商家只读本商 + Op 只读/调账（见 http.go）。未知结果必须走 MarkUnknown，禁止把超时自动当零消费 Release。
package quota

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	CurrencyInternalUnits = "iu"
	DefaultPriceVersionID = "pv-placeholder-v0"

	StatusReserved              = "reserved"
	StatusSettled               = "settled"
	StatusReleased              = "released"
	StatusPendingReconciliation = "pending_reconciliation"

	EventReserve     = "reserve"
	EventSettle      = "settle"
	EventRelease     = "release"
	EventAdjust      = "adjust"
	EventMarkUnknown = "mark_unknown"
)

// Service 是商家额度账本命令入口。
type Service struct {
	DB *gorm.DB
}

// Account 是商家额度余额投影。
type Account struct {
	MerchantID     string
	Currency       string
	AvailableUnits int64
	ReservedUnits  int64
	PriceVersionID string
}

// Hold 是一次消费预留行投影。
type Hold struct {
	ID             string
	MerchantID     string
	IdempotencyKey string
	AmountUnits    int64
	SettledUnits   *int64
	Status         string
	PriceVersionID string
}

// GetAccount 读取商家额度账户；尚无行时返回零余额账户投影（不自动建行）。
func (s *Service) GetAccount(ctx context.Context, merchantID string) (Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return Account{}, apperr.Validation("缺少商家")
	}
	var row schema.MerchantQuotaAccounts
	err := s.DB.WithContext(ctx).Where("merchant_id = ?", merchantID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Account{
			MerchantID:     merchantID,
			Currency:       CurrencyInternalUnits,
			AvailableUnits: 0,
			ReservedUnits:  0,
			PriceVersionID: DefaultPriceVersionID,
		}, nil
	}
	if err != nil {
		return Account{}, apperr.Internal("读取额度账户失败")
	}
	return accountFromRow(row), nil
}

// Adjust 由 Operator 增减 available（正数入账、负数扣减）。相同幂等键重放不重复调账。
func (s *Service) Adjust(ctx context.Context, merchantID, idempotencyKey string, deltaUnits int64, reason, actorUserID string) (Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	reason = strings.TrimSpace(reason)
	actorUserID = strings.TrimSpace(actorUserID)
	if merchantID == "" {
		return Account{}, apperr.Validation("缺少商家")
	}
	if idempotencyKey == "" {
		return Account{}, apperr.Validation("缺少幂等键")
	}
	if deltaUnits == 0 {
		return Account{}, apperr.Validation("调账额度不能为 0")
	}

	var out Account
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		acct, err := ensureAndLockAccount(gdb, merchantID)
		if err != nil {
			return err
		}
		if _, ok, err := loadEvent(gdb, merchantID, EventAdjust, idempotencyKey); err != nil {
			return err
		} else if ok {
			out = accountFromRow(acct)
			return nil
		}
		nextAvailable := acct.AvailableUnits + deltaUnits
		if nextAvailable < 0 {
			return apperr.Conflict("可用额度不足，无法调账")
		}
		now := time.Now().UTC()
		if err := gdb.Model(&schema.MerchantQuotaAccounts{}).
			Where("merchant_id = ?", merchantID).
			Updates(map[string]any{
				"available_units": nextAvailable,
				"updated_at":      now,
			}).Error; err != nil {
			return apperr.Internal("更新额度账户失败")
		}
		acct.AvailableUnits = nextAvailable
		acct.UpdatedAt = now
		if err := appendEvent(gdb, schema.MerchantQuotaEvents{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			EventType:      EventAdjust,
			AmountUnits:    deltaUnits,
			IdempotencyKey: idempotencyKey,
			AvailableAfter: acct.AvailableUnits,
			ReservedAfter:  acct.ReservedUnits,
			PriceVersionID: acct.PriceVersionID,
			Reason:         optionalString(reason),
			ActorUserID:    optionalString(actorUserID),
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		out = accountFromRow(acct)
		return nil
	})
	return out, err
}

// Reserve 生成前原子预留。相同幂等键重放返回原 hold，不重复扣 available。
func (s *Service) Reserve(ctx context.Context, merchantID, idempotencyKey string, amountUnits int64, priceVersionID string) (Hold, Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	priceVersionID = strings.TrimSpace(priceVersionID)
	if priceVersionID == "" {
		priceVersionID = DefaultPriceVersionID
	}
	if merchantID == "" {
		return Hold{}, Account{}, apperr.Validation("缺少商家")
	}
	if idempotencyKey == "" {
		return Hold{}, Account{}, apperr.Validation("缺少幂等键")
	}
	if amountUnits <= 0 {
		return Hold{}, Account{}, apperr.Validation("预留额度必须为正整数")
	}

	var hold Hold
	var acctOut Account
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		// 先锁账户再查 hold，保证同键并发只扣一次 available。
		acct, err := ensureAndLockAccount(gdb, merchantID)
		if err != nil {
			return err
		}
		if existing, ok, err := loadHold(gdb, merchantID, idempotencyKey); err != nil {
			return err
		} else if ok {
			if existing.AmountUnits != amountUnits {
				return apperr.Conflict("预留幂等键已存在且额度不一致")
			}
			hold = holdFromRow(existing)
			acctOut = accountFromRow(acct)
			return nil
		}
		if acct.AvailableUnits < amountUnits {
			return apperr.Conflict("可用额度不足")
		}
		now := time.Now().UTC()
		nextAvailable := acct.AvailableUnits - amountUnits
		nextReserved := acct.ReservedUnits + amountUnits
		if err := gdb.Model(&schema.MerchantQuotaAccounts{}).
			Where("merchant_id = ?", merchantID).
			Updates(map[string]any{
				"available_units":  nextAvailable,
				"reserved_units":   nextReserved,
				"price_version_id": priceVersionID,
				"updated_at":       now,
			}).Error; err != nil {
			return apperr.Internal("更新额度账户失败")
		}
		acct.AvailableUnits = nextAvailable
		acct.ReservedUnits = nextReserved
		acct.PriceVersionID = priceVersionID
		acct.UpdatedAt = now

		row := schema.MerchantQuotaHolds{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			IdempotencyKey: idempotencyKey,
			AmountUnits:    amountUnits,
			Status:         StatusReserved,
			PriceVersionID: priceVersionID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := gdb.Create(&row).Error; err != nil {
			return apperr.Internal("创建预留失败")
		}
		holdID := row.ID
		if err := appendEvent(gdb, schema.MerchantQuotaEvents{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			HoldID:         &holdID,
			EventType:      EventReserve,
			AmountUnits:    amountUnits,
			IdempotencyKey: idempotencyKey,
			AvailableAfter: acct.AvailableUnits,
			ReservedAfter:  acct.ReservedUnits,
			PriceVersionID: priceVersionID,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		hold = holdFromRow(row)
		acctOut = accountFromRow(acct)
		return nil
	})
	return hold, acctOut, err
}

// Settle 按实际消费结算；差额退回 available。相同幂等键重放不重复结算。
// actualUnits 必须在 [0, reserved]；0 表示确认零消费但仍走结算路径（与 Release/取消不同语义由调用方选择）。
func (s *Service) Settle(ctx context.Context, merchantID, idempotencyKey string, actualUnits int64) (Hold, Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if merchantID == "" {
		return Hold{}, Account{}, apperr.Validation("缺少商家")
	}
	if idempotencyKey == "" {
		return Hold{}, Account{}, apperr.Validation("缺少幂等键")
	}
	if actualUnits < 0 {
		return Hold{}, Account{}, apperr.Validation("结算额度不能为负")
	}

	var hold Hold
	var acctOut Account
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		acct, err := lockAccount(gdb, merchantID)
		if err != nil {
			return err
		}
		row, ok, err := loadHoldForUpdate(gdb, merchantID, idempotencyKey)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.NotFound("预留不存在")
		}
		if row.Status == StatusSettled {
			if row.SettledUnits != nil && *row.SettledUnits != actualUnits {
				return apperr.Conflict("结算幂等键已存在且额度不一致")
			}
			hold = holdFromRow(row)
			acctOut = accountFromRow(acct)
			return nil
		}
		if row.Status != StatusReserved && row.Status != StatusPendingReconciliation {
			return apperr.Conflict("预留状态不允许结算")
		}
		if actualUnits > row.AmountUnits {
			return apperr.Validation("结算额度不能超过预留")
		}
		refund := row.AmountUnits - actualUnits
		if acct.ReservedUnits < row.AmountUnits {
			return apperr.Internal("预留负债不一致")
		}
		now := time.Now().UTC()
		nextAvailable := acct.AvailableUnits + refund
		nextReserved := acct.ReservedUnits - row.AmountUnits
		if err := gdb.Model(&schema.MerchantQuotaAccounts{}).
			Where("merchant_id = ?", merchantID).
			Updates(map[string]any{
				"available_units": nextAvailable,
				"reserved_units":  nextReserved,
				"updated_at":      now,
			}).Error; err != nil {
			return apperr.Internal("更新额度账户失败")
		}
		acct.AvailableUnits = nextAvailable
		acct.ReservedUnits = nextReserved
		acct.UpdatedAt = now

		settled := actualUnits
		if err := gdb.Model(&schema.MerchantQuotaHolds{}).
			Where("id = ?", row.ID).
			Updates(map[string]any{
				"status":        StatusSettled,
				"settled_units": settled,
				"updated_at":    now,
			}).Error; err != nil {
			return apperr.Internal("更新预留失败")
		}
		row.Status = StatusSettled
		row.SettledUnits = &settled
		row.UpdatedAt = now

		holdID := row.ID
		if err := appendEvent(gdb, schema.MerchantQuotaEvents{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			HoldID:         &holdID,
			EventType:      EventSettle,
			AmountUnits:    actualUnits,
			IdempotencyKey: idempotencyKey,
			AvailableAfter: acct.AvailableUnits,
			ReservedAfter:  acct.ReservedUnits,
			PriceVersionID: row.PriceVersionID,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		hold = holdFromRow(row)
		acctOut = accountFromRow(acct)
		return nil
	})
	return hold, acctOut, err
}

// Release 取消已明确未发出的调用：把预留退回 available。unknown 不得走此路径。
func (s *Service) Release(ctx context.Context, merchantID, idempotencyKey string) (Hold, Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if merchantID == "" {
		return Hold{}, Account{}, apperr.Validation("缺少商家")
	}
	if idempotencyKey == "" {
		return Hold{}, Account{}, apperr.Validation("缺少幂等键")
	}

	var hold Hold
	var acctOut Account
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		acct, err := lockAccount(gdb, merchantID)
		if err != nil {
			return err
		}
		row, ok, err := loadHoldForUpdate(gdb, merchantID, idempotencyKey)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.NotFound("预留不存在")
		}
		if row.Status == StatusReleased {
			hold = holdFromRow(row)
			acctOut = accountFromRow(acct)
			return nil
		}
		if row.Status != StatusReserved {
			return apperr.Conflict("预留状态不允许释放")
		}
		if acct.ReservedUnits < row.AmountUnits {
			return apperr.Internal("预留负债不一致")
		}
		now := time.Now().UTC()
		nextAvailable := acct.AvailableUnits + row.AmountUnits
		nextReserved := acct.ReservedUnits - row.AmountUnits
		if err := gdb.Model(&schema.MerchantQuotaAccounts{}).
			Where("merchant_id = ?", merchantID).
			Updates(map[string]any{
				"available_units": nextAvailable,
				"reserved_units":  nextReserved,
				"updated_at":      now,
			}).Error; err != nil {
			return apperr.Internal("更新额度账户失败")
		}
		acct.AvailableUnits = nextAvailable
		acct.ReservedUnits = nextReserved
		acct.UpdatedAt = now

		if err := gdb.Model(&schema.MerchantQuotaHolds{}).
			Where("id = ?", row.ID).
			Updates(map[string]any{
				"status":     StatusReleased,
				"updated_at": now,
			}).Error; err != nil {
			return apperr.Internal("更新预留失败")
		}
		row.Status = StatusReleased
		row.UpdatedAt = now

		holdID := row.ID
		if err := appendEvent(gdb, schema.MerchantQuotaEvents{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			HoldID:         &holdID,
			EventType:      EventRelease,
			AmountUnits:    row.AmountUnits,
			IdempotencyKey: idempotencyKey,
			AvailableAfter: acct.AvailableUnits,
			ReservedAfter:  acct.ReservedUnits,
			PriceVersionID: row.PriceVersionID,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		hold = holdFromRow(row)
		acctOut = accountFromRow(acct)
		return nil
	})
	return hold, acctOut, err
}

// MarkUnknown 将已发出但结果不明的预留标为待核对；保留 reserved 负债，不自动按零消费释放。
func (s *Service) MarkUnknown(ctx context.Context, merchantID, idempotencyKey string) (Hold, Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if merchantID == "" {
		return Hold{}, Account{}, apperr.Validation("缺少商家")
	}
	if idempotencyKey == "" {
		return Hold{}, Account{}, apperr.Validation("缺少幂等键")
	}

	var hold Hold
	var acctOut Account
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		acct, err := lockAccount(gdb, merchantID)
		if err != nil {
			return err
		}
		row, ok, err := loadHoldForUpdate(gdb, merchantID, idempotencyKey)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.NotFound("预留不存在")
		}
		if row.Status == StatusPendingReconciliation {
			hold = holdFromRow(row)
			acctOut = accountFromRow(acct)
			return nil
		}
		if row.Status != StatusReserved {
			return apperr.Conflict("预留状态不允许标为未知")
		}
		now := time.Now().UTC()
		if err := gdb.Model(&schema.MerchantQuotaHolds{}).
			Where("id = ?", row.ID).
			Updates(map[string]any{
				"status":     StatusPendingReconciliation,
				"updated_at": now,
			}).Error; err != nil {
			return apperr.Internal("更新预留失败")
		}
		row.Status = StatusPendingReconciliation
		row.UpdatedAt = now

		holdID := row.ID
		if err := appendEvent(gdb, schema.MerchantQuotaEvents{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			HoldID:         &holdID,
			EventType:      EventMarkUnknown,
			AmountUnits:    row.AmountUnits,
			IdempotencyKey: idempotencyKey,
			AvailableAfter: acct.AvailableUnits,
			ReservedAfter:  acct.ReservedUnits,
			PriceVersionID: row.PriceVersionID,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		hold = holdFromRow(row)
		acctOut = accountFromRow(acct)
		return nil
	})
	return hold, acctOut, err
}

func ensureAndLockAccount(gdb *gorm.DB, merchantID string) (schema.MerchantQuotaAccounts, error) {
	var acct schema.MerchantQuotaAccounts
	err := gdb.Clauses(pfdb.ForUpdate()).Where("merchant_id = ?", merchantID).Take(&acct).Error
	if err == nil {
		return acct, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.MerchantQuotaAccounts{}, apperr.Internal("锁定额度账户失败")
	}
	now := time.Now().UTC()
	acct = schema.MerchantQuotaAccounts{
		MerchantID:     merchantID,
		Currency:       CurrencyInternalUnits,
		AvailableUnits: 0,
		ReservedUnits:  0,
		PriceVersionID: DefaultPriceVersionID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := gdb.Create(&acct).Error; err != nil {
		if !isUniqueViolation(err) {
			return schema.MerchantQuotaAccounts{}, apperr.Internal("创建额度账户失败")
		}
	}
	return lockAccount(gdb, merchantID)
}

func lockAccount(gdb *gorm.DB, merchantID string) (schema.MerchantQuotaAccounts, error) {
	var acct schema.MerchantQuotaAccounts
	err := gdb.Clauses(pfdb.ForUpdate()).Where("merchant_id = ?", merchantID).Take(&acct).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.MerchantQuotaAccounts{}, apperr.NotFound("额度账户不存在")
	}
	if err != nil {
		return schema.MerchantQuotaAccounts{}, apperr.Internal("锁定额度账户失败")
	}
	return acct, nil
}

func loadHold(gdb *gorm.DB, merchantID, key string) (schema.MerchantQuotaHolds, bool, error) {
	var row schema.MerchantQuotaHolds
	err := gdb.Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.MerchantQuotaHolds{}, false, nil
	}
	if err != nil {
		return schema.MerchantQuotaHolds{}, false, apperr.Internal("读取预留失败")
	}
	return row, true, nil
}

func loadHoldForUpdate(gdb *gorm.DB, merchantID, key string) (schema.MerchantQuotaHolds, bool, error) {
	var row schema.MerchantQuotaHolds
	err := gdb.Clauses(pfdb.ForUpdate()).
		Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.MerchantQuotaHolds{}, false, nil
	}
	if err != nil {
		return schema.MerchantQuotaHolds{}, false, apperr.Internal("锁定预留失败")
	}
	return row, true, nil
}

func loadEvent(gdb *gorm.DB, merchantID, eventType, key string) (schema.MerchantQuotaEvents, bool, error) {
	var row schema.MerchantQuotaEvents
	err := gdb.Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, eventType, key).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.MerchantQuotaEvents{}, false, nil
	}
	if err != nil {
		return schema.MerchantQuotaEvents{}, false, apperr.Internal("读取额度事件失败")
	}
	return row, true, nil
}

func appendEvent(gdb *gorm.DB, ev schema.MerchantQuotaEvents) error {
	if err := gdb.Create(&ev).Error; err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return apperr.Internal("写入额度事件失败")
	}
	return nil
}

func accountFromRow(row schema.MerchantQuotaAccounts) Account {
	return Account{
		MerchantID:     row.MerchantID,
		Currency:       row.Currency,
		AvailableUnits: row.AvailableUnits,
		ReservedUnits:  row.ReservedUnits,
		PriceVersionID: row.PriceVersionID,
	}
}

func holdFromRow(row schema.MerchantQuotaHolds) Hold {
	return Hold{
		ID:             row.ID,
		MerchantID:     row.MerchantID,
		IdempotencyKey: row.IdempotencyKey,
		AmountUnits:    row.AmountUnits,
		SettledUnits:   row.SettledUnits,
		Status:         row.Status,
		PriceVersionID: row.PriceVersionID,
	}
}

func optionalString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
