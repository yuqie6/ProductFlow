package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestExpiredCreateProductWorkspaceExactlyOnceUnderFourScanners(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)

	const n = 25
	prefix := "capws-" + clockid.New()[:12]
	turns := make([]claimedJournalTurn, n)
	keys := make([]string, n)
	for i := range turns {
		turns[i] = createClaimedJournalTurn(t, as)
		keys[i] = clockid.New()
		intent := workspaceIntent(fmt.Sprintf("%s-%02d", prefix, i))
		intent.IdempotencyKey = keys[i]
		intent.ToolCallID = "call-" + keys[i]
		insertIntentCheckpoint(t, as, turns[i], intent)
		if _, err := as.svc.AppendEvents(context.Background(), turns[i].conversationID, turns[i].lease.ExecutionID, "worker-1", turns[i].lease.LeaseToken, []EventAppendInput{{
			Sequence: 1, SchemaVersion: 1, RunID: turns[i].turn.HarnessRunID, TurnID: *turns[i].turn.HarnessTurnID,
			Kind: "text.chunk", Payload: []byte(`{"delta":"容量扫描","attempt_id":"a","content_index":0}`),
		}}); err != nil {
			t.Fatal(err)
		}
		expireClaimedTurn(t, as, turns[i])
	}

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, recErr := recoverUnfinishedTurns(context.Background(), as.svc, 7)
			errs <- recErr
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	drainAgentRecovery(t, as)

	assertExpiredWorkspaceOnce(t, as, prefix, turns, keys)

	summary, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownExecutions != 0 {
		t.Fatalf("second scan recovered more unknowns: %+v", summary)
	}
	assertExpiredWorkspaceOnce(t, as, prefix, turns, keys)
}

func assertExpiredWorkspaceOnce(t *testing.T, as *agentServer, prefix string, turns []claimedJournalTurn, keys []string) {
	t.Helper()
	ctx := context.Background()
	var products int
	if err := as.pool.QueryRow(ctx, `SELECT COUNT(*) FROM products WHERE name LIKE $1`, prefix+"%").Scan(&products); err != nil {
		t.Fatal(err)
	}
	if products != len(turns) {
		t.Fatalf("products %d want %d", products, len(turns))
	}

	ids := make([]string, len(turns))
	for i, claimed := range turns {
		ids[i] = claimed.turn.ID
	}

	var mutations int
	if err := as.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_conversations WHERE creation_idempotency_key = ANY($1)
	`, keys).Scan(&mutations); err != nil {
		t.Fatal(err)
	}
	if mutations != len(keys) {
		t.Fatalf("workspace creation keys %d want %d", mutations, len(keys))
	}

	var applied int
	if err := as.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_turn_effect_reconciliations
		WHERE turn_projection_id = ANY($1) AND effect_result = 'applied'
	`, ids).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != len(ids) {
		t.Fatalf("applied reconciliations %d want %d", applied, len(ids))
	}

	var unknown, turnEnds, toolResults int
	if err := as.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_turn_projections WHERE id = ANY($1) AND status = 'unknown'
	`, ids).Scan(&unknown); err != nil {
		t.Fatal(err)
	}
	if unknown != len(ids) {
		t.Fatalf("unknown projections %d want %d", unknown, len(ids))
	}
	if err := as.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_turn_events WHERE turn_projection_id = ANY($1) AND kind = 'turn/end'
	`, ids).Scan(&turnEnds); err != nil {
		t.Fatal(err)
	}
	if turnEnds != len(ids) {
		t.Fatalf("turn/end %d want %d", turnEnds, len(ids))
	}
	if err := as.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_turn_events WHERE turn_projection_id = ANY($1) AND kind = 'tool/result'
	`, ids).Scan(&toolResults); err != nil {
		t.Fatal(err)
	}
	if toolResults != len(ids) {
		t.Fatalf("tool/result %d want %d", toolResults, len(ids))
	}
}
