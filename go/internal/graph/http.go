package graph

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

type HTTP struct {
	Service  Service
	Settings interface {
		settings.RuntimeReader
	}
}

// Register 挂上 schema-v3 画布读写。跑图 HTTP 归 P8，本切片不伪造入队成功。
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
	v3.GET("/node-catalog", h.catalog)
	v3.POST("/products/:product_id/workflows", h.createEmpty)
	v3.GET("/products/:product_id/workflows/current", h.current)
	v3.GET("/products/:product_id/workflows/:workflow_id", h.get)
	v3.POST("/products/:product_id/workflows/:workflow_id/changesets", h.applyChangeSet)
	v3.POST("/products/:product_id/workflows/:workflow_id/undo", h.undo)
	v3.POST("/products/:product_id/workflows/:workflow_id/redo", h.redo)
	v3.POST("/products/:product_id/workflows/:workflow_id/proposals/:proposal_id/confirm", h.confirmProposal)
	v3.POST("/products/:product_id/workflows/:workflow_id/proposals/:proposal_id/discard", h.discardProposal)
}

func (h HTTP) catalog(c *gin.Context) {
	c.JSON(http.StatusOK, CatalogJSON())
}

func (h HTTP) createEmpty(c *gin.Context) {
	out, err := h.Service.CreateEmpty(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) current(c *gin.Context) {
	out, err := h.Service.Current(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) applyChangeSet(c *gin.Context) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	cs, err := ParseChangeSet(raw)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ApplyChangeSet(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), cs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) undo(c *gin.Context) {
	out, err := h.Service.Undo(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) redo(c *gin.Context) {
	out, err := h.Service.Redo(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) confirmProposal(c *gin.Context) {
	out, err := h.Service.ConfirmProposal(
		c.Request.Context(),
		c.Param("product_id"),
		c.Param("workflow_id"),
		c.Param("proposal_id"),
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) discardProposal(c *gin.Context) {
	out, err := h.Service.DiscardProposal(
		c.Request.Context(),
		c.Param("product_id"),
		c.Param("workflow_id"),
		c.Param("proposal_id"),
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
