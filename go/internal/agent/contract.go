package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/prompts"
	"gorm.io/gorm"
)

//go:embed global_draft_schema.json
var globalDraftSchemaJSON []byte

// ConversationContract 读取 conversation 的 system prompt、工具合同与 Draft schema。conversation 不存在返回 NotFound。商品工作流缺少商品返回 Conflict。
func (s Service) ConversationContract(ctx context.Context, conversationID string) (ContractResponse, error) {
	var out ContractResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := contractForConversation(ctx, pgxTx, conversationID, nil)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

// TaskContract 读取绑定 conversation 的 Task 合同；已取消 Task 返回 Conflict。
func (s Service) TaskContract(ctx context.Context, taskID string) (ContractResponse, error) {
	var out ContractResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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

// RuntimeContext 读取 Session / Task 的有界 operational summary。conversation 未绑定 Session 或 Task 不匹配返回 Conflict。找不到 conversation / Session / Task 返回 NotFound。
func (s Service) RuntimeContext(ctx context.Context, conversationID string, taskID *string) (RuntimeContextResponse, error) {
	var out RuntimeContextResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
			SchemaVersion: 1, SessionID: session.ID, ConversationID: conv.ID, MerchantID: conv.MerchantID,
			TaskID: taskID, SessionSummary: session.Summary, TaskSummary: taskSummary,
		}
		return nil
	})
	return out, err
}

// contractForConversation 组装发给 Pi 的 system prompt、工具合同与 Draft schema。
//
// ConversationContract / TaskContract 调用。商品工作流且带 Task 时追加 AgentGoalLoop 提示：Turn/GraphRun 成功不完成 Goal。不写表。已取消 Task 由调用方拦截。
func contractForConversation(ctx context.Context, pgxTx *gorm.DB, conversationID string, task *TaskResponse) (ContractResponse, error) {
	conv, err := loadConversationByID(ctx, pgxTx, conversationID)
	if err != nil {
		return ContractResponse{}, err
	}
	out := ContractResponse{
		SchemaVersion: 1, ScopeType: conv.ScopeType, ConversationID: conv.ID, MerchantID: conv.MerchantID,
		ProductID: conv.ProductID, HarnessRunID: conv.HarnessRunID,
		ToolContractVersion: toolContractVersion, DraftSchema: map[string]any{},
	}
	if conv.ScopeType == "global" {
		var draft struct {
			Version int `gorm:"column:version"`
		}
		_ = pgxTx.Model(&schema.LibraryOrganizationDrafts{}).
			Select("COALESCE(library_organization_draft_revisions.version, 0) AS version").
			Joins("LEFT JOIN library_organization_draft_revisions ON library_organization_draft_revisions.id = library_organization_drafts.current_revision_id").
			Where("library_organization_drafts.conversation_id = ?", conversationID).
			Take(&draft).Error
		var schemaDoc map[string]any
		_ = json.Unmarshal(globalDraftSchemaJSON, &schemaDoc)
		out.CurrentDraftVersion = draft.Version
		out.SystemPrompt = prompts.AgentGlobal()
		out.DraftKind = ptr("global")
		out.DraftSchema = schemaDoc
		out.HasLiveGraph = false
	} else {
		if conv.ProductID == nil {
			return ContractResponse{}, apperr.Conflict("商品工作流 Agent conversation 缺少商品")
		}
		var graph schema.WorkflowGraphs
		err := pgxTx.Select("id").Where("product_id = ? AND active = TRUE", *conv.ProductID).Take(&graph).Error
		out.SystemPrompt = prompts.AgentWorkflow()
		out.DraftKind = ptr("workflow")
		out.HasLiveGraph = err == nil
	}
	out.ToolContractVersion = resolvedToolContractVersion(out.DraftSchema)
	if task != nil {
		out.TaskID = &task.ID
		out.TaskGoal = &task.Goal
		out.HarnessRunID = task.ID
		if conv.ScopeType == "product_workflow" {
			out.SystemPrompt = out.SystemPrompt + "\n" + prompts.AgentGoalLoop()
		}
	}
	return out, nil
}

