package localedit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 是局部编辑的 Gin 处理器集合，不是画布 GraphRun。
// 路由前缀 /api/v3；提交只写 River 作业。未 adopt 前结果不是节点当前图。
type HTTP struct {
	Service Service // 必须注入；拥有草稿/提交/adopt
	// Settings 为 nil 时 RequireAdmin 视为不要求访问令牌。
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
}

// Register 挂上 /api/v3 局部编辑路由，全部走管理员 session。
// 路径与成功状态码见各处理器注释。
func (h HTTP) Register(engine *gin.Engine) {
	admin := httpx.RequireAdmin(func(c *gin.Context) (bool, error) {
		if h.Settings == nil {
			return true, nil
		}
		runtime, err := h.Settings.Runtime(c.Request.Context())
		if err != nil {
			return false, err
		}
		return runtime.AdminAccessRequired, nil
	})
	v3 := engine.Group("/api/v3", admin, auth.RequireWorkingMerchant())
	v3.GET("/local-image-edits/capability", h.capability)
	v3.GET("/products/:product_id/image-edits", h.list)
	v3.POST("/products/:product_id/image-edits", h.create)
	v3.GET("/products/:product_id/image-edits/:task_id", h.get)
	v3.PATCH("/products/:product_id/image-edits/:task_id", h.update)
	v3.POST("/products/:product_id/image-edits/:task_id/submit", h.submit)
	v3.POST("/products/:product_id/image-edits/:task_id/cancel", h.cancel)
	v3.POST("/products/:product_id/image-edits/:task_id/retry", h.retry)
	v3.POST("/products/:product_id/image-edits/:task_id/adopt", h.adopt)
	v3.POST("/products/:product_id/image-edits/:task_id/adoptions/:adoption_event_id/revert", h.revert)
}

// capability 是 GET /api/v3/local-image-edits/capability：200 返回 CapabilityResponse。
func (h HTTP) capability(c *gin.Context) {
	c.JSON(http.StatusOK, h.Service.Capability())
}

