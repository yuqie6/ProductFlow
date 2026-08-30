package agent

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestWorkflowRunRequestHashMatchesPythonShape(t *testing.T) {
	convID := "conv-1"
	workflowID := "graph-1"
	stepID := "run-1"
	productID := "prod-1"

	productPayload := workflowRunRequestHashPayload(convID, nil, workflowID, stepID, 1, nil, nil, runScopeSpec{})
	productJSON, err := canonjson.Compact(productPayload)
	if err != nil {
		t.Fatal(err)
	}
	got := string(productJSON)
	if !strings.Contains(got, `"schema_version":1`) {
		t.Fatalf("product hash missing schema_version: %s", got)
	}
	if strings.Contains(got, `"product_id"`) {
		t.Fatalf("product-scoped hash must omit product_id: %s", got)
	}
	if strings.Contains(got, `"source_run_id"`) {
		t.Fatalf("empty source_run_id must be omitted: %s", got)
	}
	if !strings.Contains(got, `"task_id":null`) {
		t.Fatalf("nil task_id must encode as JSON null: %s", got)
	}

	globalPayload := workflowRunRequestHashPayload(convID, &productID, workflowID, stepID, 1, nil, nil, runScopeSpec{})
	globalJSON, err := canonjson.Compact(globalPayload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(globalJSON), `"product_id":"prod-1"`) {
		t.Fatalf("global hash must include product_id: %s", globalJSON)
	}

	productHash, err := hashWorkflowRunRequest(convID, nil, workflowID, stepID, 1, nil, nil, runScopeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	globalHash, err := hashWorkflowRunRequest(convID, &productID, workflowID, stepID, 1, nil, nil, runScopeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if productHash == globalHash {
		t.Fatal("product-scoped and global hashes must differ")
	}

	emptySource := ptr("")
	withEmptySource, err := hashWorkflowRunRequest(convID, nil, workflowID, stepID, 1, nil, emptySource, runScopeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if withEmptySource != productHash {
		t.Fatal("empty source_run_id must be omitted from the hash")
	}

	sourceID := "run-source"
	withSource, err := hashWorkflowRunRequest(convID, nil, workflowID, stepID, 1, nil, &sourceID, runScopeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	sourceJSON, err := canonjson.Compact(workflowRunRequestHashPayload(convID, nil, workflowID, stepID, 1, nil, &sourceID, runScopeSpec{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sourceJSON), `"source_run_id":"run-source"`) {
		t.Fatalf("present source_run_id must be hashed: %s", sourceJSON)
	}
	if withSource == productHash {
		t.Fatal("source_run_id must change the hash when present")
	}

	trimmedStep, err := hashWorkflowRunRequest(convID, nil, " "+workflowID+" ", " "+stepID+" ", 1, nil, nil, runScopeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if trimmedStep != productHash {
		t.Fatal("source_step_id and workflow_id must be trimmed before hashing")
	}
}

func TestCreateGlobalWorkflowRunRequestRejectsProductConversation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)
	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	resp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/global-workflow-run-requests", map[string]any{
		"product_id":                 *task.ProductID,
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-1",
	}, auth)
	mustErrorDetail(t, as, resp, http.StatusConflict, globalWorkflowConversationConflict)
}

func TestCreateWorkflowRunRequestRejectsGlobalConversation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)
	convID := newGlobalConversationID(t, as)
	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	resp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/workflow-run-requests", map[string]any{
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-1",
	}, auth)
	mustErrorDetail(t, as, resp, http.StatusConflict, "当前 conversation 不是商品工作流作用域")
}

func TestReconcileGlobalWorkflowRunRequestRejectsProductConversation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)
	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	resp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/global-workflow-run-requests/reconcile", map[string]any{
		"product_id":                 *task.ProductID,
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-1",
	}, auth)
	mustErrorDetail(t, as, resp, http.StatusConflict, globalWorkflowConversationConflict)
}

func TestCancelWorkflowRunRequestRejectsFinishedGraphRun(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "unknown"} {
		t.Run(status, func(t *testing.T) {
			as := newAgentServer(t, mockGateway{}, "tok")
			task, pending := createProductGoalRunRequest(t, as, true)
			if pending.WorkflowRunID == nil {
				t.Fatal("confirm must attach graph_run_id")
			}
			if _, err := as.pool.Exec(context.Background(), `
				UPDATE workflow_graph_runs SET status = $1, finished_at = NOW() WHERE id = $2
			`, status, *pending.WorkflowRunID); err != nil {
				t.Fatal(err)
			}
			resp := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/cancel", nil, "", nil)
			mustErrorDetail(t, as, resp, http.StatusConflict, finishedGraphRunCannotCancelDetail)
		})
	}
}

