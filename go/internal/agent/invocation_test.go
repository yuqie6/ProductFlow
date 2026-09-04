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

const testHarnessHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestModelInvocationIdempotentCreateAndUsageDedup(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := claimOpenTurn(t, as)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	payload := json.RawMessage(`{"model_request_id":"model:req-1","provider":"openai-responses","model":"test-model","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`)
	cp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": payload,
	}, auth)
	as.mustStatus(t, cp, http.StatusOK)
	cp.Body.Close()
	replay := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": payload,
	}, auth)
	as.mustStatus(t, replay, http.StatusOK)
	replay.Body.Close()

	created := time.Now().UTC()
	responseID := "resp-" + clockid.New()
	first := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 1, "schema_version": 1, "run_id": claimed.runID, "turn_id": claimed.turnID,
				"kind":       "assistant/message",
				"payload":    json.RawMessage(`{"model_request_id":"model:req-1","reason":"stop","duration_ms":42,"provider_response_id":"` + responseID + `","provider_response_cursor":"cursor-1","usage":{"input":11,"output":7,"total_tokens":18}}`),
				"created_at": created,
			},
		},
	}, auth)
	as.mustStatus(t, first, http.StatusOK)
	first.Body.Close()

	second := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 2, "schema_version": 1, "run_id": claimed.runID, "turn_id": claimed.turnID,
				"kind":       "assistant/message",
				"payload":    json.RawMessage(`{"model_request_id":"model:req-1","reason":"stop","duration_ms":999,"usage":{"input":0,"output":0,"total_tokens":0}}`),
				"created_at": created,
			},
		},
	}, auth)
	as.mustStatus(t, second, http.StatusOK)
	second.Body.Close()

	var row struct {
		Status             string
		DurationMS         int64
		InputTokens        int64
		OutputTokens       int64
		TotalTokens        int64
		UsageSource        string
		ProviderResponseID string
		ProviderCursor     string
		Count              int64
	}
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status, duration_ms, input_tokens, output_tokens, total_tokens, usage_source,
			provider_response_id, provider_cursor, COUNT(*) OVER ()
		FROM agent_model_invocations
		WHERE turn_projection_id = $1 AND model_request_id = 'model:req-1'
	`, claimed.projectionID).Scan(
		&row.Status, &row.DurationMS, &row.InputTokens, &row.OutputTokens, &row.TotalTokens,
		&row.UsageSource, &row.ProviderResponseID, &row.ProviderCursor, &row.Count,
	); err != nil {
		t.Fatal(err)
	}
	if row.Count != 1 || row.Status != "completed" || row.DurationMS != 42 || row.InputTokens != 11 || row.OutputTokens != 7 || row.TotalTokens != 18 || row.UsageSource != "provider" {
		t.Fatalf("invocation %+v", row)
	}
	if row.ProviderResponseID != responseID || row.ProviderCursor != "cursor-1" {
		t.Fatalf("response identity %+v", row)
	}
}

func TestModelInvocationMissingUsageStaysUnavailable(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := claimOpenTurn(t, as)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	start := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": json.RawMessage(`{"model_request_id":"model:req-missing","provider":"openai-responses","model":"test-model","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`),
	}, auth)
	as.mustStatus(t, start, http.StatusOK)
	start.Body.Close()
	finish := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 1, "schema_version": 1, "run_id": claimed.runID, "turn_id": claimed.turnID,
				"kind":    "assistant/message",
				"payload": json.RawMessage(`{"model_request_id":"model:req-missing","reason":"stop","usage":{"input":0,"output":0,"total_tokens":0}}`),
			},
		},
	}, auth)
	as.mustStatus(t, finish, http.StatusOK)
	finish.Body.Close()
	var source string
	var inputTokens *int64
	if err := as.pool.QueryRow(context.Background(), `
		SELECT usage_source, input_tokens FROM agent_model_invocations
		WHERE turn_projection_id = $1 AND model_request_id = 'model:req-missing'
	`, claimed.projectionID).Scan(&source, &inputTokens); err != nil {
		t.Fatal(err)
	}
	if source != "unavailable" || inputTokens != nil {
		t.Fatalf("missing usage source=%s tokens=%v", source, inputTokens)
	}
}

