package auth

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

const principalContextKey = "productflow.auth.principal"

// LoadPrincipal 根据 cookie 中的 auth_session_id 装载有效用户；无效会话不中断请求。
func (h HTTP) LoadPrincipal() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := httpx.SessionString(c, sessionCookieSessionKey)
		if sessionID == "" {
			c.Next()
			return
		}
		principal, err := h.Service.LoadPrincipal(c.Request.Context(), sessionID)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		if principal != nil {
			c.Set(principalContextKey, principal)
		}
		c.Next()
	}
}

// PrincipalFrom 返回 LoadPrincipal 挂上的主体；未登录为 nil。
func PrincipalFrom(c *gin.Context) *Principal {
	if c == nil {
		return nil
	}
	value, ok := c.Get(principalContextKey)
	if !ok {
		return nil
	}
	principal, _ := value.(*Principal)
	return principal
}

// Authenticated 供 httpx.RequireAdmin 使用：有效 User 会话存在则通过。
func Authenticated(c *gin.Context) bool {
	return PrincipalFrom(c) != nil
}

// ActorFrom 将 Principal 收成写入用 UserRef。
func ActorFrom(c *gin.Context) (UserRef, bool) {
	principal := PrincipalFrom(c)
	if principal == nil {
		return UserRef{}, false
	}
	return UserRef{UserID: principal.UserID, IsOperator: principal.IsOperator}, true
}

// RequireOperator 要求已登录且 is_operator。
func RequireOperator() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := PrincipalFrom(c)
		if principal == nil {
			httpx.Unauthorized(c, "请先登录")
			return
		}
		if !principal.IsOperator {
			httpx.AbortDetail(c, http.StatusForbidden, "仅站点 Operator 可访问")
			return
		}
		c.Next()
	}
}

// RequireMembership 校验当前用户对 merchantID 有有效成员关系；请求中的商家 ID 不授予权限。
func (h HTTP) RequireMembership(merchantIDParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := PrincipalFrom(c)
		if principal == nil {
			httpx.Unauthorized(c, "请先登录")
			return
		}
		merchantID := c.Param(merchantIDParam)
		if merchantID == "" {
			httpx.AbortDetail(c, http.StatusBadRequest, "缺少商家 ID")
			return
		}
		membership, err := h.Service.ActiveMembership(c.Request.Context(), principal.UserID, merchantID)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		c.Set("productflow.auth.membership", membership)
		c.Next()
	}
}

// ContextWithPrincipal 测试辅助：把主体写入 request context（非 Gin）。
func ContextWithPrincipal(ctx context.Context, principal *Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, principal)
}
