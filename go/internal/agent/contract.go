package agent

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

//go:embed global_draft_schema.json
var globalDraftSchemaJSON []byte

const (
	workflowAgentLiveGraphPrompt = `你是 ProductFlow 的商品工作流协作 Agent。
当前商品已有 live schema-v3 图。用户是画布的主编辑者。

加载匹配任务的 ProductFlow Skill。只使用本轮工具列表里的工具。
不得提交第二份完整拓扑。不得编造商品事实或资产。
不得输出 base64、data URL、存储路径或内部 URL。
提案、跑图和素材整理的确认只在 ProductFlow UI 完成。
`

	goalLoopPrompt = `
当前是用户显式开始的 Goal，不是开聊入场券。
循环使用已有工具：request_workflow_run → 等用户在画布确认 → inspect 结果 → apply 或 propose → 再请求跑图。
不得自行宣布 Goal 完成。完成只能由用户点完成。
一次 WorkflowGraphRun 结束不等于 Goal 结束，不要接管跑图状态机。
默认仍须用户在画布确认跑图。
`

	globalAgentSystemPrompt = `你是 ProductFlow 的全局素材与工作流辅助 Agent。
作用域是整个应用，不绑定某一个商品画布。

加载匹配任务的 ProductFlow Skill。只使用本轮工具列表里的工具。
不能在全局会话上改某个商品的 live graph。
素材整理必须先提交可审阅 Draft。不得编造事实。
不得输出 base64、data URL、存储路径或内部 URL。
`
)

func (s Service) ConversationContract(ctx context.Context, conversationID string) (ContractResponse, error) {
	var out ContractResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		item, err := contractForConversation(ctx, pgxTx, conversationID, nil)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) TaskContract(ctx context.Context, taskID string) (ContractResponse, error) {
	var out ContractResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		task, err := loadTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		if task.ConversationID == nil {
			return apperr.Conflict("当前 Agent Task 尚未绑定可执行的 Agent conversation")
		}
		if task.Status == "canceled" {
			return apperr.Conflict("已取消的 Agent Task 不能继续运行")
		}
		item, err := contractForConversation(ctx, pgxTx, *task.ConversationID, &task)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) RuntimeContext(ctx context.Context, conversationID string, taskID *string) (RuntimeContextResponse, error) {
	var out RuntimeContextResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		conv, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		if conv.SessionID == nil {
			return apperr.Conflict("Agent conversation 尚未绑定 Session")
		}
		session, err := loadSession(ctx, pgxTx, *conv.SessionID)
		if err != nil {
			return err
		}
		var taskSummary *string
		if taskID != nil {
			task, err := loadTask(ctx, pgxTx, *taskID)
			if err != nil {
				return err
			}
			if task.ConversationID == nil || *task.ConversationID != conv.ID || task.SessionID != session.ID {
				return apperr.Conflict("Agent Task 与当前 Agent conversation 不匹配")
			}
			taskSummary = task.Summary
		}
		out = RuntimeContextResponse{
			SchemaVersion: 1, SessionID: session.ID, ConversationID: conv.ID,
			TaskID: taskID, SessionSummary: session.Summary, TaskSummary: taskSummary,
		}
		return nil
	})
	return out, err
}

