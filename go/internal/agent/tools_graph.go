package agent

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

const (
	applyGraphTool     = "apply_graph_change_set_v1"
	proposeGraphTool   = "propose_graph_change_set_v1"
	discardProposalTool = "discard_workflow_proposal_v1"
	cancelRunTool      = "cancel_workflow_run_v1"
	focusCanvasTool    = "focus_canvas_items_v1"
)

func (s Service) loadScopedConversation(ctx context.Context, conversationID string) (conversationRow, error) {
	var conv conversationRow
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		loaded, err := loadConversationByID(ctx, pgxTx, conversationID)
		conv = loaded
		return err
	})
	return conv, err
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

func (s Service) ApplyGraphTool(ctx context.Context, conversationID string, changeSet json.RawMessage, idempotencyKey string) (map[string]any, error) {
	parsed, err := graph.ParseChangeSet(changeSet)
	if err != nil {
		return nil, err
	}
	target, _ := json.Marshal(parsed)
	var targetMap map[string]any
	_ = json.Unmarshal(target, &targetMap)
	before := map[string]any{"base_graph_revision": parsed.BaseGraphRevision}
	replay, found, err := s.lookupMutation(ctx, conversationID, applyGraphTool, idempotencyKey, applyGraphTool, before, targetMap)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
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

func (s Service) ProposeGraphTool(ctx context.Context, conversationID string, changeSet json.RawMessage, idempotencyKey string) (map[string]any, error) {
	parsed, err := graph.ParseChangeSet(changeSet)
	if err != nil {
		return nil, err
	}
	target, _ := json.Marshal(parsed)
	var targetMap map[string]any
	_ = json.Unmarshal(target, &targetMap)
	before := map[string]any{"base_graph_revision": parsed.BaseGraphRevision}
	replay, found, err := s.lookupMutation(ctx, conversationID, proposeGraphTool, idempotencyKey, proposeGraphTool, before, targetMap)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
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

func (s Service) DiscardProposalTool(ctx context.Context, conversationID string, proposalID, idempotencyKey string) (map[string]any, error) {
	target := map[string]any{"proposal_id": nullable(proposalID)}
	replay, found, err := s.lookupMutation(ctx, conversationID, discardProposalTool, idempotencyKey, discardProposalTool, map[string]any{}, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
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

func (s Service) CancelRunTool(ctx context.Context, conversationID, runID, idempotencyKey string) (map[string]any, error) {
	target := map[string]any{"run_id": runID}
	replay, found, err := s.lookupMutation(ctx, conversationID, cancelRunTool, idempotencyKey, cancelRunTool, map[string]any{}, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
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

func (s Service) ReconcileGraphTool(ctx context.Context, conversationID, toolName, idempotencyKey string, before, target map[string]any) (ReconcileResponse, error) {
	return s.ReconcileTool(ctx, conversationID, toolName, idempotencyKey, toolPrepared(conversationID, toolName, before, target))
}

func (s Service) GetNodeDetail(ctx context.Context, conversationID, nodeID string) (map[string]any, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
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

func (s Service) ListWorkflowRuns(ctx context.Context, conversationID string, limit int) (map[string]any, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
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

func (s Service) InspectWorkflowRuns(ctx context.Context, conversationID string, workflowIDs []string, limit int) (map[string]any, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
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
		var productID, title string
		var revision int
		err := s.Pool.QueryRow(ctx, `
			SELECT product_id, title, revision FROM workflow_graphs WHERE id = $1 AND active = TRUE
		`, workflowID).Scan(&productID, &title, &revision)
		if err != nil {
			return nil, apperr.NotFound("工作流不存在")
		}
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
