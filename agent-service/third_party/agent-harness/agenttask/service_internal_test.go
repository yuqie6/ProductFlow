package agenttask

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turnprotocol "github.com/yuqie6/agent-harness/turn"
)

func TestServiceTurnsTextDeltaPersistenceFailureIntoUnknown(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"lost\"}\n\n")
		_, _ = fmt.Fprint(writer, `data: {"type":"response.completed","response":{"id":"sink_failure","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"lost"}]}]}}`+"\n\n")
	}))
	t.Cleanup(provider.Close)
	workspace := t.TempDir()
	service, err := OpenService(ServiceConfig{Runner: Config{
		Database: filepath.Join(t.TempDir(), "sink-failure.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: ProviderConfig{APIKey: "secret", BaseURL: provider.URL, Model: "model", HTTPClient: provider.Client()},
		Policy:   Policy{MaxIterations: 4, ModelContextWindow: 100000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if _, err := service.store.db.Exec(`CREATE TRIGGER reject_text_delta
		BEFORE INSERT ON agent_turn_events_v1
		WHEN NEW.kind = 'text.delta'
		BEGIN SELECT RAISE(FAIL, 'text delta store unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	state, err := service.StartTurn(t.Context(), StartTurnRequest{
		RunID: "sink-failure-run", Input: TextInput("stream"), IdempotencyKey: "sink-failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, err = service.GetTurn(t.Context(), state.RunID, state.TurnID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == turnprotocol.StatusUnknown {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state.Status != turnprotocol.StatusUnknown || !strings.Contains(state.Error, "text delta store unavailable") {
		t.Fatalf("state = %#v", state)
	}
	events, err := service.Events(t.Context(), state.RunID, state.TurnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == EventTextDelta {
			t.Fatalf("failed text.delta was persisted: %#v", event)
		}
	}
}

func TestControlStoreSerializesRunByInsertionSequenceWhenClockMovesBackward(t *testing.T) {
	store, err := openControlStore(filepath.Join(t.TempDir(), "run-sequence.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.db.Close() })

	ctx := t.Context()
	base := time.Now().UTC()
	if _, _, err := store.insertTurn(ctx, "run", "first", "first-key", TextInput("first"), base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.insertTurn(ctx, "run", "second", "second-key", TextInput("second"), base.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	var firstSequence, secondSequence int64
	if err := store.db.QueryRowContext(ctx, `SELECT run_sequence FROM agent_turns_v1 WHERE turn_id = 'first'`).Scan(&firstSequence); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT run_sequence FROM agent_turns_v1 WHERE turn_id = 'second'`).Scan(&secondSequence); err != nil {
		t.Fatal(err)
	}
	if firstSequence != 1 || secondSequence != 2 {
		t.Fatalf("run sequences = first:%d second:%d", firstSequence, secondSequence)
	}

	claimed, err := store.claimTurn(ctx, "run", "second", base)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("second turn bypassed the earlier queued turn")
	}
	claimed, err = store.claimTurn(ctx, "run", "first", base)
	if err != nil || !claimed {
		t.Fatalf("claim first = %v, err = %v", claimed, err)
	}
	if err := store.setOutcome(ctx, "run", "first", TurnSucceeded, "done", "", nil, nil, base); err != nil {
		t.Fatal(err)
	}
	previous, found, err := store.latestSuccessfulTurnBefore(ctx, "run", "second")
	if err != nil || !found || previous != "first" {
		t.Fatalf("previous = %q, found = %v, err = %v", previous, found, err)
	}
	next, autoResume, err := store.nextRunnable(ctx, "run")
	if err != nil || next != "second" || !autoResume {
		t.Fatalf("next = %q, auto resume = %v, err = %v", next, autoResume, err)
	}
}

func TestControlStoreMigratesExistingTurnOrderToRunSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := `CREATE TABLE agent_turns_v1 (
		turn_id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		idempotency_key TEXT NOT NULL,
		input_json BLOB NOT NULL,
		input_sha256 TEXT NOT NULL,
		status TEXT NOT NULL,
		output TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		question_json BLOB,
		artifact_json BLOB,
		durable_sequence INTEGER NOT NULL DEFAULT 0,
		cancel_requested INTEGER NOT NULL DEFAULT 0,
		resume_required INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		started_at INTEGER,
		finished_at INTEGER,
		UNIQUE(run_id, idempotency_key)
	)`
	if _, err := database.ExecContext(context.Background(), legacySchema); err != nil {
		t.Fatal(err)
	}
	input, digest, err := inputBytes(TextInput("legacy"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		turnID  string
		runID   string
		key     string
		created int64
	}{
		{turnID: "run-a-second", runID: "run-a", key: "a2", created: 20},
		{turnID: "run-a-first", runID: "run-a", key: "a1", created: 10},
		{turnID: "run-b-first", runID: "run-b", key: "b1", created: 5},
	} {
		if _, err := database.ExecContext(context.Background(), `INSERT INTO agent_turns_v1(
			turn_id, run_id, idempotency_key, input_json, input_sha256, status, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, row.turnID, row.runID, row.key, input, digest, TurnSucceeded, row.created, row.created); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := openControlStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.db.Close() })
	for turnID, expected := range map[string]int64{
		"run-a-first": 1, "run-a-second": 2, "run-b-first": 1,
	} {
		var sequence int64
		if err := store.db.QueryRowContext(t.Context(), `SELECT run_sequence FROM agent_turns_v1 WHERE turn_id = ?`, turnID).Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		if sequence != expected {
			t.Fatalf("%s run sequence = %d, want %d", turnID, sequence, expected)
		}
	}
}
