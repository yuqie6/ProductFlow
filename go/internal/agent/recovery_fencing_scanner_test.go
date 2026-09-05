package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestRecoverExpiredExecutionsRejectsStaleFencingWriter(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	claimed := createClaimedJournalTurn(t, as)
	oldLease := claimed.lease.LeaseToken
	oldFence := claimed.lease.FencingToken
	expireClaimedTurn(t, as, claimed)
	drainAgentRecovery(t, as)
	var fencing int
	var owner *string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT fencing_token, owner_id FROM agent_turn_executions WHERE id = $1
	`, claimed.lease.ExecutionID).Scan(&fencing, &owner); err != nil {
		t.Fatal(err)
	}
	if owner != nil {
		t.Fatalf("recovered execution still owned by %s", *owner)
	}
	if fencing <= oldFence {
		t.Fatalf("fencing %d want > %d", fencing, oldFence)
	}
	_, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", oldLease, []EventAppendInput{{
		Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
		Kind: "text.chunk", Payload: []byte(`{"delta":"旧 writer","attempt_id":"a","content_index":0}`),
	}})
	var appErr apperr.Error
	if !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Fatalf("old lease writer err %v", err)
	}
	if appErr.Detail != "Agent execution lease 已失效" {
		t.Fatalf("old lease detail %q", appErr.Detail)
	}

	live := createClaimedJournalTurn(t, as)
	staleAttempt := live.lease.Attempt
	staleFence := live.lease.FencingToken
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions SET fencing_token = fencing_token + 1, updated_at = NOW() WHERE id = $1
	`, live.lease.ExecutionID); err != nil {
		t.Fatal(err)
	}
	err = tx.WithGorm(context.Background(), as.db, func(gdb *gorm.DB) error {
		return as.svc.applyTurnState(context.Background(), gdb, nil, live.conversationID, live.turn.ID, TurnState{
			RunID:            live.turn.HarnessRunID,
			TurnID:           *live.turn.HarnessTurnID,
			Status:           "running",
			ExecutionAttempt: &staleAttempt,
			ExecutionFence:   &staleFence,
		})
	})
	if !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Fatalf("stale fencing writer err %v", err)
	}
	if appErr.Detail != "Agent Turn execution fencing token 已过期" {
		t.Fatalf("stale fencing detail %q", appErr.Detail)
	}
}

func drainAgentRecovery(t *testing.T, as *agentServer) {
	t.Helper()
	for range 20 {
		summary, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if !summary.HasMore {
			return
		}
	}
	t.Fatal("recovery still has more after 20 drain rounds")
}

