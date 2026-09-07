package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

// B3：Agent 模型调用在 before_model_request 准入前 Reserve；成功 Settle、中断不明 MarkUnknown。
// 单价占位 1 内部单位；幂等键绑定 turn_projection_id + model_request_id。
// 准入闸通过后即视为可能已发出，恢复中断走 MarkUnknown（禁止当零消费 Release）。

const agentModelQuotaUnits int64 = 1

func modelInvocationQuotaKey(projectionID, modelRequestID string) string {
	return fmt.Sprintf("agent-model-invocation:%s:%s",
		strings.TrimSpace(projectionID), strings.TrimSpace(modelRequestID))
}

func merchantIDForProjection(ctx context.Context, gdb *gorm.DB, projectionID string) (string, error) {
	projectionID = strings.TrimSpace(projectionID)
	if projectionID == "" {
		return "", apperr.Internal("缺少 Turn 投影")
	}
	var merchantID string
	err := gdb.WithContext(ctx).Raw(`
		SELECT c.merchant_id
		FROM agent_turn_projections p
		JOIN agent_conversations c ON c.id = p.conversation_id
		WHERE p.id = ?`, projectionID).Scan(&merchantID).Error
	if err != nil {
		return "", apperr.Internal("读取会话商家失败")
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return "", apperr.Internal("会话缺少商家归属")
	}
	return merchantID, nil
}

func reserveModelInvocationQuota(ctx context.Context, gdb *gorm.DB, merchantID, projectionID, modelRequestID string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return apperr.Internal("缺少商家额度归属")
	}
	_, _, err := (&quota.Service{DB: gdb}).Reserve(ctx, merchantID,
		modelInvocationQuotaKey(projectionID, modelRequestID), agentModelQuotaUnits, quota.DefaultPriceVersionID)
	return err
}

func settleModelInvocationQuota(ctx context.Context, gdb *gorm.DB, merchantID, projectionID, modelRequestID string) error {
	return finalizeQuotaIgnoreMissing((&quota.Service{DB: gdb}).Settle(ctx, merchantID,
		modelInvocationQuotaKey(projectionID, modelRequestID), agentModelQuotaUnits))
}

func markModelInvocationQuotaUnknown(ctx context.Context, gdb *gorm.DB, merchantID, projectionID, modelRequestID string) error {
	return finalizeQuotaIgnoreMissing((&quota.Service{DB: gdb}).MarkUnknown(ctx, merchantID,
		modelInvocationQuotaKey(projectionID, modelRequestID)))
}

func finalizeQuotaIgnoreMissing(hold quota.Hold, acct quota.Account, err error) error {
	if err == nil {
		return nil
	}
	if apperr.IsNotFound(err) {
		// 夹具或跳过路径未 Reserve；不要把缺 hold 升级成 worker 失败。
		return nil
	}
	return err
}

// finalizeQuotaForInterruptedInvocations 在 lease 过期把 started 调用标 interrupted 后，对仍 reserved 的 hold 走 MarkUnknown。
func finalizeQuotaForInterruptedInvocations(ctx context.Context, gdb *gorm.DB, projectionID string) error {
	merchantID, err := merchantIDForProjection(ctx, gdb, projectionID)
	if err != nil {
		return err
	}
	var requestIDs []string
	if err := gdb.WithContext(ctx).Raw(`
		SELECT model_request_id FROM agent_model_invocations
		WHERE turn_projection_id = ? AND status = 'interrupted'`, projectionID).
		Scan(&requestIDs).Error; err != nil {
		return err
	}
	for _, requestID := range requestIDs {
		if err := markModelInvocationQuotaUnknown(ctx, gdb, merchantID, projectionID, requestID); err != nil {
			return err
		}
	}
	return nil
}
