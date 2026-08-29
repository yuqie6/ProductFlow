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

type HTTP struct {
	Service  Service
	Settings interface {
		settings.RuntimeReader
	}
}

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

func (h HTTP) presets(c *gin.Context) {
	c.JSON(http.StatusOK, ListPresets())
}

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

func (h HTTP) list(c *gin.Context) {
	out, err := h.Service.List(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) retry(c *gin.Context) {
	out, err := h.Service.Retry(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, out)
}

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
