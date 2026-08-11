package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEngineRunsFileEditAndPersistsJournal(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	path := filepath.Join(workspace, "source.txt")
	before := []byte("before\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := NewFileEditTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	engine := openTestEngine(t, database, Options{Owner: "worker-one"}, tool)
	input, _ := json.Marshal(FileEditInput{
		Path: "source.txt", ExpectedSHA256: digestBytes(before), OldText: "before", NewText: "after",
	})
	job, err := engine.Submit(t.Context(), JobSpec{Name: "edit source", Steps: []StepSpec{{Tool: "edit_file", Input: input}}})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobSucceeded || len(job.Steps) != 1 || job.Steps[0].Status != StepSucceeded {
		t.Fatalf("job = %#v", job)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "after\n" {
		t.Fatalf("content = %q", content)
	}
	attempts, err := engine.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != AttemptSucceeded || attempts[0].IdempotencyKey == "" {
		t.Fatalf("attempts = %#v", attempts)
	}
	events, err := engine.Events(t.Context(), job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, event := range events {
		kinds = append(kinds, event.Kind)
		if event.SchemaVersion != EventSchemaVersion || !json.Valid(event.Payload) {
			t.Fatalf("event = %#v", event)
		}
	}
	wantKinds := []string{"job.submitted", "job.claimed", "attempt.prepared", "attempt.started", "attempt.succeeded", "job.succeeded"}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("event kinds = %v, want %v", kinds, wantKinds)
	}
	info, err := os.Stat(database)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %v", info.Mode().Perm())
	}
}

func TestEngineReconcilesEditAppliedBeforeResultCommit(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	path := filepath.Join(workspace, "value.txt")
	before := []byte("old\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	tool, _ := NewFileEditTool(workspace)
	crashed := openTestEngine(t, database, Options{
		Owner: "crashed-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(point FaultPoint, _ FaultContext) {
			if point == FaultAfterEffect {
				panic("simulated process loss")
			}
		},
	}, tool)
	input, _ := json.Marshal(FileEditInput{
		Path: "value.txt", ExpectedSHA256: digestBytes(before), OldText: "old", NewText: "new",
	})
	job, err := crashed.Submit(t.Context(), JobSpec{Name: "recover edit", Steps: []StepSpec{{Tool: "edit_file", Input: input}}})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("fault injector did not interrupt Run")
			}
		}()
		_, _ = crashed.Run(t.Context(), job.ID)
	}()
	if content, _ := os.ReadFile(path); string(content) != "new\n" {
		t.Fatalf("effect did not happen before crash: %q", content)
	}
	if err := crashed.Close(); err != nil {
		t.Fatal(err)
	}
	recovered := openTestEngine(t, database, Options{
		Owner: "recovery-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, tool)
	waitForPersistedLeasesToExpire(t, recovered, job.ID)
	job, err = recovered.Run(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobSucceeded {
		t.Fatalf("job = %#v", job)
	}
	attempts, _ := recovered.Attempts(t.Context(), job.ID)
	if len(attempts) != 1 || attempts[0].Status != AttemptSucceeded {
		t.Fatalf("attempts = %#v", attempts)
	}
	var result FileEditResult
	if err := json.Unmarshal(job.Steps[0].Result, &result); err != nil || !result.AlreadyApplied {
		t.Fatalf("reconciled result = %#v, err = %v", result, err)
	}
}

func TestOpaqueEffectBecomesUnknownAfterWorkerLoss(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	marker := filepath.Join(t.TempDir(), "opaque.log")
	var executions atomic.Int32
	tool := FuncTool{
		ToolName: "opaque_command", EffectClass: EffectOpaque,
		ExecuteFunc: func(_ context.Context, _ Invocation) (json.RawMessage, error) {
			executions.Add(1)
			file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return nil, err
			}
			_, writeErr := file.WriteString("effect\n")
			closeErr := file.Close()
			return json.RawMessage(`{"exit_code":0}`), errors.Join(writeErr, closeErr)
		},
	}
	crashed := openTestEngine(t, database, Options{
		Owner: "opaque-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(point FaultPoint, _ FaultContext) {
			if point == FaultAfterEffect {
				panic("lost opaque result")
			}
		},
	}, tool)
	job, err := crashed.Submit(t.Context(), JobSpec{Name: "opaque", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = crashed.Run(t.Context(), job.ID)
	}()
	_ = crashed.Close()
	recovery := openTestEngine(t, database, Options{
		Owner: "recovery-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, tool)
	waitForPersistedLeasesToExpire(t, recovery, job.ID)
	job, err = recovery.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrUnknown) || job.Status != JobUnknown {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	if executions.Load() != 1 {
		t.Fatalf("opaque effect executed %d times", executions.Load())
	}
	content, _ := os.ReadFile(marker)
	if string(content) != "effect\n" {
		t.Fatalf("marker = %q", content)
	}
}

func TestToolCanReportUnknownOutcomeImmediately(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	tool := FuncTool{
		ToolName: "ambiguous_remote_create", EffectClass: EffectOpaque,
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			return nil, fmt.Errorf("provider connection lost: %w", ErrOutcomeUnknown)
		},
	}
	engine := openTestEngine(t, database, Options{}, tool)
	job, err := engine.Submit(t.Context(), JobSpec{Name: "ambiguous", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrUnknown) || job.Status != JobUnknown || job.Steps[0].Status != StepUnknown {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	attempts, err := engine.Attempts(t.Context(), job.ID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != AttemptUnknown {
		t.Fatalf("attempts = %#v, err = %v", attempts, err)
	}
}

func TestReconcilableToolPersistsInFlightCheckpoint(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	var reconciled json.RawMessage
	tool := FuncTool{
		ToolName: "remote_response", EffectClass: EffectReconcilable,
		ExecuteFunc: func(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
			checkpoint := json.RawMessage(`{"response_id":"resp_123"}`)
			if err := invocation.RecordCheckpoint(ctx, checkpoint); err != nil {
				return nil, err
			}
			return json.RawMessage(`{"done":true}`), nil
		},
		ReconcileFunc: func(_ context.Context, invocation Invocation) (ReconcileResult, error) {
			reconciled = append(json.RawMessage(nil), invocation.Checkpoint...)
			return ReconcileResult{State: ReconcileApplied, Result: json.RawMessage(`{"done":true}`)}, nil
		},
	}
	engine := openTestEngine(t, database, Options{
		LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(point FaultPoint, _ FaultContext) {
			if point == FaultAfterEffect {
				panic("lose local result")
			}
		},
	}, tool)
	job, err := engine.Submit(t.Context(), JobSpec{Name: "checkpoint", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = engine.Run(t.Context(), job.ID)
	}()
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	recovery := openTestEngine(t, database, Options{LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond}, tool)
	waitForPersistedLeasesToExpire(t, recovery, job.ID)
	job, err = recovery.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobSucceeded {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	if string(reconciled) != `{"response_id":"resp_123"}` {
		t.Fatalf("checkpoint = %s", reconciled)
	}
	events, err := recovery.Events(t.Context(), job.ID, 0)
	if err != nil || countEvent(events, "attempt.checkpointed") != 1 {
		t.Fatalf("events = %#v, err = %v", events, err)
	}
}

func TestOpaqueEffectCancellationWaitsForLeaseBeforeUnknown(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	started := make(chan struct{})
	var executions atomic.Int32
	tool := FuncTool{
		ToolName: "cancelled_opaque", EffectClass: EffectOpaque,
		ExecuteFunc: func(ctx context.Context, _ Invocation) (json.RawMessage, error) {
			executions.Add(1)
			close(started)
			<-ctx.Done()
			return json.RawMessage(`{"effect_started":true}`), ctx.Err()
		},
	}
	engine := openTestEngine(t, database, Options{
		Owner: "cancelled-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, tool)
	job, err := engine.Submit(t.Context(), JobSpec{
		Name: "cancel opaque", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, runErr := engine.Run(ctx, job.ID)
		result <- runErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("opaque effect did not start")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Run error = %v", err)
	}

	job, err = engine.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := engine.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobRunning || len(job.Steps) != 1 || job.Steps[0].Status != StepRunning ||
		len(attempts) != 1 || attempts[0].Status != AttemptRunning {
		t.Fatalf("cancelled job = %#v, attempts = %#v", job, attempts)
	}
	if executions.Load() != 1 {
		t.Fatalf("opaque effect executed %d times before recovery", executions.Load())
	}

	recovery := openTestEngine(t, database, Options{
		Owner: "recovery-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, tool)
	waitForPersistedLeasesToExpire(t, recovery, job.ID)
	job, err = recovery.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrUnknown) || job.Status != JobUnknown {
		t.Fatalf("recovered job = %#v, err = %v", job, err)
	}
	if executions.Load() != 1 {
		t.Fatalf("opaque effect executed %d times after recovery", executions.Load())
	}
}

func TestEngineRejectsConcurrentLeaseOwner(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	started := make(chan struct{})
	release := make(chan struct{})
	tool := FuncTool{
		ToolName: "blocking_read", EffectClass: EffectPure,
		ExecuteFunc: func(ctx context.Context, _ Invocation) (json.RawMessage, error) {
			close(started)
			select {
			case <-release:
				return json.RawMessage(`{"done":true}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	first := openTestEngine(t, database, Options{Owner: "worker-one", LeaseTTL: time.Second, HeartbeatInterval: 100 * time.Millisecond}, tool)
	job, err := first.Submit(t.Context(), JobSpec{Name: "lease", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, runErr := first.Run(context.Background(), job.ID)
		result <- runErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first worker did not start")
	}
	second := openTestEngine(t, database, Options{Owner: "worker-two", LeaseTTL: time.Second, HeartbeatInterval: 100 * time.Millisecond}, tool)
	if _, err := second.Run(t.Context(), job.ID); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second worker error = %v", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestFileEditConflictRequiresActionWithoutOverwrite(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "source.txt")
	before := []byte("base\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	tool, _ := NewFileEditTool(workspace)
	database := filepath.Join(t.TempDir(), "jobs.db")
	engine := openTestEngine(t, database, Options{
		Owner: "worker",
		FaultInjector: func(point FaultPoint, _ FaultContext) {
			if point == FaultBeforeEffect {
				_ = os.WriteFile(path, []byte("external\n"), 0o600)
			}
		},
	}, tool)
	input, _ := json.Marshal(FileEditInput{
		Path: path, ExpectedSHA256: digestBytes(before), OldText: "base", NewText: "agent",
	})
	job, _ := engine.Submit(t.Context(), JobSpec{Name: "conflict", Steps: []StepSpec{{Tool: tool.Name(), Input: input}}})
	job, err := engine.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrRequiresAction) || job.Status != JobRequiresAction {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "external\n" || !strings.Contains(job.Error, "前置条件冲突") {
		t.Fatalf("content = %q, job error = %q", content, job.Error)
	}
}

func openTestEngine(t *testing.T, database string, options Options, tools ...Tool) *Engine {
	t.Helper()
	engine, err := Open(database, options, tools...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}
