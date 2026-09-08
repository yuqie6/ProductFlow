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
// 迟到 worker 只能收口原 attempt 的预留，禁止查询最新活跃预留替代。

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
	key := imageNodeQuotaKey(nodeRunID, attemptID)
	_, _, err := e.quota().Settle(ctx, merchantID, key, graphImageQuotaUnits)
	return err
}

func (e Executor) markImageQuotaUnknown(ctx context.Context, merchantID, nodeRunID, attemptID string) error {
	var effect schema.WorkflowGraphProviderEffects
	if err := e.DB.WithContext(ctx).Select("quota_key").Where("node_run_id = ? AND attempt_id = ?", nodeRunID, attemptID).Take(&effect).Error; err != nil {
		return err
	}
	if effect.QuotaKey == nil {
		return nil
	}
	_, _, err := e.quota().MarkUnknown(ctx, merchantID, *effect.QuotaKey)
	return err
}

func (e Executor) releaseImageQuota(ctx context.Context, merchantID, nodeRunID, attemptID string) error {
	key := imageNodeQuotaKey(nodeRunID, attemptID)
	return releaseQuotaIgnoreMissing(e.quota().Release(ctx, merchantID, key))
}

func releaseQuotaIgnoreMissing(hold quota.Hold, acct quota.Account, err error) error {
	if err == nil {
		return nil
	}
	if apperr.IsNotFound(err) {
		// 调用准备提交前或非图像调用可能没有预留，释放为空操作。
		return nil
	}
	return err
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
		return "", errors.Join(apperr.Internal("读取运行商家失败"), err)
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
	key := imageNodeQuotaKey(nodeRunID, attemptID)
	if started {
		return (Executor{DB: s.DB}).markImageQuotaUnknown(ctx, merchantID, nodeRunID, attemptID)
	}
	return releaseQuotaIgnoreMissing(s.quota().Release(ctx, merchantID, key))
}
