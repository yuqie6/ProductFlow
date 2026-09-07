package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	applyGraphTool      = "apply_graph_change_set_v1"
	proposeGraphTool    = "propose_graph_change_set_v1"
	discardProposalTool = "discard_workflow_proposal_v1"
	cancelRunTool       = "cancel_workflow_run_v1"
	focusCanvasTool     = "focus_canvas_items_v1"
)

func (s Service) loadScopedConversation(ctx context.Context, conversationID string) (context.Context, conversationRow, error) {
	var conv conversationRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		// 内部工具可能尚无商家上下文：先按 id 读出合同商家，再绑定。
		var rec schema.AgentConversations
		scanErr := pgxTx.WithContext(ctx).Where("id = ?", conversationID).Take(&rec).Error
		if errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return apperr.NotFound("Agent conversation 不存在")
		}
		if scanErr != nil {
			return scanErr
		}
		conv = conversationFromSchema(rec)
		return nil
	})
	if err != nil {
		return ctx, conversationRow{}, err
	}
	ctx, err = bindConversationMerchant(ctx, conv)
	return ctx, conv, err
}

func requireProductWorkflow(conv conversationRow) error {
	if conv.ScopeType != "product_workflow" || conv.ProductID == nil {
		return apperr.Conflict("当前 conversation 不是商品工作流作用域")
	}
	return nil
}

func requireGlobalScope(conv conversationRow) error {
	if conv.ScopeType != "global" {
		return apperr.Conflict("只有全局 Agent conversation 可以使用该工具")
	}
	return nil
}

func graphChangeSetPrepared(changeSet json.RawMessage) (before, target map[string]any, parsed graph.ChangeSet, err error) {
	parsed, err = graph.ParseChangeSet(changeSet)
	if err != nil {
		return nil, nil, graph.ChangeSet{}, err
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		return nil, nil, graph.ChangeSet{}, err
	}
	if err := json.Unmarshal(raw, &target); err != nil {
		return nil, nil, graph.ChangeSet{}, err
	}
	before = map[string]any{"base_graph_revision": parsed.BaseGraphRevision}
	return before, target, parsed, nil
}

