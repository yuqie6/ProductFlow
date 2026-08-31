package agent

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/product"
)

func (h HTTP) registerInternal(engine *gin.Engine) {
	internal := engine.Group("/api/internal/v1", h.requireInternal)
	internal.GET("/agent-runtime/provider-config", h.providerConfig)
	internal.GET("/agent-tasks/:task_id/contract", h.taskContract)

	conv := internal.Group("/agent-conversations/:conversation_id")
	conv.GET("/contract", h.conversationContract)
	conv.GET("/runtime-context", h.runtimeContext)
	conv.GET("/product-context", h.productContext)
	conv.GET("/global-workflow-context", h.globalWorkflowContext)
	conv.POST("/global-draft/validate", h.validateGlobalDraft)

	conv.POST("/graph/apply-change-set", h.applyGraph)
	conv.POST("/graph/apply-change-set/reconcile", h.reconcileApplyGraph)
	conv.POST("/graph/proposals", h.proposeGraph)
	conv.POST("/graph/proposals/reconcile", h.reconcileProposeGraph)
	conv.GET("/graph/nodes/:node_id", h.nodeDetail)
	conv.POST("/graph/proposals/discard", h.discardProposal)
	conv.POST("/graph/proposals/discard/reconcile", h.reconcileDiscardProposal)
	conv.POST("/workflow-runs/:run_id/cancel", h.cancelRunTool)
	conv.POST("/workflow-runs/:run_id/cancel/reconcile", h.reconcileCancelRun)
	conv.POST("/canvas/focus", h.focusCanvas)
	conv.POST("/canvas/focus/reconcile", h.reconcileFocusCanvas)
	conv.GET("/workflow-runs", h.listRuns)
	conv.GET("/workflow-runs/:run_id", h.workflowRunDetail)
	conv.POST("/workflow-runs/inspect", h.inspectRuns)

	conv.POST("/turn-executions/claim", h.claimExecution)
	conv.POST("/turn-executions/:execution_id/heartbeat", h.heartbeatExecution)
	conv.POST("/turn-executions/:execution_id/checkpoints", h.appendCheckpoint)
	conv.POST("/turn-executions/:execution_id/release", h.releaseExecution)
	conv.POST("/turn-executions/:execution_id/events/batch", h.appendEvents)

	conv.POST("/workflow-run-requests/prepare", h.prepareRunRequest)
	conv.POST("/global-workflow-run-requests/prepare", h.prepareGlobalRunRequest)
	conv.POST("/workflow-run-requests", h.createRunRequest)
	conv.POST("/global-workflow-run-requests", h.createGlobalRunRequest)
	conv.POST("/workflow-run-requests/reconcile", h.reconcileRunRequest)
	conv.POST("/global-workflow-run-requests/reconcile", h.reconcileGlobalRunRequest)

	conv.GET("/assets", h.listAssets)
	conv.POST("/assets/inspect", h.inspectAssets)
	conv.GET("/assets/:asset_id/content", h.assetContent)
	conv.GET("/media-library", h.listLibrary)
	conv.POST("/media-library/inspect", h.inspectLibrary)
	conv.GET("/media-library/:asset_id/content", h.libraryContent)
	conv.GET("/products", h.listProducts)
	conv.POST("/products/inspect", h.inspectProducts)
	conv.POST("/product-workspaces", h.createWorkspace)
	conv.POST("/product-workspaces/reconcile", h.reconcileWorkspace)
	conv.POST("/product-intake", h.finalizeIntake)
	conv.POST("/product-intake/reconcile", h.reconcileIntake)

}

func (h HTTP) requireInternal(c *gin.Context) {
	if h.InternalToken == "" {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "Agent 内部服务尚未配置")
		return
	}
	scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare([]byte(token), []byte(h.InternalToken)) != 1 {
		c.Header("WWW-Authenticate", "Bearer")
		httpx.AbortDetail(c, http.StatusUnauthorized, "Agent 内部服务认证失败")
		return
	}
}

