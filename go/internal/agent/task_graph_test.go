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
			id, graph_id, node_type, title, position_x, position_y, config_json, created_at, updated_at
		) VALUES ($1, $2, 'creative_brief', '创作要求', 0, 0, '{}', NOW(), NOW())
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
