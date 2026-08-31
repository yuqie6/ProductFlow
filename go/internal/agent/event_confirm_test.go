package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

type eventConfirmContextKey struct{}

func TestConfirmEventsWaitsForConcurrentAppendCommit(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	input := EventAppendInput{
		Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
		Kind: "turn/start", Payload: json.RawMessage(`{"status":"running"}`),
	}
	appendLocked := make(chan struct{})
	releaseAppend := make(chan struct{})
	var once sync.Once
	callbackName := "agent_confirm_linearization_after_projection_lock"
	if err := as.db.Callback().Query().After("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Error != nil || db.Statement.Table != "agent_turn_projections" || db.Statement.Context.Value(eventConfirmContextKey{}) != "append" {
			return
		}
		once.Do(func() {
			close(appendLocked)
			<-releaseAppend
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { as.db.Callback().Query().Remove(callbackName) })

	appendResult := make(chan error, 1)
	go func() {
		_, err := as.svc.AppendEvents(
			context.WithValue(context.Background(), eventConfirmContextKey{}, "append"),
			claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken,
			[]EventAppendInput{input},
		)
		appendResult <- err
	}()
	select {
	case <-appendLocked:
	case <-time.After(2 * time.Second):
		t.Fatal("append did not acquire the projection lock")
	}

	confirmResult := make(chan struct {
		response EventConfirmationResponse
		err      error
	}, 1)
	go func() {
		response, err := as.svc.ConfirmEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, []EventAppendInput{input})
		confirmResult <- struct {
			response EventConfirmationResponse
			err      error
		}{response: response, err: err}
	}()
	select {
	case result := <-confirmResult:
		t.Fatalf("confirm returned before the concurrent append committed: %+v", result)
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseAppend)
	if err := <-appendResult; err != nil {
		t.Fatal(err)
	}
	result := <-confirmResult
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.response.Status != "confirmed" || result.response.ConfirmedThrough != 1 || len(result.response.Items) != 1 {
		t.Fatalf("confirmation=%+v", result.response)
	}
}

