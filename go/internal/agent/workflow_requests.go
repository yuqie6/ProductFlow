package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type runRequestScan struct {
	schema.AgentWorkflowRunRequests
	ProductName       string  `gorm:"column:product_name"`
	WorkflowTitle     string  `gorm:"column:workflow_title"`
	WorkflowRunStatus *string `gorm:"column:workflow_run_status"`
}

func runRequestFromScan(row runRequestScan) WorkflowRunRequestResponse {
	return WorkflowRunRequestResponse{
		ID: row.ID, ConversationID: row.ConversationID, TaskID: row.TaskID,
		ProductID: row.ProductID, ProductName: row.ProductName, WorkflowID: row.GraphID,
		WorkflowTitle: row.WorkflowTitle, ExpectedWorkflowRevision: row.ExpectedWorkflowRevision,
		Status: row.Status, SourceRunID: row.SourceGraphRunID, WorkflowRunID: row.GraphRunID,
		WorkflowRunStatus: row.WorkflowRunStatus, SourceStepID: row.SourceStepID,
		FailureReason: row.FailureReason, ConfirmedAt: row.ConfirmedAt, FinishedAt: row.FinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		RunScope:       agentRunScope(row.RunScope),
		TargetNodeID:   row.TargetNodeID,
		TargetNodeIDs:  parseAgentRunNodeIDs(row.TargetNodeIDsJSON),
		Force:          row.Force,
		DocumentAction: row.DocumentAction,
	}
}

func agentRunScope(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "graph"
	}
	return strings.TrimSpace(*value)
}

func parseAgentRunNodeIDs(raw *string) []string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(*raw), &ids); err != nil {
		return nil
	}
	return ids
}

func graphRunRequestFromAgent(item WorkflowRunRequestResponse) graph.GraphRunRequest {
	scope := item.RunScope
	if scope == "" {
		scope = "graph"
	}
	action := ""
	if item.DocumentAction != nil {
		action = *item.DocumentAction
	}
	return graph.GraphRunRequest{Scope: scope, NodeID: item.TargetNodeID, NodeIDs: item.TargetNodeIDs, Force: item.Force, DocumentAction: action}
}

func runRequestBaseQuery(pgxTx *gorm.DB) *gorm.DB {
	return pgxTx.Model(&schema.AgentWorkflowRunRequests{}).
		Select(`agent_workflow_run_requests.*,
			products.name AS product_name,
			COALESCE(workflow_graphs.title, '') AS workflow_title,
			workflow_graph_runs.status AS workflow_run_status`).
		Joins("JOIN products ON products.id = agent_workflow_run_requests.product_id").
		Joins("LEFT JOIN workflow_graphs ON workflow_graphs.id = agent_workflow_run_requests.graph_id").
		Joins("LEFT JOIN workflow_graph_runs ON workflow_graph_runs.id = agent_workflow_run_requests.graph_run_id")
}

func (s Service) GetWorkflowRunRequest(ctx context.Context, productID *string, conversationID string, taskID *string) (*WorkflowRunRequestResponse, error) {
	var out *WorkflowRunRequestResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadConversation(ctx, pgxTx, productID, conversationID); err != nil {
			return err
		}
		q := runRequestBaseQuery(pgxTx).Where("agent_workflow_run_requests.conversation_id = ?", conversationID)
		if taskID != nil {
			q = q.Where("agent_workflow_run_requests.task_id = ?", *taskID)
		}
		item, err := scanRunRequest(q.Order("agent_workflow_run_requests.created_at DESC, agent_workflow_run_requests.id DESC"))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if item.WorkflowRunID != nil {
			if err := SyncGraphRunToTasks(ctx, pgxTx, *item.WorkflowRunID); err != nil {
				return err
			}
			item, err = loadRunRequest(ctx, pgxTx, productID, conversationID, item.ID)
			if err != nil {
				return err
			}
		}
		out = &item
		return nil
	})
	return out, err
}

