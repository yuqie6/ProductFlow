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

func TestUpdateTaskFromTurnProductGoalStaysOpen(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	task := seedProductGoalTask(t, as)
	for _, turnStatus := range []string{"succeeded", "failed", "canceled", "unknown"} {
		if _, err := as.pool.Exec(context.Background(), `
			UPDATE agent_tasks SET status = 'running', waiting_reason = NULL, finished_at = NULL, canceled_at = NULL WHERE id = $1
		`, task.ID); err != nil {
			t.Fatal(err)
		}
		err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
			return updateTaskFromTurn(context.Background(), pgxTx, task.ID, turnStatus, "工具失败", "执行失败：工具失败")
		})
		if err != nil {
			t.Fatal(err)
		}
		got := mustGetTask(t, as, task.ID)
		if got.Status != "waiting_user" {
			t.Fatalf("%s status %s", turnStatus, got.Status)
		}
		if got.WaitingReason == nil || *got.WaitingReason != "goal_loop" {
			t.Fatalf("%s waiting %+v", turnStatus, got.WaitingReason)
		}
		if got.FinishedAt != nil {
			t.Fatalf("%s must keep goal open", turnStatus)
		}
		if got.FailureReason != nil {
			t.Fatalf("%s failure %+v", turnStatus, got.FailureReason)
		}
	}
}

func TestUpdateTaskFromTurnGlobalTerminals(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)

	cases := []struct {
		turn           string
		errText        string
		wantStatus     string
		wantFailure    string
		wantCanceledAt bool
	}{
		{"succeeded", "", "succeeded", "", false},
		{"failed", "boom", "failed", "boom", false},
		{"canceled", "", "canceled", "", true},
		{"unknown", "unproven", "unknown", "unproven", false},
	}
	for _, tc := range cases {
		created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
			"session_id": sess.ID, "title": tc.turn, "goal": "全局任务 " + tc.turn,
			"conversation_id": sess.Conversations[0].ConversationID,
		})
		as.mustStatus(t, created, http.StatusCreated)
		var task TaskResponse
		as.decode(t, created, &task)
		err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
			return updateTaskFromTurn(context.Background(), pgxTx, task.ID, tc.turn, tc.errText, "")
		})
		if err != nil {
			t.Fatal(err)
		}
		got := mustGetTask(t, as, task.ID)
		if got.Status != tc.wantStatus {
			t.Fatalf("%s status %s", tc.turn, got.Status)
		}
		if got.WaitingReason != nil {
			t.Fatalf("%s waiting %+v", tc.turn, got.WaitingReason)
		}
		if got.FinishedAt == nil {
			t.Fatalf("%s missing finished_at", tc.turn)
		}
		if tc.wantFailure == "" {
			if got.FailureReason != nil {
				t.Fatalf("%s failure %+v", tc.turn, got.FailureReason)
			}
		} else if got.FailureReason == nil || *got.FailureReason != tc.wantFailure {
			t.Fatalf("%s failure %+v", tc.turn, got.FailureReason)
		}
		if tc.wantCanceledAt && got.CanceledAt == nil {
			t.Fatalf("%s missing canceled_at", tc.turn)
		}
		if !tc.wantCanceledAt && got.CanceledAt != nil {
			t.Fatalf("%s unexpected canceled_at", tc.turn)
		}
	}
}

func TestUpdateTaskFromTurnLeavesUserOwnedTask(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)

	created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "人工结束", "goal": "用户完成",
	})
	as.mustStatus(t, created, http.StatusCreated)
	var task TaskResponse
	as.decode(t, created, &task)
	complete := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/complete", nil, "", nil)
	as.mustStatus(t, complete, http.StatusOK)

	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return updateTaskFromTurn(context.Background(), pgxTx, task.ID, "failed", "should not apply", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustGetTask(t, as, task.ID)
	if got.Status != "succeeded" {
		t.Fatalf("user-owned overwritten: %s", got.Status)
	}

	paused := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "暂停", "goal": "用户暂停",
	})
	as.mustStatus(t, paused, http.StatusCreated)
	as.decode(t, paused, &task)
	pause := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/pause", nil, "", nil)
	as.mustStatus(t, pause, http.StatusOK)
	err = tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return updateTaskFromTurn(context.Background(), pgxTx, task.ID, "succeeded", "", "Agent Turn 状态：succeeded")
	})
	if err != nil {
		t.Fatal(err)
	}
	got = mustGetTask(t, as, task.ID)
	if got.Status != "succeeded" {
		t.Fatalf("paused global turn success should complete: %s", got.Status)
	}
}

