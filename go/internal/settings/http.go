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

// LockState 是设置页锁状态的 HTTP 投影，不是管理员 session。
// Unlocked 表示 cookie 里 settings_unlocked=true；Configured 表示进程配了 SETTINGS_ACCESS_TOKEN。
// 管理员登录成功不等于设置页已解锁；写配置必须先 POST /unlock。
type LockState struct {
	Unlocked   bool `json:"unlocked"`   // cookie 里 settings_unlocked=true
	Configured bool `json:"configured"` // 进程配了 SETTINGS_ACCESS_TOKEN
}

// HTTP 是设置页与生成队列的 Gin 处理器集合。
// 设置路由前缀 /api/settings；生成队列单独挂 GET /api/generation-queue。
// 读 lock-state/unlock/runtime 不要求设置页解锁；其余读写要 requireUnlocked。
type HTTP struct {
	Store               RuntimeReader // RequireAdmin 读 AdminAccessRequired
	DB                  *Store        // 设置读写；导出/供应商档案走这里
	SettingsAccessToken string        // 与 POST /unlock 比较；空则 Configured=false
}

// Register 挂上 /api/settings；写操作要求设置页已解锁。
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

// requireUnlocked 是设置写路由中间件：未配令牌 503；cookie 未解锁 403。
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

// lockState 是 GET /api/settings/lock-state：200 返回 LockState。
func (h HTTP) lockState(c *gin.Context) {
	configured := strings.TrimSpace(h.SettingsAccessToken) != ""
	c.JSON(http.StatusOK, LockState{
		Unlocked:   configured && httpx.SessionBool(c, "settings_unlocked"),
		Configured: configured,
	})
}

// unlock 是 POST /api/settings/unlock：200 返回已解锁 LockState。
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

// runtime 是 GET /api/settings/runtime：200 返回 Runtime 投影。
func (h HTTP) runtime(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	c.JSON(http.StatusOK, runtime)
}

// getConfig 是 GET /api/settings：200 返回 ConfigResponse。
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

// patchConfig 是 PATCH /api/settings：200 返回更新后的 ConfigResponse。
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

// providerConfig 是 GET /api/settings/provider-config：200 返回 ProviderConfigResponse。
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

// createProfile 是 POST /api/settings/provider-profiles：200 返回 ProviderProfile。
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

// updateProfile 是 PATCH /api/settings/provider-profiles/:profile_id：200 返回 ProviderProfile。
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

// archiveProfile 是 DELETE /api/settings/provider-profiles/:profile_id：200 返回已归档 ProviderProfile。
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

// updateBinding 是 PATCH /api/settings/provider-bindings/:purpose：200 返回 ProviderBindingView。
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

// export 是 GET /api/settings/export：200 返回 SettingsExport。
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

// importPreview 是 POST /api/settings/import/preview：200 返回 ImportPreview。
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

// importCommit 是 POST /api/settings/import：200 返回 preview、config、provider_config。
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

// generationQueue 是 GET /api/generation-queue：200 返回 GenerationQueueOverview。
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

// bindJSONStrict 用 DisallowUnknownFields 解码 JSON。多字段或尾随内容一律 400「请求体无效」。
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

// readObject 把请求体解成 JSON object，供导入配置。未知键或非 object 返回「配置文件格式不正确」，文案与普通 bind 不同。
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
