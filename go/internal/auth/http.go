// Package auth 管理 User 密码会话与直接商家归属：POST/GET/DELETE /api/auth/session，cookie 名 session。
//
// 登录使用可撤销 AuthSession。空实例可用 ADMIN_ACCESS_KEY 引导管理员及其开发商家；
// 之后禁止 admin_key 登录回退。浏览器中的商家 ID 不授予权限。
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	Mailer         Mailer                 // SMTP-backed public registration
	DB             *gorm.DB
	Service        Service
	// AttemptLimiter is shared by all API instances. Production injects the
	// Redis implementation; a missing limiter fails closed with 503.
	AttemptLimiter    AttemptLimiter
	TrustedProxyCIDRs []string
	// EnsureMerchantQuota 在商家创建或重新激活后建试用额度账；由主进程注入，避免 auth↔quota 循环导入。
	EnsureMerchantQuota func(ctx context.Context, merchantID string) error
}

func (h *HTTP) svc() Service {
	svc := h.Service
	if svc.DB == nil {
		svc.DB = h.DB
	}
	return svc
}

// Mailer is the narrow settings boundary required by public registration and
// password recovery. It deliberately keeps SMTP configuration and transport out of auth.
type Mailer interface {
	RegistrationAvailable(context.Context) (bool, error)
	SendVerificationCode(context.Context, string, string) error
	SendPasswordResetCode(context.Context, string, string) error
}

// Register 挂上会话、引导、公开注册与 Operator 商家路由。
func (h HTTP) Register(engine *gin.Engine) {
	svc := h.svc()
	h.Service = svc
	group := engine.Group("/api/auth")
	group.POST("/session", h.create)
	group.GET("/session", h.state)
	group.DELETE("/session", h.destroy)
	group.POST("/bootstrap", h.bootstrap)
	group.POST("/registration-code", h.registrationCode)
	group.POST("/register", h.register)
	group.POST("/password-recovery/request", h.passwordRecoveryRequest)
	group.POST("/password-recovery/confirm", h.passwordRecoveryConfirm)

	account := engine.Group("/api/account")
	account.GET("", h.account)
	account.PATCH("", h.updateAccount)
	account.PATCH("/preferences", h.updatePreferences)
	account.PATCH("/merchant", h.updateMerchant)
	account.POST("/password", h.changePassword)
	account.GET("/sessions", h.listAccountSessions)
	account.DELETE("/sessions/:id", h.revokeAccountSession)

	ops := engine.Group("/api/ops")
	ops.Use(RequireOperator())
	ops.GET("/merchants", h.listMerchants)
	ops.GET("/merchants/:merchant_id", h.getMerchant)
	ops.PATCH("/merchants/:merchant_id/status", h.RequireOperatorMerchantTarget("merchant_id"), h.setMerchantStatus)
}

