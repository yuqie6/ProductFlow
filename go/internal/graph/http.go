package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 给 Web 管理员 session 挂 schema-v3 画布与跑图路由，路径前缀 /api/v3。
// Service 必须注入；Settings 为 nil 时 RequireAdmin 视为不要求访问令牌。
// 提交运行只写 PENDING dispatch，禁止在 handler 里打 broker。不要把本类型当成 graph.Service。
type HTTP struct {
	Service           Service // 必须注入；改图与跑图都经此入口
	GenerationOptions func(context.Context) (map[string][]string, error)
	// Settings 为 nil 时 RequireAdmin 视为不要求访问令牌；Runtime 失败则拒绝该请求。
	Settings interface {
		settings.RuntimeReader
	}
}

// Register 挂上 schema-v3 画布读写与跑图 HTTP，全部走管理员 session。
// 路径前缀 /api/v3。提交运行只写 PENDING dispatch，不在请求里打 broker。
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
	v3.GET("/image-generation-options", h.generationOptions)
	v3.POST("/products/:product_id/workflows", h.createEmpty)
	v3.GET("/products/:product_id/workflows/current", h.current)
	v3.GET("/products/:product_id/workflows/:workflow_id", h.get)
	v3.POST("/products/:product_id/workflows/:workflow_id/changesets", h.applyChangeSet)
	v3.POST("/products/:product_id/workflows/:workflow_id/undo", h.undo)
	v3.POST("/products/:product_id/workflows/:workflow_id/redo", h.redo)
	v3.POST("/products/:product_id/workflows/:workflow_id/proposals/:proposal_id/confirm", h.confirmProposal)
	v3.POST("/products/:product_id/workflows/:workflow_id/proposals/:proposal_id/discard", h.discardProposal)
	v3.GET("/products/:product_id/workflows/:workflow_id/nodes/:node_id/candidate", h.getDocumentCandidate)
	v3.POST("/products/:product_id/workflows/:workflow_id/nodes/:node_id/candidate/apply", h.applyDocumentCandidate)
	v3.POST("/products/:product_id/workflows/:workflow_id/nodes/:node_id/candidate/discard", h.discardDocumentCandidate)
	v3.POST("/products/:product_id/workflows/:workflow_id/runs", h.submitRun)
	v3.POST("/products/:product_id/workflows/:workflow_id/runs/preview", h.previewRun)
	v3.GET("/products/:product_id/workflows/:workflow_id/runs", h.listRuns)
	v3.GET("/products/:product_id/workflows/:workflow_id/runs/:run_id", h.getRun)
	v3.GET("/products/:product_id/workflows/:workflow_id/runs/:run_id/events", h.streamRunEvents)
	v3.POST("/products/:product_id/workflows/:workflow_id/runs/:run_id/cancel", h.cancelRun)
	v3.POST("/products/:product_id/workflows/:workflow_id/runs/:run_id/retry", h.retryRun)
}

// catalog 是 GET /api/v3/node-catalog：200 返回 CatalogJSON。
func (h HTTP) catalog(c *gin.Context) {
	c.JSON(http.StatusOK, CatalogJSON())
}

