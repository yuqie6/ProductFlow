package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

type accountUpdateRequest struct {
	DisplayName string `json:"display_name"`
}

type accountPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type passwordRecoveryRequest struct {
	Email string `json:"email"`
}

type passwordRecoveryConfirmRequest struct {
	Email       string `json:"email"`
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

const passwordRecoveryCleanupTimeout = 5 * time.Second

func accountPrincipal(c *gin.Context) (*Principal, bool) {
	principal := PrincipalFrom(c)
	if principal == nil {
		httpx.Unauthorized(c, "请先登录")
		return nil, false
	}
	return principal, true
}

func (h HTTP) account(c *gin.Context) {
	principal, ok := accountPrincipal(c)
	if !ok {
		return
	}
	view, err := h.svc().Account(c.Request.Context(), principal.UserID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h HTTP) updateAccount(c *gin.Context) {
	principal, ok := accountPrincipal(c)
	if !ok {
		return
	}
	var payload accountUpdateRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	view, err := h.svc().UpdateDisplayName(c.Request.Context(), principal.UserID, payload.DisplayName)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h HTTP) changePassword(c *gin.Context) {
	principal, ok := accountPrincipal(c)
	if !ok {
		return
	}
	var payload accountPasswordRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if !h.admitCredential(c, principal.Email) {
		return
	}
	if err := h.svc().ChangePassword(c.Request.Context(), principal.UserID, payload.CurrentPassword, payload.NewPassword); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	// ChangePassword revokes this session in the same transaction. Clear the
	// browser cookie only after the database commit has succeeded.
	_ = httpx.ClearSession(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func parseAccountSessionLimit(c *gin.Context) (int, error) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return accountSessionDefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > accountSessionMaxLimit {
		return 0, apperr.Validation("会话列表 limit 必须在 1 到 100 之间")
	}
	return limit, nil
}

func (h HTTP) listAccountSessions(c *gin.Context) {
	principal, ok := accountPrincipal(c)
	if !ok {
		return
	}
	limit, err := parseAccountSessionLimit(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	page, err := h.svc().ListAccountSessions(c.Request.Context(), principal.UserID, principal.SessionID, c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h HTTP) revokeAccountSession(c *gin.Context) {
	principal, ok := accountPrincipal(c)
	if !ok {
		return
	}
	sessionID := c.Param("id")
	if err := h.svc().RevokeOwnSession(c.Request.Context(), principal.UserID, sessionID); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if strings.TrimSpace(sessionID) == principal.SessionID {
		_ = httpx.ClearSession(c)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h HTTP) passwordRecoveryAvailable(ctx context.Context) (bool, error) {
	if h.Mailer == nil {
		return false, nil
	}
	return h.Mailer.RegistrationAvailable(ctx)
}

func (h HTTP) passwordRecoveryRequest(c *gin.Context) {
	var payload passwordRecoveryRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	email, err := normalizeEmail(payload.Email)
	if err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, err.Error())
		return
	}
	// Check global SMTP readiness before looking up the account. This gives an
	// honest 503 when recovery cannot operate without exposing account state.
	available, err := h.passwordRecoveryAvailable(c.Request.Context())
	if err != nil || !available {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "密码恢复服务暂时不可用，请稍后再试")
		return
	}
	if !h.admitCredential(c, email) {
		return
	}
	issue, err := h.svc().CreatePasswordRecoveryChallenge(c.Request.Context(), email)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if issue.ShouldSend {
		if err := h.Mailer.SendPasswordResetCode(c.Request.Context(), email, issue.Code); err != nil {
			// Do not leave a credential usable after a failed SMTP attempt. The
			// response remains the same 202 shape as unknown/disabled accounts.
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), passwordRecoveryCleanupTimeout)
			invalidateErr := h.svc().InvalidatePasswordRecoveryChallenge(cleanupCtx, issue.ID, issue.CodeHash)
			cleanupCancel()
			if invalidateErr != nil {
				httpx.AbortErr(c, invalidateErr)
				return
			}
			_ = c.Error(errors.New("password recovery SMTP delivery failed"))
			issue.Code = ""
			issue.ShouldSend = false
		}
	}
	c.JSON(http.StatusAccepted, gin.H{
		"challenge_id":         issue.ID,
		"expires_in_seconds":   int(passwordRecoveryChallengeTTL / time.Second),
		"resend_after_seconds": int(passwordRecoveryResendInterval / time.Second),
	})
}

func (h HTTP) passwordRecoveryConfirm(c *gin.Context) {
	var payload passwordRecoveryConfirmRequest
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
	if err := h.svc().ConfirmPasswordRecovery(c.Request.Context(), email, payload.ChallengeID, payload.Code, payload.NewPassword); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	_ = httpx.ClearSession(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
