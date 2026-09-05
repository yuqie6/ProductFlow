package agent

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestExpiredRecoveryAdvancesPastLockedProjectionPrefix(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	ctx := context.Background()
	turns := make([]claimedJournalTurn, 26)
	ids := make([]string, 0, 25)
	for i := range turns {
		turns[i] = createClaimedJournalTurn(t, as)
		expireClaimedTurn(t, as, turns[i])
	}
	slices.SortFunc(turns, func(a, b claimedJournalTurn) int {
		return strings.Compare(a.lease.ExecutionID, b.lease.ExecutionID)
	})
	for _, turn := range turns[:25] {
		ids = append(ids, turn.turn.ID)
	}
	// Freeze the prefix in the scanner's actual execution-ID order.
	var last string
	if err := as.pool.QueryRow(ctx, `SELECT turn_projection_id FROM agent_turn_executions
	 WHERE owner_id IS NOT NULL AND lease_expires_at <= NOW() ORDER BY id DESC LIMIT 1`).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if last != turns[25].turn.ID {
		t.Fatal("fixture does not place the unlocked execution after the locked prefix")
	}
	locked, err := as.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(ctx)
	if _, err := locked.Exec(ctx, `SELECT id FROM agent_turn_projections WHERE id = ANY($1) FOR UPDATE`, ids); err != nil {
		t.Fatal(err)
	}
	for round := range 3 {
		runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		n, more, err := recoverExpiredExecutions(runCtx, as.svc, 25)
		cancel()
		want := 0
		if round == 0 {
			want = 1
		}
		if err != nil || n != want || !more {
			t.Fatalf("round=%d unknown=%d has_more=%v err=%v, want %d/true", round, n, more, err, want)
		}
	}
	var held int
	if err := as.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_turn_executions
	 WHERE turn_projection_id = ANY($1) AND owner_id IS NOT NULL`, ids).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held != 25 {
		t.Fatalf("locked execution owners=%d, want 25", held)
	}
	if err := locked.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	n, more, err := recoverExpiredExecutions(ctx, as.svc, 25)
	if err != nil || n != 25 || more {
		t.Fatalf("after unlock unknown=%d has_more=%v err=%v", n, more, err)
	}
	for _, turn := range turns {
		var status string
		var terminals, count, maxSeq int
		if err := as.pool.QueryRow(ctx, `SELECT status FROM agent_turn_projections WHERE id=$1`, turn.turn.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := as.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE kind='turn/end'), COUNT(*), COALESCE(MAX(sequence),0)
	 FROM agent_turn_events WHERE turn_projection_id=$1`, turn.turn.ID).Scan(&terminals, &count, &maxSeq); err != nil {
			t.Fatal(err)
		}
		if status != "unknown" || terminals != 1 || count != maxSeq {
			t.Fatalf("turn=%s status=%s terminals=%d count/max=%d/%d", turn.turn.ID, status, terminals, count, maxSeq)
		}
	}
}
