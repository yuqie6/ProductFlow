package graph

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

// B2：Graph image_generation 在 provider 出图前 Reserve；成功 Settle、未发出 Release、已发出不明 MarkUnknown。
// 单价占位 1 内部单位；幂等键绑定 node_run + attempt（重试另开 attempt）。

const graphImageQuotaUnits int64 = 1

func (e Executor) quota() *quota.Service {
	return &quota.Service{DB: e.DB}
}

func (s Service) quota() *quota.Service {
	return &quota.Service{DB: s.DB}
}

func imageNodeQuotaKey(nodeRunID, attemptID string) string {
	nodeRunID = strings.TrimSpace(nodeRunID)
	attemptID = strings.TrimSpace(attemptID)
	return fmt.Sprintf("graph-image-node:%s:%s", nodeRunID, attemptID)
}

func (e Executor) reserveImageQuota(ctx context.Context, merchantID, nodeRunID, attemptID string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return apperr.Internal("缺少商家额度归属")
	}
	_, _, err := e.quota().Reserve(ctx, merchantID, imageNodeQuotaKey(nodeRunID, attemptID), graphImageQuotaUnits, quota.DefaultPriceVersionID)
	return err
}

func (e Executor) settleImageQuota(ctx context.Context, merchantID, nodeRunID, attemptID string) error {
	key := mustActiveImageQuotaKey(ctx, e.DB, merchantID, nodeRunID, attemptID)
	return finalizeQuotaIgnoreMissing(e.quota().Settle(ctx, merchantID, key, graphImageQuotaUnits))
}

func (e Executor) markImageQuotaUnknown(ctx context.Context, merchantID, nodeRunID, attemptID string) error {
	key := mustActiveImageQuotaKey(ctx, e.DB, merchantID, nodeRunID, attemptID)
	return finalizeQuotaIgnoreMissing(e.quota().MarkUnknown(ctx, merchantID, key))
}

func (e Executor) releaseImageQuota(ctx context.Context, merchantID, nodeRunID, attemptID string) error {
	key := mustActiveImageQuotaKey(ctx, e.DB, merchantID, nodeRunID, attemptID)
	return finalizeQuotaIgnoreMissing(e.quota().Release(ctx, merchantID, key))
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

func mustActiveImageQuotaKey(ctx context.Context, db *gorm.DB, merchantID, nodeRunID, attemptID string) string {
	key, err := activeImageQuotaKey(ctx, db, merchantID, nodeRunID, attemptID)
	if err != nil || key == "" {
		return imageNodeQuotaKey(nodeRunID, attemptID)
	}
	return key
}

func activeImageQuotaKey(ctx context.Context, db *gorm.DB, merchantID, nodeRunID, attemptID string) (string, error) {
	merchantID = strings.TrimSpace(merchantID)
	nodeRunID = strings.TrimSpace(nodeRunID)
	attemptID = strings.TrimSpace(attemptID)
	if merchantID == "" || nodeRunID == "" {
		return imageNodeQuotaKey(nodeRunID, attemptID), nil
	}
	if attemptID != "" {
		exact := imageNodeQuotaKey(nodeRunID, attemptID)
		var row schema.MerchantQuotaHolds
		err := db.WithContext(ctx).
			Where("merchant_id = ? AND idempotency_key = ? AND status IN ?", merchantID, exact, []string{
				quota.StatusReserved, quota.StatusPendingReconciliation,
			}).
			Take(&row).Error
		if err == nil {
			return row.IdempotencyKey, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", apperr.Internal("读取额度预留失败")
		}
	}
	prefix := "graph-image-node:" + nodeRunID + ":"
	var row schema.MerchantQuotaHolds
	err := db.WithContext(ctx).
		Where("merchant_id = ? AND idempotency_key LIKE ? AND status IN ?", merchantID, prefix+"%", []string{
			quota.StatusReserved, quota.StatusPendingReconciliation,
		}).
		Order("created_at DESC").
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return imageNodeQuotaKey(nodeRunID, attemptID), nil
	}
	if err != nil {
		return "", apperr.Internal("读取额度预留失败")
	}
	return row.IdempotencyKey, nil
}

func merchantIDForGraphRun(ctx context.Context, db *gorm.DB, runID string) (string, error) {
	var merchantID string
	err := db.WithContext(ctx).Raw(`
		SELECT p.merchant_id
		FROM workflow_graph_runs r
		JOIN workflow_graphs g ON g.id = r.graph_id
		JOIN products p ON p.id = g.product_id
		WHERE r.id = ?`, runID).Scan(&merchantID).Error
	if err != nil {
		return "", apperr.Internal("读取运行商家失败")
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return "", apperr.Internal("运行缺少商家归属")
	}
	return merchantID, nil
}

func imageProviderEffectExists(ctx context.Context, db *gorm.DB, nodeRunID, attemptID string) (bool, error) {
	nodeRunID = strings.TrimSpace(nodeRunID)
	attemptID = strings.TrimSpace(attemptID)
	if nodeRunID == "" {
		return false, nil
	}
	q := db.WithContext(ctx).Model(&schema.WorkflowGraphProviderEffects{}).Where("node_run_id = ?", nodeRunID)
	if attemptID != "" {
		q = q.Where("attempt_id = ?", attemptID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// finalizeImageQuotaOnCancel 取消收口：未写 provider effect → Release；已写 → MarkUnknown（禁止当零消费）。
func (s Service) finalizeImageQuotaOnCancel(ctx context.Context, merchantID, nodeRunID, attemptID string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return nil
	}
	started, err := imageProviderEffectExists(ctx, s.DB, nodeRunID, attemptID)
	if err != nil {
		return err
	}
	key := mustActiveImageQuotaKey(ctx, s.DB, merchantID, nodeRunID, attemptID)
	if started {
		return finalizeQuotaIgnoreMissing(s.quota().MarkUnknown(ctx, merchantID, key))
	}
	return finalizeQuotaIgnoreMissing(s.quota().Release(ctx, merchantID, key))
}

func (e Executor) finalizeImageQuotaAfterClaimedFailure(ctx context.Context, runID, nodeRunID, attemptID string) error {
	merchantID, err := merchantIDForGraphRun(ctx, e.DB, runID)
	if err != nil {
		return err
	}
	key := mustActiveImageQuotaKey(ctx, e.DB, merchantID, nodeRunID, attemptID)
	var hold schema.MerchantQuotaHolds
	err = e.DB.WithContext(ctx).
		Where("merchant_id = ? AND idempotency_key = ? AND status = ?", merchantID, key, quota.StatusReserved).
		Take(&hold).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return apperr.Internal("读取额度预留失败")
	}
	var node schema.WorkflowGraphNodeRuns
	if takeErr := e.DB.WithContext(ctx).Select("status").Where("id = ?", nodeRunID).Take(&node).Error; takeErr != nil {
		return takeErr
	}
	if node.Status == NodeRunUnknown {
		return e.markImageQuotaUnknown(ctx, merchantID, nodeRunID, attemptID)
	}
	started, err := imageProviderEffectExists(ctx, e.DB, nodeRunID, attemptID)
	if err != nil {
		return err
	}
	if started {
		return e.settleImageQuota(ctx, merchantID, nodeRunID, attemptID)
	}
	return e.releaseImageQuota(ctx, merchantID, nodeRunID, attemptID)
}