func (s Service) ConfirmWorkflowRunRequest(ctx context.Context, productID *string, conversationID, requestID string) (WorkflowRunRequestResponse, error) {
	var out WorkflowRunRequestResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
		if err != nil {
			return err
		}
		if item.Status == "cancelled" {
			return apperr.NotPending("已取消的工作流执行请求不能确认")
		}
		if item.WorkflowRunID != nil {
			if err := SyncGraphRunToTasks(ctx, pgxTx, *item.WorkflowRunID); err != nil {
				return err
			}
			loaded, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
			out = loaded
			return err
		}
		if item.Status != "awaiting_confirmation" {
			return apperr.NotPending("当前工作流执行请求不在待确认状态")
		}
		if err := requireLiveWorkflowRevision(ctx, pgxTx, item.ProductID, item.WorkflowID, item.ExpectedWorkflowRevision); err != nil {
			return err
		}
		var run graph.GraphRunResponse
		if item.SourceRunID != nil {
			run, err = s.Graph.RetryRunTx(ctx, pgxTx, item.ProductID, item.WorkflowID, *item.SourceRunID)
		} else {
			run, err = s.Graph.SubmitRunTx(ctx, pgxTx, item.ProductID, item.WorkflowID, graphRunRequestFromAgent(item))
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.AgentWorkflowRunRequests{}).Where("id = ?", requestID).Updates(map[string]any{
			"status":       "confirmed",
			"graph_run_id": run.ID,
			"confirmed_at": now,
			"updated_at":   now,
		}).Error; err != nil {
			return err
		}
		if err := markTurnSucceededForRequest(ctx, pgxTx, requestID, conversationID); err != nil {
			return err
		}
		if err := SyncGraphRunToTasks(ctx, pgxTx, run.ID); err != nil {
			return err
		}
		loaded, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
		out = loaded
		return err
	})
	return out, err
}

func (s Service) CancelWorkflowRunRequestHTTP(ctx context.Context, productID *string, conversationID, requestID string) (WorkflowRunRequestResponse, error) {
	var out WorkflowRunRequestResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := cancelWorkflowRunRequest(ctx, pgxTx, s, productID, conversationID, requestID)
		out = item
		return err
	})
	return out, err
}

func cancelWorkflowRunRequest(ctx context.Context, pgxTx *gorm.DB, s Service, productID *string, conversationID, requestID string) (WorkflowRunRequestResponse, error) {
	item, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	if item.Status == "cancelled" {
		return item, nil
	}
	if item.WorkflowRunID != nil {
		if _, err := s.Graph.CancelRunTx(ctx, pgxTx, item.ProductID, item.WorkflowID, *item.WorkflowRunID); err != nil {
			return WorkflowRunRequestResponse{}, err
		}
		if err := SyncGraphRunToTasks(ctx, pgxTx, *item.WorkflowRunID); err != nil {
			return WorkflowRunRequestResponse{}, err
		}
	} else {
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.AgentWorkflowRunRequests{}).Where("id = ?", requestID).Updates(map[string]any{
			"status":         "cancelled",
			"failure_reason": gorm.Expr("COALESCE(failure_reason, ?)", graph.GraphCancelledReason),
			"finished_at":    now,
			"updated_at":     now,
		}).Error; err != nil {
			return WorkflowRunRequestResponse{}, err
		}
		if err := resolveWorkflowRequestApproval(ctx, pgxTx, requestID, "denied"); err != nil {
			return WorkflowRunRequestResponse{}, err
		}
		if err := markTurnCanceledForRequest(ctx, pgxTx, requestID, conversationID); err != nil {
			return WorkflowRunRequestResponse{}, err
		}
		if err := parkTaskAfterCancelledRunRequest(ctx, pgxTx, item.TaskID); err != nil {
			return WorkflowRunRequestResponse{}, err
		}
	}
	return loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
}

func (s Service) PrepareWorkflowRunRequest(ctx context.Context, conversationID string, expectedRevision int, sourceRunID *string, taskID *string) (PreparedWorkflowRunRequest, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	sourceRunID, err = normalizeOptionalID(sourceRunID, "source_run_id")
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	return s.prepareProductGraphRequest(ctx, s.DB, conv, expectedRevision, sourceRunID, taskID)
}