func (h HTTP) ensureMerchantQuota(ctx context.Context, merchantID string) error {
	if h.EnsureMerchantQuota == nil {
		return nil
	}
	return h.EnsureMerchantQuota(ctx, merchantID)
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

type registrationCodeRequest struct {
	Email string `json:"email"`
}

type registerRequest struct {
	Email        string `json:"email"`
	ChallengeID  string `json:"challenge_id"`
	Code         string `json:"code"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	MerchantName string `json:"merchant_name"`
}

func (h HTTP) listMerchants(c *gin.Context) {
	page, err := parseMerchantListInt(c, "page", 1, 1, merchantListMaxPage)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	pageSize, err := parseMerchantListInt(c, "page_size", merchantListDefaultPageSize, 1, merchantListMaxPageSize)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	pageResult, err := h.svc().ListMerchants(c.Request.Context(), page, pageSize, c.Query("q"), c.Query("status"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, pageResult)
}

func (h HTTP) getMerchant(c *gin.Context) {
	merchant, err := h.svc().GetMerchant(c.Request.Context(), c.Param("merchant_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, merchant)
}

func parseMerchantListInt(c *gin.Context, key string, def, min, max int) (int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return def, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, apperr.Validation("商家列表参数无效")
	}
	return value, nil
}

func (h HTTP) create(c *gin.Context) {
	var payload sessionCreateRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if !h.admitCredential(c, strings.ToLower(strings.TrimSpace(payload.Email))) {
		return
	}
	principal, sessionID, err := h.svc().Login(c.Request.Context(), payload.Email, payload.Password)
	if err != nil {
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
	var payload bootstrapRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if !h.admitCredential(c, strings.ToLower(strings.TrimSpace(payload.Email))) {
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
		httpx.AbortErr(c, err)
		return
	}
	if err := writeAuthSession(c, principal.UserID, sessionID); err != nil {
		httpx.AbortErr(c, apperr.Internal("写入会话失败"))
		return
	}
	if principal.MerchantID != nil {
		if err := h.ensureMerchantQuota(c.Request.Context(), *principal.MerchantID); err != nil {
			httpx.AbortErr(c, err)
			return
		}
	}
	merchantID := ""
	if principal.MerchantID != nil {
		merchantID = *principal.MerchantID
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"user_id":     principal.UserID,
		"merchant_id": merchantID,
	})
}

func (h HTTP) state(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	needsBootstrap := false
	if h.DB != nil {
		n, err := h.svc().UserCount(c.Request.Context())
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		needsBootstrap = n == 0
	}
	principal := PrincipalFrom(c)
	authenticated := principal != nil
	body := gin.H{
		"authenticated":   authenticated,
		"access_required": runtime.AdminAccessRequired,
		"needs_bootstrap": needsBootstrap,
	}
	registrationAvailable, err := h.registrationAvailable(c.Request.Context())
	if err != nil {
		// Registration readiness is an optional capability projection. A
		// settings/SMTP read failure must not invalidate an existing session.
		registrationAvailable = false
	}
	body["registration_available"] = registrationAvailable
	if principal != nil {
		body["user"] = gin.H{
			"id":           principal.UserID,
			"email":        principal.Email,
			"display_name": principal.DisplayName,
			"is_operator":  principal.IsOperator,
		}
		merchant, err := h.svc().OwnMerchant(c.Request.Context(), principal.UserID)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		body["merchant"] = merchant
		body["preferences"] = AccountPreferences{Locale: principal.Locale, Theme: principal.Theme}
	}
	c.JSON(http.StatusOK, body)
}

func (h HTTP) registrationAvailable(ctx context.Context) (bool, error) {
	if h.Mailer == nil {
		return false, nil
	}
	available, err := h.Mailer.RegistrationAvailable(ctx)
	if err != nil || !available {
		return false, err
	}
	svc := h.svc()
	if svc.DB == nil {
		return false, nil
	}
	users, err := svc.UserCount(ctx)
	if err != nil {
		return false, err
	}
	return users > 0, nil
}

func (h HTTP) registrationCode(c *gin.Context) {
	if PrincipalFrom(c) != nil {
		httpx.AbortDetail(c, http.StatusConflict, "当前已登录，请先退出当前账号")
		return
	}
	available, err := h.registrationAvailable(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "注册服务暂时不可用，请稍后再试")
		return
	}
	if !available {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "注册服务暂未开放")
		return
	}
	var payload registrationCodeRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	email, err := normalizeEmail(payload.Email)
	if err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, err.Error())
		return
	}
	if !h.admitCredential(c, email) {
		return
	}
	issue, err := h.svc().CreateRegistrationChallenge(c.Request.Context(), email)
	if err != nil {
		var retry registrationRetryError
		if errors.As(err, &retry) {
			writeRegistrationRetry(c, retry.RetryAfter)
			return
		}
		httpx.AbortErr(c, err)
		return
	}
	if err := h.Mailer.SendVerificationCode(c.Request.Context(), email, issue.Code); err != nil {
		_ = h.svc().InvalidateRegistrationChallenge(c.Request.Context(), issue.ID)
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "验证码发送失败，请稍后再试")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"challenge_id":        issue.ID,
		"retry_after_seconds": int(registrationResendInterval / time.Second),
	})
}

func (h HTTP) register(c *gin.Context) {
	if PrincipalFrom(c) != nil {
		httpx.AbortDetail(c, http.StatusConflict, "当前已登录，请先退出当前账号")
		return
	}
	available, err := h.registrationAvailable(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "注册服务暂时不可用，请稍后再试")
		return
	}
	if !available {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "注册服务暂未开放")
		return
	}
	var payload registerRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	email, err := normalizeEmail(payload.Email)
	if err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, err.Error())
		return
	}
	if !h.admitCredential(c, email) {
		return
	}
	principal, sessionID, merchantID, err := h.svc().Register(
		c.Request.Context(), email, payload.ChallengeID, payload.Code, payload.Password,
		payload.DisplayName, payload.MerchantName, "",
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if err := writeAuthSession(c, principal.UserID, sessionID); err != nil {
		httpx.AbortErr(c, apperr.Internal("写入会话失败"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "user_id": principal.UserID, "merchant_id": merchantID})
}

func writeRegistrationRetry(c *gin.Context, after time.Duration) {
	seconds := int((after + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
	httpx.WriteDetail(c, http.StatusTooManyRequests, "验证码发送过于频繁，请稍后再试")
}

func (h HTTP) destroy(c *gin.Context) {
	sessionID := httpx.SessionString(c, sessionCookieSessionKey)
	if err := h.svc().RevokeSession(c.Request.Context(), sessionID); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	_ = httpx.ClearSession(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
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
	if merchant.Status == MerchantStatusActive {
		if err := h.ensureMerchantQuota(c.Request.Context(), merchant.ID); err != nil {
			httpx.AbortErr(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": merchant.ID, "name": merchant.Name, "status": merchant.Status})
}

func writeAuthSession(c *gin.Context, userID, sessionID string) error {
	return httpx.ReplaceSession(c, map[any]any{
		sessionCookieUserKey:    userID,
		sessionCookieSessionKey: sessionID,
	})
}

func (h HTTP) admitCredential(c *gin.Context, subject string) bool {
	if h.AttemptLimiter == nil {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "认证限流服务暂时不可用，请稍后再试")
		return false
	}
	decision, err := h.AttemptLimiter.Allow(c.Request.Context(), ClientIP(c.Request, h.TrustedProxyCIDRs), subject)
	if err != nil {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "认证限流服务暂时不可用，请稍后再试")
		return false
	}
	if decision.Allowed {
		return true
	}
	seconds := int((decision.RetryAfter + time.Second - 1) / time.Second)
	c.Header("Retry-After", strconv.Itoa(max(1, seconds)))
	httpx.WriteDetail(c, http.StatusTooManyRequests, "尝试过于频繁，请稍后再试")
	return false
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
