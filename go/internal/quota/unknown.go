package quota

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// DefaultUnknownHoldTTL 是 pending_reconciliation 在无 Op 裁定前的最长停留时长。
// 到期后释放用户预留；供应商调用事实仍由各自的 unknown 账本保留。
const DefaultUnknownHoldTTL = 72 * time.Hour

// UnknownExpiryReason 是到期自动释放事件的固定原因。
const UnknownExpiryReason = "unknown hold TTL expiry; released user reservation; provider result remains unknown"

// DefaultExpireUnknownBatch 是单轮到期扫描默认批大小。
const DefaultExpireUnknownBatch = 100

// UnknownHoldTTLFromEnv 读取 QUOTA_UNKNOWN_HOLD_TTL（Go duration，如 72h）。
// 空或非法回落 DefaultUnknownHoldTTL；必须为正。
func UnknownHoldTTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("QUOTA_UNKNOWN_HOLD_TTL"))
	if raw == "" {
		return DefaultUnknownHoldTTL
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return DefaultUnknownHoldTTL
	}
	return d
}

func (s *Service) unknownHoldTTL() time.Duration {
	if s != nil && s.UnknownHoldTTL != nil {
		d := *s.UnknownHoldTTL
		if d > 0 {
			return d
		}
	}
	return UnknownHoldTTLFromEnv()
}

// ResolveUnknown 是 Operator 对 pending_reconciliation hold 的明示裁定。
// 复用 Settle：actualUnits ∈ [0, reserved]；0 表示核查后确认零消费（≠自动到期释放）。
// 非 pending 拒绝；普通 Release 对 unknown 仍禁止。
func (s *Service) ResolveUnknown(ctx context.Context, merchantID, idempotencyKey string, actualUnits int64, reason, actorUserID string) (Hold, Account, error) {
	merchantID = strings.TrimSpace(merchantID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	reason = strings.TrimSpace(reason)
	actorUserID = strings.TrimSpace(actorUserID)
	if merchantID == "" {
		return Hold{}, Account{}, apperr.Validation("缺少商家")
	}
	if idempotencyKey == "" {
		return Hold{}, Account{}, apperr.Validation("缺少幂等键")
	}
	if reason == "" {
		return Hold{}, Account{}, apperr.Validation("缺少裁定原因")
	}
	if actualUnits < 0 {
		return Hold{}, Account{}, apperr.Validation("结算额度不能为负")
	}

	return s.settle(ctx, merchantID, idempotencyKey, actualUnits, settleOptions{
		RequirePending: true,
		Reason:         reason,
		ActorUserID:    actorUserID,
	})
}

// ExpireUnknownHolds 扫描已超过 TTL 的 pending_reconciliation，释放用户预留。
// 复用 Release 的账本更新；已终态（含 Op/迟到结果抢先收口）跳过。limit<=0 用 DefaultExpireUnknownBatch。
func (s *Service) ExpireUnknownHolds(ctx context.Context, now time.Time, limit int) (expired int, hasMore bool, err error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if limit <= 0 {
		limit = DefaultExpireUnknownBatch
	}
	ttl := s.unknownHoldTTL()
	cutoff := now.Add(-ttl)

	var rows []schema.MerchantQuotaHolds
	if err := s.DB.WithContext(ctx).
		Where("status = ? AND updated_at <= ?", StatusPendingReconciliation, cutoff).
		Order("updated_at ASC, id ASC").
		Limit(limit + 1).
		Find(&rows).Error; err != nil {
		return 0, false, apperr.Internal("扫描待核对预留失败")
	}
	hasMore = len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	for _, row := range rows {
		_, _, changed, releaseErr := s.release(ctx, row.MerchantID, row.IdempotencyKey, releaseOptions{
			RequirePending: true,
			Reason:         UnknownExpiryReason,
		})
		if releaseErr == nil {
			if changed {
				expired++
			}
			continue
		}
		// Op 或并发已收口：跳过；其它错误中止本轮。
		if isConflict(releaseErr) {
			terminal, termErr := s.holdIsTerminal(ctx, row.MerchantID, row.IdempotencyKey)
			if termErr != nil {
				return expired, hasMore, termErr
			}
			if terminal {
				continue
			}
		}
		return expired, hasMore, releaseErr
	}
	return expired, hasMore, nil
}

func (s *Service) holdIsTerminal(ctx context.Context, merchantID, key string) (bool, error) {
	var row schema.MerchantQuotaHolds
	err := s.DB.WithContext(ctx).
		Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, apperr.Internal("读取预留失败")
	}
	return row.Status == StatusSettled || row.Status == StatusReleased, nil
}

func isConflict(err error) bool {
	var ae apperr.Error
	return errors.As(err, &ae) && ae.Status == 409
}