func (s Service) PrepareGlobalWorkflowRunRequest(ctx context.Context, conversationID, productID, workflowID string, expectedRevision int, sourceRunID, taskID *string) (PreparedWorkflowRunRequest, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if err := requireGlobalWorkflowConversation(conv); err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	productID, err = normalizeRequiredID(productID, "product_id")
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	workflowID, err = normalizeRequiredID(workflowID, "workflow_id")
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	sourceRunID, err = normalizeOptionalID(sourceRunID, "source_run_id")
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	return s.prepareGlobalGraphRequest(ctx, s.DB, conv, productID, workflowID, expectedRevision, sourceRunID, taskID)
}

func (s Service) prepareProductGraphRequest(ctx context.Context, db *gorm.DB, conv conversationRow, expectedRevision int, sourceRunID, taskID *string) (PreparedWorkflowRunRequest, error) {
	prep, err := s.resolveGraphRunnable(ctx, db, *conv.ProductID, "", true, expectedRevision, sourceRunID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if err := validateTaskScope(ctx, db, conv, taskID); err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	prep.TaskID = taskID
	return prep, nil
}

func (s Service) prepareGlobalGraphRequest(ctx context.Context, db *gorm.DB, conv conversationRow, productID, workflowID string, expectedRevision int, sourceRunID, taskID *string) (PreparedWorkflowRunRequest, error) {
	if err := validateTaskScope(ctx, db, conv, taskID); err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	prep, err := s.resolveGraphRunnable(ctx, db, productID, workflowID, false, expectedRevision, sourceRunID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	prep.TaskID = taskID
	return prep, nil
}

func (s Service) resolveGraphRunnable(ctx context.Context, db *gorm.DB, productID, workflowID string, requireActive bool, expectedRevision int, sourceRunID *string) (PreparedWorkflowRunRequest, error) {
	if err := requireExpectedWorkflowRevision(expectedRevision); err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	q := db.WithContext(ctx).Model(&schema.WorkflowGraphs{}).Where("product_id = ?", productID)
	if requireActive {
		q = q.Where("active = TRUE")
	}
	if workflowID != "" {
		q = q.Where("id = ?", workflowID)
	}
	var g schema.WorkflowGraphs
	err := q.Select("id, title, revision").Take(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if workflowID != "" && !requireActive {
			return PreparedWorkflowRunRequest{}, apperr.NotFound("工作流不存在")
		}
		if workflowID != "" {
			return PreparedWorkflowRunRequest{}, apperr.NotFound("工作流不存在")
		}
		return PreparedWorkflowRunRequest{}, apperr.Conflict("当前商品还没有可执行的工作流")
	}
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if g.Revision != expectedRevision {
		return PreparedWorkflowRunRequest{}, apperr.Conflict(workflowRevisionChangedDetail)
	}
	runnable, err := requireRunnableWorkflow(ctx, db, productID, g.ID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	normalizedSource, err := validateSourceRun(ctx, db, productID, g.ID, sourceRunID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	return PreparedWorkflowRunRequest{
		ProductID: productID, WorkflowID: g.ID, WorkflowTitle: g.Title,
		WorkflowRevision: g.Revision, RunnableNodeCount: runnable, SourceRunID: normalizedSource,
	}, nil
}

func (s Service) CreateWorkflowRunRequest(ctx context.Context, conversationID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, spec runScopeSpec) (WorkflowRunRequestResponse, error) {
	return s.createRunRequest(ctx, conversationID, "", workflowID, idempotencyKey, sourceStepID, expectedRevision, taskID, sourceRunID, false, spec)
}

func (s Service) CreateGlobalWorkflowRunRequest(ctx context.Context, conversationID, productID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, spec runScopeSpec) (WorkflowRunRequestResponse, error) {
	return s.createRunRequest(ctx, conversationID, productID, workflowID, idempotencyKey, sourceStepID, expectedRevision, taskID, sourceRunID, true, spec)
}

func (s Service) createRunRequest(ctx context.Context, conversationID, productID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, global bool, spec runScopeSpec) (WorkflowRunRequestResponse, error) {
	if err := requireExpectedWorkflowRevision(expectedRevision); err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	sourceStepID, err = normalizeSourceStepID(sourceStepID)
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	workflowID, err = normalizeRequiredID(workflowID, "workflow_id")
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	sourceRunID, err = normalizeOptionalID(sourceRunID, "source_run_id")
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	spec, err = parseRunScopeSpec(spec.Scope, spec.NodeID, spec.NodeIDs, spec.Force, spec.DocumentAction)
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	if global {
		productID, err = normalizeRequiredID(productID, "product_id")
		if err != nil {
			return WorkflowRunRequestResponse{}, err
		}
	}
	var out WorkflowRunRequestResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		conv, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		var hashProductID *string
		targetProduct := productID
		if global {
			if err := requireGlobalWorkflowConversation(conv); err != nil {
				return err
			}
			hashProductID = &targetProduct
		} else {
			if err := requireProductWorkflow(conv); err != nil {
				return err
			}
			targetProduct, err = normalizeRequiredID(*conv.ProductID, "product_id")
			if err != nil {
				return err
			}
		}
		hash, err := hashWorkflowRunRequest(conversationID, hashProductID, workflowID, sourceStepID, expectedRevision, taskID, sourceRunID, spec)
		if err != nil {
			return err
		}
		var existing schema.AgentWorkflowRunRequests
		err = pgxTx.Select("id, request_hash").Where("conversation_id = ? AND idempotency_key = ?", conversationID, key).Take(&existing).Error
		if err == nil {
			if existing.RequestHash != hash {
				return apperr.Conflict("同一 idempotency key 不能提交不同的工作流执行请求")
			}
			item, err := loadRunRequest(ctx, pgxTx, &targetProduct, conversationID, existing.ID)
			out = item
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var prep PreparedWorkflowRunRequest
		if global {
			prep, err = s.prepareGlobalGraphRequest(ctx, pgxTx, conv, targetProduct, workflowID, expectedRevision, sourceRunID, taskID)
		} else {
			prep, err = s.prepareProductGraphRequest(ctx, pgxTx, conv, expectedRevision, sourceRunID, taskID)
		}
		if err != nil {
			return err
		}
		if prep.ProductID != targetProduct {
			return apperr.Conflict("工作流执行请求的 product_id 与当前目标不一致")
		}
		if prep.WorkflowID != workflowID {
			return apperr.Conflict("工作流执行请求的 workflow_id 与当前 active 工作流不一致")
		}
		id := newID()
		now := time.Now().UTC()
		rec := schema.AgentWorkflowRunRequests{
			ID:                       id,
			ConversationID:           conversationID,
			TaskID:                   taskID,
			ProductID:                targetProduct,
			GraphID:                  prep.WorkflowID,
			ExpectedWorkflowRevision: expectedRevision,
			Status:                   "awaiting_confirmation",
			SourceGraphRunID:         prep.SourceRunID,
			SourceStepID:             sourceStepID,
			IdempotencyKey:           key,
			RequestHash:              hash,
			RunScope:                 spec.columnScope(),
			TargetNodeID:             spec.NodeID,
			TargetNodeIDsJSON:        spec.nodeIDsJSON(),
			Force:                    spec.Force,
			DocumentAction:           stringPtrOrNil(spec.DocumentAction),
			CreatedAt:                now,
			UpdatedAt:                now,
		}
		if err := pgxTx.Create(&rec).Error; err != nil {
			return err
		}
		if err := markRequestWaiting(ctx, pgxTx, conversationID, taskID); err != nil {
			return err
		}
		item, err := loadRunRequest(ctx, pgxTx, &targetProduct, conversationID, id)
		out = item
		return err
	})
	return out, err
}

func (s Service) ReconcileWorkflowRunRequest(ctx context.Context, conversationID, idempotencyKey, productID, workflowID, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, spec runScopeSpec) (ReconcileResponse, error) {
	return s.reconcileRunRequest(ctx, conversationID, idempotencyKey, productID, workflowID, sourceStepID, expectedRevision, taskID, sourceRunID, false, spec)
}

func (s Service) ReconcileGlobalWorkflowRunRequest(ctx context.Context, conversationID, idempotencyKey, productID, workflowID, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, spec runScopeSpec) (ReconcileResponse, error) {
	return s.reconcileRunRequest(ctx, conversationID, idempotencyKey, productID, workflowID, sourceStepID, expectedRevision, taskID, sourceRunID, true, spec)
}

func (s Service) reconcileRunRequest(ctx context.Context, conversationID, idempotencyKey, productID, workflowID, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, global bool, spec runScopeSpec) (ReconcileResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return ReconcileResponse{}, err
	}
	sourceStepID, err = normalizeSourceStepID(sourceStepID)
	if err != nil {
		return ReconcileResponse{}, err
	}
	workflowID, err = normalizeRequiredID(workflowID, "workflow_id")
	if err != nil {
		return ReconcileResponse{}, err
	}
	sourceRunID, err = normalizeOptionalID(sourceRunID, "source_run_id")
	if err != nil {
		return ReconcileResponse{}, err
	}
	spec, err = parseRunScopeSpec(spec.Scope, spec.NodeID, spec.NodeIDs, spec.Force, spec.DocumentAction)
	if err != nil {
		return ReconcileResponse{}, err
	}
	var hashProductID *string
	if global {
		productID, err = normalizeRequiredID(productID, "product_id")
		if err != nil {
			return ReconcileResponse{}, err
		}
		hashProductID = &productID
	}
	var out ReconcileResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		conv, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		targetProduct := productID
		if global {
			if err := requireGlobalWorkflowConversation(conv); err != nil {
				return err
			}
		} else {
			if err := requireProductWorkflow(conv); err != nil {
				return err
			}
			targetProduct, err = normalizeRequiredID(*conv.ProductID, "product_id")
			if err != nil {
				return err
			}
		}
		hash, err := hashWorkflowRunRequest(conversationID, hashProductID, workflowID, sourceStepID, expectedRevision, taskID, sourceRunID, spec)
		if err != nil {
			return err
		}
		var existing schema.AgentWorkflowRunRequests
		err = pgxTx.Select("id, request_hash").Where("conversation_id = ? AND idempotency_key = ?", conversationID, key).Take(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			out = ReconcileResponse{State: "not_applied", Detail: ptr("工作流执行请求尚未提交")}
			return nil
		}
		if err != nil {
			return err
		}
		if existing.RequestHash != hash {
			out = ReconcileResponse{State: "conflict", Detail: ptr("同一 idempotency key 已对应其他工作流执行请求")}
			return nil
		}
		loaded, err := loadRunRequest(ctx, pgxTx, &targetProduct, conversationID, existing.ID)
		if err != nil {
			out = ReconcileResponse{State: "unknown", Detail: ptr("工作流执行请求对账结果仍不明确")}
			return nil
		}
		raw, _ := json.Marshal(loaded)
		out = ReconcileResponse{State: "applied", Result: raw, Detail: ptr("工作流执行请求已提交")}
		return nil
	})
	return out, err
}

