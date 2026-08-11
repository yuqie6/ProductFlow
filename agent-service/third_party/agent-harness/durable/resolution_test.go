package durable

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveUnknownAppliedContinuesWithoutRepeatingOpaqueEffect(t *testing.T) {
	fixture := makeUnknownFixture(t, FaultAfterEffect, true)
	if fixture.opaqueExecutions.Load() != 1 {
		t.Fatalf("opaque executions before resolution = %d", fixture.opaqueExecutions.Load())
	}
	target := mustResolutionTarget(t, fixture.engine, fixture.job.ID)
	result := json.RawMessage(`{"exit_code":0,"verified":"external receipt"}`)
	if _, err := fixture.engine.ResolveUnknown(t.Context(), fixture.job.ID, UnknownResolution{
		StepID: target.StepID, AttemptID: "different-attempt",
		Outcome: ResolutionApplied, Actor: "operator", Reason: "stale receipt", Result: result,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale attempt resolution error = %v", err)
	}
	job, err := fixture.engine.ResolveUnknown(t.Context(), fixture.job.ID, UnknownResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionApplied,
		Actor:   "operator@example.com",
		Reason:  "外部系统确认请求已经提交",
		Result:  result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobPending || job.Steps[0].Status != StepSucceeded || string(job.Steps[0].Result) != string(result) {
		t.Fatalf("resolved job = %#v", job)
	}
	job, err = fixture.engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobSucceeded {
		t.Fatalf("continued job = %#v, err = %v", job, err)
	}
	if fixture.opaqueExecutions.Load() != 1 || fixture.followupExecutions.Load() != 1 {
		t.Fatalf("opaque = %d, followup = %d", fixture.opaqueExecutions.Load(), fixture.followupExecutions.Load())
	}
	if _, err := fixture.engine.ResolveUnknown(t.Context(), job.ID, UnknownResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionFailed, Actor: "second-operator", Reason: "duplicate decision",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second resolution error = %v", err)
	}
	events, err := fixture.engine.Events(t.Context(), job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundAudit := false
	for _, event := range events {
		if event.Kind != "attempt.resolved" {
			continue
		}
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
		foundAudit = payload.StepID == target.StepID && payload.AttemptID == target.AttemptID &&
			payload.Outcome == ResolutionApplied && payload.Actor == "operator@example.com" && payload.Reason != ""
	}
	if !foundAudit {
		t.Fatalf("resolution audit event missing: %#v", events)
	}
}

func TestResolveUnknownFailedMakesJobTerminal(t *testing.T) {
	fixture := makeUnknownFixture(t, FaultBeforeEffect, false)
	target := mustResolutionTarget(t, fixture.engine, fixture.job.ID)
	job, err := fixture.engine.ResolveUnknown(t.Context(), fixture.job.ID, UnknownResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionFailed,
		Actor:   "operator",
		Reason:  "目标系统明确返回未接受请求,不允许重试",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobFailed || job.Steps[0].Status != StepFailed || job.Error == "" {
		t.Fatalf("resolved job = %#v", job)
	}
	if _, err := fixture.engine.Run(t.Context(), job.ID); !errors.Is(err, ErrJobFailed) {
		t.Fatalf("Run error = %v", err)
	}
	attempts, err := fixture.engine.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != AttemptFailed {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestResolveUnknownRetryCreatesNewAttemptWithoutExecutingIt(t *testing.T) {
	fixture := makeUnknownFixture(t, FaultBeforeEffect, false)
	target := mustResolutionTarget(t, fixture.engine, fixture.job.ID)
	job, err := fixture.engine.ResolveUnknown(t.Context(), fixture.job.ID, UnknownResolution{
		StepID: target.StepID, AttemptID: target.AttemptID,
		Outcome: ResolutionRetry,
		Actor:   "operator",
		Reason:  "审计日志证明命令尚未开始,授权一次新 attempt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobPending || job.Steps[0].Status != StepPrepared || fixture.opaqueExecutions.Load() != 0 {
		t.Fatalf("resolved job = %#v, executions = %d", job, fixture.opaqueExecutions.Load())
	}
	attempts, err := fixture.engine.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].Status != AttemptUnknown || attempts[1].Status != AttemptPrepared {
		t.Fatalf("attempts = %#v", attempts)
	}
	if attempts[0].IdempotencyKey == attempts[1].IdempotencyKey || attempts[1].Number != attempts[0].Number+1 {
		t.Fatalf("attempt identity was reused: %#v", attempts)
	}
	job, err = fixture.engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != JobSucceeded || fixture.opaqueExecutions.Load() != 1 {
		t.Fatalf("retried job = %#v, executions = %d, err = %v", job, fixture.opaqueExecutions.Load(), err)
	}
}

func TestResolveUnknownValidatesAuditDecision(t *testing.T) {
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{})
	for _, resolution := range []UnknownResolution{
		{Outcome: ResolutionApplied, Actor: "operator", Reason: "missing target"},
		{StepID: "step", Outcome: ResolutionApplied, Actor: "operator", Reason: "missing attempt"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionApplied, Reason: "missing actor"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionApplied, Actor: "operator"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionOutcome("invalid"), Actor: "operator", Reason: "invalid outcome"},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionApplied, Actor: "operator", Reason: "invalid result", Result: json.RawMessage(`{`)},
		{StepID: "step", AttemptID: "attempt", Outcome: ResolutionRetry, Actor: "operator", Reason: "unexpected result", Result: json.RawMessage(`{}`)},
	} {
		if _, err := engine.ResolveUnknown(t.Context(), "missing-job", resolution); err == nil {
			t.Fatalf("ResolveUnknown(%#v) succeeded", resolution)
		}
	}
}

type unknownFixture struct {
	engine             *Engine
	job                Job
	opaqueExecutions   *atomic.Int32
	followupExecutions *atomic.Int32
}

func makeUnknownFixture(t *testing.T, point FaultPoint, withFollowup bool) unknownFixture {
	t.Helper()
	database := filepath.Join(t.TempDir(), "jobs.db")
	opaqueExecutions := &atomic.Int32{}
	followupExecutions := &atomic.Int32{}
	opaque := FuncTool{
		ToolName: "opaque_fixture", EffectClass: EffectOpaque,
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			opaqueExecutions.Add(1)
			return json.RawMessage(`{"submitted":true}`), nil
		},
	}
	registered := []Tool{opaque}
	steps := []StepSpec{{Tool: opaque.Name(), Input: json.RawMessage(`{}`)}}
	if withFollowup {
		followup := FuncTool{
			ToolName: "followup_fixture", EffectClass: EffectPure,
			ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
				followupExecutions.Add(1)
				return json.RawMessage(`{"done":true}`), nil
			},
		}
		registered = append(registered, followup)
		steps = append(steps, StepSpec{Tool: followup.Name(), Input: json.RawMessage(`{}`)})
	}
	crashed, err := Open(database, Options{
		Owner: "crashed-operator-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(actual FaultPoint, _ FaultContext) {
			if actual == point {
				panic("simulated worker loss")
			}
		},
	}, registered...)
	if err != nil {
		t.Fatal(err)
	}
	job, err := crashed.Submit(t.Context(), JobSpec{Name: "unknown fixture", Steps: steps})
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
	if err := crashed.Close(); err != nil {
		t.Fatal(err)
	}
	recovery := openTestEngine(t, database, Options{
		Owner: "reconciliation-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, registered...)
	waitForPersistedLeasesToExpire(t, recovery, job.ID)
	job, err = recovery.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrUnknown) || job.Status != JobUnknown {
		t.Fatalf("unknown job = %#v, err = %v", job, err)
	}
	return unknownFixture{
		engine: recovery, job: job, opaqueExecutions: opaqueExecutions, followupExecutions: followupExecutions,
	}
}

func waitForPersistedLeasesToExpire(t *testing.T, engine *Engine, jobID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var expiredSince time.Time
	for {
		job, err := engine.Get(t.Context(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		attempts, err := engine.Attempts(t.Context(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		expired := job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now)
		for _, attempt := range attempts {
			expired = expired && (attempt.LeaseExpiresAt == nil || !attempt.LeaseExpiresAt.After(now))
		}
		if expired {
			if expiredSince.IsZero() {
				expiredSince = now
			}
			if now.Sub(expiredSince) >= 2*engine.heartbeatInterval {
				return
			}
		} else {
			expiredSince = time.Time{}
		}
		if now.After(deadline) {
			t.Fatalf("persisted leases did not expire for job %s", jobID)
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-timer.C:
		case <-t.Context().Done():
			timer.Stop()
			t.Fatal(t.Context().Err())
		}
	}
}
