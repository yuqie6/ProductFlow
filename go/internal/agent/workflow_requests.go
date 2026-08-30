package agent

import (
	"context"
	"encoding/json"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func (s Service) GetWorkflowRunRequest(ctx context.Context, productID *string, conversationID string, taskID *string) (*WorkflowRunRequestResponse, error) {
	var out *WorkflowRunRequestResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadConversation(ctx, pgxTx, productID, conversationID); err != nil {
			return err
		}
		q := `
			SELECT r.id, r.conversation_id, r.task_id, r.product_id, p.name, r.graph_id, COALESCE(g.title, ''),
				r.expected_workflow_revision, r.status, r.source_graph_run_id, r.graph_run_id, run.status,
				r.source_step_id, r.failure_reason, r.confirmed_at, r.finished_at, r.created_at, r.updated_at
			FROM agent_workflow_run_requests r
			JOIN products p ON p.id = r.product_id
			LEFT JOIN workflow_graphs g ON g.id = r.graph_id
			LEFT JOIN workflow_graph_runs run ON run.id = r.graph_run_id
			WHERE r.conversation_id = $1
		`
		args := []any{conversationID}
		if taskID != nil {
			q += ` AND r.task_id = $2`
			args = append(args, *taskID)
		}
		q += ` ORDER BY r.created_at DESC, r.id DESC LIMIT 1`
		item, err := scanRunRequest(ctx, pgxTx, q, args...)
		if errors.Is(err, sqldb.ErrNoRows) {
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
			return apperr.Conflict("已取消的工作流执行请求不能确认")
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
			return apperr.Conflict("当前工作流执行请求不在待确认状态")
		}
		if err := requireLiveWorkflowRevision(ctx, pgxTx, item.ProductID, item.WorkflowID, item.ExpectedWorkflowRevision); err != nil {
			return err
		}
		var run graph.GraphRunResponse
		if item.SourceRunID != nil {
			run, err = s.Graph.RetryRunTx(ctx, pgxTx, item.ProductID, item.WorkflowID, *item.SourceRunID)
		} else {
			run, err = s.Graph.SubmitRunTx(ctx, pgxTx, item.ProductID, item.WorkflowID, "graph", nil)
		}
		if err != nil {
			return err
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_workflow_run_requests
			SET status = 'confirmed', graph_run_id = $2, confirmed_at = NOW(), updated_at = NOW()
			WHERE id = $1
		`, requestID, run.ID); err != nil {
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
			var app apperr.Error
			if !errors.As(err, &app) || app.Status != 409 {
				return WorkflowRunRequestResponse{}, err
			}
		}
		if err := SyncGraphRunToTasks(ctx, pgxTx, *item.WorkflowRunID); err != nil {
			return WorkflowRunRequestResponse{}, err
		}
	} else {
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_workflow_run_requests
			SET status = 'cancelled', failure_reason = COALESCE(failure_reason, $2), finished_at = NOW(), updated_at = NOW()
			WHERE id = $1
		`, requestID, graph.GraphCancelledReason); err != nil {
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
	return s.prepareGraphRequest(ctx, *conv.ProductID, "", expectedRevision, sourceRunID, taskID)
}

func (s Service) PrepareGlobalWorkflowRunRequest(ctx context.Context, conversationID, productID, workflowID string, expectedRevision int, sourceRunID, taskID *string) (PreparedWorkflowRunRequest, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return PreparedWorkflowRunRequest{}, apperr.Conflict("只有全局 Agent conversation 可以请求跨商品执行工作流")
	}
	return s.prepareGraphRequest(ctx, productID, workflowID, expectedRevision, sourceRunID, taskID)
}

func (s Service) prepareGraphRequest(ctx context.Context, productID, workflowID string, expectedRevision int, sourceRunID, taskID *string) (PreparedWorkflowRunRequest, error) {
	if err := requireExpectedWorkflowRevision(expectedRevision); err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	var graphID, title string
	var revision, runnable int
	q := `SELECT id, title, revision FROM workflow_graphs WHERE product_id = $1 AND active = TRUE`
	args := []any{productID}
	if workflowID != "" {
		q += ` AND id = $2`
		args = append(args, workflowID)
	}
	err := pfdb.QueryRow(ctx, s.DB, q, args...).Scan(&graphID, &title, &revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		if workflowID != "" {
			return PreparedWorkflowRunRequest{}, apperr.NotFound("工作流不存在")
		}
		return PreparedWorkflowRunRequest{}, apperr.Conflict("当前商品还没有可执行的工作流")
	}
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if revision != expectedRevision {
		return PreparedWorkflowRunRequest{}, apperr.Conflict(workflowRevisionChangedDetail)
	}
	runnable, err = requireRunnableWorkflow(ctx, s.DB, productID, graphID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	return PreparedWorkflowRunRequest{
		ProductID: productID, WorkflowID: graphID, WorkflowTitle: title,
		WorkflowRevision: revision, RunnableNodeCount: runnable, TaskID: taskID, SourceRunID: sourceRunID,
	}, nil
}

func (s Service) CreateWorkflowRunRequest(ctx context.Context, conversationID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (WorkflowRunRequestResponse, error) {
	return s.createRunRequest(ctx, conversationID, "", workflowID, idempotencyKey, sourceStepID, expectedRevision, taskID, sourceRunID)
}

func (s Service) CreateGlobalWorkflowRunRequest(ctx context.Context, conversationID, productID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (WorkflowRunRequestResponse, error) {
	return s.createRunRequest(ctx, conversationID, productID, workflowID, idempotencyKey, sourceStepID, expectedRevision, taskID, sourceRunID)
}

func (s Service) createRunRequest(ctx context.Context, conversationID, productID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (WorkflowRunRequestResponse, error) {
	if err := requireExpectedWorkflowRevision(expectedRevision); err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	var out WorkflowRunRequestResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		conv, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		targetProduct := productID
		if targetProduct == "" {
			if conv.ProductID == nil {
				return apperr.Conflict("商品工作流 Agent conversation 缺少商品")
			}
			targetProduct = *conv.ProductID
		}
		hash, err := hashWorkflowRunRequest(conversationID, targetProduct, workflowID, sourceStepID, expectedRevision, taskID, sourceRunID)
		if err != nil {
			return err
		}
		var existing, existingHash string
		err = pfdb.QueryRow(ctx, pgxTx, `
			SELECT id, request_hash FROM agent_workflow_run_requests WHERE conversation_id = $1 AND idempotency_key = $2
		`, conversationID, key).Scan(&existing, &existingHash)
		if err == nil {
			if existingHash != hash {
				return apperr.Conflict("同一 idempotency key 不能提交不同的工作流执行请求")
			}
			item, err := loadRunRequest(ctx, pgxTx, &targetProduct, conversationID, existing)
			out = item
			return err
		}
		graphID, err := resolveActiveWorkflow(ctx, pgxTx, targetProduct, workflowID, expectedRevision)
		if err != nil {
			return err
		}
		if _, err := requireRunnableWorkflow(ctx, pgxTx, targetProduct, graphID); err != nil {
			return err
		}
		id := newID()
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO agent_workflow_run_requests (
				id, conversation_id, task_id, product_id, graph_id, expected_workflow_revision, status,
				source_graph_run_id, source_step_id, idempotency_key, request_hash, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, 'awaiting_confirmation', $7, $8, $9, $10, NOW(), NOW())
		`, id, conversationID, taskID, targetProduct, graphID, expectedRevision, sourceRunID, sourceStepID, key, hash); err != nil {
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

func (s Service) ReconcileWorkflowRunRequest(ctx context.Context, conversationID, idempotencyKey, productID, workflowID, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (ReconcileResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return ReconcileResponse{}, err
	}
	var out ReconcileResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		conv, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		targetProduct := productID
		if targetProduct == "" {
			if conv.ProductID == nil {
				return apperr.Conflict("商品工作流 Agent conversation 缺少商品")
			}
			targetProduct = *conv.ProductID
		}
		hash, err := hashWorkflowRunRequest(conversationID, targetProduct, workflowID, sourceStepID, expectedRevision, taskID, sourceRunID)
		if err != nil {
			return err
		}
		var existing, existingHash string
		err = pfdb.QueryRow(ctx, pgxTx, `
			SELECT id, request_hash FROM agent_workflow_run_requests WHERE conversation_id = $1 AND idempotency_key = $2
		`, conversationID, key).Scan(&existing, &existingHash)
		if errors.Is(err, sqldb.ErrNoRows) {
			out = ReconcileResponse{State: "not_applied", Detail: ptr("工作流执行请求尚未提交")}
			return nil
		}
		if err != nil {
			return err
		}
		if existingHash != hash {
			out = ReconcileResponse{State: "conflict", Detail: ptr("同一 idempotency key 已对应其他工作流执行请求")}
			return nil
		}
		loaded, err := loadRunRequest(ctx, pgxTx, &targetProduct, conversationID, existing)
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

const workflowRevisionChangedDetail = "工作流 revision 已变化，请重新读取当前工作流"

func requireExpectedWorkflowRevision(expected int) error {
	if expected <= 0 {
		return apperr.Validation("expected_workflow_revision 必须大于 0")
	}
	return nil
}

func hashWorkflowRunRequest(conversationID, productID, workflowID, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (string, error) {
	return canonjson.SHA256Hex(map[string]any{
		"conversation_id": conversationID, "expected_workflow_revision": expectedRevision,
		"source_step_id": sourceStepID, "task_id": taskID, "source_run_id": sourceRunID,
		"product_id": productID, "workflow_id": workflowID,
	})
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
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_tasks SET
			status = 'awaiting_confirmation',
			waiting_reason = 'workflow_run_confirmation',
			failure_reason = NULL,
			finished_at = NULL,
			updated_at = NOW()
		WHERE id = $1
	`, task.ID); err != nil {
		return err
	}
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}

func resolveActiveWorkflow(ctx context.Context, pgxTx *gorm.DB, productID, workflowID string, expectedRevision int) (string, error) {
	var graphID string
	var revision int
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT id, revision FROM workflow_graphs WHERE product_id = $1 AND active = TRUE
	`, productID).Scan(&graphID, &revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return "", apperr.Conflict("当前商品没有可执行的 schema-v3 工作流")
	}
	if err != nil {
		return "", err
	}
	if workflowID != "" && workflowID != graphID {
		return "", apperr.Conflict("工作流执行请求的 workflow_id 与当前 active 工作流不一致")
	}
	if revision != expectedRevision {
		return "", apperr.Conflict(workflowRevisionChangedDetail)
	}
	return graphID, nil
}

func requireLiveWorkflowRevision(ctx context.Context, pgxTx *gorm.DB, productID, workflowID string, expectedRevision int) error {
	var revision int
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT revision FROM workflow_graphs WHERE id = $1 AND product_id = $2
	`, workflowID, productID).Scan(&revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return apperr.Conflict(workflowRevisionChangedDetail)
	}
	if err != nil {
		return err
	}
	if revision != expectedRevision {
		return apperr.Conflict(workflowRevisionChangedDetail)
	}
	return nil
}

func loadRunRequest(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID, requestID string) (WorkflowRunRequestResponse, error) {
	q := `
		SELECT r.id, r.conversation_id, r.task_id, r.product_id, p.name, r.graph_id, COALESCE(g.title, ''),
			r.expected_workflow_revision, r.status, r.source_graph_run_id, r.graph_run_id, run.status,
			r.source_step_id, r.failure_reason, r.confirmed_at, r.finished_at, r.created_at, r.updated_at
		FROM agent_workflow_run_requests r
		JOIN products p ON p.id = r.product_id
		LEFT JOIN workflow_graphs g ON g.id = r.graph_id
		LEFT JOIN workflow_graph_runs run ON run.id = r.graph_run_id
		WHERE r.id = $1 AND r.conversation_id = $2
	`
	args := []any{requestID, conversationID}
	if productID != nil {
		q += ` AND r.product_id = $3`
		args = append(args, *productID)
	}
	item, err := scanRunRequest(ctx, pgxTx, q, args...)
	if errors.Is(err, sqldb.ErrNoRows) {
		return WorkflowRunRequestResponse{}, apperr.NotFound("Agent workflow run request 不存在")
	}
	return item, err
}

func scanRunRequest(ctx context.Context, pgxTx *gorm.DB, q string, args ...any) (WorkflowRunRequestResponse, error) {
	var item WorkflowRunRequestResponse
	err := pfdb.QueryRow(ctx, pgxTx, q, args...).Scan(
		&item.ID, &item.ConversationID, &item.TaskID, &item.ProductID, &item.ProductName,
		&item.WorkflowID, &item.WorkflowTitle, &item.ExpectedWorkflowRevision, &item.Status,
		&item.SourceRunID, &item.WorkflowRunID, &item.WorkflowRunStatus, &item.SourceStepID,
		&item.FailureReason, &item.ConfirmedAt, &item.FinishedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}
