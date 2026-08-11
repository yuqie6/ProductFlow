package agenttask_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/durable"
)

func TestExternalSupervisorCanSubmitStableTaskID(t *testing.T) {
	runner, err := agenttask.Open(agenttask.Config{
		Database: filepath.Join(t.TempDir(), "tasks.db"), Workspace: t.TempDir(),
		Provider: agenttask.ProviderConfig{APIKey: "unused", BaseURL: "http://127.0.0.1:1", Model: "test"},
		Policy:   testPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	job, err := runner.SubmitWithID(t.Context(), "outer-task-1", "inspect the repository")
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "outer-task-1" || job.Status != durable.JobPending {
		t.Fatalf("job = %#v", job)
	}
	if _, err := runner.SubmitWithID(t.Context(), "outer-task-1", "duplicate"); err == nil {
		t.Fatal("duplicate stable task ID was accepted")
	}
	loaded, err := runner.Task(t.Context(), "outer-task-1")
	if err != nil || loaded.Name != job.Name {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}

func TestTaskPersistsEditModeForControlPlaneResume(t *testing.T) {
	tests := []struct {
		name       string
		allowEdit  bool
		reviewEdit bool
		want       agenttask.EditMode
	}{
		{name: "read only", want: agenttask.EditModeReadOnly},
		{name: "review edit", reviewEdit: true, want: agenttask.EditModeReview},
		{name: "auto edit", allowEdit: true, want: agenttask.EditModeAuto},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			runner, err := agenttask.Open(agenttask.Config{
				Database: filepath.Join(t.TempDir(), "tasks.db"), Workspace: workspace,
				Provider: agenttask.ProviderConfig{APIKey: "unused", BaseURL: "http://127.0.0.1:1", Model: "test"},
				Policy:   testPolicy(), AllowEdit: test.allowEdit, ReviewEdit: test.reviewEdit,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runner.Close() })
			job, err := runner.Submit(t.Context(), "inspect persisted policy")
			if err != nil {
				t.Fatal(err)
			}
			got, err := agenttask.EditModeFromJob(job)
			if err != nil || got != test.want {
				t.Fatalf("edit mode = %q, want %q, err = %v", got, test.want, err)
			}
			var legacyInput map[string]any
			if err := json.Unmarshal(job.Steps[0].Input, &legacyInput); err != nil {
				t.Fatal(err)
			}
			delete(legacyInput, "edit_mode")
			job.Steps[0].Input, err = json.Marshal(legacyInput)
			if err != nil {
				t.Fatal(err)
			}
			legacyMode, err := agenttask.EditModeFromJob(job)
			if err != nil || legacyMode != test.want {
				t.Fatalf("legacy edit mode = %q, want %q, err = %v", legacyMode, test.want, err)
			}
		})
	}
}

func TestToolNamesFromJobReadsPersistedModelCatalog(t *testing.T) {
	job := durable.Job{
		ID:   "catalog-task",
		Kind: durable.JobKindAgentTurn,
		Steps: []durable.Step{{
			Tool: "agent_model",
			Input: json.RawMessage(`{
				"tools":[
					{"type":"function","function":{"name":"read_file"}},
					{"type":"function","function":{"name":"run_check"}},
					{"type":"function","function":{"name":"read_file"}}
				]
			}`),
		}},
	}

	names, err := agenttask.ToolNamesFromJob(job)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"read_file", "run_check"}
	if len(names) != len(want) {
		t.Fatalf("tool names = %#v, want %#v", names, want)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("tool name %d = %q, want %q", index, names[index], want[index])
		}
	}
}

