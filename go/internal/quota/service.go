// Package quota 实现商家商业额度账本（MP-C B0）。
//
// 与平台调用事实分离：agent_model_invocations.usage_source 仍是 provider/estimated/unavailable
// 操作事实；本包只管商家被收取的服务额度（available/reserved/事件），不另造平台成本账。
//
// B0 账本已交付；B1–B3 已接图会话 / Graph / Agent 主入口。
// B4 余额 HTTP：商家只读本商 + Op 只读/调账（见 http.go）。未知结果必须走 MarkUnknown；到期释放预留不代表供应商成功或零成本。
// 价格目录 B0：quota_price_versions / entries 为单价真相；DefaultPriceVersionID 仅命名默认种子行；Reserve 校验版本。
// 新商家首次建账按 QUOTA_TRIAL_UNITS（默认 DefaultTrialUnits）种子可用额度；≠真实支付。
//
// unknown / pending_reconciliation 运营合同（B0）：
//   - MarkUnknown 后保留 reserved 负债；普通 Release 一律拒绝。
//   - Op 明示裁定：ResolveUnknown → Settle(actual∈[0,reserved])，须写 reason；0 仅表示核查后确认零消费。
//   - 到期：停留超过 QUOTA_UNKNOWN_HOLD_TTL（默认 72h）后 ExpireUnknownHolds 释放用户预留；原始 unknown 事件仍保留。
//   - Settle/Release 幂等键与 hold 相同，重复裁定/到期不双结或双释。
package quota

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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

	quotaEventDefaultPageSize = 20
	quotaEventMaxPageSize     = 100
	quotaEventMaxPage         = 100000

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
	// TrialUnits 非 nil 时覆盖 QUOTA_TRIAL_UNITS，仅作用于新建账户行（含 0）。
	TrialUnits *int64
	// UnknownHoldTTL 非 nil 且 >0 时覆盖 QUOTA_UNKNOWN_HOLD_TTL（测试常用）。
	UnknownHoldTTL *time.Duration
}