const (
	workflowRevisionChangedDetail      = "工作流 revision 已变化，请重新读取当前工作流"
	workflowRunRequestMaxStepIDLength  = 120
	workflowRunRequestMaxIDLength      = 64
	sourceRunNotRetryableDetail        = "只有失败且可重试的工作流运行可以再次请求执行"
	globalWorkflowConversationConflict = "只有全局 Agent conversation 可以请求跨商品执行工作流"
	taskConversationMismatchDetail     = "Agent Task 与当前 Agent conversation 不匹配"
	finishedGraphRunCannotCancelDetail = "已结束的工作流运行不能取消"
)

func requireExpectedWorkflowRevision(expected int) error {
	if expected <= 0 {
		return apperr.Validation("expected_workflow_revision 必须大于 0")
	}
	return nil
}

func requireGlobalWorkflowConversation(conv conversationRow) error {
	if conv.ScopeType != "global" {
		return apperr.Conflict(globalWorkflowConversationConflict)
	}
	return nil
}

type runScopeSpec struct {
	Scope          string
	NodeID         *string
	NodeIDs        []string
	Force          bool
	DocumentAction string
}

func parseRunScopeSpec(scope string, nodeID *string, nodeIDs []string, force bool, documentAction string) (runScopeSpec, error) {
	normalized := strings.TrimSpace(scope)
	documentAction = strings.TrimSpace(documentAction)
	if documentAction != "" && documentAction != "complete" && documentAction != "rewrite" && documentAction != "replace" {
		return runScopeSpec{}, apperr.Validation("document_action 无效")
	}
	if documentAction != "" && (!force || normalized != "node") {
		return runScopeSpec{}, apperr.Validation("document_action 只支持强制运行单个文稿节点")
	}
	if normalized == "" {
		normalized = "graph"
	}
	switch normalized {
	case "graph":
		return runScopeSpec{Scope: "graph", Force: force, DocumentAction: documentAction}, nil
	case "node", "to_node":
		if nodeID == nil || strings.TrimSpace(*nodeID) == "" {
			return runScopeSpec{}, apperr.Validation("node_id 不能为空")
		}
		id := strings.TrimSpace(*nodeID)
		return runScopeSpec{Scope: normalized, NodeID: &id, Force: force, DocumentAction: documentAction}, nil
	case "selection":
		cleaned := make([]string, 0, len(nodeIDs))
		for _, id := range nodeIDs {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}
		if len(cleaned) == 0 {
			return runScopeSpec{}, apperr.Validation("node_ids 不能为空")
		}
		return runScopeSpec{Scope: normalized, NodeIDs: cleaned, Force: force, DocumentAction: documentAction}, nil
	default:
		return runScopeSpec{}, apperr.Validation("scope 无效")
	}
}