func TestRecoverUnfinishedTurnsPreservesExpiredHasMore(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_agent_expired_%d", time.Now().UnixNano()))
	as := newAgentServerOnDB(t, mockGateway{}, "tok", pool, db)
	turns := []claimedJournalTurn{
		createClaimedJournalTurn(t, as),
		createClaimedJournalTurn(t, as),
	}
	for _, claimed := range turns {
		expireClaimedTurn(t, as, claimed)
	}

	summary, err := recoverUnfinishedTurns(context.Background(), as.svc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownExecutions != 1 {
		t.Fatalf("unknown executions %d want 1", summary.UnknownExecutions)
	}
	if !summary.HasMore {
		t.Fatalf("has_more=false after expired recovery filled its batch: %+v", summary)
	}

	second, err := recoverUnfinishedTurns(context.Background(), as.svc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.UnknownExecutions != 1 {
		t.Fatalf("second round unknown executions %d want 1", second.UnknownExecutions)
	}
	if second.HasMore {
		t.Fatalf("has_more=true after second expired round: %+v", second)
	}
}

func TestRecoverUnfinishedTurnsPreservesQueuedTaskHasMore(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var created SessionResponse
	as.decode(t, session, &created)
	convID := created.Conversations[0].ConversationID
	for i := range 2 {
		if _, err := as.svc.CreateTask(context.Background(), created.ID, "queued-recovery-"+string(rune('a'+i)), "补首轮 Turn", &convID); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := recoverUnfinishedTurns(context.Background(), as.svc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RecoveredTaskTurns != 1 {
		t.Fatalf("recovered task turns %d want 1: %+v", summary.RecoveredTaskTurns, summary)
	}
	if !summary.HasMore {
		t.Fatalf("has_more=false after queued task recovery filled its batch: %+v", summary)
	}
}

func TestRecoverUnfinishedTurnsPreservesPendingRestageHasMore(t *testing.T) {
	as := newAgentServer(t, &questionGateway{startErr: errors.New("start unavailable")}, "")
	drainAgentRecovery(t, as)
	resp := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, resp, http.StatusCreated)
	var session SessionResponse
	as.decode(t, resp, &session)
	var turns [2]SubmitTurnResponse
	for i := range turns {
		resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+session.Conversations[0].ConversationID+"/turns", map[string]any{
			"input_text": "pending recovery", "idempotency_key": session.ID + string(rune('a'+i)),
		})
		as.mustStatus(t, resp, http.StatusAccepted)
		as.decode(t, resp, &turns[i])
		if turns[i].Turn.HarnessTurnID != nil {
			t.Fatal("pending recovery fixture must be unbound")
		}
	}
	for _, submitted := range turns {
		if _, err := as.pool.Exec(context.Background(), `
			UPDATE async_dispatches
			SET status = $1, consumed_at = NOW(), updated_at = NOW()
			WHERE actor_name = $2 AND aggregate_id = $3
		`, queue.StatusConsumed, queue.ActorAgentTurnSync, submitted.Turn.ID); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := recoverUnfinishedTurns(context.Background(), as.svc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.EnqueuedTurns != 1 {
		t.Fatalf("enqueued turns %d want 1: %+v", summary.EnqueuedTurns, summary)
	}
	if !summary.HasMore {
		t.Fatalf("has_more=false after pending restage filled its batch: %+v", summary)
	}
}

func TestRecoverUnfinishedTurnsHasMoreORsExpiredQueuedAndPending(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	expired := createClaimedJournalTurn(t, as)
	expireClaimedTurn(t, as, expired)
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var created SessionResponse
	as.decode(t, session, &created)
	convID := created.Conversations[0].ConversationID
	if _, err := as.svc.CreateTask(context.Background(), created.ID, "queued-or", "补首轮 Turn", &convID); err != nil {
		t.Fatal(err)
	}
	pending := createClaimedJournalTurn(t, as)
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE async_dispatches
		SET status = $1, consumed_at = NOW(), updated_at = NOW()
		WHERE actor_name = $2 AND aggregate_id = $3
	`, queue.StatusConsumed, queue.ActorAgentTurnSync, pending.turn.ID); err != nil {
		t.Fatal(err)
	}

	summary, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownExecutions < 1 {
		t.Fatalf("expected expired unknown: %+v", summary)
	}
	if summary.RecoveredTaskTurns < 1 {
		t.Fatalf("expected queued task recovery: %+v", summary)
	}
	if summary.EnqueuedTurns < 1 {
		t.Fatalf("expected pending restage: %+v", summary)
	}
}

func TestConcurrentExpiredRecoverySkipLockedDoesNotDoubleTerminate(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	turns := make([]claimedJournalTurn, 4)
	for i := range turns {
		turns[i] = createClaimedJournalTurn(t, as)
		if _, err := as.svc.AppendEvents(context.Background(), turns[i].conversationID, turns[i].lease.ExecutionID, "worker-1", turns[i].lease.LeaseToken, []EventAppendInput{{
			Sequence: 1, SchemaVersion: 1, RunID: turns[i].turn.HarnessRunID, TurnID: *turns[i].turn.HarnessTurnID,
			Kind: "text.chunk", Payload: []byte(`{"delta":"并发扫描","attempt_id":"a","content_index":0}`),
		}}); err != nil {
			t.Fatal(err)
		}
		expireClaimedTurn(t, as, turns[i])
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- func() error {
				_, _, recErr := recoverExpiredExecutions(context.Background(), as.svc, 2)
				return recErr
			}()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var unknown int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM agent_turn_projections
		WHERE id IN ($1, $2, $3, $4) AND status = 'unknown'
	`, turns[0].turn.ID, turns[1].turn.ID, turns[2].turn.ID, turns[3].turn.ID).Scan(&unknown); err != nil {
		t.Fatal(err)
	}
	if unknown != 4 {
		t.Fatalf("unknown recovered turns %d want 4", unknown)
	}
	for _, claimed := range turns {
		var n int
		if err := as.pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM agent_turn_events
			WHERE turn_projection_id = $1 AND kind = 'turn/end'
		`, claimed.turn.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("turn %s terminal events %d", claimed.turn.ID, n)
		}
		var maxSeq, count int
		if err := as.pool.QueryRow(context.Background(), `
			SELECT COALESCE(MAX(sequence), 0), COUNT(*) FROM agent_turn_events WHERE turn_projection_id = $1
		`, claimed.turn.ID).Scan(&maxSeq, &count); err != nil {
			t.Fatal(err)
		}
		if maxSeq != count {
			t.Fatalf("turn %s sequence hole max=%d count=%d", claimed.turn.ID, maxSeq, count)
		}
	}
}

func TestAppendEventsAndExpiredRecoveryDoNotDeadlock(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	claimed := createClaimedJournalTurn(t, as)
	appendProjectionLocked := make(chan struct{})
	releaseAppend := make(chan struct{})
	var appendOnce sync.Once
	const afterName = "agent_append_recovery_after_projection"
	if err := as.db.Callback().Query().After("gorm:query").Register(afterName, func(db *gorm.DB) {
		if db.Error != nil || db.Statement.Table != "agent_turn_projections" || db.Statement.Context.Value(journalLockContextKey{}) != "append" {
			return
		}
		appendOnce.Do(func() {
			close(appendProjectionLocked)
			<-releaseAppend
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		as.db.Callback().Query().Remove(afterName)
	})

	type result struct {
		operation string
		err       error
	}
	results := make(chan result, 2)
	appendCtx, cancelAppend := context.WithTimeout(context.WithValue(context.Background(), journalLockContextKey{}, "append"), 8*time.Second)
	defer cancelAppend()
	go func() {
		_, err := as.svc.AppendEvents(appendCtx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{{
			Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"live writer","attempt_id":"a","content_index":0}`),
		}})
		results <- result{operation: "append", err: err}
	}()
	select {
	case <-appendProjectionLocked:
	case <-time.After(3 * time.Second):
		t.Fatal("append did not acquire projection lock")
	}
	expireClaimedTurn(t, as, claimed)
	go func() {
		_, _, recErr := recoverExpiredExecutions(context.Background(), as.svc, 25)
		results <- result{operation: "recovery", err: recErr}
	}()
	select {
	case outcome := <-results:
		if outcome.operation != "recovery" {
			close(releaseAppend)
			t.Fatalf("expected recovery to skip the locked projection, got %s: %v", outcome.operation, outcome.err)
		}
		var pgErr *pgconn.PgError
		if errors.As(outcome.err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("recovery deadlocked: %v", outcome.err)
		}
		if outcome.err != nil {
			t.Fatalf("recovery failed: %v", outcome.err)
		}
	case <-time.After(4 * time.Second):
		close(releaseAppend)
		t.Fatal("recovery blocked behind the live append writer")
	}
	close(releaseAppend)
	appendOutcome := <-results
	if appendOutcome.operation != "append" {
		t.Fatalf("expected append after recovery, got %s: %v", appendOutcome.operation, appendOutcome.err)
	}
	var pgErr *pgconn.PgError
	if errors.As(appendOutcome.err, &pgErr) && pgErr.Code == "40P01" {
		t.Fatalf("append deadlocked: %v", appendOutcome.err)
	}
	drainAgentRecovery(t, as)
	var status string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM agent_turn_projections WHERE id = $1
	`, claimed.turn.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "unknown" {
		t.Fatalf("recovered status=%s want unknown", status)
	}
}

func TestAppendEventsAndCheckpointDoNotDeadlock(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	appendProjectionLocked := make(chan struct{})
	releaseAppend := make(chan struct{})
	var appendOnce sync.Once
	const afterName = "agent_append_checkpoint_after_projection"
	if err := as.db.Callback().Query().After("gorm:query").Register(afterName, func(db *gorm.DB) {
		if db.Error != nil || db.Statement.Table != "agent_turn_projections" || db.Statement.Context.Value(journalLockContextKey{}) != "append" {
			return
		}
		appendOnce.Do(func() {
			close(appendProjectionLocked)
			<-releaseAppend
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		as.db.Callback().Query().Remove(afterName)
	})

	type result struct {
		operation string
		err       error
	}
	results := make(chan result, 2)
	appendCtx, cancelAppend := context.WithTimeout(context.WithValue(context.Background(), journalLockContextKey{}, "append"), 8*time.Second)
	defer cancelAppend()
	go func() {
		_, err := as.svc.AppendEvents(appendCtx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{{
			Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"live writer","attempt_id":"a","content_index":0}`),
		}})
		results <- result{operation: "append", err: err}
	}()
	select {
	case <-appendProjectionLocked:
	case <-time.After(3 * time.Second):
		t.Fatal("append did not acquire projection lock")
	}

	checkpointCtx, cancelCheckpoint := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancelCheckpoint()
	go func() {
		_, err := as.svc.AppendCheckpoint(checkpointCtx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, 1, "before_model_request", json.RawMessage(`{"model_request_id":"req-checkpoint-lock","provider":"openai","model":"test","execution_mode":"foreground","harness_hash":"`+testHarnessHash+`"}`))
		results <- result{operation: "checkpoint", err: err}
	}()

	select {
	case outcome := <-results:
		close(releaseAppend)
		var pgErr *pgconn.PgError
		if errors.As(outcome.err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("%s deadlocked: %v", outcome.operation, outcome.err)
		}
		t.Fatalf("expected checkpoint to wait behind the live append writer, got %s: %v", outcome.operation, outcome.err)
	case <-time.After(1500 * time.Millisecond):
	}
	close(releaseAppend)
	seen := map[string]error{}
	for range 2 {
		outcome := <-results
		var pgErr *pgconn.PgError
		if errors.As(outcome.err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("%s deadlocked: %v", outcome.operation, outcome.err)
		}
		if outcome.err != nil {
			t.Fatalf("%s failed: %v", outcome.operation, outcome.err)
		}
		seen[outcome.operation] = outcome.err
	}
	if _, ok := seen["append"]; !ok {
		t.Fatal("missing append result")
	}
	if _, ok := seen["checkpoint"]; !ok {
		t.Fatal("missing checkpoint result")
	}
}

