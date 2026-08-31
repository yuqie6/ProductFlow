package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func TestConfirmWorkflowRunRequestWritesApprovalResolved(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	productID := clockid.New()
	graphID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '确认商品', NOW(), NOW())
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
	if convID == "" {
		t.Fatal("missing conversation")
	}

	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	req := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/workflow-run-requests", map[string]any{
		"expected_workflow_revision": 1,
		"workflow_id":                graphID,
		"source_step_id":             "run-1",
	}, auth)
	as.mustStatus(t, req, http.StatusOK)
	var pending WorkflowRunRequestResponse
	as.decode(t, req, &pending)

	projectionID := clockid.New()
	harnessTurnID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, harness_turn_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, workflow_run_request_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, '执行', '[]'::jsonb, 'awaiting_confirmation', FALSE, '[]'::jsonb, $6, NOW(), NOW())
	`, projectionID, convID, harnessTurnID, clockid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", pending.ID); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"approval_id": pending.ID, "approval_kind": "workflow_run",
	})
	if err := as.db.Create(&schema.AgentTurnEvents{
		ID: clockid.New(), TurnProjectionID: projectionID, RunID: convID, TurnID: harnessTurnID,
		SchemaVersion: 1, Sequence: 1, Kind: "approval/requested", PayloadJSON: string(payload),
		CreatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	confirm := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-conversations/"+convID+"/workflow-run-request/"+pending.ID+"/confirm", nil, "", nil)
	as.mustStatus(t, confirm, http.StatusOK)

	var kinds []string
	if err := as.db.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", projectionID).
		Order("sequence").Pluck("kind", &kinds).Error; err != nil {
		t.Fatal(err)
	}
	if len(kinds) < 2 || kinds[len(kinds)-1] != "approval/resolved" {
		t.Fatalf("journal kinds %v", kinds)
	}
}

func TestSubmitTurnRejectedWhileAwaitingConfirmation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_conversations SET status = 'awaiting_confirmation' WHERE id = $1
	`, convID); err != nil {
		t.Fatal(err)
	}
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "此时不应提交", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, turn, http.StatusConflict)
}

func seedProductGraphConversation(t *testing.T, as *agentServer) (productID, graphID, convID string) {
	t.Helper()
	productID = clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '提案商品', NOW(), NOW())
	`, productID); err != nil {
		t.Fatal(err)
	}
	created := as.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "", nil)
	as.mustStatus(t, created, http.StatusCreated)
	var projection graph.Projection
	as.decode(t, created, &projection)
	graphID = projection.ID
	ensure := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, ensure, http.StatusOK)
	var bench WorkbenchResponse
	as.decode(t, ensure, &bench)
	convID = bench.Conversation.ID
	if convID == "" {
		t.Fatal("missing conversation")
	}
	return productID, graphID, convID
}

func seedPendingGraphProposalTurn(t *testing.T, as *agentServer, productID, graphID, convID string) (proposalID, projectionID string) {
	t.Helper()
	changeSet, err := json.Marshal(map[string]any{
		"base_graph_revision": 1,
		"summary":             "提案加节点",
		"actor_type":          "agent",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "n1", "node_type": "product_source", "title": "商品资料", "config": map[string]any{"source_product_id": productID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	proposalID = clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_proposals (
			id, graph_id, conversation_id, status, summary, base_graph_revision, change_set_json, created_at
		) VALUES ($1, $2, $3, 'pending', '提案加节点', 1, $4, NOW())
	`, proposalID, graphID, convID, changeSet); err != nil {
		t.Fatal(err)
	}
	projectionID = clockid.New()
	harnessTurnID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, harness_turn_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, '改图', '[]'::jsonb, 'succeeded', FALSE, '[]'::jsonb, NOW(), NOW())
	`, projectionID, convID, harnessTurnID, clockid.New(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"approval_id": proposalID, "approval_kind": "graph_proposal",
	})
	if err := as.db.Create(&schema.AgentTurnEvents{
		ID: clockid.New(), TurnProjectionID: projectionID, RunID: convID, TurnID: harnessTurnID,
		SchemaVersion: 1, Sequence: 1, Kind: "approval/requested", PayloadJSON: string(payload),
		CreatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	return proposalID, projectionID
}

func lastJournalEvent(t *testing.T, as *agentServer, projectionID string) schema.AgentTurnEvents {
	t.Helper()
	var events []schema.AgentTurnEvents
	if err := as.db.Where("turn_projection_id = ?", projectionID).Order("sequence").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("empty journal")
	}
	return events[len(events)-1]
}

func TestConfirmGraphProposalWritesApprovalResolved(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	productID, graphID, convID := seedProductGraphConversation(t, as)
	proposalID, projectionID := seedPendingGraphProposalTurn(t, as, productID, graphID, convID)

	confirm := as.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/proposals/"+proposalID+"/confirm", nil, "", nil)
	as.mustStatus(t, confirm, http.StatusOK)

	last := lastJournalEvent(t, as, projectionID)
	if last.Kind != "approval/resolved" {
		t.Fatalf("journal kind %s payload %s", last.Kind, last.PayloadJSON)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(last.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["approval_kind"] != "graph_proposal" || payload["decision"] != "confirmed" || payload["approval_id"] != proposalID {
		t.Fatalf("payload %+v", payload)
	}
}

func TestDiscardGraphProposalWritesApprovalResolved(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	productID, graphID, convID := seedProductGraphConversation(t, as)
	proposalID, projectionID := seedPendingGraphProposalTurn(t, as, productID, graphID, convID)

	discard := as.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/proposals/"+proposalID+"/discard", nil, "", nil)
	as.mustStatus(t, discard, http.StatusOK)

	last := lastJournalEvent(t, as, projectionID)
	if last.Kind != "approval/resolved" {
		t.Fatalf("journal kind %s payload %s", last.Kind, last.PayloadJSON)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(last.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["approval_kind"] != "graph_proposal" || payload["decision"] != "discarded" || payload["approval_id"] != proposalID {
		t.Fatalf("payload %+v", payload)
	}
}