func (s *Service) trialUnits() int64 {
	if s != nil && s.TrialUnits != nil {
		return *s.TrialUnits
	}
	return TrialUnitsFromEnv()
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

// Event 是额度账本的原始审计投影。幂等键和价格版本仍留在账本中，
// 但不属于管理员事件页的公开字段。
type Event struct {
	ID             string    `json:"id"`
	MerchantID     string    `json:"merchant_id"`
	HoldID         *string   `json:"hold_id"`
	EventType      string    `json:"event_type"`
	AmountUnits    int64     `json:"amount_units"`
	AvailableAfter int64     `json:"available_after"`
	ReservedAfter  int64     `json:"reserved_after"`
	Reason         *string   `json:"reason"`
	ActorUserID    *string   `json:"actor_user_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// EventPage 是管理员额度事件页的有界响应。
type EventPage struct {
	Items    []Event `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
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

// ListEvents 直接读取指定商家的追加式额度账本。商家存在但没有事件时返回空数组；
// 不存在的商家返回 404，避免把一个不存在的目标伪装成空账本。
func (s *Service) ListEvents(ctx context.Context, merchantID string, page, pageSize int) (EventPage, error) {
	if s == nil || s.DB == nil {
		return EventPage{}, apperr.Internal("额度存储未配置")
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return EventPage{}, apperr.Validation("缺少商家")
	}
	if page < 1 || page > quotaEventMaxPage {
		return EventPage{}, apperr.Validation("额度事件页码无效")
	}
	if pageSize < 1 || pageSize > quotaEventMaxPageSize {
		return EventPage{}, apperr.Validation("额度事件每页数量无效")
	}

	db := s.DB.WithContext(ctx)
	var merchant schema.Merchants
	err := db.Select("id").Where("id = ?", merchantID).Take(&merchant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return EventPage{}, apperr.NotFound("资源不存在")
	}
	if err != nil {
		return EventPage{}, apperr.Internal("读取商家失败")
	}

	query := db.Model(&schema.MerchantQuotaEvents{}).Where("merchant_id = ?", merchantID)
	out := EventPage{
		Items:    make([]Event, 0, pageSize),
		Page:     page,
		PageSize: pageSize,
	}
	if err := query.Count(&out.Total).Error; err != nil {
		return EventPage{}, apperr.Internal("读取额度事件失败")
	}
	var rows []schema.MerchantQuotaEvents
	if err := query.Select("id", "merchant_id", "hold_id", "event_type", "amount_units", "available_after", "reserved_after", "reason", "actor_user_id", "created_at").
		Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return EventPage{}, apperr.Internal("读取额度事件失败")
	}
	for _, row := range rows {
		out.Items = append(out.Items, Event{
			ID:             row.ID,
			MerchantID:     row.MerchantID,
			HoldID:         row.HoldID,
			EventType:      row.EventType,
			AmountUnits:    row.AmountUnits,
			AvailableAfter: row.AvailableAfter,
			ReservedAfter:  row.ReservedAfter,
			Reason:         row.Reason,
			ActorUserID:    row.ActorUserID,
			CreatedAt:      row.CreatedAt,
		})
	}
	return out, nil
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
	if utf8.RuneCountInString(idempotencyKey) > 200 {
		return Account{}, apperr.Validation("幂等键不能超过 200 个字符")
	}
	if deltaUnits == 0 {
		return Account{}, apperr.Validation("调账额度不能为 0")
	}
	if reason == "" {
		return Account{}, apperr.Validation("调账原因不能为空")
	}
	if utf8.RuneCountInString(reason) > 2000 {
		return Account{}, apperr.Validation("调账原因不能超过 2000 个字符")
	}

	var out Account
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		acct, err := ensureAndLockAccount(gdb, merchantID, s.trialUnits())
		if err != nil {
			return err
		}
		if existing, ok, err := loadEvent(gdb, merchantID, EventAdjust, idempotencyKey); err != nil {
			return err
		} else if ok {
			if existing.AmountUnits != deltaUnits || normalizedOptionalString(existing.Reason) != reason || normalizedOptionalString(existing.ActorUserID) != actorUserID {
				return apperr.Conflict("幂等键已用于其他调账")
			}
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
			return errors.Join(apperr.Internal("更新额度账户失败"), err)
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

func normalizedOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
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
		if err := requirePriceVersion(gdb, priceVersionID); err != nil {
			return err
		}
		// 先锁账户再查 hold，保证同键并发只扣一次 available。
		acct, err := ensureAndLockAccount(gdb, merchantID, s.trialUnits())
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
			return errors.Join(apperr.Internal("更新额度账户失败"), err)
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
			return errors.Join(apperr.Internal("创建预留失败"), err)
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
// reserved 与 pending_reconciliation 均可收口；unknown 运营裁定见 ResolveUnknown / ExpireUnknownHolds。
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
	return s.settle(ctx, merchantID, idempotencyKey, actualUnits, settleOptions{})
}

type settleOptions struct {
	RequirePending bool
	Reason         string
	ActorUserID    string
}

func (s *Service) settle(ctx context.Context, merchantID, idempotencyKey string, actualUnits int64, opts settleOptions) (Hold, Account, error) {
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
			if opts.RequirePending {
				// Op/到期重放：已裁定且额度一致视为幂等成功。
			}
			if row.SettledUnits != nil && *row.SettledUnits != actualUnits {
				return apperr.Conflict("结算幂等键已存在且额度不一致")
			}
			hold = holdFromRow(row)
			acctOut = accountFromRow(acct)
			return nil
		}
		if opts.RequirePending {
			if row.Status != StatusPendingReconciliation {
				return apperr.Conflict("预留状态不允许裁定")
			}
		} else if row.Status != StatusReserved && row.Status != StatusPendingReconciliation {
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
			return errors.Join(apperr.Internal("更新额度账户失败"), err)
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
			return errors.Join(apperr.Internal("更新预留失败"), err)
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
			Reason:         optionalString(opts.Reason),
			ActorUserID:    optionalString(opts.ActorUserID),
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

// Release 取消已明确未发出的调用：把预留退回 available。unknown 只由 TTL 到期内部路径释放。
func (s *Service) Release(ctx context.Context, merchantID, idempotencyKey string) (Hold, Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if merchantID == "" {
		return Hold{}, Account{}, apperr.Validation("缺少商家")
	}
	if idempotencyKey == "" {
		return Hold{}, Account{}, apperr.Validation("缺少幂等键")
	}
	hold, acct, _, err := s.release(ctx, merchantID, idempotencyKey, releaseOptions{})
	return hold, acct, err
}

type releaseOptions struct {
	RequirePending bool
	Reason         string
}

// release performs the account and hold transition under one account-first transaction.
// changed is false for an idempotent replay, which lets scanners report one effective release
// when two workers selected the same stale row before either one committed.
func (s *Service) release(ctx context.Context, merchantID, idempotencyKey string, opts releaseOptions) (hold Hold, acctOut Account, changed bool, err error) {
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
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
		if opts.RequirePending {
			if row.Status != StatusPendingReconciliation {
				return apperr.Conflict("预留状态不允许到期释放")
			}
		} else if row.Status != StatusReserved {
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
			return errors.Join(apperr.Internal("更新额度账户失败"), err)
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
			return errors.Join(apperr.Internal("更新预留失败"), err)
		}
		row.Status = StatusReleased
		row.UpdatedAt = now
		changed = true

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
			Reason:         optionalString(opts.Reason),
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		hold = holdFromRow(row)
		acctOut = accountFromRow(acct)
		return nil
	})
	return hold, acctOut, changed, err
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
			return errors.Join(apperr.Internal("更新预留失败"), err)
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

// EnsureAccount 在商家创建/激活后显式建账；尚无行时按试用配置种子 available，已有行不改余额。
func (s *Service) EnsureAccount(ctx context.Context, merchantID string) (Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return Account{}, apperr.Validation("缺少商家")
	}
	var out Account
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		acct, err := ensureAndLockAccount(gdb, merchantID, s.trialUnits())
		if err != nil {
			return err
		}
		out = accountFromRow(acct)
		return nil
	})
	return out, err
}