func TestAppendEventsAndHeartbeatDoNotDeadlock(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	appendProjectionLocked := make(chan struct{})
	releaseAppend := make(chan struct{})
	var appendOnce sync.Once
	const afterName = "agent_append_heartbeat_after_projection"
	if err := as.db.Callback().Query().After("gorm:query").Register(afterName, func(db *gorm.DB) {
		if db.Error != nil || db.Statement.Table != "agent_turn_projections" || db.Statement.Context.Value(journalLockContextKey{}) != "append" {
			return
		}
		appendOnce.Do(func() {
			close(appendProjectionLocked)
			<-releaseAppend
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		as.db.Callback().Query().Remove(afterName)
	})

	type result struct {
		operation string
		err       error
	}
	results := make(chan result, 2)
	appendCtx, cancelAppend := context.WithTimeout(context.WithValue(context.Background(), journalLockContextKey{}, "append"), 8*time.Second)
	defer cancelAppend()
	go func() {
		_, err := as.svc.AppendEvents(appendCtx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{{
			Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"live writer","attempt_id":"a","content_index":0}`),
		}})
		results <- result{operation: "append", err: err}
	}()
	select {
	case <-appendProjectionLocked:
	case <-time.After(3 * time.Second):
		t.Fatal("append did not acquire projection lock")
	}

	heartbeatCtx, cancelHeartbeat := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancelHeartbeat()
	go func() {
		_, err := as.svc.HeartbeatExecution(heartbeatCtx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, "model")
		results <- result{operation: "heartbeat", err: err}
	}()

	select {
	case outcome := <-results:
		close(releaseAppend)
		var pgErr *pgconn.PgError
		if errors.As(outcome.err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("%s deadlocked: %v", outcome.operation, outcome.err)
		}
		t.Fatalf("expected heartbeat to wait behind the live append writer, got %s: %v", outcome.operation, outcome.err)
	case <-time.After(1500 * time.Millisecond):
	}
	close(releaseAppend)
	seen := map[string]error{}
	for range 2 {
		outcome := <-results
		var pgErr *pgconn.PgError
		if errors.As(outcome.err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("%s deadlocked: %v", outcome.operation, outcome.err)
		}
		if outcome.err != nil {
			t.Fatalf("%s failed: %v", outcome.operation, outcome.err)
		}
		seen[outcome.operation] = outcome.err
	}
	if _, ok := seen["append"]; !ok {
		t.Fatal("missing append result")
	}
	if _, ok := seen["heartbeat"]; !ok {
		t.Fatal("missing heartbeat result")
	}
}