func TestCancelWorkflowRunRequestAllowsAlreadyCancelledGraphRun(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task, pending := createProductGoalRunRequest(t, as, true)
	if pending.WorkflowRunID == nil {
		t.Fatal("confirm must attach graph_run_id")
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs SET status = 'cancelled', finished_at = NOW() WHERE id = $1
	`, *pending.WorkflowRunID); err != nil {
		t.Fatal(err)
	}
	resp := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/cancel", nil, "", nil)
	as.mustStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

func TestCancelWorkflowRunRequestIsIdempotentWhenAlreadyCancelled(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task, pending := createProductGoalRunRequest(t, as, false)
	path := "/api/v2/products/" + *task.ProductID + "/agent-conversations/" + *task.ConversationID + "/workflow-run-request/" + pending.ID + "/cancel"
	first := as.do(t, http.MethodPost, path, nil, "", nil)
	as.mustStatus(t, first, http.StatusOK)
	first.Body.Close()
	second := as.do(t, http.MethodPost, path, nil, "", nil)
	as.mustStatus(t, second, http.StatusOK)
	second.Body.Close()
}

func TestCreateWorkflowRunRequestSourceRunMustBeFailedRetryable(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)

	okRun := insertGraphRun(t, as, graphID, "failed", true)
	created := createWorkflowRunRequest(t, as, task, graphID, &okRun, http.StatusOK)
	if created.SourceRunID == nil || *created.SourceRunID != okRun {
		t.Fatalf("source_run_id %+v", created.SourceRunID)
	}

	succeeded := insertGraphRun(t, as, graphID, "succeeded", true)
	resp := postWorkflowRunRequest(t, as, *task.ConversationID, graphID, task.ID, &succeeded)
	mustErrorDetail(t, as, resp, http.StatusBadRequest, sourceRunNotRetryableDetail)

	notRetryable := insertGraphRun(t, as, graphID, "failed", false)
	resp = postWorkflowRunRequest(t, as, *task.ConversationID, graphID, task.ID, &notRetryable)
	mustErrorDetail(t, as, resp, http.StatusBadRequest, sourceRunNotRetryableDetail)

	other := seedProductGoalTask(t, as)
	foreignRun := insertGraphRun(t, as, activeGraphID(t, as, *other.ProductID), "failed", true)
	resp = postWorkflowRunRequest(t, as, *task.ConversationID, graphID, task.ID, &foreignRun)
	mustErrorDetail(t, as, resp, http.StatusNotFound, "工作流运行不存在")
}

func TestCreateWorkflowRunRequestRejectsMismatchedTask(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task := seedProductGoalTask(t, as)
	other := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)
	resp := postWorkflowRunRequest(t, as, *task.ConversationID, graphID, other.ID, nil)
	mustErrorDetail(t, as, resp, http.StatusConflict, taskConversationMismatchDetail)
}

func mustErrorDetail(t *testing.T, as *agentServer, resp *http.Response, status int, detail string) {
	t.Helper()
	as.mustStatus(t, resp, status)
	var body map[string]string
	as.decode(t, resp, &body)
	if body["detail"] != detail {
		t.Fatalf("detail %q want %q", body["detail"], detail)
	}
}

func newGlobalConversationID(t *testing.T, as *agentServer) string {
	t.Helper()
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	if len(sess.Conversations) == 0 {
		t.Fatal("global session missing conversation")
	}
	return sess.Conversations[0].ConversationID
}

func insertGraphRun(t *testing.T, as *agentServer, graphID, status string, retryable bool) string {
	t.Helper()
	runID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at, finished_at
		) VALUES ($1, $2, $3, 'graph', 1, '{}'::json, $4, NOW(), NOW())
	`, runID, graphID, status, retryable); err != nil {
		t.Fatal(err)
	}
	return runID
}

func postWorkflowRunRequest(t *testing.T, as *agentServer, conversationID, graphID, taskID string, sourceRunID *string) *http.Response {
	t.Helper()
	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	body := map[string]any{
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-1",
		"task_id":                    taskID,
	}
	if sourceRunID != nil {
		body["source_run_id"] = *sourceRunID
	}
	return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+conversationID+"/workflow-run-requests", body, auth)
}

func createWorkflowRunRequest(t *testing.T, as *agentServer, task TaskResponse, graphID string, sourceRunID *string, wantStatus int) WorkflowRunRequestResponse {
	t.Helper()
	resp := postWorkflowRunRequest(t, as, *task.ConversationID, graphID, task.ID, sourceRunID)
	as.mustStatus(t, resp, wantStatus)
	var pending WorkflowRunRequestResponse
	as.decode(t, resp, &pending)
	return pending
}
