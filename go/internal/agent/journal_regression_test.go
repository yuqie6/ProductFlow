package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"gorm.io/gorm"
)

type claimedJournalTurn struct {
	conversationID string
	idempotencyKey string
	turn           TurnResponse
	lease          ExecutionLeaseResponse
}

func createClaimedJournalTurn(t *testing.T, as *agentServer) claimedJournalTurn {
	t.Helper()
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var created SessionResponse
	as.decode(t, session, &created)
	conversationID := created.Conversations[0].ConversationID
	key := clockid.New()
	response := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+conversationID+"/turns", map[string]any{
		"input_text": "写入 durable journal", "idempotency_key": key,
	})
	as.mustStatus(t, response, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, response, &submitted)
	if submitted.Turn.HarnessTurnID == nil {
		t.Fatal("missing harness turn ID")
	}
	lease, err := as.svc.ClaimExecution(context.Background(), conversationID, nil, key, *submitted.Turn.HarnessTurnID, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	return claimedJournalTurn{conversationID: conversationID, idempotencyKey: key, turn: submitted.Turn, lease: lease}
}

func TestInitialExecutionClaimRecordsHeartbeatAtClaimTime(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	var heartbeat, expires time.Time
	if err := as.pool.QueryRow(context.Background(), `
		SELECT last_heartbeat_at, lease_expires_at FROM agent_turn_executions WHERE id = $1
	`, claimed.lease.ExecutionID).Scan(&heartbeat, &expires); err != nil {
		t.Fatal(err)
	}
	remaining := expires.Sub(heartbeat)
	if remaining < (leaseSeconds-2)*time.Second || remaining > (leaseSeconds+2)*time.Second {
		t.Fatalf("lease expiry minus heartbeat = %s, want about %ds", remaining, leaseSeconds)
	}
}

func TestJournalBatchExactReplayReturnsOriginalReceiptWithoutDuplicateRows(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	batch := []EventAppendInput{
		{
			Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"durable","attempt_id":"a","content_index":0}`), CreatedAt: createdAt,
		},
		{
			Sequence: 2, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "assistant/message", Payload: json.RawMessage(`{"attempt_id":"a","reason":"stop","usage":{"input":2,"output":1,"total_tokens":3}}`), CreatedAt: createdAt,
		},
	}
	first, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, batch)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(replayed) != 2 {
		t.Fatalf("receipt lengths first=%d replayed=%d", len(first), len(replayed))
	}
	for index := range first {
		if replayed[index].ID != first[index].ID || !replayed[index].CreatedAt.Equal(first[index].CreatedAt) {
			t.Fatalf("receipt[%d] changed: first=%+v replayed=%+v", index, first[index], replayed[index])
		}
	}
	var count int64
	if err := as.db.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", claimed.turn.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("event count=%d, want 2", count)
	}
}

func TestJournalBatchRejectsEventsAfterTerminalWithoutMutation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	claimed := createClaimedJournalTurn(t, as)
	_, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{
		{
			Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "turn/end", Payload: json.RawMessage(`{"status":"succeeded","reason":"completed"}`),
		},
		{
			Sequence: 2, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"after terminal","attempt_id":"a","step_id":"s","content_index":0}`),
		},
	})
	if err == nil {
		t.Fatal("expected events after turn/end to be rejected")
	}
	var count int64
	if err := as.db.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", claimed.turn.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("event count=%d, want 0", count)
	}
}

