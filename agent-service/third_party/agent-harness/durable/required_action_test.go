package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestResolveRequiredActionAppliedContinuesJob(t *testing.T) {
	followupExecutions := &atomic.Int32{}
	gate := FuncTool{
		ToolName: "manual_gate", EffectClass: EffectPure,
		PrepareFunc: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: choose an option", ErrConflict)
		},
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			t.Fatal("manual gate must not execute")
			return nil, nil
		},
	}
	followup := FuncTool{
		ToolName: "followup", EffectClass: EffectPure,
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			followupExecutions.Add(1)
			return json.RawMessage(`{"done":true}`), nil
		},
	}
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{}, gate, followup)
	job, err := engine.Submit(t.Context(), JobSpec{Name: "manual gate", Steps: []StepSpec{
		{Tool: gate.Name(), Input: json.RawMessage(`{"question":"continue?"}`)},
		{Tool: followup.Name(), Input: json.RawMessage(`{}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrRequiresAction) || job.Status != JobRequiresAction {
		t.Fatalf("required action job = %#v, err = %v", job, err)
	}
	target := mustResolutionTarget(t, engine, job.ID)

	answer := json.RawMessage(`{"option":1,"label":"continue"}`)
	if _, err := engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
		StepID: "different-step", AttemptID: target.AttemptID,
		Outcome: ResolutionApplied, Actor: "operator", Reason: "stale decision", Result: answer,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale step resolution error = %v", err)
	}
	if _, err := engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
		StepID: target.StepID, AttemptID: "different-attempt",
		Outcome: ResolutionApplied, Actor: "operator", Reason: "stale attempt", Result: answer,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale attempt resolution error = %v", err)
	}
	job, err = engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionApplied, Actor: "operator", Reason: "selected in TUI", Result: answer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobPending || job.Steps[0].Status != StepSucceeded || string(job.Steps[0].Result) != string(answer) {
		t.Fatalf("resolved job = %#v", job)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobSucceeded || followupExecutions.Load() != 1 {
		t.Fatalf("continued job = %#v, executions = %d, err = %v", job, followupExecutions.Load(), err)
	}
	if _, err := engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionFailed, Actor: "second", Reason: "duplicate",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("duplicate resolution error = %v", err)
	}
}

func TestResolveRequiredActionRetryPreparesWithoutExecuting(t *testing.T) {
	prepareCalls := &atomic.Int32{}
	executeCalls := &atomic.Int32{}
	blocked := atomic.Bool{}
	blocked.Store(true)
	tool := FuncTool{
		ToolName: "retry_gate", EffectClass: EffectReconcilable,
		PrepareFunc: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			prepareCalls.Add(1)
			if blocked.Load() {
				return nil, fmt.Errorf("%w: stale precondition", ErrConflict)
			}
			return json.RawMessage(`{"prepared":true}`), nil
		},
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			executeCalls.Add(1)
			return json.RawMessage(`{"applied":true}`), nil
		},
		ReconcileFunc: func(context.Context, Invocation) (ReconcileResult, error) {
			return ReconcileResult{State: ReconcileNotApplied}, nil
		},
	}
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{}, tool)
	job, err := engine.Submit(t.Context(), JobSpec{Name: "retry action", Steps: []StepSpec{{
		Tool: tool.Name(), Input: json.RawMessage(`{}`),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrRequiresAction) {
		t.Fatalf("Run error = %v", err)
	}
	target := mustResolutionTarget(t, engine, job.ID)
	blocked.Store(false)
	job, err = engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionRetry, Actor: "operator", Reason: "precondition repaired",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobPending || job.Steps[0].Status != StepPrepared || executeCalls.Load() != 0 || prepareCalls.Load() != 2 {
		t.Fatalf("resolved job = %#v, prepare = %d, execute = %d", job, prepareCalls.Load(), executeCalls.Load())
	}
	attempts, err := engine.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].Status != AttemptRequiresAction || attempts[1].Status != AttemptPrepared {
		t.Fatalf("attempts = %#v", attempts)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobSucceeded || executeCalls.Load() != 1 {
		t.Fatalf("retried job = %#v, execute = %d, err = %v", job, executeCalls.Load(), err)
	}
}

func TestResolveRequiredActionFailedMakesJobTerminal(t *testing.T) {
	gate := FuncTool{
		ToolName: "failed_gate", EffectClass: EffectPure,
		PrepareFunc: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: operator decision required", ErrConflict)
		},
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			return nil, errors.New("must not execute")
		},
	}
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{}, gate)
	job, err := engine.Submit(t.Context(), JobSpec{Name: "failed action", Steps: []StepSpec{{Tool: gate.Name(), Input: json.RawMessage(`{}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrRequiresAction) {
		t.Fatalf("Run error = %v", err)
	}
	target := mustResolutionTarget(t, engine, job.ID)
	job, err = engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionFailed, Actor: "operator", Reason: "request denied",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobFailed || job.Steps[0].Status != StepFailed || job.Error != "request denied" {
		t.Fatalf("resolved job = %#v", job)
	}
}

func TestResolveRequiredActionAppliedPreservesContinuationBoundary(t *testing.T) {
	gate := FuncTool{
		ToolName: "continuation_gate", EffectClass: EffectPure,
		PrepareFunc: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: answer required", ErrConflict)
		},
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			return nil, errors.New("must not execute")
		},
	}
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{}, gate)
	job, err := engine.Submit(t.Context(), JobSpec{Name: "continuation action", Steps: []StepSpec{{
		Tool: gate.Name(), Input: json.RawMessage(`{}`), AwaitContinuation: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Run(t.Context(), job.ID); !errors.Is(err, ErrRequiresAction) {
		t.Fatalf("Run error = %v", err)
	}
	target := mustResolutionTarget(t, engine, job.ID)
	job, err = engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionApplied, Actor: "operator", Reason: "answered",
	})
	if err != nil || job.Status != JobAwaitingSteps || job.Steps[0].Status != StepSucceeded {
		t.Fatalf("resolved job = %#v, err = %v", job, err)
	}
}

func TestResolveRequiredActionDecisionUsesCASAndWritesAudit(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	gate := FuncTool{
		ToolName: "cas_gate", EffectClass: EffectPure,
		PrepareFunc: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: decision required", ErrConflict)
		},
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			return nil, errors.New("must not execute")
		},
	}
	first := openTestEngine(t, database, Options{Owner: "resolver-one"}, gate)
	second := openTestEngine(t, database, Options{Owner: "resolver-two"}, gate)
	job, err := first.Submit(t.Context(), JobSpec{Name: "CAS action", Steps: []StepSpec{{Tool: gate.Name(), Input: json.RawMessage(`{}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Run(t.Context(), job.ID); !errors.Is(err, ErrRequiresAction) {
		t.Fatalf("Run error = %v", err)
	}
	target := mustResolutionTarget(t, first, job.ID)

	start := make(chan struct{})
	errorsByResolver := make([]error, 2)
	var wait sync.WaitGroup
	for index, engine := range []*Engine{first, second} {
		wait.Add(1)
		go func(index int, engine *Engine) {
			defer wait.Done()
			<-start
			_, errorsByResolver[index] = engine.ResolveRequiredAction(t.Context(), job.ID, RequiredActionResolution{
				StepID: target.StepID, AttemptID: target.AttemptID,
				Outcome: ResolutionApplied, Actor: fmt.Sprintf("operator-%d", index), Reason: "concurrent decision",
			})
		}(index, engine)
	}
	close(start)
	wait.Wait()
	succeeded, rejected := 0, 0
	for _, err := range errorsByResolver {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrInvalidState):
			rejected++
		default:
			t.Fatalf("resolution error = %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("resolution results = %#v", errorsByResolver)
	}
	events, err := first.Events(t.Context(), job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	auditEvents := 0
	for _, event := range events {
		if event.Kind != "attempt.action_resolved" {
			continue
		}
		auditEvents++
		var payload struct {
			StepID    string            `json:"step_id"`
			AttemptID string            `json:"attempt_id"`
			Outcome   ResolutionOutcome `json:"outcome"`
			Actor     string            `json:"actor"`
			Reason    string            `json:"reason"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.StepID != target.StepID || payload.AttemptID != target.AttemptID || payload.Outcome != ResolutionApplied || payload.Actor == "" || payload.Reason != "concurrent decision" {
			t.Fatalf("audit payload = %#v", payload)
		}
	}
	if auditEvents != 1 {
		t.Fatalf("action audit events = %d", auditEvents)
	}
}

func TestResolveRequiredActionValidatesDecision(t *testing.T) {
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{})
	for _, resolution := range []RequiredActionResolution{
		{Outcome: ResolutionApplied, Actor: "operator", Reason: "missing step"},
		{StepID: "step", Outcome: ResolutionApplied, Actor: "operator", Reason: "missing attempt"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionApplied, Reason: "missing actor"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionApplied, Actor: "operator"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionOutcome("invalid"), Actor: "operator", Reason: "invalid outcome"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionApplied, Actor: "operator", Reason: "invalid result", Result: json.RawMessage(`{`)},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionRetry, Actor: "operator", Reason: "unexpected result", Result: json.RawMessage(`{}`)},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionFailed, Actor: "operator", Reason: "unexpected result", Result: json.RawMessage(`{}`)},
	} {
		if _, err := engine.ResolveRequiredAction(t.Context(), "missing-job", resolution); err == nil {
			t.Fatalf("ResolveRequiredAction(%#v) succeeded", resolution)
		}
	}
}

func mustResolutionTarget(t *testing.T, engine *Engine, jobID string) ResolutionTarget {
	t.Helper()
	target, err := engine.PendingResolution(t.Context(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
