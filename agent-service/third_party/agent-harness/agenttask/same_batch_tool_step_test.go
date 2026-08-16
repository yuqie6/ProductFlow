package agenttask

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	turnprotocol "github.com/yuqie6/agent-harness/turn"
)

type fakeJournalReader struct {
	events    []durable.Event
	job       durable.Job
	taskCalls int
}

func (reader *fakeJournalReader) Events(_ context.Context, _ string, afterSequence int64) ([]durable.Event, error) {
	var events []durable.Event
	for _, event := range reader.events {
		if event.Sequence > afterSequence {
			events = append(events, event)
		}
	}
	return events, nil
}

func (reader *fakeJournalReader) Task(_ context.Context, _ string) (durable.Job, error) {
	reader.taskCalls++
	return reader.job, nil
}

func TestSyncDurableEventsPreservesSameBatchAttemptStatusesAndReplay(t *testing.T) {
	service, reader := newSyncDurableEventsTestService(t, []durable.Event{
		{Sequence: 1, JobID: "turn-batch", StepID: "step-1", AttemptID: "attempt-1", Kind: "attempt.started"},
		{Sequence: 2, JobID: "turn-batch", StepID: "step-1", AttemptID: "attempt-1", Kind: "attempt.succeeded"},
	}, durable.Job{ID: "turn-batch", Steps: []durable.Step{{ID: "step-1"}}})
	ctx := context.Background()

	if err := service.syncDurableEvents(ctx, "run-batch", "turn-batch"); err != nil {
		t.Fatal(err)
	}
	assertToolStepReplay(t, service.store, []string{"running", "succeeded"})
	assertDurableCursor(t, service.store, 2)
	if reader.taskCalls != 1 {
		t.Fatalf("Task calls = %d, want 1", reader.taskCalls)
	}

	if err := service.syncDurableEvents(ctx, "run-batch", "turn-batch"); err != nil {
		t.Fatal(err)
	}
	assertToolStepReplay(t, service.store, []string{"running", "succeeded"})
	assertDurableCursor(t, service.store, 2)
	if reader.taskCalls != 1 {
		t.Fatalf("Task calls after second sync = %d, want 1", reader.taskCalls)
	}
}

func TestSyncDurableEventsAdvancesCursorWithoutLifecycleEvents(t *testing.T) {
	service, reader := newSyncDurableEventsTestService(t, []durable.Event{
		{Sequence: 1, JobID: "turn-batch", StepID: "step-1", Kind: "step.output"},
		{Sequence: 2, JobID: "turn-batch", StepID: "step-1", Kind: "job.updated"},
	}, durable.Job{ID: "turn-batch", Steps: []durable.Step{{ID: "step-1"}}})

	if err := service.syncDurableEvents(context.Background(), "run-batch", "turn-batch"); err != nil {
		t.Fatal(err)
	}
	assertToolStepReplay(t, service.store, nil)
	assertDurableCursor(t, service.store, 2)
	if reader.taskCalls != 0 {
		t.Fatalf("Task calls = %d, want 0", reader.taskCalls)
	}
}

func newSyncDurableEventsTestService(t *testing.T, events []durable.Event, job durable.Job) (*Service, *fakeJournalReader) {
	t.Helper()
	store, err := openControlStore(filepath.Join(t.TempDir(), "sync-durable-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	input, digest, err := inputBytes(TextInput("same batch"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO agent_turns_v1(
		turn_id, run_id, run_sequence, idempotency_key, input_json, input_sha256, status, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, "turn-batch", "run-batch", 1, "batch", input, digest,
		turnprotocol.StatusRunning, now.UnixNano(), now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	reader := &fakeJournalReader{events: events, job: job}
	service := &Service{
		store:   store,
		journal: reader,
		toolProjector: func(job durable.Job) []turnprotocol.ToolStep {
			return []turnprotocol.ToolStep{{
				StepID: job.Steps[0].ID, Kind: "inspect_image", Summary: "Inspect product image assets", Status: "succeeded",
			}}
		},
	}
	return service, reader
}

func assertToolStepReplay(t *testing.T, store *controlStore, wantStatuses []string) {
	t.Helper()
	events, err := store.events(context.Background(), "run-batch", "turn-batch", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(wantStatuses) {
		t.Fatalf("public events = %#v, want %d", events, len(wantStatuses))
	}
	for index, event := range events {
		if event.Kind != turnprotocol.EventToolStep {
			t.Fatalf("event %d kind = %q, want %q", index, event.Kind, turnprotocol.EventToolStep)
		}
		if index > 0 && event.Sequence <= events[index-1].Sequence {
			t.Fatalf("event sequences are not increasing: %d then %d", events[index-1].Sequence, event.Sequence)
		}
		var step turnprotocol.ToolStep
		if err := json.Unmarshal(event.Payload, &step); err != nil {
			t.Fatal(err)
		}
		if step.Status != wantStatuses[index] {
			t.Fatalf("event %d status = %q, want %q", index, step.Status, wantStatuses[index])
		}
	}
}

func assertDurableCursor(t *testing.T, store *controlStore, want int64) {
	t.Helper()
	cursor, err := store.durableCursor(context.Background(), "turn-batch")
	if err != nil {
		t.Fatal(err)
	}
	if cursor != want {
		t.Fatalf("durable cursor = %d, want %d", cursor, want)
	}
}