func (h HTTP) generationOptions(c *gin.Context) {
	if h.GenerationOptions == nil {
		c.JSON(http.StatusOK, map[string][]string{})
		return
	}
	out, err := h.GenerationOptions(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// createEmpty 是 POST /api/v3/products/:product_id/workflows：201 空画布；已有 active 图 409。
func (h HTTP) createEmpty(c *gin.Context) {
	out, err := h.Service.CreateEmpty(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// current 是 GET /api/v3/products/:product_id/workflows/current：200 投影；无 active 图 404。
func (h HTTP) current(c *gin.Context) {
	out, err := h.Service.Current(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// get 是 GET /api/v3/products/:product_id/workflows/:workflow_id：200 投影；不属于该商品 404。
func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// applyChangeSet 是 POST .../changesets：200 新投影；体非法 400；revision 不匹配 409。
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

// undo 是 POST .../undo：200 新投影；无可撤销或空 inverse 409。
func (h HTTP) undo(c *gin.Context) {
	out, err := h.Service.Undo(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// redo 是 POST .../redo：200 新投影；栈顶不是 Undo 409。
func (h HTTP) redo(c *gin.Context) {
	out, err := h.Service.Redo(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// confirmProposal 是 POST .../proposals/:proposal_id/confirm：200 应用后投影；非 pending 409。
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

// discardProposal 是 POST .../proposals/:proposal_id/discard：200；非 pending 409；不改 live 图。
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

// getDocumentCandidate 是 GET .../nodes/:node_id/candidate：200 候选；无候选 404。
func (h HTTP) getDocumentCandidate(c *gin.Context) {
	out, err := h.Service.GetDocumentCandidate(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("node_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// applyDocumentCandidate 是 POST .../candidate/apply：200 新投影；revision/artifact 变了 409。
func (h HTTP) applyDocumentCandidate(c *gin.Context) {
	var input ApplyDocumentCandidateInput
	if err := decodeStrictJSON(c, &input); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ApplyDocumentCandidate(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("node_id"), input)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// discardDocumentCandidate 是 POST .../candidate/discard：200；artifact 已变 409。
func (h HTTP) discardDocumentCandidate(c *gin.Context) {
	var input DiscardDocumentCandidateInput
	if err := decodeStrictJSON(c, &input); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.DiscardDocumentCandidate(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("node_id"), input)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// decodeStrictJSON 拒绝未知 JSON 字段和粘连的第二份文档，失败统一 400「请求体无效」。改图/跑图入口都走这里。
func decodeStrictJSON(c *gin.Context, target any) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return apperr.Validation("请求体无效")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return apperr.Validation("请求体无效")
	}
	return nil
}

// submitRun 是 POST .../runs：201 新建或合并后的 run；体非法 400；非 active 图 409。
func (h HTTP) submitRun(c *gin.Context) {
	req, err := parseGraphRunRequest(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.SubmitRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), req)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// previewRun 是 POST .../runs/preview：200 planned_action；不写库、不入队。
func (h HTTP) previewRun(c *gin.Context) {
	req, err := parseGraphRunRequest(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.PreviewRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), req)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// listRuns 是 GET .../runs：200 最近最多 20 条。
func (h HTTP) listRuns(c *gin.Context) {
	out, err := h.Service.ListRuns(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// getRun 是 GET .../runs/:run_id：200；找不到 404。
func (h HTTP) getRun(c *gin.Context) {
	out, err := h.Service.GetRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("run_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// cancelRun 是 POST .../runs/:run_id/cancel：200 当前行；已结束 409；已取消幂等 200。
func (h HTTP) cancelRun(c *gin.Context) {
	out, err := h.Service.CancelRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("run_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// retryRun 是 POST .../runs/:run_id/retry：201 新提交。仅 failed+is_retryable；unknown 或不可重试 400。
func (h HTTP) retryRun(c *gin.Context) {
	out, err := h.Service.RetryRun(c.Request.Context(), c.Param("product_id"), c.Param("workflow_id"), c.Param("run_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// parseGraphRunRequest 解码跑图请求体；空 body 也是 400。force 是否合法由 Service 再查范围，这里不放行未知键。
func parseGraphRunRequest(c *gin.Context) (GraphRunRequest, error) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return GraphRunRequest{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return GraphRunRequest{}, apperr.Validation("请求体无效")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req GraphRunRequest
	if err := dec.Decode(&req); err != nil {
		return GraphRunRequest{}, apperr.Validation("请求体无效")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return GraphRunRequest{}, apperr.Validation("请求体无效")
	}
	if req.Scope == "" {
		req.Scope = RunScopeGraph
	}
	switch req.Scope {
	case RunScopeGraph, RunScopeNode, RunScopeToNode, RunScopeSelection:
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
	req.NodeIDs = normalizeRunNodeIDs(req.NodeIDs)
	if strings.TrimSpace(req.DocumentAction) != "" && validDocumentAction(req.DocumentAction) == "" {
		return GraphRunRequest{}, apperr.Validation("document_action 无效")
	}
	req.DocumentAction = validDocumentAction(req.DocumentAction)
	if err := validateGraphRunRequest(req); err != nil {
		return GraphRunRequest{}, err
	}
	if (req.Scope == RunScopeNode || req.Scope == RunScopeToNode) && req.NodeID == nil {
		return GraphRunRequest{}, apperr.Validation("节点运行范围必须指定 node_id")
	}
	if req.Scope == RunScopeSelection && len(req.NodeIDs) == 0 {
		return GraphRunRequest{}, apperr.Validation("选区运行必须指定 node_ids")
	}
	return req, nil
}
