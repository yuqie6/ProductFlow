package durableagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
)

func TestAdvanceOneStopsAtEveryDurableBoundary(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("boundary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-read", "read_file", `{"path":"note.txt"}`),
		textMessage("finished"),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{})
	job, err := runner.Submit(t.Context(), "read by boundary")
	if err != nil {
		t.Fatal(err)
	}

	type boundary struct {
		status     durable.JobStatus
		steps      int
		modelCalls int
		terminal   bool
		output     string
	}
	want := []boundary{
		{status: durable.JobAwaitingSteps, steps: 1, modelCalls: 1},
		{status: durable.JobPending, steps: 2, modelCalls: 1},
		{status: durable.JobAwaitingSteps, steps: 2, modelCalls: 1},
		{status: durable.JobPending, steps: 3, modelCalls: 1},
		{status: durable.JobAwaitingSteps, steps: 3, modelCalls: 2},
		{status: durable.JobSucceeded, steps: 3, modelCalls: 2, terminal: true, output: "finished"},
	}
	for index, expected := range want {
		advanced, err := runner.AdvanceOne(t.Context(), job.ID)
		if err != nil {
			t.Fatalf("advance %d: %v", index+1, err)
		}
		if !advanced.Progressed || advanced.Terminal != expected.terminal || advanced.Job.Status != expected.status ||
			len(advanced.Job.Steps) != expected.steps || client.callCount() != expected.modelCalls || advanced.Output != expected.output {
			t.Fatalf("advance %d = %#v, model calls = %d", index+1, advanced, client.callCount())
		}
	}
}

func TestConcurrentResumeCommitsModelEditAndContinuationOnce(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "value.txt")
	before := []byte("before\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(before)
	editArgs, _ := json.Marshal(map[string]string{
		"path": "value.txt", "expected_sha256": hex.EncodeToString(digest[:]),
		"old_text": "before", "new_text": "after",
	})
	started := make(chan struct{})
	release := make(chan struct{})
	client := &scriptedClient{
		responses: []llm.Message{
			toolCallMessage("call-edit", "edit_file", string(editArgs)),
			textMessage("edited once"),
		},
		firstStarted: started,
		releaseFirst: release,
	}
	database := filepath.Join(t.TempDir(), "jobs.db")
	firstConfig := testConfig(database, workspace, client, true, Policy{})
	firstConfig.EngineOptions = durable.Options{Owner: "runner-one", LeaseTTL: time.Second, HeartbeatInterval: 100 * time.Millisecond}
	first, err := Open(firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	secondConfig := testConfig(database, workspace, client, true, Policy{})
	secondConfig.EngineOptions = durable.Options{Owner: "runner-two", LeaseTTL: time.Second, HeartbeatInterval: 100 * time.Millisecond}
	second, err := Open(secondConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	job, err := first.Submit(t.Context(), "edit concurrently")
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		result Result
		err    error
	}
	outcomes := make(chan outcome, 2)
	go func() {
		result, runErr := first.Resume(context.Background(), job.ID)
		outcomes <- outcome{result: result, err: runErr}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first model request did not start")
	}
	go func() {
		result, runErr := second.Resume(context.Background(), job.ID)
		outcomes <- outcome{result: result, err: runErr}
	}()
	time.Sleep(30 * time.Millisecond)
	close(release)
	for range 2 {
		select {
		case got := <-outcomes:
			if got.err != nil || got.result.Job.Status != durable.JobSucceeded || got.result.Output != "edited once" {
				t.Fatalf("concurrent result = %#v, err = %v", got.result, got.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent Resume did not finish")
		}
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "after\n" {
		t.Fatalf("content = %q, err = %v", content, err)
	}
	if client.callCount() != 2 {
		t.Fatalf("model calls = %d, want 2", client.callCount())
	}
	attempts, err := first.engine.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 3 {
		t.Fatalf("attempts = %#v", attempts)
	}
	events, err := first.engine.Events(t.Context(), job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countKind(events, "job.steps_appended") != 2 || countKind(events, "job.succeeded") != 1 {
		t.Fatalf("events = %#v", events)
	}
}