func (h HTTP) providerConfig(c *gin.Context) {
	if h.Service.Settings == nil {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "工作流 Agent 供应商尚未配置")
		return
	}
	out, err := h.Service.Settings.ResolveAgentProvider(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) taskContract(c *gin.Context) {
	out, err := h.Service.TaskContract(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) conversationContract(c *gin.Context) {
	out, err := h.Service.ConversationContract(c.Request.Context(), c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) runtimeContext(c *gin.Context) {
	out, err := h.Service.RuntimeContext(c.Request.Context(), c.Param("conversation_id"), queryOpt(c, "task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) productContext(c *gin.Context) {
	out, err := h.Service.ProductContext(c.Request.Context(), c.Param("conversation_id"), c.Query("response_format"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) globalWorkflowContext(c *gin.Context) {
	out, err := h.Service.GlobalWorkflowContext(c.Request.Context(), c.Param("conversation_id"), c.Query("product_id"), c.Query("response_format"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) validateGlobalDraft(c *gin.Context) {
	var req struct {
		Value json.RawMessage `json:"value"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if err := h.Service.ValidateGlobalDraft(c.Request.Context(), c.Param("conversation_id"), req.Value); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"accepted": true})
}

func (h HTTP) applyGraph(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		ChangeSet json.RawMessage `json:"change_set"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ApplyGraphTool(c.Request.Context(), c.Param("conversation_id"), req.ChangeSet, key)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileApplyGraph(c *gin.Context) {
	h.reconcileGraphChange(c, applyGraphTool)
}

func (h HTTP) proposeGraph(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		ChangeSet json.RawMessage `json:"change_set"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ProposeGraphTool(c.Request.Context(), c.Param("conversation_id"), req.ChangeSet, key)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileProposeGraph(c *gin.Context) {
	h.reconcileGraphChange(c, proposeGraphTool)
}

func (h HTTP) reconcileGraphChange(c *gin.Context, toolName string) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		ChangeSet json.RawMessage `json:"change_set"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	var target map[string]any
	_ = json.Unmarshal(req.ChangeSet, &target)
	out, err := h.Service.ReconcileGraphTool(c.Request.Context(), c.Param("conversation_id"), toolName, key, map[string]any{}, target)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) nodeDetail(c *gin.Context) {
	out, err := h.Service.GetNodeDetail(c.Request.Context(), c.Param("conversation_id"), c.Param("node_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) discardProposal(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		ProposalID *string `json:"proposal_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	id := ""
	if req.ProposalID != nil {
		id = *req.ProposalID
	}
	out, err := h.Service.DiscardProposalTool(c.Request.Context(), c.Param("conversation_id"), id, key)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileDiscardProposal(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		ProposalID *string `json:"proposal_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	target := map[string]any{"proposal_id": req.ProposalID}
	out, err := h.Service.ReconcileGraphTool(c.Request.Context(), c.Param("conversation_id"), discardProposalTool, key, map[string]any{}, target)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) cancelRunTool(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	out, err := h.Service.CancelRunTool(c.Request.Context(), c.Param("conversation_id"), c.Param("run_id"), key)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileCancelRun(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	out, err := h.Service.ReconcileGraphTool(c.Request.Context(), c.Param("conversation_id"), cancelRunTool, key, map[string]any{}, map[string]any{"run_id": c.Param("run_id")})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) focusCanvas(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		NodeIDs  []string `json:"node_ids"`
		EdgeIDs  []string `json:"edge_ids"`
		GroupIDs []string `json:"group_ids"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.FocusCanvasTool(c.Request.Context(), c.Param("conversation_id"), key, req.NodeIDs, req.EdgeIDs, req.GroupIDs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileFocusCanvas(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		NodeIDs  []string `json:"node_ids"`
		EdgeIDs  []string `json:"edge_ids"`
		GroupIDs []string `json:"group_ids"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReconcileGraphTool(c.Request.Context(), c.Param("conversation_id"), focusCanvasTool, key, map[string]any{}, map[string]any{
		"node_ids": req.NodeIDs, "edge_ids": req.EdgeIDs, "group_ids": req.GroupIDs,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) listRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	out, err := h.Service.ListWorkflowRuns(c.Request.Context(), c.Param("conversation_id"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) inspectRuns(c *gin.Context) {
	var req struct {
		WorkflowIDs []string `json:"workflow_ids"`
		Limit       int      `json:"limit"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.InspectWorkflowRuns(c.Request.Context(), c.Param("conversation_id"), req.WorkflowIDs, req.Limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) workflowRunDetail(c *gin.Context) {
	out, err := h.Service.WorkflowRunDetail(
		c.Request.Context(),
		c.Param("conversation_id"),
		c.Param("run_id"),
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) claimExecution(c *gin.Context) {
	var req struct {
		TaskID         *string `json:"task_id"`
		IdempotencyKey string  `json:"idempotency_key"`
		HarnessTurnID  string  `json:"harness_turn_id"`
		OwnerID        string  `json:"owner_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ClaimExecution(c.Request.Context(), c.Param("conversation_id"), req.TaskID, req.IdempotencyKey, req.HarnessTurnID, req.OwnerID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) heartbeatExecution(c *gin.Context) {
	var req struct {
		OwnerID    string `json:"owner_id"`
		LeaseToken string `json:"lease_token"`
		Phase      string `json:"phase"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.HeartbeatExecution(c.Request.Context(), c.Param("conversation_id"), c.Param("execution_id"), req.OwnerID, req.LeaseToken, req.Phase)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) appendCheckpoint(c *gin.Context) {
	var req struct {
		OwnerID    string          `json:"owner_id"`
		LeaseToken string          `json:"lease_token"`
		Sequence   int             `json:"sequence"`
		Kind       string          `json:"kind"`
		Payload    json.RawMessage `json:"payload"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.AppendCheckpoint(c.Request.Context(), c.Param("conversation_id"), c.Param("execution_id"), req.OwnerID, req.LeaseToken, req.Sequence, req.Kind, req.Payload)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) releaseExecution(c *gin.Context) {
	var req struct {
		OwnerID    string `json:"owner_id"`
		LeaseToken string `json:"lease_token"`
		Phase      string `json:"phase"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReleaseExecution(c.Request.Context(), c.Param("conversation_id"), c.Param("execution_id"), req.OwnerID, req.LeaseToken, req.Phase)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

type appendEventRequest struct {
	Sequence      int             `json:"sequence"`
	SchemaVersion int             `json:"schema_version"`
	RunID         string          `json:"run_id"`
	TurnID        string          `json:"turn_id"`
	Kind          string          `json:"kind"`
	Ignorable     bool            `json:"ignorable"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"created_at"`
}

type appendEventBatchRequest struct {
	OwnerID    string               `json:"owner_id"`
	LeaseToken string               `json:"lease_token"`
	Events     []appendEventRequest `json:"events"`
}

func (h HTTP) appendEvents(c *gin.Context) {
	var req appendEventBatchRequest
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	inputs := make([]EventAppendInput, 0, len(req.Events))
	for _, event := range req.Events {
		inputs = append(inputs, EventAppendInput{
			Sequence: event.Sequence, SchemaVersion: event.SchemaVersion, RunID: event.RunID,
			TurnID: event.TurnID, Kind: event.Kind, Ignorable: event.Ignorable,
			Payload: event.Payload, CreatedAt: event.CreatedAt,
		})
	}
	out, err := h.Service.AppendEvents(c.Request.Context(), c.Param("conversation_id"), c.Param("execution_id"), req.OwnerID, req.LeaseToken, inputs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type workflowRunRequestBody struct {
	ExpectedWorkflowRevision int      `json:"expected_workflow_revision"`
	WorkflowID               string   `json:"workflow_id"`
	SourceStepID             string   `json:"source_step_id"`
	TaskID                   *string  `json:"task_id"`
	SourceRunID              *string  `json:"source_run_id"`
	Scope                    string   `json:"scope"`
	NodeID                   *string  `json:"node_id"`
	NodeIDs                  []string `json:"node_ids"`
	Force                    bool     `json:"force"`
	DocumentAction           string   `json:"document_action"`
}

type globalWorkflowRunRequestBody struct {
	ProductID                string   `json:"product_id"`
	ExpectedWorkflowRevision int      `json:"expected_workflow_revision"`
	WorkflowID               string   `json:"workflow_id"`
	SourceStepID             string   `json:"source_step_id"`
	TaskID                   *string  `json:"task_id"`
	SourceRunID              *string  `json:"source_run_id"`
	Scope                    string   `json:"scope"`
	NodeID                   *string  `json:"node_id"`
	NodeIDs                  []string `json:"node_ids"`
	Force                    bool     `json:"force"`
	DocumentAction           string   `json:"document_action"`
}

func (h HTTP) prepareRunRequest(c *gin.Context) {
	var req struct {
		ExpectedWorkflowRevision int     `json:"expected_workflow_revision"`
		TaskID                   *string `json:"task_id"`
		SourceRunID              *string `json:"source_run_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.PrepareWorkflowRunRequest(c.Request.Context(), c.Param("conversation_id"), req.ExpectedWorkflowRevision, req.SourceRunID, req.TaskID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) prepareGlobalRunRequest(c *gin.Context) {
	var req struct {
		ProductID                string  `json:"product_id"`
		WorkflowID               string  `json:"workflow_id"`
		ExpectedWorkflowRevision int     `json:"expected_workflow_revision"`
		TaskID                   *string `json:"task_id"`
		SourceRunID              *string `json:"source_run_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.PrepareGlobalWorkflowRunRequest(c.Request.Context(), c.Param("conversation_id"), req.ProductID, req.WorkflowID, req.ExpectedWorkflowRevision, req.SourceRunID, req.TaskID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) createRunRequest(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req workflowRunRequestBody
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.CreateWorkflowRunRequest(c.Request.Context(), c.Param("conversation_id"), req.WorkflowID, key, req.SourceStepID, req.ExpectedWorkflowRevision, req.TaskID, req.SourceRunID, runScopeSpec{Scope: req.Scope, NodeID: req.NodeID, NodeIDs: req.NodeIDs, Force: req.Force, DocumentAction: req.DocumentAction})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) createGlobalRunRequest(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req globalWorkflowRunRequestBody
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.CreateGlobalWorkflowRunRequest(c.Request.Context(), c.Param("conversation_id"), req.ProductID, req.WorkflowID, key, req.SourceStepID, req.ExpectedWorkflowRevision, req.TaskID, req.SourceRunID, runScopeSpec{Scope: req.Scope, NodeID: req.NodeID, NodeIDs: req.NodeIDs, Force: req.Force, DocumentAction: req.DocumentAction})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileRunRequest(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req workflowRunRequestBody
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReconcileWorkflowRunRequest(c.Request.Context(), c.Param("conversation_id"), key, "", req.WorkflowID, req.SourceStepID, req.ExpectedWorkflowRevision, req.TaskID, req.SourceRunID, runScopeSpec{Scope: req.Scope, NodeID: req.NodeID, NodeIDs: req.NodeIDs, Force: req.Force, DocumentAction: req.DocumentAction})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileGlobalRunRequest(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req globalWorkflowRunRequestBody
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReconcileGlobalWorkflowRunRequest(c.Request.Context(), c.Param("conversation_id"), key, req.ProductID, req.WorkflowID, req.SourceStepID, req.ExpectedWorkflowRevision, req.TaskID, req.SourceRunID, runScopeSpec{Scope: req.Scope, NodeID: req.NodeID, NodeIDs: req.NodeIDs, Force: req.Force, DocumentAction: req.DocumentAction})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) listAssets(c *gin.Context) {
	limit, _ := queryInt(c, "limit", assetListDefaultLimit, 1, assetListMaxLimit)
	out, err := h.Service.ListProductAssets(c.Request.Context(), c.Param("conversation_id"), c.DefaultQuery("directory_kind", "all"), c.Query("directory_key"), c.Query("query"), c.DefaultQuery("sort", "created_desc"), c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) inspectAssets(c *gin.Context) {
	var req struct {
		AssetIDs []string `json:"asset_ids"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	items, err := h.Service.InspectProductAssets(c.Request.Context(), c.Param("conversation_id"), req.AssetIDs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h HTTP) assetContent(c *gin.Context) {
	out, err := h.Service.ReadProductAssetContent(c.Request.Context(), c.Param("conversation_id"), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, out.MediaType, out.Bytes)
}

func (h HTTP) listLibrary(c *gin.Context) {
	limit, _ := queryInt(c, "limit", assetListDefaultLimit, 1, assetListMaxLimit)
	out, err := h.Service.ListLibraryAssets(c.Request.Context(), c.Param("conversation_id"), c.Query("query"), c.Query("cursor"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) inspectLibrary(c *gin.Context) {
	var req struct {
		AssetIDs []string `json:"asset_ids"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	items, err := h.Service.InspectLibraryAssets(c.Request.Context(), c.Param("conversation_id"), req.AssetIDs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h HTTP) libraryContent(c *gin.Context) {
	out, err := h.Service.ReadLibraryAssetContent(c.Request.Context(), c.Param("conversation_id"), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, out.MediaType, out.Bytes)
}

func (h HTTP) listProducts(c *gin.Context) {
	limit, err := queryInt(c, "limit", assetListDefaultLimit, 1, globalProductListMax)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ListGlobalProducts(c.Request.Context(), c.Param("conversation_id"), c.Query("query"), c.Query("cursor"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) inspectProducts(c *gin.Context) {
	var req struct {
		ProductIDs []string `json:"product_ids"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	items, err := h.Service.InspectGlobalProducts(c.Request.Context(), c.Param("conversation_id"), req.ProductIDs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h HTTP) createWorkspace(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.LaunchWorkspaceFromGlobal(c.Request.Context(), c.Param("conversation_id"), req.Name, key)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) reconcileWorkspace(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReconcileWorkspaceFromGlobal(c.Request.Context(), c.Param("conversation_id"), req.Name, key)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

type productIntakeBody struct {
	Selection         json.RawMessage `json:"selection"`
	ReferenceAssetIDs []string        `json:"reference_asset_ids"`
	TaskID            *string         `json:"task_id"`
}

func validateProductIntakeBody(req productIntakeBody) ([]string, error) {
	if _, err := product.ParseSelection(string(req.Selection)); err != nil {
		return nil, err
	}
	if len(req.ReferenceAssetIDs) == 0 {
		return nil, apperr.Validation("至少选择一张参考图")
	}
	if len(req.ReferenceAssetIDs) > 6 {
		return nil, apperr.Validation("参考图最多上传 6 张")
	}
	ids := make([]string, 0, len(req.ReferenceAssetIDs))
	for _, raw := range req.ReferenceAssetIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, apperr.Validation("参考图片 ID 不能为空")
		}
		if len(id) > 36 {
			return nil, apperr.Validation("参考图片 ID 无效")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (h HTTP) finalizeIntake(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req productIntakeBody
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	ids, err := validateProductIntakeBody(req)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.FinalizeProductIntake(c.Request.Context(), c.Param("conversation_id"), key, req.Selection, ids)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) reconcileIntake(c *gin.Context) {
	key, ok := requireIdempotency(c)
	if !ok {
		return
	}
	var req productIntakeBody
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	ids, err := validateProductIntakeBody(req)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	_ = req.TaskID
	out, err := h.Service.ReconcileTool(c.Request.Context(), c.Param("conversation_id"), "finalize_product_intake_v1", key, toolPrepared(c.Param("conversation_id"), "finalize_product_intake_v1", map[string]any{}, map[string]any{
		"selection": req.Selection, "reference_asset_ids": ids,
	}))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
