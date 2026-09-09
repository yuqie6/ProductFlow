package imagesession

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
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 是连续生图会话的 Gin 处理器集合，不是 WorkflowGraphRun 也不是 AgentTask。
// 路由挂 /api/image-sessions；attach 在 /api/v2。生成只写 River 作业，不在请求里打 broker。
type HTTP struct {
	Service Service // 必须注入；拥有会话/生成/attach
	// Settings 为 nil 时 RequireAdmin 视为不要求访问令牌。
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
}

// Register 挂上 /api/image-sessions，全部走管理员 session。
// 路径与成功状态码见各处理器注释。删除受 runtime.DeletionEnabled 门闩。
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
	api := engine.Group("/api", admin, auth.RequireWorkingMerchant())
	api.GET("/image-sessions", h.list)
	api.POST("/image-sessions", h.create)
	api.GET("/image-session-assets/:asset_id/download", h.download)
	api.GET("/image-sessions/:image_session_id/status", h.status)
	api.GET("/image-sessions/:image_session_id/events", h.streamEvents)
	api.GET("/image-sessions/:image_session_id/history", h.history)
	api.GET("/image-sessions/:image_session_id", h.get)
	api.PATCH("/image-sessions/:image_session_id", h.update)
	api.DELETE("/image-sessions/:image_session_id", h.requireDeletion, h.delete)
	api.POST("/image-sessions/:image_session_id/reference-images", h.uploadRefs)
	api.DELETE("/image-sessions/:image_session_id/reference-images/:asset_id", h.deleteRef)
	api.POST("/image-sessions/:image_session_id/generate", h.generate)
	api.POST("/image-sessions/:image_session_id/generation-tasks/:task_id/retry", h.retry)
	api.POST("/image-sessions/:image_session_id/generation-tasks/:task_id/cancel", h.cancel)
	api.POST("/image-sessions/:image_session_id/generation-tasks/:task_id/provider-effects/:candidate_start_index/reconciliation", h.reconcile)
	v2 := engine.Group("/api/v2", admin, auth.RequireWorkingMerchant())
	v2.POST("/image-sessions/:image_session_id/assets/:asset_id/attach-to-product", h.attach)
}

// requireDeletion 是 DELETE /api/image-sessions/:image_session_id 的中间件：关闭删除时 403。
func (h HTTP) requireDeletion(c *gin.Context) {
	if h.Settings == nil {
		httpx.AbortDetail(c, http.StatusForbidden, "删除功能已关闭，请联系管理员")
		return
	}
	runtime, err := h.Settings.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	if !runtime.DeletionEnabled {
		httpx.AbortDetail(c, http.StatusForbidden, "删除功能已关闭，请联系管理员")
		return
	}
}

