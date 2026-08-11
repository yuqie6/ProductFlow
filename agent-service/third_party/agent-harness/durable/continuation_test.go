package durable

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAwaitingJobAppendsExactlyOnceAndCompletes(t *testing.T) {
	tool := FuncTool{
		ToolName: "decision", EffectClass: EffectPure,
		ExecuteFunc: func(_ context.Context, invocation Invocation) (json.RawMessage, error) {
			return json.Marshal(map[string]json.RawMessage{"input": invocation.Input})
		},
	}
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{}, tool)
	job, err := engine.Submit(t.Context(), JobSpec{
		Name: "continued job",
		Steps: []StepSpec{{
			ID: "decision-1", Tool: tool.Name(), Input: json.RawMessage(`{"round":1}`), AwaitContinuation: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobAwaitingSteps || !job.Steps[0].AwaitContinuation {
		t.Fatalf("first phase job = %#v, err = %v", job, err)
	}

	job, err = engine.AppendSteps(t.Context(), job.ID, "decision-1", []StepSpec{{
		ID: "decision-2", Tool: tool.Name(), Input: json.RawMessage(`{"round":2}`), AwaitContinuation: true,
	}})
	if err != nil || job.Status != JobPending || len(job.Steps) != 2 {
		t.Fatalf("appended job = %#v, err = %v", job, err)
	}
	if _, err := engine.AppendSteps(t.Context(), job.ID, "decision-1", []StepSpec{{
		ID: "duplicate", Tool: tool.Name(), Input: json.RawMessage(`{}`),
	}}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale append error = %v", err)
	}

	job, err = engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobAwaitingSteps {
		t.Fatalf("second phase job = %#v, err = %v", job, err)
	}
	job, err = engine.Complete(t.Context(), job.ID, "decision-2")
	if err != nil || job.Status != JobSucceeded || len(job.Steps) != 2 {
		t.Fatalf("completed job = %#v, err = %v", job, err)
	}
	if _, err := engine.Complete(t.Context(), job.ID, "decision-2"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("duplicate complete error = %v", err)
	}

	events, err := engine.Events(t.Context(), job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, len(events))
	for index, event := range events {
		kinds[index] = event.Kind
	}
	want := []string{
		"job.submitted", "job.claimed", "attempt.prepared", "attempt.started", "attempt.succeeded", "job.awaiting_steps",
		"job.steps_appended", "job.claimed", "attempt.prepared", "attempt.started", "attempt.succeeded", "job.awaiting_steps",
		"job.succeeded",
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("event kinds = %v, want %v", kinds, want)
	}
}

func TestAwaitingJobCanFailAtOrchestrationBoundary(t *testing.T) {
	tool := FuncTool{
		ToolName: "decision", EffectClass: EffectPure,
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		},
	}
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{}, tool)
	job, err := engine.Submit(t.Context(), JobSpec{Steps: []StepSpec{{
		ID: "tail", Tool: tool.Name(), Input: json.RawMessage(`{}`), AwaitContinuation: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobAwaitingSteps {
		t.Fatalf("awaiting job = %#v, err = %v", job, err)
	}
	job, err = engine.Fail(t.Context(), job.ID, "tail", "agent 达到迭代上限")
	if err != nil || job.Status != JobFailed || job.Error != "agent 达到迭代上限" {
		t.Fatalf("failed job = %#v, err = %v", job, err)
	}
	if _, err := engine.Run(t.Context(), job.ID); !errors.Is(err, ErrJobFailed) {
		t.Fatalf("run failed continuation error = %v", err)
	}
}
