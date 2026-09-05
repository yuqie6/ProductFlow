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

// registerInternal 挂上 /api/internal/v1 内部工具面，全部走 requireInternal 的 Bearer token 门闩。
//
// 调用方是 Node.js/Pi agent-service（claim lease、追加 journal、图工具、读合同）。浏览器不要打这些路径，请走 Register 的 /api/v2 管理员 session 路由。
// InternalToken 未配置时整组 503。
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
	conv.POST("/turn-executions/:execution_id/effects/reconcile", h.reconcileExecutionEffect)
	conv.POST("/turn-executions/:execution_id/release", h.releaseExecution)
	conv.POST("/turn-executions/:execution_id/events/batch", h.appendEvents)
	conv.POST("/turn-executions/:execution_id/events/confirm", h.confirmEvents)

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

// requireInternal 是 /api/internal/v1 的 token 门闩：Authorization 必须是 Bearer + InternalToken（恒定时间比较）。
//
// InternalToken 空则 503「Agent 内部服务尚未配置」。scheme/token 不对则 401，并带 WWW-Authenticate: Bearer。浏览器 session cookie 过不了这扇门。
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

// providerConfig 处理 GET /api/internal/v1/agent-runtime/provider-config。
//
// 200 返回当前工作流 Agent 供应商绑定（给 Pi 用）。Settings 空或未绑定 503。浏览器不要打。不改 Goal / lease。
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

