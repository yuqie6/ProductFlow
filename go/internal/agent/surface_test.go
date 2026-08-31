package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestAgentTaskTurnControlAndInternalSurface(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	auth := http.Header{"Authorization": []string{"Bearer tok"}}

	createdTask := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "目标一", "goal": "整理全局素材并核对", "conversation_id": convID,
	})
	as.mustStatus(t, createdTask, http.StatusCreated)
	var task TaskResponse
	as.decode(t, createdTask, &task)

	gotTask := as.do(t, http.MethodGet, "/api/v2/agent-tasks/"+task.ID, nil, "", nil)
	as.mustStatus(t, gotTask, http.StatusOK)
	gotTask.Body.Close()

	listed := as.do(t, http.MethodGet, "/api/v2/agent-tasks?session_id="+sess.ID, nil, "", nil)
	as.mustStatus(t, listed, http.StatusOK)
	listed.Body.Close()

	renamed := as.doJSON(t, http.MethodPatch, "/api/v2/agent-tasks/"+task.ID, map[string]any{"title": "目标一改名"})
	as.mustStatus(t, renamed, http.StatusOK)
	renamed.Body.Close()

	contract := as.do(t, http.MethodGet, "/api/internal/v1/agent-tasks/"+task.ID+"/contract", nil, "", auth)
	as.mustStatus(t, contract, http.StatusOK)
	contract.Body.Close()

	runtime := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/runtime-context?task_id="+task.ID, nil, "", auth)
	as.mustStatus(t, runtime, http.StatusOK)
	runtime.Body.Close()

	missingWF := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/global-workflow-context", nil, "", auth)
	as.mustStatus(t, missingWF, http.StatusBadRequest)
	missingWF.Body.Close()

	library := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/media-library", nil, "", auth)
	as.mustStatus(t, library, http.StatusOK)
	library.Body.Close()

	globalDraft := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/global-draft/validate", map[string]any{
		"value": map[string]any{"draft_kind": "library_organization", "library_payload": map[string]any{"operations": []any{}}},
	}, auth)
	as.mustStatus(t, globalDraft, http.StatusOK)
	globalDraft.Body.Close()

	draft := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/library-organization-draft", nil, "", nil)
	if draft.StatusCode != http.StatusNotFound && draft.StatusCode != http.StatusOK {
		raw := make([]byte, 256)
		n, _ := draft.Body.Read(raw)
		draft.Body.Close()
		t.Fatalf("library draft %d %s", draft.StatusCode, raw[:n])
	}
	draft.Body.Close()

	runReq := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/workflow-run-request", nil, "", nil)
	as.mustStatus(t, runReq, http.StatusOK)
	var emptyRequest any
	as.decode(t, runReq, &emptyRequest)
	if emptyRequest != nil {
		t.Fatalf("empty run request %+v", emptyRequest)
	}

	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "执行目标", "idempotency_key": key, "task_id": task.ID,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)

	cancelled := as.do(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/cancel", nil, "", nil)
	as.mustStatus(t, cancelled, http.StatusOK)
	as.decode(t, cancelled, &submitted.Turn)
	if submitted.Turn.Status != "canceled" {
		t.Fatalf("cancel %s", submitted.Turn.Status)
	}
	canceledTask := as.do(t, http.MethodGet, "/api/v2/agent-tasks/"+task.ID, nil, "", nil)
	as.mustStatus(t, canceledTask, http.StatusOK)
	as.decode(t, canceledTask, &task)
	if task.Status != "canceled" {
		t.Fatalf("global task after canceled turn %s", task.Status)
	}

	resumedBusy := as.do(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/resume", nil, "", nil)
	as.mustStatus(t, resumedBusy, http.StatusConflict)
	resumedBusy.Body.Close()

	nextTask := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "目标二", "goal": "继续内部面", "conversation_id": convID,
	})
	as.mustStatus(t, nextTask, http.StatusCreated)
	as.decode(t, nextTask, &task)

	openKey := clockid.New()
	openTurn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "继续", "idempotency_key": openKey, "task_id": task.ID,
	})
	as.mustStatus(t, openTurn, http.StatusAccepted)
	var open SubmitTurnResponse
	as.decode(t, openTurn, &open)

	claim := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim", map[string]any{
		"task_id": open.Turn.TaskID, "idempotency_key": openKey,
		"harness_turn_id": *open.Turn.HarnessTurnID, "owner_id": "worker-1",
	}, auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)

	cp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": json.RawMessage(`{"model_request_id":"model:test-1","provider":"openai-responses","model":"test-model","execution_mode":"foreground"}`),
	}, auth)
	as.mustStatus(t, cp, http.StatusOK)
	cp.Body.Close()

	ev := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "events": []any{map[string]any{
			"sequence": 1, "schema_version": 1, "run_id": open.Turn.HarnessRunID, "turn_id": *open.Turn.HarnessTurnID,
			"kind": "turn/start", "payload": json.RawMessage(`{}`), "created_at": time.Now().UTC(),
		}},
	}, auth)
	as.mustStatus(t, ev, http.StatusOK)
	ev.Body.Close()

	rel := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/release", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "phase": "terminal",
	}, auth)
	as.mustStatus(t, rel, http.StatusOK)
	rel.Body.Close()

	productID := clockid.New()
	graphID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '画布商品', NOW(), NOW())
	`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, '商品创意工作流', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID); err != nil {
		t.Fatal(err)
	}
	ensure := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, ensure, http.StatusOK)
	var bench WorkbenchResponse
	as.decode(t, ensure, &bench)
	pConv := bench.Conversation.ID

	gotConv := as.do(t, http.MethodGet, "/api/v2/products/"+productID+"/agent-conversations/"+pConv, nil, "", nil)
	as.mustStatus(t, gotConv, http.StatusOK)
	gotConv.Body.Close()

	globalWF := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/global-workflow-context?product_id="+productID, nil, "", auth)
	as.mustStatus(t, globalWF, http.StatusOK)
	globalWF.Body.Close()

	productCtx := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+pConv+"/product-context", nil, "", auth)
	as.mustStatus(t, productCtx, http.StatusOK)
	productCtx.Body.Close()

	runs := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+pConv+"/workflow-runs", nil, "", auth)
	as.mustStatus(t, runs, http.StatusOK)
	runs.Body.Close()

	missingRun := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+pConv+"/workflow-runs/"+clockid.New(), nil, "", auth)
	as.mustStatus(t, missingRun, http.StatusNotFound)
	missingRun.Body.Close()

	inspect := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/workflow-runs/inspect", map[string]any{
		"workflow_ids": []string{graphID},
	}, auth)
	as.mustStatus(t, inspect, http.StatusOK)
	inspect.Body.Close()

	missingNode := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+pConv+"/graph/nodes/"+clockid.New(), nil, "", auth)
	as.mustStatus(t, missingNode, http.StatusNotFound)
	missingNode.Body.Close()

	apply := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+pConv+"/graph/apply-change-set",
		strings.NewReader(mustJSON(t, map[string]any{
			"change_set": map[string]any{
				"base_graph_revision": 1,
				"summary":             "添加商品资料",
				"operations": []map[string]any{{
					"op": "create_node", "client_ref": "n1", "node_type": "product_source",
					"title": "商品资料", "position_x": 120, "position_y": 80,
					"config": map[string]any{"source_product_id": productID},
				}},
			},
		})), "application/json", http.Header{
			"Authorization":   []string{"Bearer tok"},
			"Idempotency-Key": []string{clockid.New()},
		})
	as.mustStatus(t, apply, http.StatusOK)
	apply.Body.Close()

	assets := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+pConv+"/assets", nil, "", auth)
	as.mustStatus(t, assets, http.StatusOK)
	assets.Body.Close()

	pTurn := as.doJSON(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-conversations/"+pConv+"/turns", map[string]any{
		"input_text": "改画布", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, pTurn, http.StatusAccepted)
	pTurn.Body.Close()

	pTurns := as.do(t, http.MethodGet, "/api/v2/products/"+productID+"/agent-conversations/"+pConv+"/turns", nil, "", nil)
	as.mustStatus(t, pTurns, http.StatusOK)
	pTurns.Body.Close()

	pRun := as.do(t, http.MethodGet, "/api/v2/products/"+productID+"/agent-conversations/"+pConv+"/workflow-run-request", nil, "", nil)
	as.mustStatus(t, pRun, http.StatusOK)
	var emptyProductRequest any
	as.decode(t, pRun, &emptyProductRequest)
	if emptyProductRequest != nil {
		t.Fatalf("empty product run request %+v", emptyProductRequest)
	}

	focusKey := clockid.New()
	focus := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+pConv+"/canvas/focus",
		strings.NewReader(`{"node_ids":["n1"]}`), "application/json", http.Header{
			"Authorization":   []string{"Bearer tok"},
			"Idempotency-Key": []string{focusKey},
		})
	as.mustStatus(t, focus, http.StatusOK)
	focus.Body.Close()
	recon := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+pConv+"/canvas/focus/reconcile",
		strings.NewReader(`{"node_ids":["n1"]}`), "application/json", http.Header{
			"Authorization":   []string{"Bearer tok"},
			"Idempotency-Key": []string{focusKey},
		})
	as.mustStatus(t, recon, http.StatusOK)
	recon.Body.Close()
}
