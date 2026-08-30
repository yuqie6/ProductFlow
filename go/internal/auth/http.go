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

type HTTP struct {
	AdminAccessKey string
	Store          settings.RuntimeReader
}

type sessionCreateRequest struct {
	AdminKey string `json:"admin_key"`
}

func (h HTTP) Register(engine *gin.Engine) {
	group := engine.Group("/api/auth")
	group.POST("/session", h.create)
	group.GET("/session", h.state)
	group.DELETE("/session", h.destroy)
}

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