// list 是 GET /api/image-sessions：200 返回 ListResponse。limit 默认 20、上限 100；after 是不透明游标。
func (h HTTP) list(c *gin.Context) {
	limit := imageSessionListDefaultLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > imageSessionListMaxLimit {
			httpx.AbortErr(c, apperr.Validation("会话列表 limit 必须在 1 到 100 之间"))
			return
		}
		limit = parsed
	}
	out, err := h.Service.List(c.Request.Context(), c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// create 是 POST /api/image-sessions：201 返回 DetailResponse。
func (h HTTP) create(c *gin.Context) {
	var req CreateRequest
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Create(c.Request.Context(), req.Title)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// get 是 GET /api/image-sessions/:image_session_id：200 返回首屏 DetailResponse。
func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("image_session_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// history 是 GET /api/image-sessions/:image_session_id/history：200 返回 HistoryResponse。
// limit 默认 20、上限 100；after 是不透明游标。
func (h HTTP) history(c *gin.Context) {
	limit := imageSessionHistoryDefaultLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > imageSessionHistoryMaxLimit {
			httpx.AbortErr(c, apperr.Validation("会话历史 limit 必须在 1 到 100 之间"))
			return
		}
		limit = parsed
	}
	out, err := h.Service.History(c.Request.Context(), c.Param("image_session_id"), c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// status 是 GET /api/image-sessions/:image_session_id/status：200 返回 StatusResponse。
func (h HTTP) status(c *gin.Context) {
	out, err := h.Service.Status(c.Request.Context(), c.Param("image_session_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// update 是 PATCH /api/image-sessions/:image_session_id：200 返回 DetailResponse。
func (h HTTP) update(c *gin.Context) {
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	out, err := h.Service.Update(c.Request.Context(), c.Param("image_session_id"), req.Title)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// delete 是 DELETE /api/image-sessions/:image_session_id：204。
func (h HTTP) delete(c *gin.Context) {
	if err := h.Service.Delete(c.Request.Context(), c.Param("image_session_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// uploadRefs 是 POST /api/image-sessions/:image_session_id/reference-images：200 返回 DetailResponse。
func (h HTTP) uploadRefs(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil || form == nil {
		httpx.AbortErr(c, apperr.Validation("至少上传一张参考图"))
		return
	}
	files := form.File["reference_images"]
	limits := media.DefaultLimits()
	if h.Settings != nil {
		if got, err := h.Settings.UploadLimits(c.Request.Context()); err == nil {
			limits = got
		}
	}
	if err := limits.RejectReferenceCount(len(files)); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if len(files) == 0 {
		httpx.AbortErr(c, apperr.Validation("至少上传一张参考图"))
		return
	}
	uploads := make([]product.Upload, 0, len(files))
	for _, header := range files {
		f, err := header.Open()
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		content, err := io.ReadAll(io.LimitReader(f, int64(limits.MaxImageBytes)+1))
		_ = f.Close()
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		filename := header.Filename
		if filename == "" {
			filename = "reference.bin"
		}
		validated, err := media.ValidateUpload(filename, header.Header.Get("Content-Type"), content, limits)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		uploads = append(uploads, product.Upload{Content: validated.Content, Filename: validated.Filename, MIMEType: validated.MIMEType})
	}
	out, err := h.Service.AddReferences(c.Request.Context(), c.Param("image_session_id"), uploads)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// deleteRef 是 DELETE /api/image-sessions/:image_session_id/reference-images/:asset_id：200 返回 DetailResponse。
func (h HTTP) deleteRef(c *gin.Context) {
	out, err := h.Service.DeleteReference(c.Request.Context(), c.Param("image_session_id"), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// generate 是 POST /api/image-sessions/:image_session_id/generate：202 返回含 queued 任务的 DetailResponse。
func (h HTTP) generate(c *gin.Context) {
	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if req.ToolOptions != nil {
		known := map[string]struct{}{
			"model": {}, "quality": {}, "output_format": {}, "output_compression": {},
			"background": {}, "moderation": {}, "action": {}, "input_fidelity": {},
			"partial_images": {}, "n": {},
		}
		for k := range req.ToolOptions {
			if _, ok := known[k]; !ok {
				httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
				return
			}
		}
		if raw, ok := req.ToolOptions["n"]; ok {
			n, ok := toolOptionN(raw)
			if !ok || n < 1 || n > 10 {
				httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
				return
			}
		}
	}
	out, err := h.Service.Generate(c.Request.Context(), c.Param("image_session_id"), req)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, out)
}

// retry 是 POST /api/image-sessions/:image_session_id/generation-tasks/:task_id/retry：202 返回 DetailResponse。
func (h HTTP) retry(c *gin.Context) {
	out, err := h.Service.Retry(c.Request.Context(), c.Param("image_session_id"), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, out)
}

// cancel 是 POST /api/image-sessions/:image_session_id/generation-tasks/:task_id/cancel：200 返回 DetailResponse。
func (h HTTP) cancel(c *gin.Context) {
	out, err := h.Service.Cancel(c.Request.Context(), c.Param("image_session_id"), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// reconcile 是 POST /api/image-sessions/:image_session_id/generation-tasks/:task_id/provider-effects/:candidate_start_index/reconciliation：200 返回 DetailResponse。
func (h HTTP) reconcile(c *gin.Context) {
	idx, err := strconv.Atoi(c.Param("candidate_start_index"))
	if err != nil || idx < 1 {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	out, err := h.Service.Reconcile(c.Request.Context(), c.Param("image_session_id"), c.Param("task_id"), idx)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// download 是 GET /api/image-session-assets/:asset_id/download：200 写出文件；缺文件 404。
func (h HTTP) download(c *gin.Context) {
	asset, err := h.Service.AssetDownload(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if asset.VerificationStatus == media.StatusMissing {
		httpx.AbortDetail(c, http.StatusNotFound, "会话图片文件不存在")
		return
	}
	media.ServeExistingVariant(
		c,
		h.Service.Media,
		h.Service.DB,
		asset.StoragePath,
		asset.OriginalFilename,
		asset.MIMEType,
		c.DefaultQuery("variant", "original"),
		"会话图片文件不存在",
	)
}

// attach 是 POST /api/v2/image-sessions/:image_session_id/assets/:asset_id/attach-to-product：200 返回商品图投影。
func (h HTTP) attach(c *gin.Context) {
	var req AttachRequest
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if strings.TrimSpace(req.ProductID) == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	out, err := h.Service.Attach(c.Request.Context(), c.Param("image_session_id"), c.Param("asset_id"), req.ProductID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
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

func toolOptionN(raw any) (int, bool) {
	switch v := raw.(type) {
	case float64:
		n := int(v)
		return n, float64(n) == v
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}