func ensureAndLockAccount(gdb *gorm.DB, merchantID string, trialUnits int64) (schema.MerchantQuotaAccounts, error) {
	var acct schema.MerchantQuotaAccounts
	err := gdb.Clauses(pfdb.ForUpdate()).Where("merchant_id = ?", merchantID).Take(&acct).Error
	if err == nil {
		return acct, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.MerchantQuotaAccounts{}, apperr.Internal("锁定额度账户失败")
	}
	if trialUnits < 0 {
		trialUnits = 0
	}
	now := time.Now().UTC()
	acct = schema.MerchantQuotaAccounts{
		MerchantID:     merchantID,
		Currency:       CurrencyInternalUnits,
		AvailableUnits: trialUnits,
		ReservedUnits:  0,
		PriceVersionID: DefaultPriceVersionID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := gdb.Create(&acct).Error; err != nil {
		if !isUniqueViolation(err) {
			return schema.MerchantQuotaAccounts{}, apperr.Internal("创建额度账户失败")
		}
		return lockAccount(gdb, merchantID)
	}
	if trialUnits > 0 {
		if err := appendEvent(gdb, schema.MerchantQuotaEvents{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			EventType:      EventAdjust,
			AmountUnits:    trialUnits,
			IdempotencyKey: TrialSeedIdempotencyKey,
			AvailableAfter: acct.AvailableUnits,
			ReservedAfter:  acct.ReservedUnits,
			PriceVersionID: acct.PriceVersionID,
			Reason:         optionalString(TrialSeedReason),
			CreatedAt:      now,
		}); err != nil {
			return schema.MerchantQuotaAccounts{}, err
		}
	}
	return acct, nil
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
		return errors.Join(apperr.Internal("写入额度事件失败"), err)
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