func TestUpdateTaskFromTurnQueuedDoesNotMarkRunning(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	task := seedProductGoalTask(t, as)
	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return updateTaskFromTurn(context.Background(), pgxTx, task.ID, "queued", "", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustGetTask(t, as, task.ID)
	if got.Status != "queued" {
		t.Fatalf("queued turn marked %s", got.Status)
	}
}

func TestUpdateTaskFromTurnWritesSummary(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	task := seedProductGoalTask(t, as)
	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return updateTaskFromTurn(context.Background(), pgxTx, task.ID, "succeeded", "", "商品图已生成")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustGetTask(t, as, task.ID)
	if got.Summary == nil || *got.Summary != "商品图已生成" {
		t.Fatalf("summary %+v", got.Summary)
	}
	if got.Status != "waiting_user" {
		t.Fatalf("status %s", got.Status)
	}
}

func TestUpdateTaskFromTurnKeepsWorkflowRunConfirmation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	task := seedProductGoalTask(t, as)
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_tasks SET status = 'awaiting_confirmation', waiting_reason = 'workflow_run_confirmation' WHERE id = $1
	`, task.ID); err != nil {
		t.Fatal(err)
	}
	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return updateTaskFromTurn(context.Background(), pgxTx, task.ID, "running", "", "工具进行中")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustGetTask(t, as, task.ID)
	if got.Status != "awaiting_confirmation" {
		t.Fatalf("in-flight turn overwrote confirmation status %s", got.Status)
	}
	if got.WaitingReason == nil || *got.WaitingReason != "workflow_run_confirmation" {
		t.Fatalf("waiting %+v", got.WaitingReason)
	}
	err = tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		return updateTaskFromTurn(context.Background(), pgxTx, task.ID, "awaiting_confirmation", "", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	got = mustGetTask(t, as, task.ID)
	if got.WaitingReason == nil || *got.WaitingReason != "workflow_run_confirmation" {
		t.Fatalf("awaiting turn dropped workflow confirmation %+v", got.WaitingReason)
	}
}

func TestGetTaskAndListTasksSynchronizeGraphRun(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task := seedProductGoalTask(t, as)
	runID := attachSucceededGraphRun(t, as, task)

	got := mustGetTask(t, as, task.ID)
	if got.Status != "waiting_user" {
		t.Fatalf("GetTask status %s", got.Status)
	}
	if got.WaitingReason == nil || *got.WaitingReason != "goal_loop" {
		t.Fatalf("GetTask waiting %+v", got.WaitingReason)
	}
	if got.FinishedAt != nil {
		t.Fatal("GetTask must keep product goal open")
	}

	listed := as.do(t, http.MethodGet, "/api/v2/agent-tasks?session_id="+task.SessionID, nil, "", nil)
	as.mustStatus(t, listed, http.StatusOK)
	var page TaskListResponse
	as.decode(t, listed, &page)
	found := false
	for _, item := range page.Items {
		if item.ID != task.ID {
			continue
		}
		found = true
		if item.Status != "waiting_user" {
			t.Fatalf("ListTasks status %s", item.Status)
		}
		if item.WaitingReason == nil || *item.WaitingReason != "goal_loop" {
			t.Fatalf("ListTasks waiting %+v", item.WaitingReason)
		}
	}
	if !found {
		t.Fatal("ListTasks missing synchronized task")
	}

	complete := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/complete", nil, "", nil)
	as.mustStatus(t, complete, http.StatusOK)
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs SET status = 'failed', failure_reason = 'later fail', finished_at = NOW() WHERE id = $1
	`, runID); err != nil {
		t.Fatal(err)
	}
	owned := mustGetTask(t, as, task.ID)
	if owned.Status != "succeeded" {
		t.Fatalf("user complete overwritten by graph run: %s", owned.Status)
	}
}

