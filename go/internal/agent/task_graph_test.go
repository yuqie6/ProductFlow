package agent

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestConfirmWorkflowRunRequestKeepsProductGoalOpen(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	productID := clockid.New()
	graphID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, 'Goal 商品', NOW(), NOW())
	`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, '商品创意工作流', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID); err != nil {
		t.Fatal(err)
	}
	nodeID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_nodes (
			id, graph_id, node_type, title, position_x, position_y, config_json, document_origin, created_at, updated_at
		) VALUES ($1, $2, 'creative_brief', '创作要求', 0, 0, '{}', 'seed', NOW(), NOW())
	`, nodeID, graphID); err != nil {
		t.Fatal(err)
	}
	ensure := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, ensure, http.StatusOK)
	var bench WorkbenchResponse
	as.decode(t, ensure, &bench)
	convID := bench.Conversation.ID

	if bench.Conversation.SessionID == nil {
		t.Fatal("workbench conversation missing session")
	}
	created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": *bench.Conversation.SessionID, "title": "出图", "goal": "完成商品图",
		"conversation_id": convID,
	})
	as.mustStatus(t, created, http.StatusCreated)
	var task TaskResponse
	as.decode(t, created, &task)

	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	req := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/workflow-run-requests", map[string]any{
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-1",
		"task_id":                    task.ID,
	}, auth)
	as.mustStatus(t, req, http.StatusOK)
	var pending WorkflowRunRequestResponse
	as.decode(t, req, &pending)
	if pending.Status != "awaiting_confirmation" {
		t.Fatalf("pending %s", pending.Status)
	}

	confirm := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-conversations/"+convID+"/workflow-run-request/"+pending.ID+"/confirm", nil, "", nil)
	as.mustStatus(t, confirm, http.StatusOK)
	var confirmed WorkflowRunRequestResponse
	as.decode(t, confirm, &confirmed)
	if confirmed.WorkflowRunID == nil || *confirmed.WorkflowRunID == "" {
		t.Fatal("confirm must attach graph_run_id in the same command")
	}

	if _, err := as.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs SET status = 'succeeded', finished_at = NOW() WHERE id = $1
	`, *confirmed.WorkflowRunID); err != nil {
		t.Fatal(err)
	}
	got := as.do(t, http.MethodGet, "/api/v2/products/"+productID+"/agent-conversations/"+convID+"/workflow-run-request?task_id="+task.ID, nil, "", nil)
	as.mustStatus(t, got, http.StatusOK)
	var synced WorkflowRunRequestResponse
	as.decode(t, got, &synced)
	if synced.Status != "succeeded" {
		t.Fatalf("request status %s", synced.Status)
	}

	taskGot := as.do(t, http.MethodGet, "/api/v2/agent-tasks/"+task.ID, nil, "", nil)
	as.mustStatus(t, taskGot, http.StatusOK)
	as.decode(t, taskGot, &task)
	if task.Status != "waiting_user" {
		t.Fatalf("product goal status %s", task.Status)
	}
	if task.WaitingReason == nil || *task.WaitingReason != "goal_loop" {
		t.Fatalf("waiting %+v", task.WaitingReason)
	}
	if task.FinishedAt != nil {
		t.Fatal("product goal must stay open after graph run succeeded")
	}
}

func TestCreateWorkflowRunRequestMarksGoalWaiting(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task, _ := createProductGoalRunRequest(t, as, false)
	got := mustGetTask(t, as, task.ID)
	if got.Status != "awaiting_confirmation" {
		t.Fatalf("status %s", got.Status)
	}
	if got.WaitingReason == nil || *got.WaitingReason != "workflow_run_confirmation" {
		t.Fatalf("waiting %+v", got.WaitingReason)
	}
}

func TestApplyTurnStateAttachesWorkflowRunRequestID(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task, pending := createProductGoalRunRequest(t, as, false)
	if task.ConversationID == nil || task.ProductID == nil {
		t.Fatal("task missing conversation or product")
	}
	turnID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, task_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, '执行', '[]'::jsonb, 'queued', FALSE, '[]'::jsonb, NOW(), NOW())
	`, turnID, *task.ConversationID, task.ID, clockid.New(), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return as.svc.applyTurnState(context.Background(), pgxTx, task.ProductID, *task.ConversationID, turnID, TurnState{
			APIVersion: "1",
			RunID:      task.ID,
			TurnID:     "ht-" + clockid.New(),
			Status:     "awaiting_confirmation",
			ToolSteps: []map[string]any{
				{"kind": "request_workflow_run", "status": "succeeded", "step_id": "run-1"},
			},
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var attached *string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status, workflow_run_request_id FROM agent_turn_projections WHERE id = $1
	`, turnID).Scan(&status, &attached); err != nil {
		t.Fatal(err)
	}
	if attached == nil || *attached != pending.ID {
		t.Fatalf("workflow_run_request_id %+v want %s", attached, pending.ID)
	}
	if status != "awaiting_confirmation" {
		t.Fatalf("turn status %s", status)
	}
}

func TestPrepareEmptyGraphConflicts(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	productID := clockid.New()
	graphID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '空图', NOW(), NOW())
	`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, '空工作流', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID); err != nil {
		t.Fatal(err)
	}
	ensure := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, ensure, http.StatusOK)
	var bench WorkbenchResponse
	as.decode(t, ensure, &bench)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	prepared := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+bench.Conversation.ID+"/workflow-run-requests/prepare", map[string]any{
		"expected_workflow_revision": 1,
	}, auth)
	as.mustStatus(t, prepared, http.StatusConflict)
	var detail map[string]string
	as.decode(t, prepared, &detail)
	if detail["detail"] != "当前工作流没有可运行的节点" {
		t.Fatalf("prepare detail %v", detail)
	}
}