func contractForConversation(ctx context.Context, pgxTx pgx.Tx, conversationID string, task *TaskResponse) (ContractResponse, error) {
	conv, err := loadConversationByID(ctx, pgxTx, conversationID)
	if err != nil {
		return ContractResponse{}, err
	}
	out := ContractResponse{
		SchemaVersion: 1, ScopeType: conv.ScopeType, ConversationID: conv.ID,
		ProductID: conv.ProductID, HarnessRunID: conv.HarnessRunID,
		ToolContractVersion: toolContractVersion, DraftSchema: map[string]any{},
	}
	if conv.ScopeType == "global" {
		var version int
		_ = pgxTx.QueryRow(ctx, `
			SELECT COALESCE(r.version, 0)
			FROM library_organization_drafts d
			LEFT JOIN library_organization_draft_revisions r ON r.id = d.current_revision_id
			WHERE d.conversation_id = $1
		`, conversationID).Scan(&version)
		var schema map[string]any
		_ = json.Unmarshal(globalDraftSchemaJSON, &schema)
		out.CurrentDraftVersion = version
		out.SystemPrompt = globalAgentSystemPrompt
		out.DraftKind = ptr("global")
		out.DraftSchema = schema
		out.HasLiveGraph = false
	} else {
		if conv.ProductID == nil {
			return ContractResponse{}, apperr.Conflict("商品工作流 Agent conversation 缺少商品")
		}
		var exists int
		err := pgxTx.QueryRow(ctx, `SELECT 1 FROM workflow_graphs WHERE product_id = $1 AND active = TRUE LIMIT 1`, *conv.ProductID).Scan(&exists)
		out.SystemPrompt = workflowAgentLiveGraphPrompt
		out.DraftKind = ptr("workflow")
		out.HasLiveGraph = err == nil
	}
	if task != nil {
		out.TaskID = &task.ID
		out.TaskGoal = &task.Goal
		out.HarnessRunID = task.ID
		if conv.ScopeType == "product_workflow" {
			out.SystemPrompt = out.SystemPrompt + goalLoopPrompt
		}
	}
	return out, nil
}

func (s Service) ProductContext(ctx context.Context, conversationID string) (map[string]any, error) {
	var conv conversationRow
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		loaded, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		conv = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	if conv.ScopeType != "product_workflow" || conv.ProductID == nil {
		return nil, apperr.Conflict("当前 conversation 不是商品工作流作用域")
	}
	product, err := s.Product.Get(ctx, *conv.ProductID)
	if err != nil {
		return nil, err
	}
	live, err := s.Graph.TryCurrent(ctx, *conv.ProductID)
	if err != nil {
		return nil, err
	}
	var liveSummary any
	if live != nil {
		liveSummary = liveGraphSummary(*live)
	}
	return map[string]any{
		"schema_version": 1,
		"product": map[string]any{
			"id": product.ID, "name": product.Name, "category": product.Category,
			"price": product.Price, "source_note": product.SourceNote,
		},
		"confirmed_fact_set": nil,
		"intake":             json.RawMessage(orEmptyJSON(product.Intake)),
		"node_catalog":       graph.CatalogJSON(),
		"image_type_catalog": graph.ImageTypeCatalogJSON(),
		"live_graph":         liveSummary,
	}, nil
}

func orEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return []byte("null")
	}
	return raw
}

func liveGraphSummary(proj graph.Projection) map[string]any {
	nodes := make([]map[string]any, 0, len(proj.Nodes))
	for _, node := range proj.Nodes {
		nodes = append(nodes, map[string]any{
			"id": node.ID, "node_type": node.NodeType, "title": node.Title,
			"config_status": node.ConfigStatus, "unused": node.Unused,
			"bound_asset_id": node.BoundAssetID, "group_id": node.GroupID,
			"has_current_artifact": node.CurrentArtifactID != nil,
		})
	}
	edges := make([]map[string]any, 0, len(proj.Edges))
	for _, edge := range proj.Edges {
		edges = append(edges, map[string]any{
			"id": edge.ID, "source_node_id": edge.SourceNodeID, "target_node_id": edge.TargetNodeID,
			"role": edge.Role, "data_type": edge.DataType,
		})
	}
	groups := make([]map[string]any, 0, len(proj.Groups))
	for _, group := range proj.Groups {
		groups = append(groups, map[string]any{
			"id": group.ID, "title": group.Title, "member_ids": group.MemberIDs,
		})
	}
	return map[string]any{
		"id": proj.ID, "title": proj.Title, "schema_version": proj.SchemaVersion, "revision": proj.Revision,
		"nodes": nodes, "edges": edges, "groups": groups,
	}
}