// ProductContext 读取商品工作流 conversation 的 facts、intake 与 live 图摘要。非商品工作流 conversation 返回 Conflict。商品或 conversation 不存在返回 NotFound。
func (s Service) ProductContext(ctx context.Context, conversationID, responseFormat string) (map[string]any, error) {
	var conv conversationRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
	facts, err := s.Product.GetFacts(ctx, *conv.ProductID)
	if err != nil {
		return nil, err
	}
	var confirmed any
	if facts.FactSet != nil {
		confirmed = map[string]any{
			"version": facts.FactSet.Version,
			"facts":   facts.Facts,
		}
	}
	live, err := s.Graph.TryCurrent(ctx, *conv.ProductID)
	if err != nil {
		return nil, err
	}
	format := boundedResponseFormat(responseFormat)
	var liveSummary any
	if live != nil {
		liveSummary = liveGraphSummary(*live, format)
	}
	catalog := graph.CatalogJSON()
	if format != "detailed" {
		catalog = graph.CatalogIndexJSON()
	}
	return map[string]any{
		"schema_version": 1,
		"product": map[string]any{
			"id": product.ID, "name": product.Name, "category": product.Category,
			"price": product.Price, "source_note": product.SourceNote,
		},
		"confirmed_fact_set": confirmed,
		"intake":             json.RawMessage(orEmptyJSON(product.Intake)),
		"birth_expandable":   birthExpandable(product.Intake, live),
		"node_catalog":       catalog,
		"image_type_catalog": graph.ImageTypeCatalogJSON(),
		"live_graph":         liveSummary,
		"response_format":    format,
	}, nil
}

func boundedResponseFormat(raw string) string {
	if strings.TrimSpace(raw) == "detailed" {
		return "detailed"
	}
	return "concise"
}

func orEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return []byte("null")
	}
	return raw
}

func intakePresent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return len(trimmed) > 0 && trimmed != "null"
}

func isBirthGraph(proj graph.Projection) bool {
	sources := 0
	for _, node := range proj.Nodes {
		if node.NodeType == graph.NodeProductSource {
			sources++
			continue
		}
		return false
	}
	return sources == 1
}

func birthExpandable(intake json.RawMessage, live *graph.Projection) bool {
	if !intakePresent(intake) {
		return false
	}
	if live == nil {
		return true
	}
	return isBirthGraph(*live)
}

func resolvedToolContractVersion(draft any) string {
	if draft == nil {
		draft = map[string]any{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(draft); err != nil {
		return ToolManifestVersion
	}
	raw := bytes.TrimSuffix(buf.Bytes(), []byte{'\n'})
	sum := sha256.Sum256(append(append([]byte(ToolManifestVersion), '\n'), raw...))
	return hex.EncodeToString(sum[:])
}

func liveGraphSummary(proj graph.Projection, format string) map[string]any {
	if format == "detailed" {
		return liveGraphDetailed(proj)
	}
	return liveGraphConcise(proj)
}

// liveGraphConcise 给 Agent 有界图摘要：节点类型/状态/是否有 artifact，不含媒体 bytes 与完整 config。
func liveGraphConcise(proj graph.Projection) map[string]any {
	nodes := make([]map[string]any, 0, len(proj.Nodes))
	for _, node := range proj.Nodes {
		nodes = append(nodes, map[string]any{
			"id": node.ID, "node_type": node.NodeType, "title": node.Title,
			"config_status": node.ConfigStatus, "unused": node.Unused,
			"group_id": node.GroupID, "has_current_artifact": node.CurrentArtifactID != nil,
		})
	}
	groups := make([]map[string]any, 0, len(proj.Groups))
	for _, group := range proj.Groups {
		groups = append(groups, map[string]any{
			"id": group.ID, "title": group.Title, "member_count": len(group.MemberIDs),
		})
	}
	return map[string]any{
		"id": proj.ID, "title": proj.Title, "schema_version": proj.SchemaVersion, "revision": proj.Revision,
		"node_count": len(proj.Nodes), "edge_count": len(proj.Edges), "group_count": len(proj.Groups),
		"nodes": nodes, "groups": groups,
	}
}

// liveGraphDetailed 在 concise 基础上补边与绑定 asset id，仍不含媒体 bytes。仅当 response_format=detailed。
func liveGraphDetailed(proj graph.Projection) map[string]any {
	nodes := make([]map[string]any, 0, len(proj.Nodes))
	for _, node := range proj.Nodes {
		incoming := make([]map[string]any, 0, len(node.Incoming))
		for _, edge := range node.Incoming {
			incoming = append(incoming, map[string]any{
				"source": edge.NodeID, "target": node.ID,
				"role": edge.Role, "data_type": edge.DataType,
			})
		}
		outgoing := make([]map[string]any, 0, len(node.Outgoing))
		for _, edge := range node.Outgoing {
			outgoing = append(outgoing, map[string]any{
				"source": node.ID, "target": edge.NodeID,
				"role": edge.Role, "data_type": edge.DataType,
			})
		}
		nodes = append(nodes, map[string]any{
			"id": node.ID, "node_type": node.NodeType, "title": node.Title,
			"config_status": node.ConfigStatus, "unused": node.Unused,
			"bound_asset_id": node.BoundAssetID, "group_id": node.GroupID,
			"has_current_artifact": node.CurrentArtifactID != nil,
			"incoming":             incoming,
			"outgoing":             outgoing,
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