func TestProductContextConfirmedFactsAndLiveGraphRoles(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	productID := clockid.New()
	graphID := clockid.New()
	sourceID := clockid.New()
	targetID := clockid.New()
	edgeID := clockid.New()
	factID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '资料商品', NOW(), NOW())
	`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO product_fact_set_versions (id, product_id, version, payload_json, payload_hash, created_at)
		VALUES ($1, $2, 3, '{"schema_version":1,"facts":[{"key":"material","value":"棉","source_type":"user","status":"confirmed"}]}', $3, NOW())
	`, factID, productID, strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE products SET current_fact_set_version_id = $2 WHERE id = $1
	`, productID, factID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, '商品创意工作流', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_nodes (
			id, graph_id, node_type, title, position_x, position_y, config_json, created_at, updated_at
		) VALUES
			($1, $3, 'creative_brief', '创作要求', 0, 0, '{}', NOW(), NOW()),
			($2, $3, 'prompt_generation', '提示词', 240, 0, '{}', NOW(), NOW())
	`, sourceID, targetID, graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_edges (
			id, graph_id, source_node_id, target_node_id, data_type, role, sort_order, created_at
		) VALUES ($1, $2, $3, $4, 'creative_brief', 'brief', 0, NOW())
	`, edgeID, graphID, sourceID, targetID); err != nil {
		t.Fatal(err)
	}
	ensure := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, ensure, http.StatusOK)
	var bench WorkbenchResponse
	as.decode(t, ensure, &bench)

	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	resp := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+bench.Conversation.ID+"/product-context", nil, "", auth)
	as.mustStatus(t, resp, http.StatusOK)
	var payload map[string]any
	as.decode(t, resp, &payload)

	confirmed, _ := payload["confirmed_fact_set"].(map[string]any)
	if confirmed == nil {
		t.Fatalf("confirmed_fact_set %+v", payload["confirmed_fact_set"])
	}
	if version, _ := confirmed["version"].(float64); version != 3 {
		t.Fatalf("fact version %+v", confirmed["version"])
	}
	facts, _ := confirmed["facts"].([]any)
	if len(facts) != 1 {
		t.Fatalf("facts %+v", confirmed["facts"])
	}
	row, _ := facts[0].(map[string]any)
	if row["key"] != "material" || row["value"] != "棉" {
		t.Fatalf("fact row %+v", row)
	}

	live, _ := payload["live_graph"].(map[string]any)
	if live == nil {
		t.Fatal("missing live_graph")
	}
	edges, _ := live["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("flat edges %+v", live["edges"])
	}
	nodes, _ := live["nodes"].([]any)
	var sourceNode, targetNode map[string]any
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		switch node["id"] {
		case sourceID:
			sourceNode = node
		case targetID:
			targetNode = node
		}
	}
	if sourceNode == nil || targetNode == nil {
		t.Fatalf("nodes %+v", nodes)
	}
	outgoing, _ := sourceNode["outgoing"].([]any)
	incoming, _ := targetNode["incoming"].([]any)
	if len(outgoing) != 1 || len(incoming) != 1 {
		t.Fatalf("roles out=%+v in=%+v", outgoing, incoming)
	}
	outEdge, _ := outgoing[0].(map[string]any)
	inEdge, _ := incoming[0].(map[string]any)
	if outEdge["source"] != sourceID || outEdge["target"] != targetID || outEdge["role"] != "brief" || outEdge["data_type"] != "creative_brief" {
		t.Fatalf("outgoing %+v", outEdge)
	}
	if inEdge["source"] != sourceID || inEdge["target"] != targetID || inEdge["role"] != "brief" || inEdge["data_type"] != "creative_brief" {
		t.Fatalf("incoming %+v", inEdge)
	}
}

func seedProductGoalTask(t *testing.T, as *agentServer) TaskResponse {
	t.Helper()
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
	if bench.Conversation.SessionID == nil {
		t.Fatal("workbench conversation missing session")
	}
	created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": *bench.Conversation.SessionID, "title": "出图", "goal": "完成商品图",
		"conversation_id": bench.Conversation.ID,
	})
	as.mustStatus(t, created, http.StatusCreated)
	var task TaskResponse
	as.decode(t, created, &task)
	return task
}

func attachSucceededGraphRun(t *testing.T, as *agentServer, task TaskResponse) string {
	t.Helper()
	if task.ConversationID == nil || task.ProductID == nil {
		t.Fatal("task missing conversation or product")
	}
	var graphID string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT id FROM workflow_graphs WHERE product_id = $1 AND active = TRUE LIMIT 1
	`, *task.ProductID).Scan(&graphID); err != nil {
		t.Fatal(err)
	}
	runID := clockid.New()
	requestID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at, finished_at
		) VALUES ($1, $2, 'succeeded', 'graph', 1, '{}'::json, TRUE, NOW(), NOW())
	`, runID, graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_workflow_run_requests (
			id, conversation_id, task_id, product_id, graph_id, expected_workflow_revision, status,
			source_step_id, idempotency_key, request_hash, graph_run_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 1, 'confirmed', 'run-sync', $6, $7, $8, NOW(), NOW())
	`, requestID, *task.ConversationID, task.ID, *task.ProductID, graphID, clockid.New(), strings.Repeat("c", 64), runID); err != nil {
		t.Fatal(err)
	}
	return runID
}

func mustGetTask(t *testing.T, as *agentServer, taskID string) TaskResponse {
	t.Helper()
	resp := as.do(t, http.MethodGet, "/api/v2/agent-tasks/"+taskID, nil, "", nil)
	as.mustStatus(t, resp, http.StatusOK)
	var task TaskResponse
	as.decode(t, resp, &task)
	return task
}
