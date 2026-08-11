package agenttask_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/durable"
)

func TestExternalDurableToolRunsInsideModelJournal(t *testing.T) {
	server, recorder := newDurableToolResponsesServer(t)
	database := filepath.Join(t.TempDir(), "tasks.db")
	const businessSecret = "external-business-secret"
	var businessCalls atomic.Int32
	businessServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+businessSecret {
			t.Errorf("business authorization = %q", request.Header.Get("Authorization"))
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		businessCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(businessServer.Close)
	var calls atomic.Int32
	effect := durable.FuncTool{
		ToolName: "set_business_state", EffectClass: durable.EffectReconcilable,
		PrepareFunc: func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
			return input, nil
		},
		ExecuteFunc: func(ctx context.Context, _ durable.Invocation) (json.RawMessage, error) {
			calls.Add(1)
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, businessServer.URL, nil)
			if err != nil {
				return nil, err
			}
			request.Header.Set("Authorization", "Bearer "+businessSecret)
			response, err := businessServer.Client().Do(request)
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				return nil, errors.New("business endpoint rejected request")
			}
			return json.RawMessage(`{"status":"applied"}`), nil
		},
		ReconcileFunc: func(_ context.Context, _ durable.Invocation) (durable.ReconcileResult, error) {
			return durable.ReconcileResult{State: durable.ReconcileApplied, Result: json.RawMessage(`{"status":"applied"}`)}, nil
		},
	}
	runner, err := agenttask.Open(agenttask.Config{
		Database: database, Workspace: t.TempDir(), SkillUserHome: t.TempDir(),
		Provider: agenttask.ProviderConfig{
			APIKey: "provider-secret", BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client(),
		},
		Policy: testPolicy(),
		DurableTools: []agenttask.DurableTool{{
			Description: "Set one external business state under a durable recovery contract.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"value": map[string]any{"type": "string"}},
				"required":   []string{"value"},
			},
			Tool: effect,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	result, err := runner.Start(t.Context(), "set the business state")
	if err != nil || result.Output != "business effect complete" || calls.Load() != 1 || businessCalls.Load() != 1 {
		t.Fatalf("result = %#v, calls = %d, business calls = %d, err = %v", result, calls.Load(), businessCalls.Load(), err)
	}
	requests := recorder.snapshot()
	if len(requests) != 2 || !durableRequestHasTool(requests[0], effect.Name()) {
		t.Fatalf("requests = %#v", requests)
	}
	output, found := durableRequestToolOutput(requests[1], "call-business")
	if !found || output != `{"status":"applied"}` {
		t.Fatalf("tool output = %q, found = %v", output, found)
	}
	job, err := runner.Task(t.Context(), result.Job.ID)
	if err != nil || len(job.Steps) != 3 || job.Steps[1].Effect != durable.EffectReconcilable {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		content, readErr := os.ReadFile(database + suffix)
		if readErr != nil {
			continue
		}
		if strings.Contains(string(content), businessSecret) || strings.Contains(string(content), "provider-secret") {
			t.Fatalf("journal %s contains a credential", database+suffix)
		}
	}
}

func TestExternalDurableToolRefusesEffectDriftBeforeStepAppend(t *testing.T) {
	server, _ := newDurableToolResponsesServer(t)
	database := filepath.Join(t.TempDir(), "tasks.db")
	workspace := t.TempDir()
	var mutations atomic.Int32
	open := func(effect durable.EffectClass) *agenttask.Runner {
		t.Helper()
		implementation := durable.FuncTool{
			ToolName: "set_business_state", EffectClass: effect,
			PrepareFunc: func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
				return input, nil
			},
			ExecuteFunc: func(_ context.Context, _ durable.Invocation) (json.RawMessage, error) {
				mutations.Add(1)
				return json.RawMessage(`{"status":"applied"}`), nil
			},
			ReconcileFunc: func(_ context.Context, _ durable.Invocation) (durable.ReconcileResult, error) {
				return durable.ReconcileResult{State: durable.ReconcileApplied, Result: json.RawMessage(`{"status":"applied"}`)}, nil
			},
		}
		runner, err := agenttask.Open(agenttask.Config{
			Database: database, Workspace: workspace, SkillUserHome: workspace,
			Provider: agenttask.ProviderConfig{
				APIKey: "provider-secret", BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client(),
			},
			Policy: testPolicy(),
			DurableTools: []agenttask.DurableTool{{
				Description: "Set one external business state.", Parameters: durableBusinessParameters(), Tool: implementation,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return runner
	}

	first := open(durable.EffectReconcilable)
	job, err := first.Submit(t.Context(), "set the business state")
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := first.AdvanceOne(t.Context(), job.ID)
	if err != nil || boundary.Job.Status != durable.JobAwaitingSteps || len(boundary.Job.Steps) != 1 {
		t.Fatalf("boundary = %#v, err = %v", boundary, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	drifted := open(durable.EffectOpaque)
	defer drifted.Close()
	result, err := drifted.AdvanceOne(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrConflict) || result.Job.Status != durable.JobAwaitingSteps || len(result.Job.Steps) != 1 {
		t.Fatalf("drifted result = %#v, err = %v", result, err)
	}
	if mutations.Load() != 0 {
		t.Fatalf("effect drift executed %d mutations", mutations.Load())
	}
}

func TestExternalDurableToolRefusesReadToEffectReclassification(t *testing.T) {
	server, _ := newDurableToolResponsesServer(t)
	database := filepath.Join(t.TempDir(), "tasks.db")
	workspace := t.TempDir()
	base := func() agenttask.Config {
		return agenttask.Config{
			Database: database, Workspace: workspace, SkillUserHome: workspace,
			Provider: agenttask.ProviderConfig{
				APIKey: "provider-secret", BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client(),
			},
			Policy: testPolicy(),
		}
	}
	firstConfig := base()
	firstConfig.Tools = []agenttask.Tool{{
		Name: "set_business_state", Description: "Set one external business state.", Parameters: durableBusinessParameters(),
		Handler: func(context.Context, json.RawMessage) (string, error) { return "read-only result", nil },
	}}
	first, err := agenttask.Open(firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "inspect the business state")
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := first.AdvanceOne(t.Context(), job.ID)
	if err != nil || boundary.Job.Status != durable.JobAwaitingSteps || len(boundary.Job.Steps) != 1 {
		t.Fatalf("boundary = %#v, err = %v", boundary, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	var mutations atomic.Int32
	secondConfig := base()
	secondConfig.DurableTools = []agenttask.DurableTool{{
		Description: "Set one external business state.", Parameters: durableBusinessParameters(),
		Tool: durable.FuncTool{
			ToolName: "set_business_state", EffectClass: durable.EffectOpaque,
			PrepareFunc: func(_ context.Context, input json.RawMessage) (json.RawMessage, error) { return input, nil },
			ExecuteFunc: func(context.Context, durable.Invocation) (json.RawMessage, error) {
				mutations.Add(1)
				return json.RawMessage(`{"status":"applied"}`), nil
			},
		},
	}}
	drifted, err := agenttask.Open(secondConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer drifted.Close()
	result, err := drifted.AdvanceOne(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrConflict) || result.Job.Status != durable.JobAwaitingSteps || len(result.Job.Steps) != 1 {
		t.Fatalf("reclassified result = %#v, err = %v", result, err)
	}
	if mutations.Load() != 0 {
		t.Fatalf("read-to-effect drift executed %d mutations", mutations.Load())
	}
}

func durableBusinessParameters() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"value": map[string]any{"type": "string"}},
		"required":   []string{"value"},
	}
}

type durableToolWireRequest struct {
	Input []json.RawMessage `json:"input"`
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

type durableToolResponseRecorder struct {
	t        *testing.T
	mu       sync.Mutex
	requests []durableToolWireRequest
}

func newDurableToolResponsesServer(t *testing.T) (*httptest.Server, *durableToolResponseRecorder) {
	t.Helper()
	recorder := &durableToolResponseRecorder{t: t}
	server := httptest.NewServer(http.HandlerFunc(recorder.serveHTTP))
	t.Cleanup(server.Close)
	return server, recorder
}

func (recorder *durableToolResponseRecorder) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != "/responses" {
		http.NotFound(writer, request)
		return
	}
	var captured durableToolWireRequest
	if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	recorder.mu.Lock()
	index := len(recorder.requests)
	recorder.requests = append(recorder.requests, captured)
	recorder.mu.Unlock()
	writer.Header().Set("Content-Type", "application/json")
	if index == 0 {
		_, _ = writer.Write([]byte(`{
			"id":"response-tool","status":"completed","output":[{
				"id":"function-call","type":"function_call","status":"completed",
				"call_id":"call-business","name":"set_business_state","arguments":"{\"value\":\"ready\"}"
			}]
		}`))
		return
	}
	_, _ = writer.Write([]byte(`{
		"id":"response-final","status":"completed","output":[{
			"id":"message-final","type":"message","role":"assistant","status":"completed",
			"content":[{"type":"output_text","text":"business effect complete","annotations":[]}]
		}]
	}`))
}

func (recorder *durableToolResponseRecorder) snapshot() []durableToolWireRequest {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]durableToolWireRequest(nil), recorder.requests...)
}

func durableRequestHasTool(request durableToolWireRequest, name string) bool {
	for _, tool := range request.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func durableRequestToolOutput(request durableToolWireRequest, callID string) (string, bool) {
	for _, raw := range request.Input {
		var item struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Type == "function_call_output" && item.CallID == callID {
			return item.Output, true
		}
	}
	return "", false
}

func TestExternalDurableToolRejectsPureEffect(t *testing.T) {
	_, err := agenttask.Open(agenttask.Config{
		Database: filepath.Join(t.TempDir(), "tasks.db"), Workspace: t.TempDir(),
		Provider: agenttask.ProviderConfig{APIKey: "unused", BaseURL: "http://127.0.0.1:1", Model: "test"},
		Policy:   testPolicy(),
		DurableTools: []agenttask.DurableTool{{
			Description: "wrong registration path",
			Tool: durable.FuncTool{
				ToolName: "pure_external", EffectClass: durable.EffectPure,
				ExecuteFunc: func(context.Context, durable.Invocation) (json.RawMessage, error) {
					return json.RawMessage(`{}`), nil
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "register it in Tools") {
		t.Fatalf("error = %v", err)
	}
}
