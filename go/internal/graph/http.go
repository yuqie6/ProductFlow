package graph

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

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

// Register 挂上 schema-v3 画布读写与跑图 HTTP。提交只写 PENDING dispatch，不在请求里打 broker。
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
	v3.POST("/products/:product_id/workflows/:workflow_id/runs", h.submitRun)
	v3.GET("/products/:product_id/workflows/:workflow_id/runs", h.listRuns)
	v3.GET("/products/:product_id/workflows/:workflow_id/runs/:run_id", h.getRun)
	v3.POST("/products/:product_id/workflows/:workflow_id/runs/:run_id/cancel", h.cancelRun)
	v3.POST("/products/:product_id/workflows/:workflow_id/runs/:run_id/retry", h.retryRun)
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

func (h HTTP) submitRun(c *gin.Context) {
	req, err := parseGraphRunRequest(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if req.Scope != RunScopeGraph && (req.NodeID == nil || strings.TrimSpace(*req.NodeID) == "") {
		httpx.AbortErr(c, apperr.Validation("节点运行范围必须指定 node_id"))
		return
	}
	out, err := h.Service.SubmitRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), req.Scope, req.NodeID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) listRuns(c *gin.Context) {
	out, err := h.Service.ListRuns(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) getRun(c *gin.Context) {
	out, err := h.Service.GetRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("run_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) cancelRun(c *gin.Context) {
	out, err := h.Service.CancelRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("run_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) retryRun(c *gin.Context) {
	out, err := h.Service.RetryRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("run_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func parseGraphRunRequest(c *gin.Context) (GraphRunRequest, error) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return GraphRunRequest{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return GraphRunRequest{Scope: RunScopeGraph}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req GraphRunRequest
	if err := dec.Decode(&req); err != nil {
		return GraphRunRequest{}, apperr.Validation("请求体无效")
	}
	if req.Scope == "" {
		req.Scope = RunScopeGraph
	}
	switch req.Scope {
	case RunScopeGraph, RunScopeNode, RunScopeToNode:
	default:
		return GraphRunRequest{}, apperr.Validation("请求体无效")
	}
	if req.NodeID != nil {
		trimmed := strings.TrimSpace(*req.NodeID)
		if trimmed == "" {
			req.NodeID = nil
		} else {
			req.NodeID = &trimmed
		}
	}
	return req, nil
}
