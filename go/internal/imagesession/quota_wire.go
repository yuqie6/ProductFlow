package imagesession

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

// B1：图会话 Generate 入队前 Reserve；成功 Settle、未发出 Release、已发出不明 MarkUnknown。
// 单价占位 1 内部单位；幂等键绑定 generation task identity（重试另开 billing seq）。

const imageSessionQuotaUnits int64 = 1

func (s Service) quota() *quota.Service {
	return &quota.Service{DB: s.DB}
}

func (e Executor) quota() *quota.Service {
	return &quota.Service{DB: e.DB}
}

func generationQuotaKey(taskID string, billingSeq int) string {
	taskID = strings.TrimSpace(taskID)
	if billingSeq <= 0 {
		return "image-session-generation:" + taskID
	}
	return fmt.Sprintf("image-session-generation:%s:retry:%d", taskID, billingSeq)
}

func (s Service) reserveGenerationQuota(ctx context.Context, merchantID, taskID string, billingSeq int) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return apperr.Internal("缺少商家额度归属")
	}
	_, _, err := s.quota().Reserve(ctx, merchantID, generationQuotaKey(taskID, billingSeq), imageSessionQuotaUnits, quota.DefaultPriceVersionID)
	return err
}

func (e Executor) settleGenerationQuota(ctx context.Context, merchantID, taskID string) error {
	key, err := activeQuotaKey(ctx, e.DB, merchantID, taskID)
	if err != nil {
		return err
	}
	_, _, err = e.quota().Settle(ctx, merchantID, key, imageSessionQuotaUnits)
	return err
}

func (e Executor) markGenerationQuotaUnknown(ctx context.Context, merchantID, taskID string) error {
	key, err := activeQuotaKey(ctx, e.DB, merchantID, taskID)
	if err != nil {
		return err
	}
	return finalizeQuotaIgnoreMissing(e.quota().MarkUnknown(ctx, merchantID, key))
}

func (e Executor) releaseGenerationQuota(ctx context.Context, merchantID, taskID string) error {
	key, err := activeQuotaKey(ctx, e.DB, merchantID, taskID)
	if err != nil {
		return err
	}
	return finalizeQuotaIgnoreMissing(e.quota().Release(ctx, merchantID, key))
}

func finalizeQuotaIgnoreMissing(hold quota.Hold, acct quota.Account, err error) error {
	if err == nil {
		return nil
	}
	if apperr.IsNotFound(err) {
		// 直插任务夹具未走 Generate Reserve；不要把缺 hold 升级成 worker 失败。
		return nil
	}
	return err
}

func activeQuotaKey(ctx context.Context, db *gorm.DB, merchantID, taskID string) (string, error) {
	merchantID = strings.TrimSpace(merchantID)
	taskID = strings.TrimSpace(taskID)
	if merchantID == "" || taskID == "" {
		return generationQuotaKey(taskID, 0), nil
	}
	prefix := "image-session-generation:" + taskID
	var row schema.MerchantQuotaHolds
	err := db.WithContext(ctx).
		Where("merchant_id = ? AND idempotency_key LIKE ? AND status IN ?", merchantID, prefix+"%", []string{
			quota.StatusReserved, quota.StatusPendingReconciliation,
		}).
		Order("created_at DESC").
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return generationQuotaKey(taskID, 0), nil
	}
	if err != nil {
		return "", fmt.Errorf("读取额度预留失败: %w", err)
	}
	return row.IdempotencyKey, nil
}

func sessionMerchantID(ctx context.Context, db *gorm.DB, sessionID string) (string, error) {
	var merchantID string
	err := db.WithContext(ctx).Model(&schema.ImageSessions{}).
		Select("merchant_id").Where("id = ?", sessionID).Scan(&merchantID).Error
	if err != nil {
		return "", apperr.Internal("读取会话商家失败")
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return "", apperr.Internal("会话缺少商家归属")
	}
	return merchantID, nil
}

func (e Executor) providerLedgerExists(ctx context.Context, taskID string) (bool, error) {
	var n int64
	err := e.DB.WithContext(ctx).Model(&schema.ImageSessionProviderEffects{}).
		Where("generation_task_id = ?", taskID).Count(&n).Error
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (e Executor) finalizeQuotaOnCancel(ctx context.Context, sessionID, taskID string) error {
	merchantID, err := sessionMerchantID(ctx, e.DB, sessionID)
	if err != nil {
		return err
	}
	started, err := e.providerLedgerExists(ctx, taskID)
	if err != nil {
		return err
	}
	if started {
		// 已写 provider effect：可能已发出或在途，禁止当零消费 Release。
		return e.markGenerationQuotaUnknown(ctx, merchantID, taskID)
	}
	return e.releaseGenerationQuota(ctx, merchantID, taskID)
}

func (e Executor) finalizeQuotaOnTerminalFailure(ctx context.Context, sessionID, taskID string) error {
	merchantID, err := sessionMerchantID(ctx, e.DB, sessionID)
	if err != nil {
		return err
	}
	started, err := e.providerLedgerExists(ctx, taskID)
	if err != nil {
		return err
	}
	if started {
		return e.settleGenerationQuota(ctx, merchantID, taskID)
	}
	return e.releaseGenerationQuota(ctx, merchantID, taskID)
}
