package queue

import (
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// resolveStageMerchant 合并 context 声明与已有快照：已有商家不可改写；空快照可被 context 填入。
func resolveStageMerchant(ctx context.Context, existing *Dispatch) (string, error) {
	requested := strings.TrimSpace(auth.ResolveMerchantID(ctx))
	if existing != nil {
		frozen := strings.TrimSpace(existing.MerchantID)
		if frozen != "" {
			if requested != "" && requested != frozen {
				return "", apperr.Conflict("同一 delivery key 不能改写受理商家快照")
			}
			return frozen, nil
		}
	}
	return requested, nil
}
