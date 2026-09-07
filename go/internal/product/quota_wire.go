package product

import (
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/quota"
)

// MP-C：source-note 生成在 prompt provider 调用前 Reserve；成功 Settle、未发出 Release、已发出不明 MarkUnknown。
// 单价占位 1 内部单位；幂等键绑定该次请求 identity（每次 generate 新开 requestID）。

const sourceNoteQuotaUnits int64 = 1

func (s Service) quota() *quota.Service {
	return &quota.Service{DB: s.DB}
}

func sourceNoteQuotaKey(requestID string) string {
	return "product-source-note:" + strings.TrimSpace(requestID)
}

func (s Service) reserveSourceNoteQuota(ctx context.Context, merchantID, requestID string) error {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return apperr.Internal("缺少商家额度归属")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return apperr.Internal("缺少 source-note 请求标识")
	}
	_, _, err := s.quota().Reserve(ctx, merchantID, sourceNoteQuotaKey(requestID), sourceNoteQuotaUnits, quota.DefaultPriceVersionID)
	return err
}

func (s Service) settleSourceNoteQuota(ctx context.Context, merchantID, requestID string) error {
	return finalizeQuotaIgnoreMissing(s.quota().Settle(ctx, merchantID, sourceNoteQuotaKey(requestID), sourceNoteQuotaUnits))
}

func (s Service) releaseSourceNoteQuota(ctx context.Context, merchantID, requestID string) error {
	return finalizeQuotaIgnoreMissing(s.quota().Release(ctx, merchantID, sourceNoteQuotaKey(requestID)))
}

func (s Service) markSourceNoteQuotaUnknown(ctx context.Context, merchantID, requestID string) error {
	return finalizeQuotaIgnoreMissing(s.quota().MarkUnknown(ctx, merchantID, sourceNoteQuotaKey(requestID)))
}

func finalizeQuotaIgnoreMissing(hold quota.Hold, acct quota.Account, err error) error {
	if err == nil {
		return nil
	}
	if apperr.IsNotFound(err) {
		// 夹具或跳过路径未 Reserve；不要把缺 hold 升级成失败。
		return nil
	}
	return err
}
