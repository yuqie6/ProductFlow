package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfmetrics "github.com/yuqie6/productflow/internal/platform/metrics"
)

func TestTurnEventSequenceCapacity(t *testing.T) {
	_, err := (Service{}).AppendEvents(context.Background(), "conversation", "execution", "owner", "lease", []EventAppendInput{{
		Sequence: maxEventSequence + 1, SchemaVersion: 1, RunID: "run", TurnID: "turn",
		Kind: "turn/start", Payload: []byte(`{}`),
	}})
	if err == nil {
		t.Fatal("expected event sequence capacity error")
	}
}

func TestAgentSSEConnectionCapacity(t *testing.T) {
	previous := pfmetrics.AgentSSEConnections.Load()
	pfmetrics.AgentSSEConnections.Store(maxSSEConnections)
	defer pfmetrics.AgentSSEConnections.Store(previous)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/events", nil)
	(Service{}).StreamTurnEvents(ctx, nil, "conversation", "projection", 0, "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestSessionTurnCapacity(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var created SessionResponse
	as.decode(t, session, &created)
	conversationID := created.Conversations[0].ConversationID
	rowPrefix := clockid.New()[:20]

	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, idempotency_key, request_hash, input_text,
			input_asset_ids_json, status, resume_required, tool_steps_json, created_at, updated_at
		)
		SELECT
			$3 || '-' || lpad(value::text, 4, '0'), $1,
			'capacity-key-' || value::text, repeat('a', 64), 'capacity',
			'[]'::jsonb, 'succeeded', FALSE, '[]'::jsonb, NOW(), NOW()
		FROM generate_series(1, $2) AS value
	`, conversationID, maxTurnsPerSession, rowPrefix); err != nil {
		t.Fatal(err)
	}

	response := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+conversationID+"/turns", map[string]any{
		"input_text": "超过容量", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, response, http.StatusConflict)
}
