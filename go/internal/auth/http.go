// Package auth 管理单管理员 session：POST/GET/DELETE /api/auth/session，cookie 名 session。
//
// 登录用 ADMIN_ACCESS_KEY 恒定时间比较，不要改成普通 ==。没有多用户表。
// RequireAdmin 是否强制看 settings.Runtime.AdminAccessRequired（DB 可覆盖 env）。
// 错误一律 {"detail"}；未登录 401。不要在这里发 JWT。
package auth

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 是单管理员登录的 Gin 处理器集合，不是用户表。
// 只挂 /api/auth/session；cookie 名必须是 session。不要在这里发 JWT 或多租户账号。
type HTTP struct {
	AdminAccessKey string                 // env ADMIN_ACCESS_KEY；登录时恒定时间比较
	Store          settings.RuntimeReader // Runtime.AdminAccessRequired 决定是否强制登录
}

type sessionCreateRequest struct {
	AdminKey string `json:"admin_key"`
}

// Register 挂上 POST/GET/DELETE /api/auth/session。
func (h HTTP) Register(engine *gin.Engine) {
	group := engine.Group("/api/auth")
	group.POST("/session", h.create)
	group.GET("/session", h.state)
	group.DELETE("/session", h.destroy)
}

// create 是 POST /api/auth/session：200 返回 {"ok":true} 并写 session cookie。
func (h HTTP) create(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	if !runtime.AdminAccessRequired {
		_ = httpx.SetSessionValue(c, "is_authenticated", true)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	var payload sessionCreateRequest
	body, _ := io.ReadAll(c.Request.Body)
	if len(bytes.TrimSpace(body)) == 0 {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if dec.More() {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if !secretEqual(payload.AdminKey, h.AdminAccessKey) {
		httpx.WriteDetail(c, http.StatusUnauthorized, "管理员密钥不正确")
		return
	}
	_ = httpx.ReplaceSession(c, map[any]any{"is_authenticated": true})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// state 是 GET /api/auth/session：200 返回 authenticated 与 access_required。
func (h HTTP) state(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"authenticated":   !runtime.AdminAccessRequired || httpx.SessionBool(c, "is_authenticated"),
		"access_required": runtime.AdminAccessRequired,
	})
}

// destroy 是 DELETE /api/auth/session：200 返回 {"ok":true} 并清 cookie。
func (h HTTP) destroy(c *gin.Context) {
	_ = httpx.ClearSession(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func secretEqual(provided, expected string) bool {
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
