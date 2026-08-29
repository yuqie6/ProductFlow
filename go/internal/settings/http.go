package settings

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

type LockState struct {
	Unlocked   bool `json:"unlocked"`
	Configured bool `json:"configured"`
}

type HTTP struct {
	Store               RuntimeReader
	DB                  *Store
	SettingsAccessToken string
}

func (h HTTP) Register(engine *gin.Engine) {
	group := engine.Group("/api/settings")
	group.Use(httpx.RequireAdmin(func(c *gin.Context) (bool, error) {
		runtime, err := h.Store.Runtime(c.Request.Context())
		if err != nil {
			return false, err
		}
		return runtime.AdminAccessRequired, nil
	}))
	group.GET("/lock-state", h.lockState)
	group.POST("/unlock", h.unlock)
	group.GET("/runtime", h.runtime)
	group.GET("/provider-config", h.requireUnlocked, h.providerConfig)
	group.GET("/export", h.requireUnlocked, h.export)
	group.POST("/import/preview", h.requireUnlocked, h.importPreview)
	group.POST("/import", h.requireUnlocked, h.importCommit)
	group.POST("/provider-profiles", h.requireUnlocked, h.createProfile)
	group.PATCH("/provider-profiles/:profile_id", h.requireUnlocked, h.updateProfile)
	group.DELETE("/provider-profiles/:profile_id", h.requireUnlocked, h.archiveProfile)
	group.PATCH("/provider-bindings/:purpose", h.requireUnlocked, h.updateBinding)
	group.GET("", h.requireUnlocked, h.getConfig)
	group.PATCH("", h.requireUnlocked, h.patchConfig)

	engine.GET("/api/generation-queue", httpx.RequireAdmin(func(c *gin.Context) (bool, error) {
		runtime, err := h.Store.Runtime(c.Request.Context())
		if err != nil {
			return false, err
		}
		return runtime.AdminAccessRequired, nil
	}), h.generationQueue)
}

func (h HTTP) requireUnlocked(c *gin.Context) {
	if strings.TrimSpace(h.SettingsAccessToken) == "" {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "设置解锁令牌未配置，请联系管理员")
		return
	}
	if !httpx.SessionBool(c, "settings_unlocked") {
		httpx.AbortDetail(c, http.StatusForbidden, "请先解锁系统配置")
		return
	}
}

func (h HTTP) lockState(c *gin.Context) {
	configured := strings.TrimSpace(h.SettingsAccessToken) != ""
	c.JSON(http.StatusOK, LockState{
		Unlocked:   configured && httpx.SessionBool(c, "settings_unlocked"),
		Configured: configured,
	})
}

func (h HTTP) unlock(c *gin.Context) {
	expected := strings.TrimSpace(h.SettingsAccessToken)
	if expected == "" {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "设置解锁令牌未配置，请联系管理员")
		return
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if payload.Token == "" {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	if !secretEqual(payload.Token, expected) {
		httpx.AbortDetail(c, http.StatusUnauthorized, "设置解锁令牌不正确")
		return
	}
	_ = httpx.SetSessionValue(c, "settings_unlocked", true)
	c.JSON(http.StatusOK, LockState{Unlocked: true, Configured: true})
}

func (h HTTP) runtime(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	c.JSON(http.StatusOK, runtime)
}

func (h HTTP) getConfig(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	view, err := h.DB.ConfigView(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h HTTP) patchConfig(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	var payload struct {
		Values    map[string]any `json:"values"`
		ResetKeys []string       `json:"reset_keys"`
	}
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if payload.Values == nil {
		payload.Values = map[string]any{}
	}
	view, err := h.DB.UpdateConfig(c.Request.Context(), payload.Values, payload.ResetKeys)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h HTTP) providerConfig(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	view, err := h.DB.ProviderConfig(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h HTTP) createProfile(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	var payload struct {
		Name          string         `json:"name"`
		ProviderType  string         `json:"provider_type"`
		BaseURL       *string        `json:"base_url"`
		APIKey        *string        `json:"api_key"`
		Capabilities  []string       `json:"capabilities"`
		DefaultModels map[string]any `json:"default_models"`
		Config        map[string]any `json:"config"`
		Enabled       *bool          `json:"enabled"`
	}
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if payload.ProviderType == "" {
		payload.ProviderType = "openai_compatible"
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	profile, err := h.DB.CreateProfile(c.Request.Context(), payload.Name, payload.ProviderType, payload.BaseURL, payload.APIKey, payload.Capabilities, payload.DefaultModels, payload.Config, enabled)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h HTTP) updateProfile(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	fields := map[string]json.RawMessage{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fields); err != nil || dec.More() {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	profile, err := h.DB.UpdateProfile(c.Request.Context(), c.Param("profile_id"), fields)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h HTTP) archiveProfile(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	profile, err := h.DB.ArchiveProfile(c.Request.Context(), c.Param("profile_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h HTTP) updateBinding(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	var payload struct {
		ProviderKind      string         `json:"provider_kind"`
		ProviderProfileID *string        `json:"provider_profile_id"`
		ModelSettings     map[string]any `json:"model_settings"`
		Config            map[string]any `json:"config"`
	}
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if payload.ModelSettings == nil {
		payload.ModelSettings = map[string]any{}
	}
	if payload.Config == nil {
		payload.Config = map[string]any{}
	}
	binding, err := h.DB.UpdateBinding(c.Request.Context(), c.Param("purpose"), payload.ProviderKind, payload.ProviderProfileID, payload.ModelSettings, payload.Config)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, binding)
}

func (h HTTP) export(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	doc, err := h.DB.Export(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (h HTTP) importPreview(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	doc, err := readObject(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	preview, _, err := h.DB.PreviewImport(doc)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, preview)
}

func (h HTTP) importCommit(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	doc, err := readObject(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	preview, normalized, err := h.DB.PreviewImport(doc)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if err := h.DB.ApplyImport(c.Request.Context(), normalized); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	configView, err := h.DB.ConfigView(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	providerView, err := h.DB.ProviderConfig(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"preview": preview, "config": configView, "provider_config": providerView,
	})
}

func (h HTTP) generationQueue(c *gin.Context) {
	if h.DB == nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	view, err := h.DB.GenerationQueue(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func bindJSONStrict(c *gin.Context, dest any) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return apperr.Validation("请求体无效")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return apperr.Validation("请求体无效")
	}
	if dec.More() {
		return apperr.Validation("请求体无效")
	}
	return nil
}

func readObject(c *gin.Context) (map[string]any, error) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, apperr.Validation("请求体无效")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || dec.More() {
		return nil, apperr.Validation("配置文件格式不正确")
	}
	return doc, nil
}

func secretEqual(provided, expected string) bool {
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