func TestTurnEndStatusReasonContract(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		valid   bool
	}{
		{name: "succeeded", payload: `{"status":"succeeded","reason":"completed"}`, valid: true},
		{name: "failed", payload: `{"status":"failed","reason":"failed"}`, valid: true},
		{name: "canceled", payload: `{"status":"canceled","reason":"canceled"}`, valid: true},
		{name: "unknown", payload: `{"status":"unknown","reason":"unknown"}`, valid: true},
		{name: "awaiting confirmation", payload: `{"status":"awaiting_confirmation","reason":"awaiting_confirmation"}`, valid: true},
		{name: "mismatched", payload: `{"status":"succeeded","reason":"failed"}`},
		{name: "missing reason", payload: `{"status":"failed"}`},
		{name: "noncanonical succeeded reason", payload: `{"status":"succeeded","reason":"succeeded"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseTerminalEventPayload(json.RawMessage(test.payload))
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRejectedTurnEndCannotSplitJournalAndProjection(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	_, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{{
		Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
		Kind: "turn/end", Payload: json.RawMessage(`{"status":"succeeded","reason":"failed"}`),
	}})
	if err == nil {
		t.Fatal("expected mismatched terminal event to be rejected")
	}
	var eventCount int64
	if err := as.db.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", claimed.turn.ID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	var projection schema.AgentTurnProjections
	if err := as.db.Where("id = ?", claimed.turn.ID).Take(&projection).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 || projection.Status == "succeeded" {
		t.Fatalf("event_count=%d projection_status=%s", eventCount, projection.Status)
	}
}

func TestTerminalProjectionRebuildsCompleteOutputAcrossJournalBatches(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	base := func(sequence int, kind, payload string) EventAppendInput {
		return EventAppendInput{
			Sequence: sequence, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: kind, Payload: json.RawMessage(payload), CreatedAt: time.Now().UTC(),
		}
	}
	if _, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{
		base(1, "text.chunk", `{"delta":"前半","attempt_id":"a","content_index":0}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{
		base(2, "text.chunk", `{"delta":"完整","attempt_id":"a","content_index":1}`),
		base(3, "text.chunk", `{"delta":"，后半","attempt_id":"a","content_index":2}`),
		base(4, "assistant/message", `{"attempt_id":"a","reason":"stop","usage":{"input":10,"output":3,"total_tokens":13}}`),
		base(5, "turn/end", `{"status":"succeeded","reason":"completed"}`),
	}); err != nil {
		t.Fatal(err)
	}
	var projection schema.AgentTurnProjections
	if err := as.db.Where("id = ?", claimed.turn.ID).Take(&projection).Error; err != nil {
		t.Fatal(err)
	}
	if projection.OutputText == nil || *projection.OutputText != "前半完整，后半" {
		t.Fatalf("output_text=%v", projection.OutputText)
	}
}