func (spec runScopeSpec) columnScope() *string {
	if spec.Scope == "" || spec.Scope == "graph" {
		return nil
	}
	scope := spec.Scope
	return &scope
}

func (spec runScopeSpec) nodeIDsJSON() *string {
	if len(spec.NodeIDs) == 0 {
		return nil
	}
	raw, err := json.Marshal(spec.NodeIDs)
	if err != nil {
		return nil
	}
	encoded := string(raw)
	return &encoded
}

func workflowRunRequestHashPayload(conversationID string, productID *string, workflowID, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, spec runScopeSpec) map[string]any {
	payload := map[string]any{
		"schema_version":             1,
		"conversation_id":            strings.TrimSpace(conversationID),
		"task_id":                    taskID,
		"workflow_id":                strings.TrimSpace(workflowID),
		"expected_workflow_revision": expectedRevision,
		"source_step_id":             strings.TrimSpace(sourceStepID),
	}
	if productID != nil {
		payload["product_id"] = strings.TrimSpace(*productID)
	}
	if sourceRunID != nil {
		if trimmed := strings.TrimSpace(*sourceRunID); trimmed != "" {
			payload["source_run_id"] = trimmed
		}
	}
	if spec.Scope != "" && spec.Scope != "graph" {
		payload["run_scope"] = spec.Scope
		if spec.NodeID != nil {
			payload["node_id"] = *spec.NodeID
		}
		if len(spec.NodeIDs) > 0 {
			payload["node_ids"] = spec.NodeIDs
		}
	}
	if spec.Force {
		payload["force"] = true
	}
	if spec.DocumentAction != "" {
		payload["document_action"] = spec.DocumentAction
	}
	return payload
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func hashWorkflowRunRequest(conversationID string, productID *string, workflowID, sourceStepID string, expectedRevision int, taskID, sourceRunID *string, spec runScopeSpec) (string, error) {
	return canonjson.SHA256Hex(workflowRunRequestHashPayload(conversationID, productID, workflowID, sourceStepID, expectedRevision, taskID, sourceRunID, spec))
}

func normalizeRequiredID(value, field string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > workflowRunRequestMaxIDLength {
		return "", apperr.Validation(field + " 无效")
	}
	return normalized, nil
}

func normalizeOptionalID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeRequiredID(*value, field)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeSourceStepID(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > workflowRunRequestMaxStepIDLength {
		return "", apperr.Validation("工作流执行请求 source_step_id 无效")
	}
	return normalized, nil
}

func validateTaskScope(ctx context.Context, db *gorm.DB, conv conversationRow, taskID *string) error {
	if taskID == nil || strings.TrimSpace(*taskID) == "" {
		return nil
	}
	var task schema.AgentTasks
	err := db.WithContext(ctx).Where("id = ?", strings.TrimSpace(*taskID)).Take(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.NotFound("Agent Task 不存在")
	}
	if err != nil {
		return err
	}
	if conv.SessionID == nil || task.SessionID != *conv.SessionID {
		return apperr.Conflict(taskConversationMismatchDetail)
	}
	if task.ConversationID == nil || *task.ConversationID != conv.ID {
		return apperr.Conflict(taskConversationMismatchDetail)
	}
	if (task.ProductID == nil) != (conv.ProductID == nil) {
		return apperr.Conflict(taskConversationMismatchDetail)
	}
	if task.ProductID != nil && *task.ProductID != *conv.ProductID {
		return apperr.Conflict(taskConversationMismatchDetail)
	}
	return nil
}

func validateSourceRun(ctx context.Context, db *gorm.DB, productID, graphID string, sourceRunID *string) (*string, error) {
	if sourceRunID == nil {
		return nil, nil
	}
	var run schema.WorkflowGraphRuns
	err := db.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
		Joins("JOIN workflow_graphs ON workflow_graphs.id = workflow_graph_runs.graph_id").
		Where("workflow_graph_runs.id = ? AND workflow_graphs.id = ? AND workflow_graphs.product_id = ?", *sourceRunID, graphID, productID).
		Select("workflow_graph_runs.status, workflow_graph_runs.is_retryable").
		Take(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("工作流运行不存在")
	}
	if err != nil {
		return nil, err
	}
	if run.Status != "failed" || !run.IsRetryable {
		return nil, apperr.Validation(sourceRunNotRetryableDetail)
	}
	return sourceRunID, nil
}

func requireRunnableWorkflow(ctx context.Context, db *gorm.DB, productID, graphID string) (int, error) {
	id, err := graph.LoadGraph(ctx, db, productID, graphID)
	if err != nil {
		return 0, err
	}
	applied, err := graph.LoadAppliedGraph(ctx, db, id)
	if err != nil {
		return 0, err
	}
	selected, err := graph.SelectRunNodeIDs(applied, graph.RunScopeGraph, "", nil)
	if err != nil {
		var appErr apperr.Error
		if errors.As(err, &appErr) && appErr.Status == 400 {
			return 0, apperr.Conflict("当前工作流没有可运行的节点")
		}
		return 0, err
	}
	if len(selected) == 0 {
		return 0, apperr.Conflict("当前工作流没有可运行的节点")
	}
	return len(selected), nil
}

func markRequestWaiting(ctx context.Context, pgxTx *gorm.DB, conversationID string, taskID *string) error {
	if err := applyConversationStatus(ctx, pgxTx, conversationID, "awaiting_confirmation"); err != nil {
		return err
	}
	if taskID == nil || *taskID == "" {
		return nil
	}
	task, err := lockTask(ctx, pgxTx, *taskID)
	if err != nil {
		return err
	}
	if inSet(terminalTask, task.Status) || task.Status == "paused" {
		return nil
	}
	now := time.Now().UTC()
	if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status":         "awaiting_confirmation",
		"waiting_reason": "workflow_run_confirmation",
		"failure_reason": nil,
		"finished_at":    nil,
		"updated_at":     now,
	}).Error; err != nil {
		return err
	}
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}

