package agent

import (
	"context"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestRiverInsertFailureRollsBackTurnBeforeGateway(t *testing.T) {
	gw := &questionGateway{}
	as := newAgentServer(t, gw, "")
	response := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, response, http.StatusCreated)
	var session SessionResponse
	as.decode(t, response, &session)
	conversation := session.Conversations[0].ConversationID
	if err := as.db.Exec("ALTER TABLE river_job ADD CONSTRAINT test_turn_acceptance CHECK (args->>'actor' <> 'run_agent_turn_sync') NOT VALID").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := as.db.Exec("ALTER TABLE river_job DROP CONSTRAINT IF EXISTS test_turn_acceptance").Error; err != nil {
			t.Error(err)
		}
	})
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	key := clockid.New()
	_, err := as.svc.SubmitTurn(ctx, nil, conversation, startTurnInput{InputText: "atomic acceptance", IdempotencyKey: key})
	if err == nil {
		t.Fatal("accepted without a durable job")
	}
	var count int64
	if err := as.db.Table("agent_turn_projections").Where("conversation_id=? AND idempotency_key=?", conversation, key).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 || gw.startCount != 0 {
		t.Fatalf("projections=%d gateway starts=%d", count, gw.startCount)
	}
}
