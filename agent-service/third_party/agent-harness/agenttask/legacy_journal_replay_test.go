package agenttask

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turnprotocol "github.com/yuqie6/agent-harness/turn"
)

func TestEventsFiltersPersistedLegacyJournalRowsWithoutDeletingOrStallingCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-journal.db")
	store, err := openControlStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	input, digest, err := inputBytes(TextInput("legacy replay"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO agent_turns_v1(
		turn_id, run_id, run_sequence, idempotency_key, input_json, input_sha256, status, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, "turn-legacy", "run-legacy", 1, "legacy", input, digest,
		turnprotocol.StatusSucceeded, now.UnixNano(), now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	legacyPayload, _ := json.Marshal(map[string]any{"input": "SECRET_SENTINEL", "result": "SECRET_SENTINEL", "checkpoint": "SECRET_SENTINEL", "lease": "SECRET_SENTINEL"})
	if _, err := store.db.ExecContext(ctx, `INSERT INTO agent_turn_events_v1(
		turn_id, sequence, schema_version, run_id, kind, payload_json, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?)`, "turn-legacy", 1, turnprotocol.EventSchemaVersion, "run-legacy", "journal.attempt.started", legacyPayload, now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	publicPayload := json.RawMessage(`{"delta":"safe"}`)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO agent_turn_events_v1(
		turn_id, sequence, schema_version, run_id, kind, payload_json, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?)`, "turn-legacy", 2, turnprotocol.EventSchemaVersion, "run-legacy", turnprotocol.EventTextDelta, publicPayload, now.UnixNano()); err != nil {
		t.Fatal(err)
	}

	events, err := store.events(ctx, "run-legacy", "turn-legacy", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 2 || events[0].Kind != turnprotocol.EventTextDelta {
		t.Fatalf("filtered events = %#v", events)
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "journal.") || strings.Contains(string(encoded), "SECRET_SENTINEL") {
		t.Fatalf("legacy payload crossed public boundary: %s", encoded)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_turn_events_v1 WHERE turn_id = ? AND kind LIKE 'journal.%'`, "turn-legacy").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("legacy row count = %d, want 1", count)
	}
	replayed, err := store.events(ctx, "run-legacy", "turn-legacy", events[0].Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 0 {
		t.Fatalf("replayed after public cursor = %#v", replayed)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT sequence FROM agent_turn_events_v1 WHERE turn_id = ? AND kind = ?`, "turn-legacy", turnprotocol.EventTextDelta).Scan(&count); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
}