func TestModelInvocationEstimatedUsagePersists(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := claimOpenTurn(t, as)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	start := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": json.RawMessage(`{"model_request_id":"model:req-est","provider":"openai-responses","model":"test-model","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`),
	}, auth)
	as.mustStatus(t, start, http.StatusOK)
	start.Body.Close()
	finish := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 1, "schema_version": 1, "run_id": claimed.runID, "turn_id": claimed.turnID,
				"kind":    "assistant/message",
				"payload": json.RawMessage(`{"model_request_id":"model:req-est","reason":"stop","usage_source":"estimated","usage":{"input":3,"output":5,"total_tokens":8}}`),
			},
		},
	}, auth)
	as.mustStatus(t, finish, http.StatusOK)
	finish.Body.Close()
	var source string
	var total int64
	if err := as.pool.QueryRow(context.Background(), `
		SELECT usage_source, total_tokens FROM agent_model_invocations
		WHERE turn_projection_id = $1 AND model_request_id = 'model:req-est'
	`, claimed.projectionID).Scan(&source, &total); err != nil {
		t.Fatal(err)
	}
	if source != "estimated" || total != 8 {
		t.Fatalf("estimated usage source=%s total=%d", source, total)
	}
}

func TestAppendCheckpointAcceptsModelResponseKindsWithoutWritingThemInForeground(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := claimOpenTurn(t, as)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	start := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": json.RawMessage(`{"model_request_id":"model:req-fg","provider":"openai-responses","model":"test-model","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`),
	}, auth)
	as.mustStatus(t, start, http.StatusOK)
	start.Body.Close()
	bound := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 2,
		"kind": "model_response_bound", "payload": json.RawMessage(`{"model_request_id":"model:req-fg"}`),
	}, auth)
	as.mustStatus(t, bound, http.StatusOK)
	bound.Body.Close()
	cursor := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 3,
		"kind": "model_response_cursor", "payload": json.RawMessage(`{"model_request_id":"model:req-fg","cursor":"tok_2"}`),
	}, auth)
	as.mustStatus(t, cursor, http.StatusOK)
	cursor.Body.Close()
	background := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 4,
		"kind": "before_model_request", "payload": json.RawMessage(`{"model_request_id":"model:req-bg","provider":"openai-responses","model":"test-model","execution_mode":"background","harness_hash":"` + testHarnessHash + `"}`),
	}, auth)
	as.mustDetail(t, background, http.StatusBadRequest, "当前 Agent adapter 不支持 background 模型调用")
}

func TestProviderResponseIDUniqueAcrossInvocations(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	first := claimOpenTurn(t, as)
	second := claimOpenTurn(t, as)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	for _, claimed := range []claimedTurn{first, second} {
		cp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
			"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
			"kind":    "before_model_request",
			"payload": json.RawMessage(`{"model_request_id":"model:` + claimed.turnID[:12] + `","provider":"openai-responses","model":"test-model","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`),
		}, auth)
		as.mustStatus(t, cp, http.StatusOK)
		cp.Body.Close()
	}
	sharedResponseID := "resp-" + clockid.New()
	closeFirst := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+first.conversationID+"/turn-executions/"+first.lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": first.lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 1, "schema_version": 1, "run_id": first.runID, "turn_id": first.turnID,
				"kind": "assistant/message", "payload": json.RawMessage(`{"model_request_id":"model:` + first.turnID[:12] + `","reason":"stop","provider_response_id":"` + sharedResponseID + `"}`),
			},
		},
	}, auth)
	as.mustStatus(t, closeFirst, http.StatusOK)
	closeFirst.Body.Close()
	conflict := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+second.conversationID+"/turn-executions/"+second.lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": second.lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 1, "schema_version": 1, "run_id": second.runID, "turn_id": second.turnID,
				"kind": "assistant/message", "payload": json.RawMessage(`{"model_request_id":"model:` + second.turnID[:12] + `","reason":"stop","provider_response_id":"` + sharedResponseID + `"}`),
			},
		},
	}, auth)
	if conflict.StatusCode != http.StatusConflict {
		raw := make([]byte, 256)
		n, _ := conflict.Body.Read(raw)
		conflict.Body.Close()
		t.Fatalf("duplicate provider response ID status %d %s", conflict.StatusCode, raw[:n])
	}
	conflict.Body.Close()
}

type claimedTurn struct {
	conversationID string
	projectionID   string
	runID          string
	turnID         string
	lease          ExecutionLeaseResponse
}

func claimOpenTurn(t *testing.T, as *agentServer) claimedTurn {
	t.Helper()
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+sess.Conversations[0].ConversationID+"/turns", map[string]any{
		"input_text": "模型调用合同", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	claim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+sess.Conversations[0].ConversationID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-1",
		})), "application/json", auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)
	return claimedTurn{
		conversationID: sess.Conversations[0].ConversationID,
		projectionID:   submitted.Turn.ID,
		runID:          submitted.Turn.HarnessRunID,
		turnID:         *submitted.Turn.HarnessTurnID,
		lease:          lease,
	}
}
