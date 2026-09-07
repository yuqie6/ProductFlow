// Package auth 管理 User 密码会话与商家成员：POST/GET/DELETE /api/auth/session，cookie 名 session。
//
// B0 用可撤销 AuthSession 替换共享 admin 布尔登录。空实例可用 ADMIN_ACCESS_KEY 引导唯一开发商家；
// 之后禁止 admin_key 登录回退。浏览器中的商家 ID 不授予权限。
package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

// HTTP 是身份面 Gin 处理器集合。
type HTTP struct {
	AdminAccessKey string                 // 仅空实例 bootstrap；正式登录不用它
	Store          settings.RuntimeReader // Runtime.AdminAccessRequired
	DB             *gorm.DB
	Service        Service
	limiter        *loginLimiter
}

func (h *HTTP) svc() Service {
	if h.Service.DB != nil {
		return h.Service
	}
	return Service{DB: h.DB}
}

func (h *HTTP) rate() *loginLimiter {
	if h.limiter == nil {
		h.limiter = newLoginLimiter(time.Minute, 10)
	}
	return h.limiter
}

// Register 挂上会话、引导、商家与邀请路由。
func (h HTTP) Register(engine *gin.Engine) {
	svc := h.svc()
	h.Service = svc
	group := engine.Group("/api/auth")
	group.POST("/session", h.create)
	group.GET("/session", h.state)
	group.DELETE("/session", h.destroy)
	group.POST("/bootstrap", h.bootstrap)
	group.POST("/invites/accept", h.acceptInvite)

	merchants := engine.Group("/api/merchants")
	merchants.Use(func(c *gin.Context) {
		if PrincipalFrom(c) == nil {
			required, err := h.accessRequired(c)
			if err != nil {
				httpx.AbortErr(c, err)
				return
			}
			if required {
				httpx.Unauthorized(c, "请先登录")
				return
			}
		}
		c.Next()
	})
	merchants.GET("", h.listMerchants)
	merchants.POST("", h.createMerchant)
	merchants.PATCH("/:merchant_id/status", h.setMerchantStatus)
	merchants.POST("/:merchant_id/invites", h.createInvite)
	merchants.DELETE("/:merchant_id/invites/:invite_id", h.revokeInvite)
	merchants.DELETE("/:merchant_id/memberships/:user_id", h.revokeMembership)
	merchants.POST("/:merchant_id/memberships/:user_id/restore", h.restoreMembership)

	h.registerSupportOps(engine)
}

func (h HTTP) accessRequired(c *gin.Context) (bool, error) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		return false, apperr.Internal("读取运行时设置失败")
	}
	return runtime.AdminAccessRequired, nil
}

type sessionCreateRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type bootstrapRequest struct {
	AdminKey     string `json:"admin_key"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	MerchantName string `json:"merchant_name"`
}

type acceptInviteRequest struct {
	Token       string `json:"token"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type merchantCreateRequest struct {
	Name string `json:"name"`
}

type inviteCreateRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h HTTP) create(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	if !runtime.AdminAccessRequired {
		// 未开启门禁时不发 User 会话，也不回退 admin 布尔 cookie。
		c.JSON(http.StatusOK, gin.H{"ok": true, "access_required": false})
		return
	}
	key := clientKey(c.Request)
	now := time.Now().UTC()
	if !h.rate().allow(key, now) {
		httpx.WriteDetail(c, http.StatusTooManyRequests, "登录尝试过于频繁，请稍后再试")
		return
	}
	var payload sessionCreateRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	principal, sessionID, err := h.svc().Login(c.Request.Context(), payload.Email, payload.Password)
	if err != nil {
		h.rate().fail(key, now)
		httpx.AbortErr(c, err)
		return
	}
	if err := writeAuthSession(c, principal.UserID, sessionID); err != nil {
		httpx.AbortErr(c, apperr.Internal("写入会话失败"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h HTTP) bootstrap(c *gin.Context) {
	key := clientKey(c.Request)
	now := time.Now().UTC()
	if !h.rate().allow(key, now) {
		httpx.WriteDetail(c, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
		return
	}
	var payload bootstrapRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	principal, sessionID, err := h.svc().Bootstrap(
		c.Request.Context(),
		payload.AdminKey,
		h.AdminAccessKey,
		payload.Email,
		payload.Password,
		payload.DisplayName,
		payload.MerchantName,
	)
	if err != nil {
		h.rate().fail(key, now)
		httpx.AbortErr(c, err)
		return
	}
	if err := writeAuthSession(c, principal.UserID, sessionID); err != nil {
		httpx.AbortErr(c, apperr.Internal("写入会话失败"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"user_id":     principal.UserID,
		"merchant_id": firstMerchantID(c, h, principal.UserID),
	})
}

func firstMerchantID(c *gin.Context, h HTTP, userID string) string {
	rows, err := h.svc().ListMemberships(c.Request.Context(), userID)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return rows[0].MerchantID
}

func (h HTTP) state(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	needsBootstrap := false
	if runtime.AdminAccessRequired && h.DB != nil {
		n, err := h.svc().UserCount(c.Request.Context())
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		needsBootstrap = n == 0
	}
	principal := PrincipalFrom(c)
	authenticated := !runtime.AdminAccessRequired || principal != nil
	body := gin.H{
		"authenticated":   authenticated,
		"access_required": runtime.AdminAccessRequired,
		"needs_bootstrap": needsBootstrap,
	}
	if principal != nil {
		body["user"] = gin.H{
			"id":           principal.UserID,
			"email":        principal.Email,
			"display_name": principal.DisplayName,
			"is_operator":  principal.IsOperator,
		}
		memberships, err := h.svc().ListMemberships(c.Request.Context(), principal.UserID)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		body["memberships"] = memberships
	}
	c.JSON(http.StatusOK, body)
}

func (h HTTP) destroy(c *gin.Context) {
	sessionID := httpx.SessionString(c, sessionCookieSessionKey)
	_ = h.svc().RevokeSession(c.Request.Context(), sessionID)
	_ = httpx.ClearSession(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h HTTP) acceptInvite(c *gin.Context) {
	var payload acceptInviteRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	principal, sessionID, err := h.svc().AcceptInvite(c.Request.Context(), payload.Token, payload.Password, payload.DisplayName)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if err := writeAuthSession(c, principal.UserID, sessionID); err != nil {
		httpx.AbortErr(c, apperr.Internal("写入会话失败"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "user_id": principal.UserID})
}

func (h HTTP) listMerchants(c *gin.Context) {
	principal := PrincipalFrom(c)
	if principal == nil {
		c.JSON(http.StatusOK, gin.H{"items": []MembershipView{}})
		return
	}
	items, err := h.svc().ListMemberships(c.Request.Context(), principal.UserID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h HTTP) createMerchant(c *gin.Context) {
	actor, ok := ActorFrom(c)
	if !ok {
		httpx.Unauthorized(c, "请先登录")
		return
	}
	var payload merchantCreateRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	merchant, err := h.svc().CreateMerchant(c.Request.Context(), actor, payload.Name)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": merchant.ID, "name": merchant.Name, "status": merchant.Status})
}

type merchantStatusRequest struct {
	Status string `json:"status"`
}

func (h HTTP) setMerchantStatus(c *gin.Context) {
	actor, ok := ActorFrom(c)
	if !ok {
		httpx.Unauthorized(c, "请先登录")
		return
	}
	var payload merchantStatusRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	merchant, err := h.svc().SetMerchantStatus(c.Request.Context(), actor, c.Param("merchant_id"), payload.Status)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": merchant.ID, "name": merchant.Name, "status": merchant.Status})
}

func (h HTTP) createInvite(c *gin.Context) {
	actor, ok := ActorFrom(c)
	if !ok {
		httpx.Unauthorized(c, "请先登录")
		return
	}
	var payload inviteCreateRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	result, err := h.svc().CreateInvite(c.Request.Context(), actor, c.Param("merchant_id"), payload.Email, payload.Role)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h HTTP) revokeInvite(c *gin.Context) {
	actor, ok := ActorFrom(c)
	if !ok {
		httpx.Unauthorized(c, "请先登录")
		return
	}
	if err := h.svc().RevokeInvite(c.Request.Context(), actor, c.Param("merchant_id"), c.Param("invite_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h HTTP) revokeMembership(c *gin.Context) {
	actor, ok := ActorFrom(c)
	if !ok {
		httpx.Unauthorized(c, "请先登录")
		return
	}
	if err := h.svc().RevokeMembership(c.Request.Context(), actor, c.Param("merchant_id"), c.Param("user_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h HTTP) restoreMembership(c *gin.Context) {
	actor, ok := ActorFrom(c)
	if !ok {
		httpx.Unauthorized(c, "请先登录")
		return
	}
	if err := h.svc().RestoreMembership(c.Request.Context(), actor, c.Param("merchant_id"), c.Param("user_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func writeAuthSession(c *gin.Context, userID, sessionID string) error {
	return httpx.ReplaceSession(c, map[any]any{
		sessionCookieUserKey:    userID,
		sessionCookieSessionKey: sessionID,
	})
}

func bindJSONStrict(c *gin.Context, dest any) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return io.ErrUnexpectedEOF
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return io.ErrUnexpectedEOF
	}
	return nil
}