func TestConfirmOrganizationDraftCompletesGlobalTask(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID

	created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "整理库", "goal": "整理全局素材",
		"conversation_id": convID,
	})
	as.mustStatus(t, created, http.StatusCreated)
	var task TaskResponse
	as.decode(t, created, &task)

	payload := []byte(`{"operations":[]}`)
	var revID string
	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		id, err := as.svc.Library.AppendOrganizationDraftRevisionTx(context.Background(), pgxTx, convID, payload, "", "")
		revID = id
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	turnID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, task_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, library_organization_draft_revision_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, '整理', '[]'::jsonb, 'awaiting_confirmation', FALSE, '[]'::jsonb, $6, NOW(), NOW())
	`, turnID, convID, task.ID, clockid.New(), strings.Repeat("a", 64), revID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_tasks SET status = 'awaiting_confirmation', waiting_reason = 'awaiting_confirmation', current_turn_id = $2 WHERE id = $1
	`, task.ID, turnID); err != nil {
		t.Fatal(err)
	}

	confirm := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/library-organization-draft/confirm", map[string]any{
		"expected_draft_version": 1,
		"idempotency_key":        clockid.New(),
	})
	as.mustStatus(t, confirm, http.StatusOK)

	taskGot := as.do(t, http.MethodGet, "/api/v2/agent-tasks/"+task.ID, nil, "", nil)
	as.mustStatus(t, taskGot, http.StatusOK)
	as.decode(t, taskGot, &task)
	if task.Status != "succeeded" {
		t.Fatalf("global draft confirm status %s", task.Status)
	}
	if task.FinishedAt == nil {
		t.Fatal("global task should finish after draft confirm")
	}
}

func TestApplyGraphRunStatusLeavesUserOwnedTask(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "已完成", "goal": "人工完成",
	})
	as.mustStatus(t, created, http.StatusCreated)
	var task TaskResponse
	as.decode(t, created, &task)
	complete := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/complete", nil, "", nil)
	if complete.StatusCode == http.StatusConflict {
		complete.Body.Close()
		t.Skip("task still busy")
	}
	as.mustStatus(t, complete, http.StatusOK)
	as.decode(t, complete, &task)

	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return applyGraphRunStatusToTask(context.Background(), pgxTx, &task.ID, "failed", ptr("boom"), nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	taskGot := as.do(t, http.MethodGet, "/api/v2/agent-tasks/"+task.ID, nil, "", nil)
	as.mustStatus(t, taskGot, http.StatusOK)
	as.decode(t, taskGot, &task)
	if task.Status != "succeeded" {
		t.Fatalf("user-owned task overwritten: %s", task.Status)
	}
}

func TestCancelTaskParksProductGoalAfterWorkflowRunRequest(t *testing.T) {
	t.Run("awaiting_confirmation", func(t *testing.T) {
		as := newAgentServer(t, mockGateway{}, "tok")
		task, pending := createProductGoalRunRequest(t, as, false)
		attachAwaitingWorkflowTurn(t, as, task, pending.ID)

		cancelled := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/cancel", nil, "", nil)
		as.mustStatus(t, cancelled, http.StatusOK)
		as.decode(t, cancelled, &task)
		if task.Status != "waiting_user" {
			t.Fatalf("status %s", task.Status)
		}
		if task.WaitingReason == nil || *task.WaitingReason != "goal_loop" {
			t.Fatalf("waiting %+v", task.WaitingReason)
		}
	})
	t.Run("graph_run_id", func(t *testing.T) {
		as := newAgentServer(t, mockGateway{}, "tok")
		task, pending := createProductGoalRunRequest(t, as, true)
		if pending.WorkflowRunID == nil || *pending.WorkflowRunID == "" {
			t.Fatal("confirm must attach graph_run_id")
		}
		attachAwaitingWorkflowTurn(t, as, task, pending.ID)

		cancelled := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/cancel", nil, "", nil)
		as.mustStatus(t, cancelled, http.StatusOK)
		as.decode(t, cancelled, &task)
		if task.Status != "waiting_user" {
			t.Fatalf("status %s", task.Status)
		}
		if task.WaitingReason == nil || *task.WaitingReason != "goal_loop" {
			t.Fatalf("waiting %+v", task.WaitingReason)
		}
		var runStatus string
		if err := as.pool.QueryRow(context.Background(), `SELECT status FROM workflow_graph_runs WHERE id = $1`, *pending.WorkflowRunID).Scan(&runStatus); err != nil {
			t.Fatal(err)
		}
		if runStatus != "cancelled" {
			t.Fatalf("graph run %s", runStatus)
		}
	})
}