func TestDefaultProviderUsesOneOpaqueCreate(t *testing.T) {
	var posts atomic.Int32
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			gets.Add(1)
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != "/responses" {
			http.NotFound(writer, request)
			return
		}
		posts.Add(1)
		var body struct {
			Store      bool `json:"store"`
			Background bool `json:"background"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode create: %v", err)
		}
		if body.Store || body.Background {
			t.Errorf("opaque create used store=%v background=%v", body.Store, body.Background)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"id":"resp_opaque","status":"completed","output":[
				{"id":"msg_opaque","type":"message","role":"assistant","content":[{"type":"output_text","text":"opaque complete"}]}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	workspace := t.TempDir()
	runner, err := agenttask.Open(agenttask.Config{
		Database: filepath.Join(t.TempDir(), "tasks.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{
			APIKey: "secret", BaseURL: server.URL, Model: "model",
			HTTPClient: server.Client(),
		},
		Policy: testPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	result, err := runner.Start(t.Context(), "finish directly")
	if err != nil || result.Job.Status != durable.JobSucceeded || result.Output != "opaque complete" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if posts.Load() != 1 || gets.Load() != 0 {
		t.Fatalf("provider calls = POST %d, GET %d", posts.Load(), gets.Load())
	}
}

func TestProviderResponseModeMustBeExplicitlySupported(t *testing.T) {
	_, err := agenttask.Open(agenttask.Config{
		Database: filepath.Join(t.TempDir(), "tasks.db"), Workspace: t.TempDir(),
		Provider: agenttask.ProviderConfig{
			APIKey: "secret", BaseURL: "https://provider.invalid", Model: "model", ResponseMode: "auto",
		},
		Policy: testPolicy(),
	})
	if err == nil || !strings.Contains(err.Error(), "response mode") {
		t.Fatalf("error = %v", err)
	}
}

func TestStoredResponseModeRequiresExplicitWaitTimeout(t *testing.T) {
	_, err := agenttask.Open(agenttask.Config{
		Database: filepath.Join(t.TempDir(), "tasks.db"), Workspace: t.TempDir(),
		Provider: agenttask.ProviderConfig{
			APIKey: "secret", BaseURL: "https://provider.invalid", Model: "model",
			ResponseMode: agenttask.ResponseModeStoredBackground,
		},
		Policy: testPolicy(),
	})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %v", err)
	}
}

func TestExternalConsumerCanResumeQuestionedCodingTask(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("public durable value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var creates atomic.Int32
	var sawInstruction atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer public-secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost && request.URL.Path == "/responses" {
			var body struct {
				Input []struct {
					Role    string `json:"role"`
					Content any    `json:"content"`
				} `json:"input"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode create: %v", err)
			}
			for _, message := range body.Input {
				if message.Role == "system" && strings.Contains(renderJSON(message.Content), "PUBLIC_REPOSITORY_RULE") {
					sawInstruction.Store(true)
				}
			}
			id := creates.Add(1)
			_, _ = writer.Write([]byte(`{"id":"resp_` + string(rune('0'+id)) + `","status":"queued","output":[]}`))
			return
		}
		if request.Method != http.MethodGet {
			http.NotFound(writer, request)
			return
		}
		switch request.URL.Path {
		case "/responses/resp_1":
			_, _ = writer.Write([]byte(`{
				"id":"resp_1","status":"completed","output":[
					{"id":"fc_1","type":"function_call","call_id":"read_1","name":"read_file","arguments":"{\"path\":\"note.txt\"}"}
				]
			}`))
		case "/responses/resp_2":
			_, _ = writer.Write([]byte(`{
				"id":"resp_2","status":"completed","output":[
					{"id":"fc_2","type":"function_call","call_id":"question_1","name":"ask_user","arguments":"{\"header\":\"Scope\",\"question\":\"Which scope?\",\"options\":[{\"label\":\"Core\"},{\"label\":\"All\"}]}"}
				]
			}`))
		case "/responses/resp_3":
			_, _ = writer.Write([]byte(`{
				"id":"resp_3","status":"completed","output":[
					{"id":"msg_3","type":"message","role":"assistant","content":[{"type":"output_text","text":"public task complete"}]}
				]
			}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	database := filepath.Join(t.TempDir(), "jobs.db")
	runner, err := agenttask.Open(agenttask.Config{
		Database: database, Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{
			APIKey: "public-secret", BaseURL: server.URL, Model: "public-model",
			ResponseMode: agenttask.ResponseModeStoredBackground, HTTPClient: server.Client(),
			StoredResponseTimeout: time.Second,
		},
		Instructions: []string{"PUBLIC_REPOSITORY_RULE: inspect evidence"},
		Policy:       testPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	paused, runErr := runner.Start(t.Context(), "read the note and ask about scope")
	if !errors.Is(runErr, durable.ErrRequiresAction) || paused.Job.Status != durable.JobRequiresAction || paused.ToolCalls != 2 {
		t.Fatalf("paused = %#v, err = %v", paused, runErr)
	}
	question, found, err := runner.PendingQuestion(t.Context(), paused.Job.ID)
	if err != nil || !found || question.Question != "Which scope?" || len(question.Options) != 2 {
		t.Fatalf("question = %#v, found = %v, err = %v", question, found, err)
	}
	if _, err := runner.AnswerQuestion(t.Context(), paused.Job.ID, durable.ResolutionTarget{
		StepID: question.Target.StepID, AttemptID: "stale",
	}, 0, agenttask.Decision{}); !errors.Is(err, durable.ErrConflict) {
		t.Fatalf("stale answer error = %v", err)
	}
	if _, err := runner.AnswerQuestion(t.Context(), paused.Job.ID, question.Target, 0, agenttask.Decision{Actor: "public-test"}); err != nil {
		t.Fatal(err)
	}
	completed, err := runner.Resume(t.Context(), paused.Job.ID)
	if err != nil || completed.Job.Status != durable.JobSucceeded || completed.Output != "public task complete" || creates.Load() != 3 {
		t.Fatalf("completed = %#v, creates = %d, err = %v", completed, creates.Load(), err)
	}
	if !sawInstruction.Load() {
		t.Fatal("public instructions were not sent to the model")
	}
	if got, err := agenttask.WorkspaceFromJob(completed.Job); err != nil || got != workspace {
		t.Fatalf("workspace = %q, err = %v", got, err)
	}
	events, err := runner.Events(t.Context(), paused.Job.ID, 0)
	if err != nil || len(events) == 0 {
		t.Fatalf("events = %#v, err = %v", events, err)
	}
	attempts, err := runner.Attempts(t.Context(), paused.Job.ID)
	if err != nil || len(attempts) < 5 {
		t.Fatalf("attempts = %#v, err = %v", attempts, err)
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	journalFiles, err := filepath.Glob(database + "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range journalFiles {
		databaseBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(databaseBytes), "public-secret") {
			t.Fatalf("journal file %s contains provider API key", path)
		}
	}
}

func testPolicy() agenttask.Policy {
	return agenttask.Policy{MaxIterations: 50, ModelContextWindow: 100000}
}

func renderJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
