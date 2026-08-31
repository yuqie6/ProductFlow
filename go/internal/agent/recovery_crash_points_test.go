package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func TestCrashAfterModelStartRecoversUnknownAndInterruptsInvocation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	claimed := createClaimedJournalTurn(t, as)
	payload, err := json.Marshal(map[string]any{
		"model_request_id": "model:crash-start",
		"provider":         "openai-responses",
		"model":            "test-model",
		"execution_mode":   "foreground",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.svc.AppendCheckpoint(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, 1, "before_model_request", payload); err != nil {
		t.Fatal(err)
	}
	expireClaimedTurn(t, as, claimed)
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	var status, reason, invocationStatus string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT p.status, COALESCE(p.terminal_reason_code, ''), i.status
		FROM agent_turn_projections p
		JOIN agent_model_invocations i ON i.turn_projection_id = p.id
		WHERE p.id = $1
	`, claimed.turn.ID).Scan(&status, &reason, &invocationStatus); err != nil {
		t.Fatal(err)
	}
	if status != "unknown" || reason != "execution_interrupted" || invocationStatus != "interrupted" {
		t.Fatalf("status=%s reason=%s invocation=%s", status, reason, invocationStatus)
	}
}

func TestCrashAfterApprovalKeepsPendingRequestWithoutSecondRun(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	task := seedProductGoalTask(t, as)
	claimed := createClaimedTurnForConversation(t, as, *task.ConversationID, task.ProductID)
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
	if _, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{{
		Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
		Kind: "approval/requested", Payload: json.RawMessage(`{"approval_id":"` + pending.ID + `","approval_kind":"workflow_run"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections SET status = 'awaiting_confirmation', workflow_run_request_id = $2, updated_at = NOW() WHERE id = $1
	`, claimed.turn.ID, pending.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions SET phase = 'model', lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW() WHERE id = $1
	`, claimed.lease.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM agent_turn_projections WHERE id = $1
	`, claimed.turn.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "awaiting_confirmation" {
		t.Fatalf("parked approval became %s", status)
	}
	var kinds []string
	if err := as.db.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", claimed.turn.ID).
		Order("sequence").Pluck("kind", &kinds).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range kinds {
		if kind == "turn/end" {
			t.Fatalf("approval crash wrote terminal events %v", kinds)
		}
	}
	var requestCount, runCount int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM agent_workflow_run_requests WHERE conversation_id = $1
	`, claimed.conversationID).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workflow_graph_runs WHERE graph_id = $1
	`, graphID).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 || runCount != 0 {
		t.Fatalf("side effects request=%d run=%d", requestCount, runCount)
	}
}

func TestConfirmWorkflowRunRequestDoesNotCreateSecondGraphRun(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task, pending := createProductGoalRunRequest(t, as, false)
	first := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/confirm", nil, "", nil)
	as.mustStatus(t, first, http.StatusOK)
	var confirmed WorkflowRunRequestResponse
	as.decode(t, first, &confirmed)
	if confirmed.WorkflowRunID == nil {
		t.Fatal("missing workflow run")
	}
	second := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/confirm", nil, "", nil)
	as.mustStatus(t, second, http.StatusOK)
	var replay WorkflowRunRequestResponse
	as.decode(t, second, &replay)
	if replay.WorkflowRunID == nil || *replay.WorkflowRunID != *confirmed.WorkflowRunID {
		t.Fatalf("second confirm run %v want %s", replay.WorkflowRunID, *confirmed.WorkflowRunID)
	}
	var n int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workflow_graph_runs WHERE id = $1
	`, *confirmed.WorkflowRunID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("graph runs %d", n)
	}
}
