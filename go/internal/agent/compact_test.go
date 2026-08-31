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

func TestCompactExpiredTurnJournalsShrinksChunkPayloads(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "压缩旧 chunk", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	claim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-1",
		})), "application/json", auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)
	created := time.Now().UTC()
	batch := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 1, "schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
				"kind": "turn/start", "payload": json.RawMessage(`{"status":"running"}`), "created_at": created,
			},
			map[string]any{
				"sequence": 2, "schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
				"kind": "text.chunk", "payload": json.RawMessage(`{"delta":"很长的流式正文","attempt_id":"a","step_id":"s","content_index":0}`), "created_at": created,
			},
			map[string]any{
				"sequence": 3, "schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
				"kind": "assistant/message", "payload": json.RawMessage(`{"attempt_id":"a","text":"早期正文"}`), "created_at": created,
			},
			map[string]any{
				"sequence": 4, "schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
				"kind": "assistant/message", "payload": json.RawMessage(`{"attempt_id":"a","text":"很长的流式正文"}`), "created_at": created,
			},
			map[string]any{
				"sequence": 5, "schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
				"kind": "turn/end", "payload": json.RawMessage(`{"reason":"completed","status":"succeeded"}`), "created_at": created,
			},
		},
	}, auth)
	as.mustStatus(t, batch, http.StatusOK)
	batch.Body.Close()
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET status = 'succeeded', finished_at = $2, updated_at = $2
		WHERE id = $1
	`, submitted.Turn.ID, old); err != nil {
		t.Fatal(err)
	}
	n, err := CompactExpiredTurnJournals(context.Background(), as.svc, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("compacted %d", n)
	}
	var payload string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT payload_json FROM agent_turn_events
		WHERE turn_projection_id = $1 AND sequence = 2
	`, submitted.Turn.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"compacted":true}` {
		t.Fatalf("chunk payload %s", payload)
	}
	kind, projected := projectTurnEvent(eventRow{Kind: "text.chunk", Payload: []byte(payload)})
	if kind != "agent.ignored" {
		t.Fatalf("projected kind %s %v", kind, projected)
	}
	var messagePayload string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT payload_json FROM agent_turn_events
		WHERE turn_projection_id = $1 AND sequence = 4
	`, submitted.Turn.ID).Scan(&messagePayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(messagePayload, `"sourceEventSeqs":[2]`) || !strings.Contains(messagePayload, `"text":"很长的流式正文"`) {
		t.Fatalf("assistant message payload %s", messagePayload)
	}
}

func TestCompactExpiredTurnJournalsAdvancesPastEachBatch(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)

	for i := 0; i < 51; i++ {
		projectionID := clockid.New()
		turnID := clockid.New()
		if _, err := as.pool.Exec(context.Background(), `
			INSERT INTO agent_turn_projections (
				id, conversation_id, harness_turn_id, idempotency_key, request_hash,
				input_text, input_asset_ids_json, status, resume_required,
				created_at, updated_at, finished_at, tool_steps_json
			) VALUES ($1, $2, $3, $4, $5, 'compact batch', '[]'::json, 'succeeded', FALSE, $6, $6, $6, '[]'::json)
		`, projectionID, convID, turnID, "compact-batch-"+projectionID, strings.Repeat("a", 64), old.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := as.pool.Exec(context.Background(), `
			INSERT INTO agent_turn_events (
				id, turn_projection_id, run_id, turn_id, schema_version, sequence,
				kind, ignorable, payload_json, created_at
			) VALUES
				($1, $2, 'compact-run', $3, 1, 1, 'text.chunk', FALSE, '{"delta":"x","attempt_id":"a"}'::json, $4),
				($5, $2, 'compact-run', $3, 1, 2, 'assistant/message', FALSE, '{"text":"x","attempt_id":"a"}'::json, $4)
		`, clockid.New(), projectionID, turnID, old, clockid.New()); err != nil {
			t.Fatal(err)
		}
	}

	first, err := CompactExpiredTurnJournals(context.Background(), as.svc, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompactExpiredTurnJournals(context.Background(), as.svc, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if first != 50 || second != 1 {
		t.Fatalf("compacted batches %d then %d", first, second)
	}
}