// taskContract 处理 GET /api/internal/v1/agent-tasks/:task_id/contract。
//
// 200 返回 ContractResponse（system prompt + 工具合同）。找不到 404；已取消 Task 409。只读，不 claim lease。
func (h HTTP) taskContract(c *gin.Context) {
	out, err := h.Service.TaskContract(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// conversationContract 处理 GET /api/internal/v1/agent-conversations/:conversation_id/contract。
//
// 200 返回 ContractResponse。找不到 404。只读 prompt / 工具合同 / Draft schema，不写 journal。
func (h HTTP) conversationContract(c *gin.Context) {
	out, err := h.Service.ConversationContract(c.Request.Context(), c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// runtimeContext 处理 GET /api/internal/v1/agent-conversations/:conversation_id/runtime-context。
//
// 200 返回 RuntimeContextResponse（有界 Session/Task summary）。query task_id 可选。找不到 404。不是 journal，也不是 Goal 正文。
func (h HTTP) runtimeContext(c *gin.Context) {
	out, err := h.Service.RuntimeContext(c.Request.Context(), c.Param("conversation_id"), queryOpt(c, "task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// productContext 处理 GET /api/internal/v1/agent-conversations/:conversation_id/product-context。
//
// 200 返回商品有界上下文。query response_format 可选。必须是商品对话，否则 409；找不到 404。只读。
func (h HTTP) productContext(c *gin.Context) {
	out, err := h.Service.ProductContext(c.Request.Context(), c.Param("conversation_id"), c.Query("response_format"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// globalWorkflowContext 处理 GET /api/internal/v1/agent-conversations/:conversation_id/global-workflow-context。
//
// 200 返回指定商品工作流的有界上下文。query product_id 必填语义由 Service 校验；response_format 可选。必须是 global conversation，否则 409。
func (h HTTP) globalWorkflowContext(c *gin.Context) {
	out, err := h.Service.GlobalWorkflowContext(c.Request.Context(), c.Param("conversation_id"), c.Query("product_id"), c.Query("response_format"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// validateGlobalDraft 处理 POST /api/internal/v1/agent-conversations/:conversation_id/global-draft/validate。
//
// 200 返回 {"accepted":true}。JSON {"value"}。当前只接受素材整理 Draft，其它 kind 400。只校验不落库、不改 Goal。
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

// applyGraph 处理 POST /api/internal/v1/agent-conversations/:conversation_id/graph/apply-change-set。
//
// 200 返回已应用 ChangeSet 回执。必须带 Idempotency-Key，缺则 400。JSON {"change_set"}。解析失败 400；同键不同目标 409。
// 经 graph 包写入，不直接 SQL。不完成 Goal、不延长 lease。
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

// reconcileApplyGraph 处理 POST /api/internal/v1/agent-conversations/:conversation_id/graph/apply-change-set/reconcile。
//
// 转给 reconcileGraphChange(apply)。200 ReconcileResponse。只读账本，不重放 ChangeSet。
func (h HTTP) reconcileApplyGraph(c *gin.Context) {
	h.reconcileGraphChange(c, applyGraphTool)
}

// proposeGraph 处理 POST /api/internal/v1/agent-conversations/:conversation_id/graph/proposals。
//
// 200 返回提案回执。必须带 Idempotency-Key。JSON {"change_set"}。未知字段 400。提案不是已应用的图，也不完成 Goal。
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

// reconcileProposeGraph 处理 POST /api/internal/v1/agent-conversations/:conversation_id/graph/proposals/reconcile。
//
// 转给 reconcileGraphChange(propose)。200 ReconcileResponse。只读账本。
func (h HTTP) reconcileProposeGraph(c *gin.Context) {
	h.reconcileGraphChange(c, proposeGraphTool)
}

// reconcileGraphChange 是 apply/propose 对账的共用实现。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。JSON {"change_set"}。不重放副作用、不延长 lease、不改 Goal。
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
	before, target, _, err := graphChangeSetPrepared(req.ChangeSet)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReconcileGraphTool(c.Request.Context(), c.Param("conversation_id"), toolName, key, before, target)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// nodeDetail 处理 GET /api/internal/v1/agent-conversations/:conversation_id/graph/nodes/:node_id。
//
// 200 返回节点投影。必须是商品对话，否则 409；无 live 图或节点不存在 404。只读。
func (h HTTP) nodeDetail(c *gin.Context) {
	out, err := h.Service.GetNodeDetail(c.Request.Context(), c.Param("conversation_id"), c.Param("node_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// discardProposal 处理 POST /api/internal/v1/agent-conversations/:conversation_id/graph/proposals/discard。
//
// 200 返回丢弃回执。必须带 Idempotency-Key。JSON proposal_id 可选。找不到或状态不允许 404/409。不改 Goal。
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

// reconcileDiscardProposal 处理 POST /api/internal/v1/agent-conversations/:conversation_id/graph/proposals/discard/reconcile。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。只读账本，不重放 discard。
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

// cancelRunTool 处理 POST /api/internal/v1/agent-conversations/:conversation_id/workflow-runs/:run_id/cancel。
//
// 200 返回取消回执。必须带 Idempotency-Key。经 graph 包取消 GraphRun。取消 run 不取消 Goal。
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

// reconcileCancelRun 处理 POST /api/internal/v1/agent-conversations/:conversation_id/workflow-runs/:run_id/cancel/reconcile。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。只读账本，不重放取消。
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

// focusCanvas 处理 POST /api/internal/v1/agent-conversations/:conversation_id/canvas/focus。
//
// 200 返回聚焦请求回执。必须带 Idempotency-Key。JSON node_ids / edge_ids / group_ids，合计 1–20。不改图、不写 journal。
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

// reconcileFocusCanvas 处理 POST /api/internal/v1/agent-conversations/:conversation_id/canvas/focus/reconcile。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。只读账本。
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

// listRuns 处理 GET /api/internal/v1/agent-conversations/:conversation_id/workflow-runs。
//
// 200 返回 {workflow_id, workflow_revision, items}。query limit 默认 20。必须是商品对话，否则 409。无 live 图时 items 为空。只读，不改 Goal。
func (h HTTP) listRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	out, err := h.Service.ListWorkflowRuns(c.Request.Context(), c.Param("conversation_id"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// inspectRuns 处理 POST /api/internal/v1/agent-conversations/:conversation_id/workflow-runs/inspect。
//
// 200 返回 {"items":[...]}。JSON workflow_ids 与 limit。必须是 global conversation，否则 409；工作流不存在 404。按明确 id 检查，不要列全库。
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

// workflowRunDetail 处理 GET /api/internal/v1/agent-conversations/:conversation_id/workflow-runs/:run_id。
//
// 200 返回有界 GraphRun 摘要（无媒体 bytes）。run_id 空 400；找不到 404。不取消 run、不改 Goal。
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

// claimExecution 处理 POST /api/internal/v1/agent-conversations/:conversation_id/turn-executions/claim。
//
// 200 返回 ExecutionLeaseResponse。JSON task_id 可选，idempotency_key / harness_turn_id / owner_id 必填。体非法 400；projection 不存在 404；终态或他人持有有效 lease 409。
// 同一 owner 未过期 lease 回放且不递增 fencing_token。不改 Goal。浏览器不要打。
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

// heartbeatExecution 处理 POST /api/internal/v1/agent-conversations/:conversation_id/turn-executions/:execution_id/heartbeat。
//
// 200 返回 ExecutionLeaseResponse（延长 lease）。JSON owner_id / lease_token / phase。token 错配或过期 409。不追加 journal、不改 Goal。
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

// appendCheckpoint 处理 POST /api/internal/v1/agent-conversations/:conversation_id/turn-executions/:execution_id/checkpoints。
//
// 200 返回 CheckpointResponse。JSON owner_id / lease_token / sequence / kind / payload。lease 错配或序号冲突 409。不延长 lease、不写 agent_turn_events、不改 Goal。
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

// reconcileExecutionEffect 处理 live mutation 5xx 后的 lease-scoped 副作用对账。
//
// 200 返回 EffectReconciliationResponse。JSON owner_id / lease_token / tool_call_id；intent 和 recovery_policy 只从当前 execution 的 PG checkpoint 读取。
func (h HTTP) reconcileExecutionEffect(c *gin.Context) {
	var req struct {
		OwnerID    string `json:"owner_id"`
		LeaseToken string `json:"lease_token"`
		ToolCallID string `json:"tool_call_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReconcileExecutionEffect(c.Request.Context(), c.Param("conversation_id"), c.Param("execution_id"), req.OwnerID, req.LeaseToken, req.ToolCallID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// releaseExecution 处理 POST /api/internal/v1/agent-conversations/:conversation_id/turn-executions/:execution_id/release。
//
// 200 返回释放回执。JSON owner_id / lease_token / phase。token 错配 409。释放 lease 不改 Goal，也不删 journal。
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

type confirmEventBatchRequest struct {
	Events []appendEventRequest `json:"events"`
}

// appendEvents 处理 POST /api/internal/v1/agent-conversations/:conversation_id/turn-executions/:execution_id/events/batch。
//
// 200 返回 {"items":[]EventReceipt}。JSON owner_id / lease_token / events。lease/fencing 错配或 sequence 冲突 409（可能带 code=event_sequence_conflict）。
// 写入 PostgreSQL journal，这是浏览器 SSE 的权威。不完成 Goal。浏览器不要打。
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

// confirmEvents 处理 POST /api/internal/v1/agent-conversations/:conversation_id/turn-executions/:execution_id/events/confirm。
//
// 200 返回 EventConfirmationResponse。JSON {"events":[...]}，不带 lease_token，也不延长 lease。比较本地后缀与 PG journal。
// 若已见到 turn/end，Terminal 带投影终态。不改 Goal。
func (h HTTP) confirmEvents(c *gin.Context) {
	var req confirmEventBatchRequest
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
	out, err := h.Service.ConfirmEvents(c.Request.Context(), c.Param("conversation_id"), c.Param("execution_id"), inputs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
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

// prepareRunRequest 处理 POST /api/internal/v1/agent-conversations/:conversation_id/workflow-run-requests/prepare。
//
// 200 返回 PreparedWorkflowRunRequest。JSON expected_workflow_revision，task_id / source_run_id 可选。不创建确认单、不提交 GraphRun、不改 Goal。
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

// prepareGlobalRunRequest 处理 POST /api/internal/v1/agent-conversations/:conversation_id/global-workflow-run-requests/prepare。
//
// 200 返回 PreparedWorkflowRunRequest。JSON product_id / workflow_id / expected_workflow_revision，task_id / source_run_id 可选。必须是 global conversation。不创建 run。
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

// createRunRequest 处理 POST /api/internal/v1/agent-conversations/:conversation_id/workflow-run-requests。
//
// 200 返回 WorkflowRunRequestResponse（awaiting_confirmation）。必须带 Idempotency-Key。同键同 hash 回放；同键不同 hash 409。
// 只写确认单，不立刻跑图，也不完成 Goal。用户确认走 /api/v2 .../confirm。
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

// createGlobalRunRequest 处理 POST /api/internal/v1/agent-conversations/:conversation_id/global-workflow-run-requests。
//
// 200 返回 WorkflowRunRequestResponse。必须带 Idempotency-Key。JSON 含 product_id。必须是 global conversation。不完成 Goal。
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

// reconcileRunRequest 处理 POST /api/internal/v1/agent-conversations/:conversation_id/workflow-run-requests/reconcile。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。只读账本，不重放创建、不提交 GraphRun、不改 Goal。
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

// reconcileGlobalRunRequest 处理 POST /api/internal/v1/agent-conversations/:conversation_id/global-workflow-run-requests/reconcile。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。只读账本。必须是 global conversation。
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

// listAssets 处理 GET /api/internal/v1/agent-conversations/:conversation_id/assets。
//
// 200 返回 AssetListResponse（商品图元数据，无 bytes）。query directory_kind 默认 all；sort 默认 created_desc；after 是 opaque cursor 不是页码；limit 默认 50、上限 100。
// 必须是商品对话。不要把整页塞进模型上下文。
func (h HTTP) listAssets(c *gin.Context) {
	limit, _ := queryInt(c, "limit", assetListDefaultLimit, 1, assetListMaxLimit)
	out, err := h.Service.ListProductAssets(c.Request.Context(), c.Param("conversation_id"), c.DefaultQuery("directory_kind", "all"), c.Query("directory_key"), c.Query("query"), c.DefaultQuery("sort", "created_desc"), c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// inspectAssets 处理 POST /api/internal/v1/agent-conversations/:conversation_id/assets/inspect。
//
// 200 返回 {"items":[]AssetMetadata}。JSON {"asset_ids"}。必须明确给 id，空列表 400。必须是商品对话。无 bytes。
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

// assetContent 处理 GET /api/internal/v1/agent-conversations/:conversation_id/assets/:asset_id/content。
//
// 200 返回原图 bytes（Content-Type=媒体 MIME），不是 JSON。Cache-Control=private,no-store。未核验或不属于该商品 404。浏览器请走媒体 URL。
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

// listLibrary 处理 GET /api/internal/v1/agent-conversations/:conversation_id/media-library。
//
// 200 返回 LibraryAssetListResponse；素材 cursor 与目录 folders_after_id 独立分页，include_archived 可显式包含归档素材。
// folder_query 查目标目录，workflow_id 返回该工作流对本页素材的关联事实。limit 默认 50、上限 100。非 global 返回 409。
func (h HTTP) listLibrary(c *gin.Context) {
	limit, err := queryInt(c, "limit", assetListDefaultLimit, 1, assetListMaxLimit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	archived := c.Query("include_archived")
	if archived != "" && archived != "true" && archived != "false" {
		httpx.AbortErr(c, apperr.Validation("include_archived 必须为布尔值"))
		return
	}
	out, err := h.Service.ListLibraryAssets(c.Request.Context(), c.Param("conversation_id"), c.Query("query"), c.Query("cursor"), limit, LibraryReadOptions{
		IncludeArchived: archived == "true", FolderQuery: c.Query("folder_query"), FoldersAfterID: c.Query("folders_after_id"), WorkflowID: c.Query("workflow_id"),
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// inspectLibrary 处理 POST /api/internal/v1/agent-conversations/:conversation_id/media-library/inspect。
//
// 200 返回 {"items":[]AssetMetadata}。JSON {"asset_ids"}。必须是 global conversation。无 bytes。
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

// libraryContent 处理 GET /api/internal/v1/agent-conversations/:conversation_id/media-library/:asset_id/content。
//
// 200 返回原图 bytes。必须是 global conversation。找不到或未核验 404。Cache-Control=private,no-store。
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

// listProducts 处理 GET /api/internal/v1/agent-conversations/:conversation_id/products。
//
// 200 返回 GlobalProductListResponse。query query / cursor（opaque 不是页码）；limit 默认 50、上限 100。limit 非法 400；必须是 global conversation。
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

// inspectProducts 处理 POST /api/internal/v1/agent-conversations/:conversation_id/products/inspect。
//
// 200 返回 {"items":[]GlobalProductResponse}。JSON {"product_ids"}。必须是 global conversation。按明确 id 检查，不要扫全库。
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

// createWorkspace 处理 POST /api/internal/v1/agent-conversations/:conversation_id/product-workspaces。
//
// 201 返回 WorkspaceLaunchResponse。必须带 Idempotency-Key。JSON {"name"}。Created=false 是幂等回放。同键不同 name 409。不创建 Goal。
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

// reconcileWorkspace 处理 POST /api/internal/v1/agent-conversations/:conversation_id/product-workspaces/reconcile。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。只读账本，不重放创建。
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

// validateProductIntakeBody 校验 finalize intake 的 selection 与 1–6 张参考图 ID。
// 只做边界检查，不写商品、图或工具账本。
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

// finalizeIntake 处理 POST /api/internal/v1/agent-conversations/:conversation_id/product-intake。
//
// 200 返回 intake 回执（可能展开姓名-only 出生图）。必须带 Idempotency-Key。JSON selection 与 1–6 张 reference_asset_ids。校验失败 400。
// 经 product.ApplyIntake 写商品与图，不完成 Goal。
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

// reconcileIntake 处理 POST /api/internal/v1/agent-conversations/:conversation_id/product-intake/reconcile。
//
// 200 返回 ReconcileResponse。必须带 Idempotency-Key。selection / 参考图校验与 finalize 相同。只读账本，不重放 ApplyIntake、不改 Goal。
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