func TestConfirmEventsReadsActiveAndReleasedJournalWithoutMutation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := createClaimedJournalTurn(t, as)
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	inputs := []EventAppendInput{
		{
			Sequence: 1, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "turn/start", Payload: json.RawMessage(`{"status":"running"}`), CreatedAt: createdAt,
		},
		{
			Sequence: 2, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"durable","attempt_id":"a","step_id":"s","content_index":0}`), CreatedAt: createdAt.Add(time.Millisecond),
		},
		{
			Sequence: 3, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
			Kind: "turn/end", Payload: json.RawMessage(`{"status":"succeeded","reason":"completed"}`), CreatedAt: createdAt.Add(2 * time.Millisecond),
		},
	}
	receipts, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, inputs)
	if err != nil {
		t.Fatal(err)
	}
	var terminalExecution schema.AgentTurnExecutions
	if err := as.db.Where("id = ?", claimed.lease.ExecutionID).Take(&terminalExecution).Error; err != nil {
		t.Fatal(err)
	}
	if terminalExecution.Phase != "terminal" || terminalExecution.OwnerID != nil || terminalExecution.LeaseToken != nil || terminalExecution.LeaseExpiresAt != nil || terminalExecution.ReleasedAt == nil {
		t.Fatalf("terminal event did not finalize execution: %+v", terminalExecution)
	}
	requests := eventConfirmRequests(inputs)
	path := "/api/internal/v1/agent-conversations/" + claimed.conversationID + "/turn-executions/" + claimed.lease.ExecutionID + "/events/confirm"
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	var projectionBefore schema.AgentTurnProjections
	if err := as.db.Where("id = ?", claimed.turn.ID).Take(&projectionBefore).Error; err != nil {
		t.Fatal(err)
	}
	var eventsBefore []schema.AgentTurnEvents
	if err := as.db.Where("turn_projection_id = ?", claimed.turn.ID).Order("sequence").Find(&eventsBefore).Error; err != nil {
		t.Fatal(err)
	}

	activeResp := as.doJSONAuth(t, http.MethodPost, path, map[string]any{"events": requests}, auth)
	as.mustStatus(t, activeResp, http.StatusOK)
	var active EventConfirmationResponse
	as.decode(t, activeResp, &active)
	assertConfirmedEvents(t, active, receipts)
	if active.PersistedThrough != 3 || active.Terminal == nil || active.Terminal.Sequence != 3 || string(active.Terminal.Payload) != `{"status":"succeeded","reason":"completed"}` {
		t.Fatalf("terminal metadata=%+v", active)
	}
	empty, err := as.svc.ConfirmEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.ConfirmedThrough != 0 || empty.PersistedThrough != 3 || len(empty.Items) != 0 || empty.Terminal == nil || empty.Terminal.Sequence != 3 {
		t.Fatalf("empty confirmation=%+v", empty)
	}

	if _, err := as.svc.ReleaseExecution(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, "terminal"); err != nil {
		t.Fatal(err)
	}

	releasedResp := as.doJSONAuth(t, http.MethodPost, path, map[string]any{"events": requests}, auth)
	as.mustStatus(t, releasedResp, http.StatusOK)
	var released EventConfirmationResponse
	as.decode(t, releasedResp, &released)
	assertConfirmedEvents(t, released, receipts)

	missingRequests := append([]appendEventRequest{}, requests...)
	missingRequests = append(missingRequests, appendEventRequest{
		Sequence: 4, SchemaVersion: 1, RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
		Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"not committed","attempt_id":"a","step_id":"s","content_index":1}`), CreatedAt: createdAt.Add(3 * time.Millisecond),
	})
	missingResp := as.doJSONAuth(t, http.MethodPost, path, map[string]any{"events": missingRequests}, auth)
	as.mustStatus(t, missingResp, http.StatusOK)
	var missing EventConfirmationResponse
	as.decode(t, missingResp, &missing)
	if missing.Status != "missing" || missing.ConfirmedThrough != 3 || len(missing.Items) != 3 {
		t.Fatalf("missing response=%+v", missing)
	}
	for index, receipt := range receipts {
		if missing.Items[index].ID != receipt.ID || !missing.Items[index].CreatedAt.Equal(receipt.CreatedAt) {
			t.Fatalf("missing receipt[%d]=%+v receipt=%+v", index, missing.Items[index], receipt)
		}
	}

	conflicting := append([]appendEventRequest{}, requests[:2]...)
	conflicting[1].Payload = json.RawMessage(`{"delta":"different","attempt_id":"a","step_id":"s","content_index":0}`)
	conflictResp := as.doJSONAuth(t, http.MethodPost, path, map[string]any{"events": conflicting}, auth)
	as.mustStatus(t, conflictResp, http.StatusConflict)
	var conflictBody map[string]any
	as.decode(t, conflictResp, &conflictBody)
	if conflictBody["detail"] != "Agent event sequence 已绑定不同内容" || conflictBody["code"] != apperr.CodeEventSequenceConflict {
		t.Fatalf("conflict body=%+v", conflictBody)
	}

	strictResp := as.doJSONAuth(t, http.MethodPost, path, map[string]any{
		"events": requests, "owner_id": "worker-1",
	}, auth)
	as.mustStatus(t, strictResp, http.StatusBadRequest)
	strictResp.Body.Close()

	var projectionAfter schema.AgentTurnProjections
	if err := as.db.Where("id = ?", claimed.turn.ID).Take(&projectionAfter).Error; err != nil {
		t.Fatal(err)
	}
	var eventsAfter []schema.AgentTurnEvents
	if err := as.db.Where("turn_projection_id = ?", claimed.turn.ID).Order("sequence").Find(&eventsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projectionAfter, projectionBefore) {
		t.Fatalf("confirmation changed projection: before=%+v after=%+v", projectionBefore, projectionAfter)
	}
	if !reflect.DeepEqual(eventsAfter, eventsBefore) {
		t.Fatalf("confirmation changed events: before=%+v after=%+v", eventsBefore, eventsAfter)
	}
}

func eventConfirmRequests(inputs []EventAppendInput) []appendEventRequest {
	out := make([]appendEventRequest, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, appendEventRequest{
			Sequence: input.Sequence, SchemaVersion: input.SchemaVersion, RunID: input.RunID,
			TurnID: input.TurnID, Kind: input.Kind, Ignorable: input.Ignorable,
			Payload: input.Payload, CreatedAt: input.CreatedAt,
		})
	}
	return out
}

func assertConfirmedEvents(t *testing.T, confirmation EventConfirmationResponse, receipts []EventReceipt) {
	t.Helper()
	if confirmation.Status != "confirmed" || confirmation.ConfirmedThrough != receipts[len(receipts)-1].Sequence || len(confirmation.Items) != len(receipts) {
		t.Fatalf("confirmation=%+v receipts=%+v", confirmation, receipts)
	}
	for index, receipt := range receipts {
		confirmed := confirmation.Items[index]
		if confirmed.ID != receipt.ID || confirmed.Sequence != receipt.Sequence || confirmed.Kind != receipt.Kind || !confirmed.CreatedAt.Equal(receipt.CreatedAt) {
			t.Fatalf("confirmed[%d]=%+v receipt=%+v", index, confirmed, receipt)
		}
	}
}
