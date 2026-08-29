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
		item, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
		if err != nil {
			return err
		}
		if item.Status == "cancelled" {
			out = item
			return nil
		}
		if item.WorkflowRunID != nil {
			if _, err := s.Graph.CancelRunTx(ctx, pgxTx, item.ProductID, item.WorkflowID, *item.WorkflowRunID); err != nil {
				var app apperr.Error
				if !errors.As(err, &app) || app.Status != 409 {
					return err
				}
			}
			if err := SyncGraphRunToTasks(ctx, pgxTx, *item.WorkflowRunID); err != nil {
				return err
			}
		} else {
			if _, err := pfdb.Exec(ctx, pgxTx, `
				UPDATE agent_workflow_run_requests
				SET status = 'cancelled', failure_reason = COALESCE(failure_reason, $2), finished_at = NOW(), updated_at = NOW()
				WHERE id = $1
			`, requestID, graph.GraphCancelledReason); err != nil {
				return err
			}
			if err := parkTaskAfterCancelledRunRequest(ctx, pgxTx, item.TaskID); err != nil {
				return err
			}
		}
		loaded, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
		out = loaded
		return err
	})
	return out, err
}

func cancelWorkflowRunRequest(ctx context.Context, pgxTx *gorm.DB, _ Service, productID *string, conversationID, requestID string) (WorkflowRunRequestResponse, error) {
	item, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
	if err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	if item.Status == "cancelled" {
		return item, nil
	}
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_workflow_run_requests
		SET status = 'cancelled', failure_reason = COALESCE(failure_reason, '工作流运行已取消'), finished_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, requestID); err != nil {
		return WorkflowRunRequestResponse{}, err
	}
	return loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
}

func (s Service) PrepareWorkflowRunRequest(ctx context.Context, conversationID string, sourceRunID *string, taskID *string) (PreparedWorkflowRunRequest, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	return s.prepareGraphRequest(ctx, *conv.ProductID, "", sourceRunID, taskID)
}

func (s Service) PrepareGlobalWorkflowRunRequest(ctx context.Context, conversationID, productID, workflowID string, sourceRunID, taskID *string) (PreparedWorkflowRunRequest, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return PreparedWorkflowRunRequest{}, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return PreparedWorkflowRunRequest{}, apperr.Conflict("只有全局 Agent conversation 可以请求跨商品执行工作流")
	}
	return s.prepareGraphRequest(ctx, productID, workflowID, sourceRunID, taskID)
}

func (s Service) prepareGraphRequest(ctx context.Context, productID, workflowID string, sourceRunID, taskID *string) (PreparedWorkflowRunRequest, error) {
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
	_ = pfdb.QueryRow(ctx, s.DB, `
		SELECT COUNT(*) FROM workflow_graph_nodes
		WHERE graph_id = $1 AND node_type IN ('creative_brief','visual_system','prompt_generation','image_generation')
	`, graphID).Scan(&runnable)
	return PreparedWorkflowRunRequest{
		ProductID: productID, WorkflowID: graphID, WorkflowTitle: title,
		WorkflowRevision: revision, RunnableNodeCount: runnable, TaskID: taskID, SourceRunID: sourceRunID,
	}, nil
}

func (s Service) CreateWorkflowRunRequest(ctx context.Context, conversationID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (WorkflowRunRequestResponse, error) {
	return s.createRunRequest(ctx, conversationID, "", "", idempotencyKey, sourceStepID, expectedRevision, taskID, sourceRunID)
}

func (s Service) CreateGlobalWorkflowRunRequest(ctx context.Context, conversationID, productID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (WorkflowRunRequestResponse, error) {
	return s.createRunRequest(ctx, conversationID, productID, workflowID, idempotencyKey, sourceStepID, expectedRevision, taskID, sourceRunID)
}

func (s Service) createRunRequest(ctx context.Context, conversationID, productID, workflowID, idempotencyKey, sourceStepID string, expectedRevision int, taskID, sourceRunID *string) (WorkflowRunRequestResponse, error) {
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
		prepared := map[string]any{
			"conversation_id": conversationID, "expected_workflow_revision": expectedRevision,
			"source_step_id": sourceStepID, "task_id": taskID, "source_run_id": sourceRunID,
			"product_id": targetProduct, "workflow_id": workflowID,
		}
		hash, err := canonjson.SHA256Hex(prepared)
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
		graphQ := `SELECT id FROM workflow_graphs WHERE product_id = $1 AND active = TRUE`
		graphArgs := []any{targetProduct}
		if workflowID != "" {
			graphQ += ` AND id = $2`
			graphArgs = append(graphArgs, workflowID)
		}
		var graphID string
		if err := pfdb.QueryRow(ctx, pgxTx, graphQ, graphArgs...).Scan(&graphID); err != nil {
			return apperr.Conflict("当前商品没有可执行的 schema-v3 工作流")
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
		item, err := loadRunRequest(ctx, pgxTx, &targetProduct, conversationID, id)
		out = item
		return err
	})
	return out, err
}

func (s Service) ReconcileWorkflowRunRequest(ctx context.Context, conversationID, idempotencyKey string) (ReconcileResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return ReconcileResponse{}, err
	}
	var item WorkflowRunRequestResponse
	err = pfdb.QueryRow(ctx, s.DB, `
		SELECT r.id FROM agent_workflow_run_requests r WHERE r.conversation_id = $1 AND r.idempotency_key = $2
	`, conversationID, key).Scan(&item.ID)
	if errors.Is(err, sqldb.ErrNoRows) {
		return ReconcileResponse{State: "not_applied", Detail: ptr("工作流执行请求尚未提交")}, nil
	}
	if err != nil {
		return ReconcileResponse{}, err
	}
	loaded, err := s.reloadRequest(ctx, nil, conversationID, item.ID)
	if err != nil {
		return ReconcileResponse{State: "unknown", Detail: ptr("工作流执行请求对账结果仍不明确")}, nil
	}
	raw, _ := json.Marshal(loaded)
	return ReconcileResponse{State: "applied", Result: raw, Detail: ptr("工作流执行请求已提交")}, nil
}

func (s Service) reloadRequest(ctx context.Context, productID *string, conversationID, requestID string) (WorkflowRunRequestResponse, error) {
	var out WorkflowRunRequestResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := loadRunRequest(ctx, pgxTx, productID, conversationID, requestID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
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
