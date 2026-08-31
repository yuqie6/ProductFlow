package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestParseToolEffectIntentSchemaV1(t *testing.T) {
	intent, err := parseToolEffectIntent([]byte(`{
		"schema_version": 1,
		"tool_name": "create_product_workspace_v1",
		"tool_call_id": "call-1",
		"idempotency_key": "key-1",
		"recovery_policy": "reconcile_then_retry",
		"request_payload": {"name": "春季新品"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if intent.ToolName != "create_product_workspace_v1" {
		t.Fatalf("parsed %+v", intent)
	}
	var payload map[string]any
	if err := json.Unmarshal(intent.RequestPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["name"] != "春季新品" {
		t.Fatalf("request_payload %+v", payload)
	}
}

func TestParseToolEffectIntentRejectsLegacyRequestAndFlattenedFields(t *testing.T) {
	if _, err := parseToolEffectIntent([]byte(`{
		"schema_version": 1,
		"tool_name": "create_product_workspace_v1",
		"tool_call_id": "call-1",
		"idempotency_key": "key-1",
		"recovery_policy": "reconcile_then_retry",
		"request": {"name": "春季新品"},
		"request_payload": {"name": "春季新品"}
	}`)); err == nil || !strings.Contains(err.Error(), "schema v1") {
		t.Fatalf("legacy request field: %v", err)
	}
	if _, err := parseToolEffectIntent([]byte(`{
		"schema_version": 1,
		"tool_name": "create_product_workspace_v1",
		"tool_call_id": "call-1",
		"idempotency_key": "key-1",
		"recovery_policy": "reconcile_then_retry",
		"request_payload": {"name": "春季新品"},
		"name": "春季新品"
	}`)); err == nil || !strings.Contains(err.Error(), "schema v1") {
		t.Fatalf("flattened field: %v", err)
	}
}

func TestParseToolEffectIntentRejectsSecretsAndImageBytes(t *testing.T) {
	if _, err := parseToolEffectIntent([]byte(`{
		"schema_version": 1,
		"tool_name": "create_product_workspace_v1",
		"tool_call_id": "call-1",
		"idempotency_key": "key-1",
		"recovery_policy": "reconcile_then_retry",
		"request_payload": {"api_key": "secret"}
	}`)); err == nil || !strings.Contains(err.Error(), "密钥") {
		t.Fatalf("api_key: %v", err)
	}
	if _, err := parseToolEffectIntent([]byte(`{
		"schema_version": 1,
		"tool_name": "create_product_workspace_v1",
		"tool_call_id": "call-1",
		"idempotency_key": "key-1",
		"recovery_policy": "reconcile_then_retry",
		"request_payload": {"preview": "data:image/png;base64,aaaa"}
	}`)); err == nil || !strings.Contains(err.Error(), "图片字节") {
		t.Fatalf("image: %v", err)
	}
}

func TestAppendCheckpointEnforcesToolEffectIntentV1(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "intent schema", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	claim := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim", map[string]any{
		"idempotency_key": key,
		"harness_turn_id": *submitted.Turn.HarnessTurnID,
		"owner_id":        "worker-1",
	}, auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)
	path := "/api/internal/v1/agent-conversations/" + convID + "/turn-executions/" + lease.ExecutionID + "/checkpoints"
	legacy := as.doJSONAuth(t, http.MethodPost, path, map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 1,
		"kind": "tool_effect_intent",
		"payload": map[string]any{
			"tool_name": "create_product_workspace_v1", "tool_call_id": "call-1",
			"idempotency_key": "key-1", "recovery_policy": "reconcile_then_retry",
			"request": map[string]any{"name": "春季新品"}, "name": "春季新品",
		},
	}, auth)
	as.mustStatus(t, legacy, http.StatusBadRequest)
	legacy.Body.Close()
	ok := as.doJSONAuth(t, http.MethodPost, path, map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 1,
		"kind": "tool_effect_intent",
		"payload": map[string]any{
			"schema_version":  1,
			"tool_name":       "create_product_workspace_v1",
			"tool_call_id":    "call-1",
			"idempotency_key": "key-1",
			"recovery_policy": "reconcile_then_retry",
			"request_payload": map[string]any{"name": "春季新品"},
		},
	}, auth)
	as.mustStatus(t, ok, http.StatusOK)
	ok.Body.Close()
}
