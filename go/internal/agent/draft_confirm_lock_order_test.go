package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestConfirmDraftAndAppendTerminalDoNotDeadlock(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID

	created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "整理库", "goal": "整理全局素材",
		"conversation_id": convID,
	})
	as.mustStatus(t, created, http.StatusCreated)
	var task TaskResponse
	as.decode(t, created, &task)

	key := clockid.New()
	response := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "整理素材", "idempotency_key": key, "task_id": task.ID,
	})
	as.mustStatus(t, response, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, response, &submitted)
	if submitted.Turn.HarnessTurnID == nil {
		t.Fatal("missing harness turn ID")
	}
	lease, err := as.svc.ClaimExecution(context.Background(), convID, &task.ID, key, *submitted.Turn.HarnessTurnID, "worker-1")
	if err != nil {
		t.Fatal(err)
	}

	payload := libraryRenamePayload(t, as)
	var revID string
	err = tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		id, err := as.svc.Library.AppendOrganizationDraftRevisionTx(context.Background(), pgxTx, convID, payload, "", "")
		revID = id
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET library_organization_draft_revision_id = $1, status = 'awaiting_confirmation', updated_at = NOW()
		WHERE id = $2
	`, revID, submitted.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_tasks SET status = 'awaiting_confirmation', waiting_reason = 'awaiting_confirmation', current_turn_id = $2 WHERE id = $1
	`, task.ID, submitted.Turn.ID); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		op  string
		err error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := as.svc.AppendEvents(context.Background(), convID, lease.ExecutionID, "worker-1", lease.LeaseToken, []EventAppendInput{{
			Sequence: 1, SchemaVersion: 1, RunID: submitted.Turn.HarnessRunID, TurnID: *submitted.Turn.HarnessTurnID,
			Kind: "turn/end", Payload: json.RawMessage(`{"status":"succeeded","reason":"completed"}`),
		}})
		results <- outcome{op: "append", err: err}
	}()
	go func() {
		defer wg.Done()
		_, err := as.svc.ConfirmLibraryDraftHTTP(auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db)), convID, 1, clockid.New())
		results <- outcome{op: "confirm", err: err}
	}()
	wg.Wait()
	close(results)
	for item := range results {
		if postgresDeadlock(item.err) {
			t.Fatalf("%s deadlocked: %v", item.op, item.err)
		}
	}
}
