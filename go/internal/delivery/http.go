package delivery

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 是交付预设、派生任务与 ZIP 导出的 Gin 处理器集合。
// 派生只写 PENDING dispatch，不在请求里打图像模型。不要和 GraphRun 生图搞混。
type HTTP struct {
	Service Service // 必须注入；拥有提交/查询/重试/导出
	// Settings 为 nil 时 RequireAdmin 视为不要求访问令牌。
	Settings interface {
		settings.RuntimeReader
	}
}

// Register 挂上 /api/v2 交付任务与 /api/v3 预设/导出，全部走管理员 session。
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
	v2 := engine.Group("/api/v2", admin)
	// 参数名必须与 product 的 :asset_id 相同，Gin 不允许同一前缀使用不同通配符名。
	v2.POST("/product-image-assets/:asset_id/renditions", h.create)
	v2.GET("/product-image-assets/:asset_id/renditions", h.list)
	v2.GET("/delivery-rendition-jobs/:job_id", h.get)
	v2.POST("/delivery-rendition-jobs/:job_id/retry", h.retry)
	v3 := engine.Group("/api/v3", admin)
	v3.GET("/delivery-presets", h.presets)
	v3.POST("/products/:product_id/delivery-exports", h.exportZip)
}

// presets 是 GET /api/v3/delivery-presets：200 返回 PresetCatalog。
func (h HTTP) presets(c *gin.Context) {
	c.JSON(http.StatusOK, ListPresets())
}

// create 是 POST /api/v2/product-image-assets/:asset_id/renditions：queued/running 202；已有终态任务 200。
func (h HTTP) create(c *gin.Context) {
	var spec Spec
	if err := bindJSON(c, &spec); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Submit(c.Request.Context(), c.Param("asset_id"), specPayload(spec))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	status := http.StatusOK
	if out.Job.Status == "queued" || out.Job.Status == "running" {
		status = http.StatusAccepted
	}
	c.JSON(status, out.Job)
}

// list 是 GET /api/v2/product-image-assets/:asset_id/renditions：200 返回 JobListResponse。
func (h HTTP) list(c *gin.Context) {
	out, err := h.Service.List(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// get 是 GET /api/v2/delivery-rendition-jobs/:job_id：200 返回 JobResponse。
func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// retry 是 POST /api/v2/delivery-rendition-jobs/:job_id/retry：202 返回 JobResponse。
func (h HTTP) retry(c *gin.Context) {
	out, err := h.Service.Retry(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, out)
}

// exportZip 是 POST /api/v3/products/:product_id/delivery-exports：200 写出 ZIP。
func (h HTTP) exportZip(c *gin.Context) {
	var req ExportRequest
	if err := bindJSON(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	archive, err := h.Service.Export(c.Request.Context(), c.Param("product_id"), req.RenditionJobIDs, req.AllowPartial)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	defer os.Remove(archive.Path)
	c.Header("Content-Type", "application/zip")
	c.FileAttachment(archive.Path, archive.Filename)
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
