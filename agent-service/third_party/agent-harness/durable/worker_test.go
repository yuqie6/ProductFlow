package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenMigratesV1JournalToJobLeases(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	raw, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range durableSchema {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().UnixNano()
	if _, err := raw.Exec(`INSERT INTO jobs(id, name, status, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		"legacy-job", "legacy", JobPending, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO steps(
		id, job_id, position, tool, effect, input_json, status, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-step", "legacy-job", 0, "migration_tool", EffectPure, []byte(`{}`), StepPending, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	tool := successfulPureTool("migration_tool", nil)
	engine := openTestEngine(t, database, Options{Owner: "migration-worker"}, tool)
	var version int
	if err := engine.store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != storeSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, storeSchemaVersion)
	}
	legacy, err := engine.Get(t.Context(), "legacy-job")
	if err != nil || legacy.Kind != JobKindToolSequence {
		t.Fatalf("legacy job = %#v, err = %v", legacy, err)
	}
	legacy, err = engine.Run(t.Context(), legacy.ID)
	if err != nil || legacy.Status != JobSucceeded {
		t.Fatalf("legacy run = %#v, err = %v", legacy, err)
	}
	job, err := engine.Submit(t.Context(), JobSpec{
		Name: "post migration", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if err != nil || job.Kind != JobKindToolSequence || job.Status != JobSucceeded || job.LeaseOwner != "" || job.LeaseExpiresAt != nil {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
}

func TestJobHeartbeatProtectsSlowPreparation(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	prepareStarted := make(chan struct{})
	releasePrepare := make(chan struct{})
	var preparations atomic.Int32
	var executions atomic.Int32
	tool := FuncTool{
		ToolName: "slow_prepare", EffectClass: EffectPure,
		PrepareFunc: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			preparations.Add(1)
			close(prepareStarted)
			select {
			case <-releasePrepare:
				return input, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			executions.Add(1)
			return json.RawMessage(`{"done":true}`), nil
		},
	}
	first := openTestEngine(t, database, Options{
		Owner: "preparing-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, tool)
	job, err := first.Submit(t.Context(), JobSpec{
		Name: "slow preparation", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, runErr := first.Run(context.Background(), job.ID)
		result <- runErr
	}()
	select {
	case <-prepareStarted:
	case <-time.After(time.Second):
		t.Fatal("preparation did not start")
	}
	time.Sleep(180 * time.Millisecond)
	current, err := first.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != JobRunning || current.LeaseOwner != "preparing-worker" || current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(time.Now()) {
		t.Fatalf("job lease = %#v", current)
	}
	second := openTestEngine(t, database, Options{
		Owner: "competing-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, tool)
	if _, err := second.Run(t.Context(), job.ID); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("competing Run error = %v", err)
	}
	close(releasePrepare)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if preparations.Load() != 1 || executions.Load() != 1 {
		t.Fatalf("preparations = %d, executions = %d", preparations.Load(), executions.Load())
	}
}

func TestConcurrentRunNextClaimsJobOnce(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	started := make(chan struct{})
	release := make(chan struct{})
	var executions atomic.Int32
	tool := FuncTool{
		ToolName: "queued_tool", EffectClass: EffectPure,
		ExecuteFunc: func(ctx context.Context, _ Invocation) (json.RawMessage, error) {
			executions.Add(1)
			close(started)
			select {
			case <-release:
				return json.RawMessage(`{"done":true}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	first := openTestEngine(t, database, Options{Owner: "queue-worker-one"}, tool)
	if _, err := first.Submit(t.Context(), JobSpec{
		Name: "queued", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	}); err != nil {
		t.Fatal(err)
	}
	second := openTestEngine(t, database, Options{Owner: "queue-worker-two"}, tool)
	type runNextResult struct {
		job   Job
		found bool
		err   error
	}
	firstResult := make(chan runNextResult, 1)
	go func() {
		job, found, err := first.RunNext(context.Background())
		firstResult <- runNextResult{job: job, found: found, err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first worker did not execute job")
	}
	if job, found, err := second.RunNext(t.Context()); err != nil || found {
		t.Fatalf("second RunNext = job %#v, found %v, err %v", job, found, err)
	}
	close(release)
	completed := <-firstResult
	if completed.err != nil || !completed.found || completed.job.Status != JobSucceeded {
		t.Fatalf("first RunNext = %#v", completed)
	}
	if executions.Load() != 1 {
		t.Fatalf("executions = %d", executions.Load())
	}
}

func TestRunNextDoesNotStarveBehindUnsupportedJobs(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	unsupported := successfulPureTool("unsupported_tool", nil)
	submitter := openTestEngine(t, database, Options{Owner: "mixed-submitter"}, unsupported)
	for index := 0; index < 70; index++ {
		if _, err := submitter.Submit(t.Context(), JobSpec{
			Name:  fmt.Sprintf("unsupported-%d", index),
			Steps: []StepSpec{{Tool: unsupported.Name(), Input: json.RawMessage(`{}`)}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	supported := successfulPureTool("supported_tool", nil)
	supportedSubmitter := openTestEngine(t, database, Options{Owner: "supported-submitter"}, supported)
	want, err := supportedSubmitter.Submit(t.Context(), JobSpec{
		Name: "supported", Steps: []StepSpec{{Tool: supported.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := openTestEngine(t, database, Options{Owner: "supported-worker"}, supported)
	job, found, err := worker.RunNext(t.Context())
	if err != nil || !found || job.ID != want.ID || job.Status != JobSucceeded {
		t.Fatalf("RunNext = job %#v, found %v, err %v", job, found, err)
	}
}

func TestRunNextDoesNotClaimAgentTurn(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	tool := successfulPureTool("shared_tool", nil)
	engine := openTestEngine(t, database, Options{Owner: "generic-worker"}, tool)
	agent, err := engine.Submit(t.Context(), JobSpec{
		Kind: JobKindAgentTurn, Name: "agent turn",
		Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job, found, err := engine.RunNext(t.Context()); err != nil || found {
		t.Fatalf("RunNext = job %#v, found %v, err %v", job, found, err)
	}
	agent, err = engine.Run(t.Context(), agent.ID)
	if err != nil || agent.Status != JobSucceeded {
		t.Fatalf("direct Run = %#v, err = %v", agent, err)
	}
}

func TestListFiltersJobsAndRejectsInvalidOptions(t *testing.T) {
	database := filepath.Join(t.TempDir(), "jobs.db")
	tool := successfulPureTool("list_tool", nil)
	engine := openTestEngine(t, database, Options{Owner: "list-worker"}, tool)
	pending, err := engine.Submit(t.Context(), JobSpec{
		ID: "pending-list-job", Name: "pending", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	succeeded, err := engine.Submit(t.Context(), JobSpec{
		ID: "succeeded-list-job", Name: "succeeded", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Run(t.Context(), succeeded.ID); err != nil {
		t.Fatal(err)
	}
	agent, err := engine.Submit(t.Context(), JobSpec{
		ID: "agent-list-job", Kind: JobKindAgentTurn, Name: "agent",
		Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := engine.List(t.Context(), ListOptions{
		Statuses: []JobStatus{JobPending, JobPending}, Kinds: []JobKind{JobKindToolSequence}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != pending.ID {
		t.Fatalf("pending jobs = %#v", jobs)
	}
	jobs, err = engine.List(t.Context(), ListOptions{Kinds: []JobKind{JobKindAgentTurn, JobKindAgentTurn}, Limit: 10})
	if err != nil || len(jobs) != 1 || jobs[0].ID != agent.ID {
		t.Fatalf("agent jobs = %#v, err = %v", jobs, err)
	}
	jobs, err = engine.List(t.Context(), ListOptions{Limit: 1})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("limited jobs = %#v, err = %v", jobs, err)
	}
	jobs, err = engine.List(t.Context(), ListOptions{Kinds: []JobKind{JobKindToolSequence}, OldestFirst: true, Limit: 1})
	if err != nil || len(jobs) != 1 || jobs[0].ID != pending.ID {
		t.Fatalf("oldest jobs = %#v, err = %v", jobs, err)
	}
	for _, options := range []ListOptions{
		{Limit: -1}, {Limit: 501}, {Statuses: []JobStatus{"invalid"}}, {Kinds: []JobKind{"invalid"}},
	} {
		if _, err := engine.List(t.Context(), options); err == nil {
			t.Fatalf("List(%#v) succeeded", options)
		}
	}
	if _, err := engine.Submit(t.Context(), JobSpec{
		Kind: "invalid", Steps: []StepSpec{{Tool: tool.Name(), Input: json.RawMessage(`{}`)}},
	}); err == nil {
		t.Fatal("Submit accepted invalid kind")
	}
}

func successfulPureTool(name string, executions *atomic.Int32) FuncTool {
	return FuncTool{
		ToolName: name, EffectClass: EffectPure,
		ExecuteFunc: func(context.Context, Invocation) (json.RawMessage, error) {
			if executions != nil {
				executions.Add(1)
			}
			return json.RawMessage(`{"done":true}`), nil
		},
	}
}
