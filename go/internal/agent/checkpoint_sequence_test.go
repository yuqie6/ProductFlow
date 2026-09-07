package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestCheckpointSequenceRejectsDifferentParallelPayload(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	ctx := context.Background()
	appendCheckpoint := func(sequence int, payload string) error {
		_, err := as.svc.AppendCheckpoint(ctx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, sequence, "external_job_submitted", json.RawMessage(payload))
		return err
	}
	if err := appendCheckpoint(1, `{"job":"one"}`); err != nil {
		t.Fatal(err)
	}
	err := appendCheckpoint(1, `{"job":"two"}`)
	var app apperr.Error
	if !errors.As(err, &app) || app.Status != http.StatusConflict {
		t.Fatalf("different payload reused sequence: %v", err)
	}
	if err := appendCheckpoint(2, `{"job":"two"}`); err != nil {
		t.Fatal(err)
	}
	var count, last int
	if err := as.pool.QueryRow(ctx, "SELECT count(*) FROM agent_turn_checkpoints WHERE execution_id=$1", claimed.lease.ExecutionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := as.pool.QueryRow(ctx, "SELECT last_checkpoint_sequence FROM agent_turn_executions WHERE id=$1", claimed.lease.ExecutionID).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if count != 2 || last != 2 {
		t.Fatalf("rows=%d last=%d", count, last)
	}
}
