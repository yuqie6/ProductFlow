package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"gorm.io/gorm"
)

const merchantDeclareHeader = "X-ProductFlow-Merchant-Id"

// bindConversationMerchant 以 conversation 行上的商家为内部工具权威：
// 已有工作商家或声明头与合同商家不一致时统一 404（防枚举 / 伪造 scope）。
func bindConversationMerchant(ctx context.Context, conv conversationRow) (context.Context, error) {
	merchantID := strings.TrimSpace(conv.MerchantID)
	if merchantID == "" {
		return ctx, apperr.Internal("Agent conversation 缺少商家归属")
	}
	if existing, ok := auth.MerchantIDFrom(ctx); ok && existing != merchantID {
		return ctx, auth.NotFoundCrossMerchant()
	}
	return auth.WithMerchantID(ctx, merchantID), nil
}

// requireBrowserMerchant 浏览器确认/写路径：已登录但无账号自有商家时拒绝。
func requireBrowserMerchant(ctx context.Context) error {
	if _, ok := auth.MerchantIDFrom(ctx); ok {
		return nil
	}
	return apperr.Forbidden("当前用户不属于任何商家")
}

// bindInternalConversationMerchant 内部 conversation 路由：scope 商家来自合同行，不信任声明头授权。
func (h HTTP) bindInternalConversationMerchant() gin.HandlerFunc {
	return func(c *gin.Context) {
		conversationID := strings.TrimSpace(c.Param("conversation_id"))
		if conversationID == "" {
			httpx.AbortErr(c, apperr.NotFound("Agent conversation 不存在"))
			return
		}
		var rec schema.AgentConversations
		err := h.Service.DB.WithContext(c.Request.Context()).
			Select("id", "merchant_id").
			Where("id = ?", conversationID).
			Take(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.AbortErr(c, auth.NotFoundCrossMerchant())
			return
		}
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		merchantID := strings.TrimSpace(rec.MerchantID)
		if merchantID == "" {
			httpx.AbortErr(c, apperr.Internal("Agent conversation 缺少商家归属"))
			return
		}
		declared := strings.TrimSpace(c.GetHeader(merchantDeclareHeader))
		if declared != "" && declared != merchantID {
			httpx.AbortErr(c, auth.NotFoundCrossMerchant())
			return
		}
		if existing, ok := auth.MerchantIDFrom(c.Request.Context()); ok && existing != merchantID {
			httpx.AbortErr(c, auth.NotFoundCrossMerchant())
			return
		}
		c.Request = c.Request.WithContext(auth.WithMerchantID(c.Request.Context(), merchantID))
		c.Next()
	}
}

// bindInternalTaskMerchant 内部 task contract：商家来自 task 行。
func (h HTTP) bindInternalTaskMerchant() gin.HandlerFunc {
	return func(c *gin.Context) {
		taskID := strings.TrimSpace(c.Param("task_id"))
		if taskID == "" {
			httpx.AbortErr(c, apperr.NotFound("Agent Task 不存在"))
			return
		}
		var rec schema.AgentTasks
		err := h.Service.DB.WithContext(c.Request.Context()).
			Select("id", "merchant_id").
			Where("id = ?", taskID).
			Take(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.AbortErr(c, auth.NotFoundCrossMerchant())
			return
		}
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		merchantID := strings.TrimSpace(rec.MerchantID)
		if merchantID == "" {
			httpx.AbortErr(c, apperr.Internal("Agent Task 缺少商家归属"))
			return
		}
		declared := strings.TrimSpace(c.GetHeader(merchantDeclareHeader))
		if declared != "" && declared != merchantID {
			httpx.AbortErr(c, auth.NotFoundCrossMerchant())
			return
		}
		if existing, ok := auth.MerchantIDFrom(c.Request.Context()); ok && existing != merchantID {
			httpx.AbortErr(c, auth.NotFoundCrossMerchant())
			return
		}
		c.Request = c.Request.WithContext(auth.WithMerchantID(c.Request.Context(), merchantID))
		c.Next()
	}
}
