package localedit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

// MP-C：localedit 在 provider Edit 前 Reserve；成功 Settle、未发出 Release、已发出不明 MarkUnknown。
// 单价占位 1 内部单位；幂等键绑定 task + attempt（重试另开 attempt）。
// 迟到 worker 只能收口自己的预留，不能回退到任务下其它 attempt 的活跃预留。

const localEditQuotaUnits int64 = 1

func (s Service) quota() *quota.Service {
	return &quota.Service{DB: s.DB}
}

func (e Executor) quota() *quota.Service {
	return &quota.Service{DB: e.DB}
}

func editQuotaKey(taskID, attemptID string) string {
	return fmt.Sprintf("local-edit:%s:%s", strings.TrimSpace(taskID), strings.TrimSpace(attemptID))
}

func merchantIDForProduct(ctx context.Context, db *gorm.DB, productID string) (string, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return "", apperr.Internal("缺少商品")
	}
	var merchantID string
	err := db.WithContext(ctx).Model(&schema.Products{}).
		Select("merchant_id").Where("id = ?", productID).Scan(&merchantID).Error
	if err != nil {
		return "", errors.Join(apperr.Internal("读取商品商家失败"), err)
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return "", apperr.Internal("商品缺少商家归属")
	}
	return merchantID, nil
}

func (e Executor) reserveEditQuota(ctx context.Context, merchantID, taskID, attemptID string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return apperr.Internal("缺少商家额度归属")
	}
	_, _, err := e.quota().Reserve(ctx, merchantID, editQuotaKey(taskID, attemptID), localEditQuotaUnits, quota.DefaultPriceVersionID)
	return err
}

func (e Executor) settleEditQuota(ctx context.Context, merchantID, taskID, attemptID string) error {
	key := editQuotaKey(taskID, attemptID)
	_, _, err := e.quota().Settle(ctx, merchantID, key, localEditQuotaUnits)
	return err
}

func (e Executor) markEditQuotaUnknown(ctx context.Context, merchantID, taskID, attemptID string) error {
	key := editQuotaKey(taskID, attemptID)
	_, _, err := e.quota().MarkUnknown(ctx, merchantID, key)
	return err
}

func (e Executor) releaseEditQuota(ctx context.Context, merchantID, taskID, attemptID string) error {
	key := editQuotaKey(taskID, attemptID)
	return releaseQuotaIgnoreMissing(e.quota().Release(ctx, merchantID, key))
}

func releaseQuotaIgnoreMissing(hold quota.Hold, acct quota.Account, err error) error {
	if err == nil {
		return nil
	}
	if apperr.IsNotFound(err) {
		// 调用准备提交前的取消或失败尚无预留，释放为空操作。
		return nil
	}
	return err
}

func providerPhaseStarted(phase string) bool {
	switch strings.TrimSpace(phase) {
	case "provider_pending", "provider_call", "provider_result_received":
		return true
	default:
		return false
	}
}

// finalizeEditQuotaOnCancel 取消收口：未过 provider 边界 → Release；已过 → MarkUnknown（禁止当零消费）。
func (s Service) finalizeEditQuotaOnCancel(ctx context.Context, merchantID, taskID, attemptID, progressPhase string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return nil
	}
	key := editQuotaKey(taskID, attemptID)
	if providerPhaseStarted(progressPhase) {
		return (Executor{DB: s.DB}).markEditQuotaUnknown(ctx, merchantID, taskID, attemptID)
	}
	return releaseQuotaIgnoreMissing(s.quota().Release(ctx, merchantID, key))
}

func finalizeRecoveredUnknownQuota(ctx context.Context, db *gorm.DB, productID, taskID, attemptID string) error {
	merchantID, err := merchantIDForProduct(ctx, db, productID)
	if err != nil {
		return err
	}
	return (Executor{DB: db}).markEditQuotaUnknown(ctx, merchantID, taskID, attemptID)
}

func quotaConflictDetail(err error) string {
	var ae apperr.Error
	if errors.As(err, &ae) && strings.TrimSpace(ae.Detail) != "" {
		return ae.Detail
	}
	return "可用额度不足"
}