func requireLiveWorkflowRevision(ctx context.Context, pgxTx *gorm.DB, productID, workflowID string, expectedRevision int) error {
	var g schema.WorkflowGraphs
	err := pgxTx.WithContext(ctx).Select("revision").Where("id = ? AND product_id = ?", workflowID, productID).Take(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.Conflict(workflowRevisionChangedDetail)
	}
	if err != nil {
		return err
	}
	if g.Revision != expectedRevision {
		return apperr.Conflict(workflowRevisionChangedDetail)
	}
	return nil
}

func loadRunRequest(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID, requestID string) (WorkflowRunRequestResponse, error) {
	q := runRequestBaseQuery(pgxTx.WithContext(ctx)).Where("agent_workflow_run_requests.id = ? AND agent_workflow_run_requests.conversation_id = ?", requestID, conversationID)
	if productID != nil {
		q = q.Where("agent_workflow_run_requests.product_id = ?", *productID)
	}
	item, err := scanRunRequest(q)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkflowRunRequestResponse{}, apperr.NotFound("Agent workflow run request 不存在")
	}
	return item, err
}

func scanRunRequest(q *gorm.DB) (WorkflowRunRequestResponse, error) {
	var row runRequestScan
	if err := q.Take(&row).Error; err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	return runRequestFromScan(row), nil
}
