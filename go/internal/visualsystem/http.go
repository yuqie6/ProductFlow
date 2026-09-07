package visualsystem

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 是视觉方案版本保存/选择/预览与继承解析的 Gin 处理器。
type HTTP struct {
	Service Service
	Settings interface {
		settings.RuntimeReader
	}
}

// Register 挂上 /api/v3 视觉方案与商品选择路由。
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
	v3 := engine.Group("/api/v3", admin)
	v3.GET("/visual-systems", h.list)
	v3.POST("/visual-systems", h.create)
	v3.GET("/visual-systems/:system_id", h.get)
	v3.POST("/visual-systems/:system_id/versions", h.appendVersion)
	v3.GET("/visual-systems/:system_id/versions/:version_id", h.getVersion)
	v3.GET("/visual-systems/:system_id/versions/:version_id/impact", h.impact)
	v3.GET("/products/:product_id/visual-selection", h.getSelection)
	v3.PUT("/products/:product_id/visual-selection", h.selectVersion)
	v3.POST("/products/:product_id/visual-inheritance", h.inheritance)
}

func (h HTTP) list(c *gin.Context) {
	includeArchived := strings.EqualFold(c.Query("include_archived"), "true") || c.Query("include_archived") == "1"
	out, err := h.Service.List(c.Request.Context(), includeArchived)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if out == nil {
		out = []SystemView{}
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) create(c *gin.Context) {
	var body struct {
		Name    string         `json:"name"`
		Payload map[string]any `json:"payload"`
	}
	if err := bindJSON(c, &body); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Create(c.Request.Context(), CreateSystemInput{Name: body.Name, Payload: body.Payload})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("system_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) appendVersion(c *gin.Context) {
	var body struct {
		Payload map[string]any `json:"payload"`
	}
	if err := bindJSON(c, &body); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.AppendVersion(c.Request.Context(), AppendVersionInput{
		SystemID: c.Param("system_id"), Payload: body.Payload,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) getVersion(c *gin.Context) {
	out, err := h.Service.GetVersion(c.Request.Context(), c.Param("system_id"), c.Param("version_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) impact(c *gin.Context) {
	out, err := h.Service.Impact(c.Request.Context(), c.Param("system_id"), c.Param("version_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) getSelection(c *gin.Context) {
	out, err := h.Service.GetSelection(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) selectVersion(c *gin.Context) {
	var body struct {
		VisualSystemVersionID string `json:"visual_system_version_id"`
	}
	if err := bindJSON(c, &body); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Select(c.Request.Context(), SelectInput{
		ProductID: c.Param("product_id"), VisualSystemVersionID: body.VisualSystemVersionID,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) inheritance(c *gin.Context) {
	var body struct {
		ProductOverride map[string]any `json:"product_override"`
	}
	if c.Request.ContentLength != 0 {
		if err := bindJSON(c, &body); err != nil {
			httpx.AbortErr(c, err)
			return
		}
	}
	out, err := h.Service.Inheritance(c.Request.Context(), c.Param("product_id"), body.ProductOverride)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func bindJSON(c *gin.Context, dest any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		if err == io.EOF {
			return apperr.Validation("请求体无效")
		}
		return apperr.Validation("请求体无效")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return apperr.Validation("请求体无效")
	}
	return nil
}
