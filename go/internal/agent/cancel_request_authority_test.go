package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestCancelOldRequestPreservesNewConfirmation(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_cancel_request_%d", time.Now().UnixNano()))
	as := newAgentServerOnDB(t, mockGateway{}, "tok", pool, db)
	task, old := createProductGoalRunRequest(t, as, false)
	attachAwaitingWorkflowTurn(t, as, task, old.ID)
	resp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/workflow-run-requests", map[string]any{"expected_workflow_revision": 1, "workflow_id": old.WorkflowID, "source_step_id": "new-request", "task_id": task.ID}, http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}})
	as.mustStatus(t, resp, http.StatusOK)
	var latest WorkflowRunRequestResponse
	as.decode(t, resp, &latest)
	attachAwaitingWorkflowTurn(t, as, task, latest.ID)
	for _, id := range []string{old.ID, latest.ID} {
		if err := db.Model(&schema.AgentTurnProjections{}).Where("workflow_run_request_id = ?", id).Update("harness_turn_id", clockid.New()).Error; err != nil {
			t.Fatal(err)
		}
	}
	approvalCount := func(id string) int {
		t.Helper()
		var count int
		if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_turn_events e JOIN agent_turn_projections p ON p.id=e.turn_projection_id WHERE p.workflow_run_request_id=$1 AND e.kind='approval/resolved' AND e.payload_json::jsonb->>'decision'='denied'`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	path := "/api/v2/products/" + *task.ProductID + "/agent-conversations/" + *task.ConversationID + "/workflow-run-request/" + old.ID + "/cancel"
	for i := 0; i < 2; i++ {
		resp = as.do(t, http.MethodPost, path, nil, "", nil)
		as.mustStatus(t, resp, http.StatusOK)
	}
	read := func(id string) (string, string, string, string) {
		t.Helper()
		var r, p, c, status string
		err := pool.QueryRow(context.Background(), `SELECT r.status,p.status,c.status,t.status FROM agent_workflow_run_requests r JOIN agent_turn_projections p ON p.workflow_run_request_id=r.id JOIN agent_conversations c ON c.id=r.conversation_id JOIN agent_tasks t ON t.id=r.task_id WHERE r.id=$1`, id).Scan(&r, &p, &c, &status)
		if err != nil {
			t.Fatal(err)
		}
		return r, p, c, status
	}
	if approvalCount(old.ID) != 1 || approvalCount(latest.ID) != 0 {
		t.Fatal("old cancel journal duplicated or affected new approval")
	}
	r, p, c, status := read(old.ID)
	if r != "cancelled" || p != "canceled" {
		t.Fatalf("old request not canceled: request=%s turn=%s", r, p)
	}
	if c != "awaiting_confirmation" || status != "awaiting_confirmation" {
		t.Errorf("old cancel overwrote current work: conversation=%s task=%s", c, status)
	}
	r, p, _, _ = read(latest.ID)
	if r != "awaiting_confirmation" || p != "awaiting_confirmation" {
		t.Fatalf("new request/turn changed: %s/%s", r, p)
	}
	var got schema.AgentTasks
	if err := db.Where("id=?", task.ID).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.WaitingReason == nil || *got.WaitingReason != "workflow_run_confirmation" {
		t.Errorf("new waiting reason overwritten: %v", got.WaitingReason)
	}
	if err := db.Exec("ALTER TABLE agent_tasks ADD CONSTRAINT test_latest_cancel_failure CHECK(status <> 'waiting_user')").Error; err != nil {
		t.Fatal(err)
	}
	_, err := as.svc.CancelWorkflowRunRequestHTTP(context.Background(), task.ProductID, *task.ConversationID, latest.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "test_latest_cancel_failure" {
		t.Fatalf("expected final task SQL error: %v", err)
	}
	if approvalCount(latest.ID) != 0 {
		t.Fatal("failed cancel left approval event")
	}
	r, p, c, status = read(latest.ID)
	if r != "awaiting_confirmation" || p != "awaiting_confirmation" || c != "awaiting_confirmation" || status != "awaiting_confirmation" {
		t.Fatalf("partial cancel: request=%s turn=%s conversation=%s task=%s", r, p, c, status)
	}
	if err := db.Exec("ALTER TABLE agent_tasks DROP CONSTRAINT test_latest_cancel_failure").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := as.svc.CancelWorkflowRunRequestHTTP(context.Background(), task.ProductID, *task.ConversationID, latest.ID); err != nil {
		t.Fatal(err)
	}
	if approvalCount(latest.ID) != 1 {
		t.Fatal("latest cancel missing approval event")
	}
	r, p, c, status = read(latest.ID)
	if r != "cancelled" || p != "canceled" || c != "canceled" || status != "waiting_user" {
		t.Fatalf("latest cancel failed: %s/%s/%s/%s", r, p, c, status)
	}
}
