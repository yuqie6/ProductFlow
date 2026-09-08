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
	key, active, err := activeQuotaKey(ctx, e.DB, merchantID, taskID)
	if err != nil {
		return err
	}
	if !active {
		return apperr.NotFound("生成任务没有活动额度预留")
	}
	_, _, err = e.quota().Settle(ctx, merchantID, key, imageSessionQuotaUnits)
	return err
}

func (e Executor) markGenerationQuotaUnknown(ctx context.Context, merchantID, taskID string) error {
	key, _, err := activeQuotaKey(ctx, e.DB, merchantID, taskID)
	if err != nil {
		return err
	}
	_, _, err = e.quota().MarkUnknown(ctx, merchantID, key)
	return err
}

func (e Executor) releaseGenerationQuota(ctx context.Context, merchantID, taskID string) error {
	key, _, err := activeQuotaKey(ctx, e.DB, merchantID, taskID)
	if err != nil {
		return err
	}
	_, _, err = e.quota().Release(ctx, merchantID, key)
	return err
}

func activeQuotaKey(ctx context.Context, db *gorm.DB, merchantID, taskID string) (string, bool, error) {
	merchantID = strings.TrimSpace(merchantID)
	taskID = strings.TrimSpace(taskID)
	var task schema.ImageSessionGenerationTasks
	if err := db.WithContext(ctx).Select("billing_seq").Where("id = ?", taskID).Take(&task).Error; err != nil {
		return "", false, err
	}
	key := generationQuotaKey(taskID, task.BillingSeq)
	var row schema.MerchantQuotaHolds
	err := db.WithContext(ctx).
		Where("merchant_id = ? AND idempotency_key = ? AND status IN ?", merchantID, key, []string{
			quota.StatusReserved, quota.StatusPendingReconciliation,
		}).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return key, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("读取额度预留失败: %w", err)
	}
	return row.IdempotencyKey, true, nil
}

func sessionMerchantID(ctx context.Context, db *gorm.DB, sessionID string) (string, error) {
	var merchantID string
	err := db.WithContext(ctx).Model(&schema.ImageSessions{}).
		Select("merchant_id").Where("id = ?", sessionID).Scan(&merchantID).Error
	if err != nil {
		return "", errors.Join(apperr.Internal("读取会话商家失败"), err)
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
		Where("generation_task_id = ? AND billing_seq = (?)", taskID, e.DB.Model(&schema.ImageSessionGenerationTasks{}).Select("billing_seq").Where("id = ?", taskID)).Count(&n).Error
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
