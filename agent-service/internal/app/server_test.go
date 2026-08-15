package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/productflow-agent-service/internal/config"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

const (
	testConversationID = "11111111-1111-4111-8111-111111111111"
	testProductID      = "22222222-2222-4222-8222-222222222222"
	testDraftID        = "33333333-3333-4333-8333-333333333333"
	testAssetID        = "44444444-4444-4444-8444-444444444444"
	testInternalToken  = "internal-token-for-tests-with-32-chars"
)

var testAssetIDs = []string{
	testAssetID,
	"44444444-4444-4444-8444-444444444445",
	"44444444-4444-4444-8444-444444444446",
	"44444444-4444-4444-8444-444444444447",
	"44444444-4444-4444-8444-444444444448",
	"44444444-4444-4444-8444-444444444449",
}

func TestServerScopesMultimodalTurnAndReplaysTerminalEvents(t *testing.T) {
	var providerCalls atomic.Int32
	var providerMu sync.Mutex
	var providerBodies [][]byte
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		providerMu.Lock()
		providerBodies = append(providerBodies, body)
		providerMu.Unlock()
		switch providerCalls.Add(1) {
		case 1:
			writeProviderStream(t, writer, `{"id":"artifact","status":"completed","output":[{"id":"call","type":"function_call","call_id":"draft","name":"propose_workflow_draft","arguments":"{\"title\":\"Ready\"}"}]}`)
		case 2:
			writeProviderStream(t, writer, `{"id":"final","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"draft ready"}]}]}`)
		default:
			http.Error(writer, "unexpected provider call", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(provider.Close)

	png := tinyPNG(t)
	productFlow := newProductFlowFixture(t, png, testProductID)
	defer productFlow.Close()
	client, err := productflow.NewClient(productFlow.URL, testInternalToken, productFlow.Client())
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := t.TempDir()
	manager, err := NewManager(ManagerConfig{
		DataRoot: dataRoot,
		Provider: agenttask.ProviderConfig{
			APIKey: "provider-secret", BaseURL: provider.URL, Model: "test-model", HTTPClient: provider.Client(),
		},
		Policy: agenttask.Policy{
			MaxIterations: 10, ModelContextWindow: 100_000,
			AutoCompactTokenLimit: 80_000, CompactionSummaryMaxChars: 4_000,
		},
		HTTPOptions: agenttask.HTTPOptions{EventPollInterval: time.Millisecond, HeartbeatInterval: 10 * time.Millisecond},
		ProductFlow: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	server, err := NewServer(manager, testInternalToken, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(server.Handler())
	t.Cleanup(api.Close)
	healthResponse, err := api.Client().Get(api.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]string
	if err := json.NewDecoder(healthResponse.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	_ = healthResponse.Body.Close()
	if healthResponse.StatusCode != http.StatusOK || health["harness_commit"] != config.HarnessCommit {
		t.Fatalf("health status=%d body=%v", healthResponse.StatusCode, health)
	}

	unauthorized, err := api.Client().Get(api.URL + "/internal/v1/conversations/" + testConversationID + "/turns/missing")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}
	rawTokenRequest, _ := http.NewRequest(
		http.MethodGet,
		api.URL+"/internal/v1/conversations/"+testConversationID+"/turns/missing",
		nil,
	)
	rawTokenRequest.Header.Set("Authorization", testInternalToken)
	rawTokenResponse, err := api.Client().Do(rawTokenRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = rawTokenResponse.Body.Close()
	if rawTokenResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("raw token without bearer scheme status = %d", rawTokenResponse.StatusCode)
	}

	startBody := map[string]any{
		"input_text": "Create the workflow", "asset_ids": testAssetIDs, "idempotency_key": "request-1",
	}
	started := requestJSON(t, api.Client(), http.MethodPost, api.URL+"/internal/v1/conversations/"+testConversationID+"/turns", startBody)
	if started.status != http.StatusAccepted {
		t.Fatalf("start status=%d body=%s", started.status, started.body)
	}
	var turn agenttask.Turn
	if err := json.Unmarshal(started.body, &turn); err != nil {
		t.Fatal(err)
	}
	if turn.RunID != testConversationID || turn.TurnID == "" {
		t.Fatalf("started turn = %#v", turn)
	}

	turnURL := api.URL + "/internal/v1/conversations/" + testConversationID + "/turns/" + turn.TurnID
	turn = awaitHTTPStatus(t, api.Client(), turnURL, agenttask.TurnAwaitingConfirmation)
	if turn.Artifact == nil || string(turn.Artifact.Value) != `{"title":"Ready"}` || turn.Output != "draft ready" {
		t.Fatalf("terminal turn = %#v", turn)
	}

	duplicate := requestJSON(t, api.Client(), http.MethodPost, api.URL+"/internal/v1/conversations/"+testConversationID+"/turns", startBody)
	var duplicateTurn agenttask.Turn
	if err := json.Unmarshal(duplicate.body, &duplicateTurn); err != nil {
		t.Fatal(err)
	}
	if duplicate.status != http.StatusAccepted || duplicateTurn.TurnID != turn.TurnID || providerCalls.Load() != 2 {
		t.Fatalf("duplicate status=%d turn=%#v calls=%d", duplicate.status, duplicateTurn, providerCalls.Load())
	}

	eventsRequest, _ := http.NewRequest(http.MethodGet, turnURL+"/events", nil)
	eventsRequest.Header.Set("Authorization", "Bearer "+testInternalToken)
	eventsResponse, err := api.Client().Do(eventsRequest)
	if err != nil {
		t.Fatal(err)
	}
	events, _ := io.ReadAll(eventsResponse.Body)
	_ = eventsResponse.Body.Close()
	if eventsResponse.StatusCode != http.StatusOK || !bytes.Contains(events, []byte("event: artifact.proposed")) ||
		!bytes.Contains(events, []byte("event: turn.awaiting_confirmation")) || bytes.Contains(events, []byte("data:image")) {
		t.Fatalf("events status=%d body=%s", eventsResponse.StatusCode, events)
	}
	sequences := sseSequences(t, events)
	if len(sequences) < 2 {
		t.Fatalf("expected replayable event sequence, got %v", sequences)
	}
	terminalSequence := sseSequenceForEvent(t, events, "turn.awaiting_confirmation")
	cursor := terminalSequence - 1
	reconnectRequest, _ := http.NewRequest(
		http.MethodGet,
		turnURL+"/events?after="+strconv.FormatInt(cursor, 10),
		nil,
	)
	reconnectRequest.Header.Set("Authorization", "Bearer "+testInternalToken)
	reconnectResponse, err := api.Client().Do(reconnectRequest)
	if err != nil {
		t.Fatal(err)
	}
	reconnectedEvents, _ := io.ReadAll(reconnectResponse.Body)
	_ = reconnectResponse.Body.Close()
	if reconnectResponse.StatusCode != http.StatusOK ||
		!bytes.Contains(reconnectedEvents, []byte("event: turn.awaiting_confirmation")) ||
		bytes.Count(reconnectedEvents, []byte("event: turn.awaiting_confirmation")) != 1 {
		t.Fatalf("reconnected events status=%d body=%s", reconnectResponse.StatusCode, reconnectedEvents)
	}
	reconnectedSequences := sseSequences(t, reconnectedEvents)
	if len(reconnectedSequences) == 0 || reconnectedSequences[0] != terminalSequence ||
		reconnectedSequences[len(reconnectedSequences)-1] != sequences[len(sequences)-1] {
		t.Fatalf("reconnected sequences = %v, full sequence = %v", reconnectedSequences, sequences)
	}
	for index, sequence := range reconnectedSequences {
		if sequence <= cursor || (index > 0 && sequence <= reconnectedSequences[index-1]) {
			t.Fatalf("reconnected sequences are not strictly after cursor %d: %v", cursor, reconnectedSequences)
		}
	}

	providerMu.Lock()
	captured := append([][]byte(nil), providerBodies...)
	providerMu.Unlock()
	if len(captured) != 2 || bytes.Count(captured[0], []byte("data:image/png;base64,")) != len(testAssetIDs) {
		t.Fatalf("provider requests = %s", captured)
	}
	for _, assetID := range testAssetIDs {
		if !bytes.Contains(captured[0], []byte(assetID)) {
			t.Fatalf("provider request omitted asset %s", assetID)
		}
	}
	for _, path := range []string{
		filepath.Join(dataRoot, "conversations", testConversationID, "scope.json"),
		filepath.Join(dataRoot, "conversations", testConversationID, "agent.db"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(testInternalToken)) || bytes.Contains(data, []byte("provider-secret")) ||
			bytes.Contains(data, []byte("data:image")) {
			t.Fatalf("%s contains a credential or data URL", path)
		}
	}
}

func TestManagerRetriesArtifactRejectedByProductFlow(t *testing.T) {
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		switch providerCalls.Add(1) {
		case 1:
			writeProviderStream(t, writer, `{"id":"rejected","status":"completed","output":[{"id":"call_1","type":"function_call","call_id":"draft_1","name":"propose_workflow_draft","arguments":"{\"title\":\"Rejected\"}"}]}`)
		case 2:
			if !bytes.Contains(body, []byte("title violates ProductFlow rules")) {
				writeProviderStream(t, writer, `{"id":"premature_final","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"draft ready"}]}]}`)
				return
			}
			writeProviderStream(t, writer, `{"id":"accepted","status":"completed","output":[{"id":"call_2","type":"function_call","call_id":"draft_2","name":"propose_workflow_draft","arguments":"{\"title\":\"Ready\"}"}]}`)
		case 3:
			writeProviderStream(t, writer, `{"id":"final","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"draft ready"}]}]}`)
		default:
			http.Error(writer, "unexpected provider call", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(provider.Close)
	productFlow := newProductFlowFixture(t, nil, testProductID)
	t.Cleanup(productFlow.Close)
	client, err := productflow.NewClient(productFlow.URL, testInternalToken, productFlow.Client())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(ManagerConfig{
		DataRoot: t.TempDir(),
		Provider: agenttask.ProviderConfig{
			APIKey: "provider-secret", BaseURL: provider.URL, Model: "test-model", HTTPClient: provider.Client(),
		},
		Policy: agenttask.Policy{
			MaxIterations: 10, ModelContextWindow: 100_000,
			AutoCompactTokenLimit: 80_000, CompactionSummaryMaxChars: 4_000,
		},
		ProductFlow: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	entry, err := manager.Get(t.Context(), testConversationID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := entry.Service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: entry.Scope.RunID, Input: agenttask.TextInput("Create a valid draft"),
		IdempotencyKey: "productflow-artifact-validation",
	})
	if err != nil {
		t.Fatal(err)
	}
	state = awaitLiveTurn(t, entry.Service, state.RunID, state.TurnID)
	if state.Artifact == nil || string(state.Artifact.Value) != `{"title":"Ready"}` || providerCalls.Load() != 3 {
		t.Fatalf("validated artifact state=%#v provider_calls=%d", state, providerCalls.Load())
	}
}

func TestServerQuestionControlsAndQueuedCancelSurviveManagerRestart(t *testing.T) {
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		switch providerCalls.Add(1) {
		case 1:
			writeProviderStream(t, writer, `{"id":"question","status":"completed","output":[{"id":"ask-call","type":"function_call","call_id":"ask","name":"ask_user","arguments":"{\"header\":\"Language\",\"question\":\"Which language?\",\"options\":[{\"label\":\"Chinese\"}]}"}]}`)
		case 2:
			writeProviderStream(t, writer, `{"id":"artifact","status":"completed","output":[{"id":"draft-call","type":"function_call","call_id":"draft","name":"propose_workflow_draft","arguments":"{\"title\":\"Resumed\"}"}]}`)
		case 3:
			writeProviderStream(t, writer, `{"id":"final","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"resumed after restart"}]}]}`)
		default:
			http.Error(writer, "unexpected provider call", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(provider.Close)
	productFlow := newProductFlowFixture(t, nil, testProductID)
	t.Cleanup(productFlow.Close)
	client, err := productflow.NewClient(productFlow.URL, testInternalToken, productFlow.Client())
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := t.TempDir()
	openManager := func() *Manager {
		manager, openErr := NewManager(ManagerConfig{
			DataRoot: dataRoot,
			Provider: agenttask.ProviderConfig{
				APIKey: "provider-secret", BaseURL: provider.URL, Model: "test-model", HTTPClient: provider.Client(),
			},
			Policy: agenttask.Policy{
				MaxIterations: 10, ModelContextWindow: 100_000,
				AutoCompactTokenLimit: 80_000, CompactionSummaryMaxChars: 4_000,
			},
			HTTPOptions: agenttask.HTTPOptions{EventPollInterval: time.Millisecond, HeartbeatInterval: 10 * time.Millisecond},
			ProductFlow: client,
		})
		if openErr != nil {
			t.Fatal(openErr)
		}
		return manager
	}
	openAPI := func(manager *Manager) *httptest.Server {
		server, serverErr := NewServer(manager, testInternalToken, 1<<20)
		if serverErr != nil {
			t.Fatal(serverErr)
		}
		return httptest.NewServer(server.Handler())
	}

	manager := openManager()
	api := openAPI(manager)
	startURL := api.URL + "/internal/v1/conversations/" + testConversationID + "/turns"
	started := requestJSON(t, api.Client(), http.MethodPost, startURL, map[string]any{
		"input_text": "Ask one question", "idempotency_key": "question-turn",
	})
	var questionTurn agenttask.Turn
	if started.status != http.StatusAccepted || json.Unmarshal(started.body, &questionTurn) != nil {
		t.Fatalf("question start status=%d body=%s", started.status, started.body)
	}
	questionURL := startURL + "/" + questionTurn.TurnID
	questionTurn = awaitHTTPStatus(t, api.Client(), questionURL, agenttask.TurnRequiresInput)
	if questionTurn.Question == nil {
		t.Fatal("question turn has no question")
	}

	follower := requestJSON(t, api.Client(), http.MethodPost, startURL, map[string]any{
		"input_text": "Wait behind the question", "idempotency_key": "queued-follower",
	})
	var followerTurn agenttask.Turn
	if follower.status != http.StatusAccepted || json.Unmarshal(follower.body, &followerTurn) != nil {
		t.Fatalf("follower start status=%d body=%s", follower.status, follower.body)
	}
	canceled := requestJSON(t, api.Client(), http.MethodPost, startURL+"/"+followerTurn.TurnID+"/cancel", nil)
	if canceled.status != http.StatusOK || json.Unmarshal(canceled.body, &followerTurn) != nil ||
		followerTurn.Status != agenttask.TurnCanceled {
		t.Fatalf("queued cancel status=%d body=%s", canceled.status, canceled.body)
	}

	answered := requestJSON(
		t,
		api.Client(),
		http.MethodPost,
		questionURL+"/questions/"+questionTurn.Question.ID+"/answer",
		map[string]any{"answer": map[string]any{"text": "Chinese"}},
	)
	if answered.status != http.StatusOK || json.Unmarshal(answered.body, &questionTurn) != nil ||
		questionTurn.Status != agenttask.TurnQueued {
		t.Fatalf("answer status=%d body=%s", answered.status, answered.body)
	}
	api.Close()
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	manager = openManager()
	t.Cleanup(func() { _ = manager.Close() })
	api = openAPI(manager)
	t.Cleanup(api.Close)
	questionURL = api.URL + "/internal/v1/conversations/" + testConversationID + "/turns/" + questionTurn.TurnID
	resumed := requestJSON(t, api.Client(), http.MethodPost, questionURL+"/resume", nil)
	if resumed.status != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", resumed.status, resumed.body)
	}
	questionTurn = awaitHTTPStatus(t, api.Client(), questionURL, agenttask.TurnAwaitingConfirmation)
	if questionTurn.Artifact == nil || string(questionTurn.Artifact.Value) != `{"title":"Resumed"}` ||
		questionTurn.Output != "resumed after restart" || providerCalls.Load() != 3 {
		t.Fatalf("resumed turn=%#v calls=%d", questionTurn, providerCalls.Load())
	}
}

func TestPersistedScopeRejectsProductDrift(t *testing.T) {
	dataRoot := t.TempDir()
	first := Scope{
		SchemaVersion: scopeSchemaVersion, ConversationID: testConversationID, ProductID: testProductID,
		WorkflowDraftID: testDraftID, RunID: testConversationID,
	}
	if _, _, err := ensureScope(dataRoot, first); err != nil {
		t.Fatal(err)
	}
	drifted := first
	drifted.ProductID = "55555555-5555-4555-8555-555555555555"
	if _, _, err := ensureScope(dataRoot, drifted); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("scope drift error = %v", err)
	}
}

func TestManagerRejectsProductFlowToolContractDriftBeforeCreatingJournal(t *testing.T) {
	for _, contract := range []map[string]any{
		{"schema_version": 1, "tool_contract_version": 1},
		{"schema_version": 2, "tool_contract_version": 2},
	} {
		t.Run(fmt.Sprintf("schema-%v-tools-%v", contract["schema_version"], contract["tool_contract_version"]), func(t *testing.T) {
			productFlow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writeFixtureJSON(writer, contract)
			}))
			t.Cleanup(productFlow.Close)
			client, err := productflow.NewClient(productFlow.URL, testInternalToken, productFlow.Client())
			if err != nil {
				t.Fatal(err)
			}
			dataRoot := t.TempDir()
			manager, err := NewManager(ManagerConfig{DataRoot: dataRoot, ProductFlow: client})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = manager.Close() })
			if _, err := manager.Get(t.Context(), testConversationID); err == nil || !strings.Contains(err.Error(), "contract mismatch") {
				t.Fatalf("manager contract drift error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(dataRoot, "conversations")); !os.IsNotExist(err) {
				t.Fatalf("contract drift created conversation data: %v", err)
			}
		})
	}
}

func TestManagerLoadsAgentProviderConfigOnceWhenOpeningConversation(t *testing.T) {
	const secondConversationID = "11111111-1111-4111-8111-111111111112"
	var providerConfigCalls atomic.Int32
	productFlow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testInternalToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/api/internal/v1/agent-runtime/provider-config":
			providerConfigCalls.Add(1)
			writeFixtureJSON(writer, map[string]any{
				"schema_version": 1, "provider_kind": "openai", "api_key": "runtime-secret",
				"base_url": "https://provider.invalid/v1", "model": "runtime-model",
				"reasoning_effort": "high", "reasoning_summary": "concise",
				"text_verbosity": "low", "service_tier": "priority",
			})
		case "/api/internal/v1/agent-conversations/" + testConversationID + "/contract",
			"/api/internal/v1/agent-conversations/" + secondConversationID + "/contract":
			conversationID := strings.TrimSuffix(
				strings.TrimPrefix(request.URL.Path, "/api/internal/v1/agent-conversations/"),
				"/contract",
			)
			writeFixtureJSON(writer, map[string]any{
				"schema_version": 1, "conversation_id": conversationID, "product_id": testProductID,
				"workflow_draft_id": testDraftID, "harness_run_id": conversationID,
				"current_draft_version": 0, "system_prompt": "Build a workflow.",
				"workflow_draft_schema": map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{"title": map[string]any{"type": "string"}},
					"required":   []string{"title"},
				},
				"tool_contract_version": 3,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(productFlow.Close)
	client, err := productflow.NewClient(productFlow.URL, testInternalToken, productFlow.Client())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(ManagerConfig{
		DataRoot: t.TempDir(),
		Policy: agenttask.Policy{
			MaxIterations: 10, ModelContextWindow: 100_000,
			AutoCompactTokenLimit: 80_000, CompactionSummaryMaxChars: 4_000,
		},
		ProductFlow: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	first, err := manager.Get(t.Context(), testConversationID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Get(t.Context(), testConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || providerConfigCalls.Load() != 1 {
		t.Fatalf("conversation cache mismatch: same=%t provider calls=%d", first == second, providerConfigCalls.Load())
	}
	third, err := manager.Get(t.Context(), secondConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if third == first || providerConfigCalls.Load() != 2 {
		t.Fatalf("new conversation did not reload provider config: same=%t provider calls=%d", third == first, providerConfigCalls.Load())
	}
}

type httpResult struct {
	status int
	body   []byte
}

func requestJSON(t *testing.T, client *http.Client, method, target string, value any) httpResult {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, target, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testInternalToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return httpResult{status: response.StatusCode, body: body}
}

func awaitHTTPStatus(t *testing.T, client *http.Client, target string, expected agenttask.TurnStatus) agenttask.Turn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer "+testInternalToken)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		var state agenttask.Turn
		if response.StatusCode == http.StatusOK && json.Unmarshal(body, &state) == nil && state.Status == expected {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("turn did not reach %s", expected)
	return agenttask.Turn{}
}

func sseSequences(t *testing.T, body []byte) []int64 {
	t.Helper()
	var sequences []int64
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "id: ") {
			continue
		}
		sequence, err := strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
		if err != nil {
			t.Fatalf("decode SSE sequence from %q: %v", line, err)
		}
		sequences = append(sequences, sequence)
	}
	return sequences
}

func sseSequenceForEvent(t *testing.T, body []byte, event string) int64 {
	t.Helper()
	for _, block := range strings.Split(string(body), "\n\n") {
		if !strings.Contains(block, "event: "+event+"\n") {
			continue
		}
		sequences := sseSequences(t, []byte(block))
		if len(sequences) != 1 {
			t.Fatalf("event %s has invalid sequence block %q", event, block)
		}
		return sequences[0]
	}
	t.Fatalf("event %s is missing from SSE body", event)
	return 0
}

func newProductFlowFixture(t *testing.T, png []byte, productID string) *httptest.Server {
	return newProductFlowFixtureWithAgentProvider(t, png, productID, nil)
}

func newProductFlowFixtureWithAgentProvider(
	t *testing.T,
	png []byte,
	productID string,
	agentProvider *productflow.AgentProviderConfig,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testInternalToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		base := "/api/internal/v1/agent-conversations/" + testConversationID
		switch request.URL.Path {
		case "/api/internal/v1/agent-runtime/provider-config":
			if agentProvider == nil {
				http.NotFound(writer, request)
				return
			}
			writeFixtureJSON(writer, agentProvider)
		case base + "/contract":
			writeFixtureJSON(writer, map[string]any{
				"schema_version": 1, "conversation_id": testConversationID, "product_id": productID,
				"workflow_draft_id": testDraftID, "harness_run_id": testConversationID,
				"current_draft_version": 1, "system_prompt": "Design a ProductFlow workflow using only supplied facts.",
				"workflow_draft_schema": map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{"title": map[string]any{"type": "string"}}, "required": []string{"title"},
				},
				"tool_contract_version": 3,
			})
		case base + "/workflow-draft/validate":
			var payload struct {
				Value struct {
					Title string `json:"title"`
				} `json:"value"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				http.Error(writer, "invalid validation payload", http.StatusBadRequest)
				return
			}
			if payload.Value.Title == "Rejected" {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(writer).Encode(map[string]string{"detail": "title violates ProductFlow rules"})
				return
			}
			writeFixtureJSON(writer, map[string]bool{"accepted": true})
		default:
			matchedAsset := false
			for _, assetID := range testAssetIDs {
				if request.URL.Path == base+"/assets/"+assetID+"/content" {
					matchedAsset = true
					break
				}
			}
			if !matchedAsset {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "image/png")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(png)
		}
	}))
}

func writeFixtureJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func writeProviderStream(t *testing.T, writer http.ResponseWriter, response string) {
	t.Helper()
	var envelope struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal([]byte(response), &envelope); err != nil {
		t.Fatal(err)
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	for _, output := range envelope.Output {
		for _, content := range output.Content {
			if content.Type == "output_text" && content.Text != "" {
				delta, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": content.Text})
				_, _ = fmt.Fprintf(writer, "data: %s\n\n", delta)
			}
		}
	}
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