// list 是 GET /api/v3/products/:product_id/image-edits：200 返回 TaskListResponse。
func (h HTTP) list(c *gin.Context) {
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.AbortErr(c, apperr.Validation("局部编辑任务 limit 必须在 1 到 100 之间"))
			return
		}
		limit = n
	}
	out, err := h.Service.List(c.Request.Context(), c.Param("product_id"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// get 是 GET /api/v3/products/:product_id/image-edits/:task_id：200 返回 TaskResponse。
func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("product_id"), c.Param("task_id"), true)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// create 是 POST /api/v3/products/:product_id/image-edits：201 返回草稿 TaskResponse。
func (h HTTP) create(c *gin.Context) {
	if err := rejectAliases(c); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	maskPNG, err := h.readMask(c, true)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	draft, err := parseDraft(
		c.PostForm("operation"), c.PostForm("instruction"), c.PostForm("source_text"),
		c.PostForm("replacement_text"), c.PostForm("mask_geometry_json"), c.PostForm("reference_asset_ids_json"),
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Create(c.Request.Context(), c.Param("product_id"), c.PostForm("source_asset_id"), strings.TrimSpace(c.PostForm("target_node_id")), draft, maskPNG)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// update 是 PATCH /api/v3/products/:product_id/image-edits/:task_id：200 返回 TaskResponse。
func (h HTTP) update(c *gin.Context) {
	if err := rejectAliases(c); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	rev, err := strconv.Atoi(c.PostForm("expected_revision"))
	if err != nil || rev < 1 {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	maskPNG, err := h.readMask(c, false)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	draft, err := parseDraft(
		c.PostForm("operation"), c.PostForm("instruction"), c.PostForm("source_text"),
		c.PostForm("replacement_text"), c.PostForm("mask_geometry_json"), c.PostForm("reference_asset_ids_json"),
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Update(c.Request.Context(), c.Param("product_id"), c.Param("task_id"), rev, draft, maskPNG)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// submit 是 POST /api/v3/products/:product_id/image-edits/:task_id/submit：202 返回 queued TaskResponse。
func (h HTTP) submit(c *gin.Context) {
	var req SubmitRequest
	if err := bindJSON(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Submit(c.Request.Context(), c.Param("product_id"), c.Param("task_id"), req.IdempotencyKey)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, out)
}

// cancel 是 POST /api/v3/products/:product_id/image-edits/:task_id/cancel：200 返回 TaskResponse。
func (h HTTP) cancel(c *gin.Context) {
	rev, err := optionalRevision(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Cancel(c.Request.Context(), c.Param("product_id"), c.Param("task_id"), rev)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// retry 是 POST /api/v3/products/:product_id/image-edits/:task_id/retry：202 返回 TaskResponse。
func (h HTTP) retry(c *gin.Context) {
	rev, err := optionalRevision(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Retry(c.Request.Context(), c.Param("product_id"), c.Param("task_id"), rev)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, out)
}

// adopt 是 POST /api/v3/products/:product_id/image-edits/:task_id/adopt：200 返回 TaskResponse。
func (h HTTP) adopt(c *gin.Context) {
	var req AdoptRequest
	if err := bindJSON(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Adopt(c.Request.Context(), c.Param("product_id"), c.Param("task_id"), req.ExpectedCurrentArtifactID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// revert 是 POST /api/v3/products/:product_id/image-edits/:task_id/adoptions/:adoption_event_id/revert：200 返回 TaskResponse。
func (h HTTP) revert(c *gin.Context) {
	var req AdoptRequest
	if err := bindJSON(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Revert(c.Request.Context(), c.Param("product_id"), c.Param("task_id"), c.Param("adoption_event_id"), req.ExpectedCurrentArtifactID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// readMask 读 multipart 字段 mask，必须是 PNG bytes。required=false 且缺文件时返回 nil,nil，不要当成 Validation。
func (h HTTP) readMask(c *gin.Context, required bool) ([]byte, error) {
	file, err := c.FormFile("mask")
	if err != nil {
		if !required {
			return nil, nil
		}
		return nil, apperr.Validation("局部编辑 mask 必须是非空 PNG bytes")
	}
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limits := media.DefaultLimits()
	if h.Settings != nil {
		if got, err := h.Settings.UploadLimits(c.Request.Context()); err == nil {
			limits = got
		}
	}
	content, err := io.ReadAll(io.LimitReader(f, int64(limits.MaxImageBytes)+1))
	if err != nil {
		return nil, err
	}
	filename := file.Filename
	if filename == "" {
		filename = maskFilename
	}
	validated, err := media.ValidateUpload(filename, file.Header.Get("Content-Type"), content, limits)
	if err != nil {
		return nil, err
	}
	return validated.Content, nil
}

// rejectAliases 拒绝 mask_geometry / reference_asset_ids 这类别名，避免旧客户端静默写错字段。
func rejectAliases(c *gin.Context) error {
	form, err := c.MultipartForm()
	if err != nil || form == nil {
		return nil
	}
	var present []string
	if _, ok := form.Value["mask_geometry"]; ok {
		present = append(present, "mask_geometry")
	}
	if _, ok := form.Value["reference_asset_ids"]; ok {
		present = append(present, "reference_asset_ids")
	}
	if len(present) > 0 {
		return apperr.Validation("局部编辑只接受正式字段: " + strings.Join(present, ", "))
	}
	return nil
}

// optionalRevision 读表单/查询里的 expected_revision；缺省 nil。非法整数 400。
func optionalRevision(c *gin.Context) (*int, error) {
	if c.Request.ContentLength == 0 {
		return nil, nil
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, apperr.Validation("请求体无效")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var req RevisionRequest
	if err := bindJSON(c, &req); err != nil {
		return nil, err
	}
	return req.ExpectedRevision, nil
}

// bindJSON 用 DisallowUnknownFields 解码 JSON。多字段或尾随内容一律 400「请求体无效」。
func bindJSON(c *gin.Context, dest any) error {
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