func TestExpiredExecutionRecoveryKeepsChunkOutputOutOfTerminalPayload(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	if _, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{{
		Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
		Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"中断前正文","attempt_id":"a","content_index":0}`),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections SET status = 'running', updated_at = NOW() WHERE id = $1
	`, claimed.turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions SET phase = 'model', lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW() WHERE id = $1
	`, claimed.lease.ExecutionID); err != nil {
		t.Fatal(err)
	}
	summary, err := RecoverUnfinishedTurns(context.Background(), as.svc)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownExecutions != 1 {
		t.Fatalf("recovery summary=%+v", summary)
	}
	var projection schema.AgentTurnProjections
	if err := as.db.Where("id = ?", claimed.turn.ID).Take(&projection).Error; err != nil {
		t.Fatal(err)
	}
	if projection.Status != "unknown" || projection.OutputText == nil || *projection.OutputText != "中断前正文" {
		t.Fatalf("projection status=%s output=%v", projection.Status, projection.OutputText)
	}
	var events []schema.AgentTurnEvents
	if err := as.db.Where("turn_projection_id = ?", claimed.turn.ID).Order("sequence").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[1].Kind != "assistant/message" || events[2].Kind != "turn/end" {
		t.Fatalf("events=%+v", events)
	}
	var messagePayload, terminalPayload map[string]any
	if err := json.Unmarshal([]byte(events[1].PayloadJSON), &messagePayload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(events[2].PayloadJSON), &terminalPayload); err != nil {
		t.Fatal(err)
	}
	if _, exists := messagePayload["text"]; exists {
		t.Fatalf("recovery assistant message contains text: %s", events[1].PayloadJSON)
	}
	if _, exists := terminalPayload["output"]; exists {
		t.Fatalf("recovery terminal contains output: %s", events[2].PayloadJSON)
	}
}

type journalLockContextKey struct{}

func TestAppendTerminalAndClaimUseProjectionThenExecutionLockOrder(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	appendExecutionLocked := make(chan struct{})
	claimReachedExecution := make(chan struct{})
	releaseAppend := make(chan struct{})
	var appendOnce, claimOnce sync.Once
	const beforeName = "agent_lock_order_before_query"
	const afterName = "agent_lock_order_after_query"
	if err := as.db.Callback().Query().Before("gorm:query").Register(beforeName, func(db *gorm.DB) {
		if db.Statement.Table != "agent_turn_executions" || db.Statement.Context.Value(journalLockContextKey{}) != "claim" {
			return
		}
		claimOnce.Do(func() { close(claimReachedExecution) })
	}); err != nil {
		t.Fatal(err)
	}
	if err := as.db.Callback().Query().After("gorm:query").Register(afterName, func(db *gorm.DB) {
		if db.Error != nil || db.Statement.Table != "agent_turn_executions" || db.Statement.Context.Value(journalLockContextKey{}) != "append" {
			return
		}
		appendOnce.Do(func() {
			close(appendExecutionLocked)
			<-releaseAppend
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		as.db.Callback().Query().Remove(beforeName)
		as.db.Callback().Query().Remove(afterName)
	})

	type result struct {
		operation string
		err       error
	}
	results := make(chan result, 2)
	appendCtx, cancelAppend := context.WithTimeout(context.WithValue(context.Background(), journalLockContextKey{}, "append"), 5*time.Second)
	defer cancelAppend()
	go func() {
		_, err := as.svc.AppendEvents(appendCtx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{{
			Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "turn/end", Payload: json.RawMessage(`{"status":"succeeded","reason":"completed"}`),
		}})
		results <- result{operation: "append", err: err}
	}()
	select {
	case <-appendExecutionLocked:
	case <-time.After(2 * time.Second):
		t.Fatal("append did not acquire execution lock")
	}
	claimCtx, cancelClaim := context.WithTimeout(context.WithValue(context.Background(), journalLockContextKey{}, "claim"), 5*time.Second)
	defer cancelClaim()
	go func() {
		_, err := as.svc.ClaimExecution(claimCtx, claimed.conversationID, nil, claimed.idempotencyKey, *claimed.turn.HarnessTurnID, "worker-1")
		results <- result{operation: "claim", err: err}
	}()
	reversed := false
	select {
	case <-claimReachedExecution:
		reversed = true
	case <-time.After(200 * time.Millisecond):
	}
	close(releaseAppend)
	for range 2 {
		outcome := <-results
		var pgErr *pgconn.PgError
		if errors.As(outcome.err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("%s deadlocked: %v", outcome.operation, outcome.err)
		}
		if outcome.operation == "append" && outcome.err != nil {
			t.Fatalf("append failed: %v", outcome.err)
		}
	}
	if reversed {
		t.Fatal("claim reached the execution lock while append held it; projection lock order is reversed")
	}
}

func TestAgentSSESubscriptionsShareOneListenerConnection(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	baseline := as.pool.Stat().AcquiredConns()
	for i := 0; i < 12; i++ {
		channel := notify.ChannelTurn
		if i%2 == 1 {
			channel = notify.ChannelControl
		}
		_, unsubscribe := subscribeAgentNotifications(as.pool, channel)
		t.Cleanup(unsubscribe)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		acquired := as.pool.Stat().AcquiredConns()
		if acquired > baseline+1 {
			t.Fatalf("shared SSE notifications acquired %d connections from baseline %d", acquired, baseline)
		}
		if acquired == baseline+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("shared notification listener did not acquire its single connection; baseline=%d current=%d", baseline, as.pool.Stat().AcquiredConns())
}
