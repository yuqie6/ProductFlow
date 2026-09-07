package brand

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 是 Brand CRUD 的 Gin 处理器。
type HTTP struct {
	Service Service
	Settings interface {
		settings.RuntimeReader
	}
}

// Register 挂上 /api/v3/brands。
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
	v3.GET("/brands", h.list)
	v3.POST("/brands", h.create)
	v3.GET("/brands/:brand_id", h.get)
	v3.PATCH("/brands/:brand_id", h.update)
}

func (h HTTP) list(c *gin.Context) {
	out, err := h.Service.List(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if out == nil {
		out = []View{}
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) create(c *gin.Context) {
	var body struct {
		Name           string  `json:"name"`
		VisualSystemID *string `json:"visual_system_id"`
	}
	if err := bindJSON(c, &body); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Create(c.Request.Context(), CreateInput{
		Name: body.Name, VisualSystemID: body.VisualSystemID,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("brand_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) update(c *gin.Context) {
	in, err := decodeUpdate(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.Update(c.Request.Context(), c.Param("brand_id"), in)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func decodeUpdate(c *gin.Context) (UpdateInput, error) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return UpdateInput{}, apperr.Validation("请求体无效")
	}
	if len(raw) == 0 {
		return UpdateInput{}, apperr.Validation("请求体无效")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return UpdateInput{}, apperr.Validation("请求体无效")
	}
	allowed := map[string]struct{}{"name": {}, "visual_system_id": {}}
	for key := range probe {
		if _, ok := allowed[key]; !ok {
			return UpdateInput{}, apperr.Validation("请求体无效")
		}
	}
	in := UpdateInput{}
	if v, ok := probe["name"]; ok {
		var name string
		if err := json.Unmarshal(v, &name); err != nil {
			return UpdateInput{}, apperr.Validation("请求体无效")
		}
		in.Name = &name
	}
	if v, ok := probe["visual_system_id"]; ok {
		var id *string
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return UpdateInput{}, apperr.Validation("请求体无效")
			}
			id = &s
		}
		in.VisualSystemID = &id
	}
	return in, nil
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