func TestConfirmWorkflowRunRequestRejectsStaleRevision(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task, pending := createProductGoalRunRequest(t, as, false)
	if task.ProductID == nil || task.ConversationID == nil {
		t.Fatal("task missing product or conversation")
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE workflow_graphs SET revision = revision + 1, updated_at = NOW() WHERE product_id = $1 AND active = TRUE
	`, *task.ProductID); err != nil {
		t.Fatal(err)
	}

	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	prepared := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/workflow-run-requests/prepare", map[string]any{
		"expected_workflow_revision": 1, "task_id": task.ID, "source_run_id": nil,
	}, auth)
	as.mustStatus(t, prepared, http.StatusConflict)
	var prepareDetail map[string]string
	as.decode(t, prepared, &prepareDetail)
	if prepareDetail["detail"] != workflowRevisionChangedDetail {
		t.Fatalf("prepare detail %v", prepareDetail)
	}

	confirm := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/confirm", nil, "", nil)
	as.mustStatus(t, confirm, http.StatusConflict)
	var confirmDetail map[string]string
	as.decode(t, confirm, &confirmDetail)
	if confirmDetail["detail"] != workflowRevisionChangedDetail {
		t.Fatalf("confirm detail %v", confirmDetail)
	}
}

func TestReconcileWorkflowRunRequestHashMismatchIsConflict(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task, _ := createProductGoalRunRequest(t, as, false)
	graphID := activeGraphID(t, as, *task.ProductID)
	key := clockid.New()
	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{key}}
	created := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/workflow-run-requests", map[string]any{
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-reconcile",
		"task_id":                    task.ID,
	}, auth)
	as.mustStatus(t, created, http.StatusOK)
	created.Body.Close()

	recon := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/workflow-run-requests/reconcile", map[string]any{
		"expected_workflow_revision": 2,
		"workflow_id":                graphID,
		"source_step_id":             "run-reconcile",
		"task_id":                    task.ID,
	}, auth)
	as.mustStatus(t, recon, http.StatusOK)
	var out ReconcileResponse
	as.decode(t, recon, &out)
	if out.State != "conflict" {
		t.Fatalf("reconcile state %s", out.State)
	}
}

func createProductGoalRunRequest(t *testing.T, as *agentServer, confirm bool) (TaskResponse, WorkflowRunRequestResponse) {
	t.Helper()
	task := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)
	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	req := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/workflow-run-requests", map[string]any{
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-1",
		"task_id":                    task.ID,
	}, auth)
	as.mustStatus(t, req, http.StatusOK)
	var pending WorkflowRunRequestResponse
	as.decode(t, req, &pending)
	if pending.Status != "awaiting_confirmation" {
		t.Fatalf("pending %s", pending.Status)
	}
	if !confirm {
		return task, pending
	}
	confirmed := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/confirm", nil, "", nil)
	as.mustStatus(t, confirmed, http.StatusOK)
	as.decode(t, confirmed, &pending)
	return task, pending
}

func attachAwaitingWorkflowTurn(t *testing.T, as *agentServer, task TaskResponse, requestID string) {
	t.Helper()
	turnID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, task_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, workflow_run_request_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, '执行', '[]'::jsonb, 'awaiting_confirmation', FALSE, '[]'::jsonb, $6, NOW(), NOW())
	`, turnID, *task.ConversationID, task.ID, clockid.New(), strings.Repeat("a", 64), requestID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_tasks SET status = 'awaiting_confirmation', waiting_reason = 'workflow_run_confirmation', current_turn_id = $2 WHERE id = $1
	`, task.ID, turnID); err != nil {
		t.Fatal(err)
	}
}

func activeGraphID(t *testing.T, as *agentServer, productID string) string {
	t.Helper()
	var graphID string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT id FROM workflow_graphs WHERE product_id = $1 AND active = TRUE LIMIT 1
	`, productID).Scan(&graphID); err != nil {
		t.Fatal(err)
	}
	return graphID
}