// ApplyGraphTool 经 graph 包立即应用 ChangeSet；相同幂等键回放，不直接写 graph 表。ChangeSet 或幂等键无效返回 Validation。无 live 图、非商品工作流或同键不同请求返回 Conflict。
func (s Service) ApplyGraphTool(ctx context.Context, conversationID string, changeSet json.RawMessage, idempotencyKey string) (map[string]any, error) {
	before, targetMap, parsed, err := graphChangeSetPrepared(changeSet)
	if err != nil {
		return nil, err
	}
	replay, found, err := s.lookupMutation(ctx, conversationID, applyGraphTool, idempotencyKey, applyGraphTool, before, targetMap)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	live, err := s.Graph.TryCurrent(ctx, *conv.ProductID)
	if err != nil || live == nil {
		return nil, apperr.Conflict("当前商品还没有可执行的工作流")
	}
	applied, err := s.Graph.ApplyAgentChangeSet(ctx, *conv.ProductID, live.ID, parsed)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"accepted": true, "applied": true, "graph_id": applied.ID, "revision": applied.Revision, "summary": parsed.Summary,
	}
	if err := s.recordMutation(ctx, conversationID, applyGraphTool, idempotencyKey, applyGraphTool, before, targetMap, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// ProposeGraphTool 经 graph 包创建待确认提案；相同幂等键回放。ChangeSet 无效返回 Validation。无 live 图、revision 已变或已有未应用提案返回 Conflict。
func (s Service) ProposeGraphTool(ctx context.Context, conversationID string, changeSet json.RawMessage, idempotencyKey string) (map[string]any, error) {
	before, targetMap, parsed, err := graphChangeSetPrepared(changeSet)
	if err != nil {
		return nil, err
	}
	replay, found, err := s.lookupMutation(ctx, conversationID, proposeGraphTool, idempotencyKey, proposeGraphTool, before, targetMap)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	proposal, err := s.Graph.CreateAgentProposal(ctx, *conv.ProductID, conversationID, parsed)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"accepted": true, "proposal_id": proposal.ID, "graph_id": proposal.GraphID,
		"base_graph_revision": proposal.BaseGraphRevision, "summary": proposal.Summary,
	}
	if err := s.recordMutation(ctx, conversationID, proposeGraphTool, idempotencyKey, proposeGraphTool, before, targetMap, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// DiscardProposalTool 经 graph 包丢弃待处理提案；相同幂等键回放。无 live 图、没有待处理提案或非商品工作流返回 Conflict。提案非 pending 返回 NotPending。
func (s Service) DiscardProposalTool(ctx context.Context, conversationID string, proposalID, idempotencyKey string) (map[string]any, error) {
	target := map[string]any{"proposal_id": nullable(proposalID)}
	replay, found, err := s.lookupMutation(ctx, conversationID, discardProposalTool, idempotencyKey, discardProposalTool, map[string]any{}, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	live, err := s.Graph.TryCurrent(ctx, *conv.ProductID)
	if err != nil || live == nil {
		return nil, apperr.Conflict("当前商品还没有可执行的工作流")
	}
	resolved := proposalID
	if resolved == "" && live.PendingProposal != nil {
		resolved = live.PendingProposal.ID
	}
	if resolved == "" {
		return nil, apperr.Conflict("没有待处理的图提案")
	}
	if _, err := s.Graph.DiscardProposal(ctx, *conv.ProductID, live.ID, resolved); err != nil {
		return nil, err
	}
	result := map[string]any{"accepted": true, "discarded": true, "proposal_id": resolved, "graph_id": live.ID}
	if err := s.recordMutation(ctx, conversationID, discardProposalTool, idempotencyKey, discardProposalTool, map[string]any{}, target, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// CancelRunTool 经 graph 包取消 WorkflowGraphRun；相同幂等键回放。无 live 图、非商品工作流或 GraphRun 已结束返回 Conflict。run 不存在返回 NotFound。
func (s Service) CancelRunTool(ctx context.Context, conversationID, runID, idempotencyKey string) (map[string]any, error) {
	target := map[string]any{"run_id": runID}
	replay, found, err := s.lookupMutation(ctx, conversationID, cancelRunTool, idempotencyKey, cancelRunTool, map[string]any{}, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	live, err := s.Graph.TryCurrent(ctx, *conv.ProductID)
	if err != nil || live == nil {
		return nil, apperr.Conflict("当前商品还没有可执行的工作流")
	}
	run, err := s.Graph.CancelRun(ctx, *conv.ProductID, live.ID, runID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"accepted": true, "run_id": run.ID, "status": run.Status}
	if err := s.recordMutation(ctx, conversationID, cancelRunTool, idempotencyKey, cancelRunTool, map[string]any{}, target, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// FocusCanvasTool 记录画布聚焦请求；不改图，相同幂等键回放。聚焦项为空或超过 20 个返回 Validation。conversation 不存在返回 NotFound；同键不同请求返回 Conflict。
func (s Service) FocusCanvasTool(ctx context.Context, conversationID, idempotencyKey string, nodeIDs, edgeIDs, groupIDs []string) (map[string]any, error) {
	if len(nodeIDs)+len(edgeIDs)+len(groupIDs) == 0 {
		return nil, apperr.Validation("画布聚焦至少需要一个节点、边或分组")
	}
	if len(nodeIDs)+len(edgeIDs)+len(groupIDs) > 20 {
		return nil, apperr.Validation("画布聚焦项目不能超过 20 个")
	}
	target := map[string]any{"node_ids": nodeIDs, "edge_ids": edgeIDs, "group_ids": groupIDs}
	replay, found, err := s.lookupMutation(ctx, conversationID, focusCanvasTool, idempotencyKey, focusCanvasTool, map[string]any{}, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	result := map[string]any{
		"accepted": true, "request_id": newID(), "node_ids": nodeIDs, "edge_ids": edgeIDs, "group_ids": groupIDs,
	}
	if err := s.recordMutation(ctx, conversationID, focusCanvasTool, idempotencyKey, focusCanvasTool, map[string]any{}, target, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// ReconcileGraphTool 按幂等键只读对账图工具副作用，转给 ReconcileTool。
//
// agent-service 崩溃恢复时调用。不重放 ChangeSet、不延长 lease、不改 Goal。State 见 ReconcileResponse。
// conversation 不存在 NotFound；同键不同目标 Conflict。
func (s Service) ReconcileGraphTool(ctx context.Context, conversationID, toolName, idempotencyKey string, before, target map[string]any) (ReconcileResponse, error) {
	return s.ReconcileTool(ctx, conversationID, toolName, idempotencyKey, toolPrepared(conversationID, toolName, before, target))
}

// GetNodeDetail 读取商品 live 图上一个节点的投影，给内部 GET .../graph/nodes/:node_id。
//
// conversation 必须是 product_workflow。无 live 图或节点不在图上返回 NotFound。只读，不写 Graph Command、lease、journal。
func (s Service) GetNodeDetail(ctx context.Context, conversationID, nodeID string) (map[string]any, error) {
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	live, err := s.Graph.TryCurrent(ctx, *conv.ProductID)
	if err != nil || live == nil {
		return nil, apperr.NotFound("节点不存在")
	}
	for _, node := range live.Nodes {
		if node.ID == nodeID {
			raw, _ := json.Marshal(node)
			var out map[string]any
			_ = json.Unmarshal(raw, &out)
			return out, nil
		}
	}
	return nil, apperr.NotFound("节点不存在")
}

// ListWorkflowRuns 列出当前商品 live 图的 GraphRun。conversation 不存在返回 NotFound。非商品工作流返回 Conflict。
func (s Service) ListWorkflowRuns(ctx context.Context, conversationID string, limit int) (map[string]any, error) {
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	live, err := s.Graph.TryCurrent(ctx, *conv.ProductID)
	if err != nil {
		return nil, err
	}
	if live == nil {
		return map[string]any{"workflow_id": nil, "workflow_revision": 0, "items": []any{}}, nil
	}
	listed, err := s.Graph.ListRuns(ctx, *conv.ProductID, live.ID)
	if err != nil {
		return nil, err
	}
	items := listed.Items
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"workflow_id": live.ID, "workflow_revision": live.Revision, "items": items}, nil
}

// InspectWorkflowRuns 供全局 Agent 按 workflow id 检查有界 GraphRun 列表。conversation 或工作流不存在返回 NotFound。非全局 conversation 返回 Conflict。
func (s Service) InspectWorkflowRuns(ctx context.Context, conversationID string, workflowIDs []string, limit int) (map[string]any, error) {
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 5
	}
	items := make([]map[string]any, 0, len(workflowIDs))
	for _, workflowID := range workflowIDs {
		var g schema.WorkflowGraphs
		err := s.DB.WithContext(ctx).Select("product_id, title, revision").
			Where("id = ? AND active = TRUE", workflowID).Take(&g).Error
		if err != nil {
			return nil, apperr.NotFound("工作流不存在")
		}
		productID, title, revision := g.ProductID, g.Title, g.Revision
		listed, err := s.Graph.ListRuns(ctx, productID, workflowID)
		if err != nil {
			return nil, err
		}
		runs := listed.Items
		if len(runs) > limit {
			runs = runs[:limit]
		}
		items = append(items, map[string]any{
			"workflow_id": workflowID, "workflow_title": title, "workflow_revision": revision, "items": runs,
		})
	}
	return map[string]any{"items": items}, nil
}

// WorkflowRunDetail 读取一次 GraphRun 的有界摘要（无媒体 bytes），给内部 GET .../workflow-runs/:run_id。
//
// 商品对话只看本商品；全局对话按 run 反查所属商品。runID 空返回 Validation；找不到 NotFound。
// 不取消 run、不改 Goal。artifact 只保留身份字段。
func (s Service) WorkflowRunDetail(ctx context.Context, conversationID, runID string) (map[string]any, error) {
	ctx, conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, apperr.Validation("请求参数无效")
	}
	var ownerProduct, wantWorkflow string
	switch conv.ScopeType {
	case "product_workflow":
		if err := requireProductWorkflow(conv); err != nil {
			return nil, err
		}
		ownerProduct = *conv.ProductID
	default:
		if err := requireGlobalScope(conv); err != nil {
			return nil, err
		}
		var owner struct {
			ProductID  string
			WorkflowID string
		}
		err := s.DB.WithContext(ctx).
			Table("workflow_graph_runs AS run").
			Select("graph.product_id AS product_id, graph.id AS workflow_id").
			Joins("JOIN workflow_graphs AS graph ON graph.id = run.graph_id").
			Where("run.id = ?", runID).
			Take(&owner).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("工作流运行不存在")
		}
		if err != nil {
			return nil, err
		}
		ownerProduct, wantWorkflow = owner.ProductID, owner.WorkflowID
	}
	run, err := s.Graph.GetRunForProduct(ctx, ownerProduct, runID, wantWorkflow)
	if err != nil {
		return nil, err
	}
	return boundedWorkflowRunDetail(run), nil
}

// boundedWorkflowRunDetail 把 GraphRun 收成有界摘要，避免整份节点 artifact 进入 Agent 上下文。
func boundedWorkflowRunDetail(run graph.GraphRunResponse) map[string]any {
	nodes := make([]map[string]any, 0, len(run.NodeRuns))
	for _, node := range run.NodeRuns {
		item := map[string]any{
			"node_id":        node.NodeID,
			"node_title":     node.NodeTitle,
			"status":         node.Status,
			"failure_reason": node.FailureReason,
			"planned_action": node.PlannedAction,
			"progress_phase": node.ProgressPhase,
			"attempt_count":  node.AttemptCount,
		}
		if summary := boundedNodeArtifact(node.Output); summary != nil {
			item["artifact"] = summary
		}
		nodes = append(nodes, item)
	}
	return map[string]any{
		"schema_version": 1,
		"run_id":         run.ID,
		"workflow_id":    run.GraphID,
		"status":         run.Status,
		"scope":          run.Scope,
		"graph_revision": run.GraphRevision,
		"failure_reason": run.FailureReason,
		"is_retryable":   run.IsRetryable,
		"started_at":     run.StartedAt,
		"finished_at":    run.FinishedAt,
		"nodes":          nodes,
	}
}

// boundedNodeArtifact 只暴露 artifact 身份字段或有界 key 列表，不传媒体 bytes。
func boundedNodeArtifact(output map[string]any) map[string]any {
	if len(output) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"asset_id", "artifact_id", "artifact_type", "status"} {
		if value, ok := output[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		keys := make([]string, 0, len(output))
		for key := range output {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 8 {
			keys = keys[:8]
		}
		out["keys"] = keys
	}
	return out
}
