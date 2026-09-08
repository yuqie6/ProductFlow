package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"gorm.io/gorm"
)

// 跨商家根对象统一对外 404，防枚举（B1 钉死；全站一致，不用 403）。
const CrossMerchantDetail = "资源不存在"

type merchantContextKey struct{}

const ginMerchantKey = "productflow.auth.merchant_id"

// WithMerchantID 把当前工作商家写入 context，供 store 强制归属过滤。
func WithMerchantID(ctx context.Context, merchantID string) context.Context {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return ctx
	}
	return context.WithValue(ctx, merchantContextKey{}, merchantID)
}

// MerchantIDFrom 读取 context 中的工作商家；未设置返回 false。
func MerchantIDFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(merchantContextKey{}).(string)
	if !ok || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}

// RequireMerchantID 读写根对象时要求已有商家 context。
// 查询过滤请用 ScopeMerchant / MerchantIDFrom。
func RequireMerchantID(ctx context.Context) (string, error) {
	id, ok := MerchantIDFrom(ctx)
	if !ok {
		return "", apperr.Internal("缺少商家上下文")
	}
	return id, nil
}

// ResolveMerchantID 读取已有商家 context；空串表示调用方尚未建立商家作用域。
func ResolveMerchantID(ctx context.Context) string {
	id, _ := MerchantIDFrom(ctx)
	return id
}

// MerchantIDFromGin 读取 AttachWorkingMerchant 挂上的商家。
func MerchantIDFromGin(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	v, ok := c.Get(ginMerchantKey)
	if !ok {
		return "", false
	}
	id, _ := v.(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	return id, true
}

// AttachWorkingMerchant 从已认证 Principal 的直接归属写入 Gin + Request context。
// Operator 的 home merchant 可为空；它只用于普通商家入口，不改变 Operator 的跨商管理权限。
func (h HTTP) AttachWorkingMerchant() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := PrincipalFrom(c)
		if principal != nil && principal.MerchantID != nil && strings.TrimSpace(*principal.MerchantID) != "" {
			merchantID := strings.TrimSpace(*principal.MerchantID)
			c.Set(ginMerchantKey, merchantID)
			c.Request = c.Request.WithContext(WithMerchantID(c.Request.Context(), merchantID))
		}
		c.Next()
	}
}

// ScopeMerchant 在已有工作商家时追加 merchant_id 过滤；无商家上下文时不加。
// 交互式业务路由必须先经过 RequireWorkingMerchant 或显式 target middleware；
// worker/recovery 可用 WithMerchantID 固定持久化快照后继续按主键读取。
func ScopeMerchant(ctx context.Context, q *gorm.DB, column string) *gorm.DB {
	if column == "" {
		column = "merchant_id"
	}
	if id, ok := MerchantIDFrom(ctx); ok {
		return q.Where(column+" = ?", id)
	}
	return q
}

// NotFoundCrossMerchant 统一跨商/缺失根对象错误（404）。
func NotFoundCrossMerchant() error {
	return apperr.NotFound(CrossMerchantDetail)
}

// AbortIfNoMerchant 在需商家上下文的业务路由上拒绝匿名或无直接归属请求。
func AbortIfNoMerchant(c *gin.Context) bool {
	if _, ok := MerchantIDFromGin(c); ok {
		return false
	}
	if PrincipalFrom(c) == nil {
		httpx.Unauthorized(c, "请先登录")
		return true
	}
	httpx.AbortDetail(c, http.StatusForbidden, "当前用户不属于任何商家")
	return true
}

// RequireWorkingMerchant 浏览器业务路由中间件：必须已登录且有直接商家归属。
func RequireWorkingMerchant() gin.HandlerFunc {
	return func(c *gin.Context) {
		if AbortIfNoMerchant(c) {
			return
		}
		c.Next()
	}
}

// RejectSuspendedMerchantWrites 停用商家后拒绝非 Operator 的业务写请求。
// 读路径与 /api/auth、/api/settings、/api/ops、generation-queue 放行；Operator 可继续写（含启停与支持）。
func (h HTTP) RejectSuspendedMerchantWrites() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}
		if skipSuspendedWriteGate(c.Request.URL.Path) {
			c.Next()
			return
		}
		principal := PrincipalFrom(c)
		if principal != nil && principal.IsOperator {
			c.Next()
			return
		}
		merchantID, ok := MerchantIDFromGin(c)
		if !ok {
			c.Next()
			return
		}
		status, err := h.svc().MerchantStatus(c.Request.Context(), merchantID)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		if status == MerchantStatusSuspended {
			httpx.AbortDetail(c, http.StatusForbidden, "商家已停用，无法写入")
			return
		}
		c.Next()
	}
}

func skipSuspendedWriteGate(path string) bool {
	switch {
	case strings.HasPrefix(path, "/api/auth/"):
		return true
	case strings.HasPrefix(path, "/api/settings"):
		return true
	// Personal account security stays available when business writes are suspended.
	// The own-merchant command checks merchant status again under its row lock.
	case path == "/api/account" || strings.HasPrefix(path, "/api/account/"):
		return true
	case path == "/api/generation-queue":
		return true
	case strings.HasPrefix(path, "/api/ops/"):
		return true
	default:
		return false
	}
}
