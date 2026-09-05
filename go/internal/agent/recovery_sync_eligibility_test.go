package agent

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"gorm.io/gorm"
)

func TestRestagePendingMatchesWorkerEligibility(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	ctx := context.Background()
	resp := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, resp, http.StatusCreated)
	var session SessionResponse
	as.decode(t, resp, &session)
	conv := session.Conversations[0].ConversationID
	var rows []turnRow
	var ids []string
	t.Cleanup(func() {
		if _, err := as.pool.Exec(ctx, `UPDATE agent_turn_projections SET status='succeeded' WHERE id=ANY($1)`, ids); err != nil {
			t.Error(err)
		}
	})
	want := 0
	for _, status := range []string{"queued", "running", "cancel_requested", "requires_input", "awaiting_confirmation", "succeeded", "failed", "canceled", "unknown"} {
		for _, bound := range []bool{false, true} {
			for _, resume := range []bool{false, true} {
				for _, answered := range []bool{false, true} {
					row := turnRow{ID: clockid.New(), Status: status, ResumeRequired: resume}
					if bound {
						row.HarnessTurnID = ptr("h-" + row.ID)
					}
					var answer any
					if answered {
						answer = `{"text":"answer"}`
						row.QuestionAnswerJSON = []byte(answer.(string))
					}
					if _, err := as.pool.Exec(ctx, `INSERT INTO agent_turn_projections
					 (id,conversation_id,idempotency_key,request_hash,input_text,input_asset_ids_json,status,resume_required,tool_steps_json,harness_turn_id,question_answer_json,created_at,updated_at)
					 VALUES ($1,$2,$1,$3,'fixture','[]',$4,$5,'[]',$6,$7,NOW(),NOW())`, row.ID, conv, strings.Repeat("a", 64), status, resume, row.HarnessTurnID, answer); err != nil {
						t.Fatal(err)
					}
					rows = append(rows, row)
					ids = append(ids, row.ID)
					if turnNeedsSync(row) {
						want++
					}
				}
			}
		}
	}
	n, pending, more, err := restagePendingTurns(ctx, as.svc, 1000)
	if err != nil || n != want || pending != want || more {
		t.Fatalf("cases=%d enqueued=%d pending=%d more=%v err=%v, want %d", len(rows), n, pending, more, err, want)
	}
	for _, row := range rows {
		var count int
		if err := as.pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_dispatches WHERE actor_name=$1 AND aggregate_id=$2`, queue.ActorAgentTurnSync, row.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if (count == 1) != turnNeedsSync(row) || count > 1 {
			t.Fatalf("status=%s bound=%v resume=%v answered=%v dispatches=%d", row.Status, row.HarnessTurnID != nil, row.ResumeRequired, len(row.QuestionAnswerJSON) > 0, count)
		}
	}
}

func TestRestageRechecksBindingAfterDiscovery(t *testing.T) {
	as := newAgentServer(t, &questionGateway{startErr: errors.New("start unavailable")}, "")
	drainAgentRecovery(t, as)
	ctx := context.Background()
	resp := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, resp, http.StatusCreated)
	var session SessionResponse
	as.decode(t, resp, &session)
	resp = as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+session.Conversations[0].ConversationID+"/turns", map[string]any{
		"input_text": "binding race", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, resp, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, resp, &submitted)
	if submitted.Turn.HarnessTurnID != nil {
		t.Fatal("fixture already bound")
	}
	if _, err := as.pool.Exec(ctx, `UPDATE async_dispatches SET status='consumed' WHERE actor_name=$1 AND aggregate_id=$2`, queue.ActorAgentTurnSync, submitted.Turn.ID); err != nil {
		t.Fatal(err)
	}
	bound := false
	const callback = "test:restage_binding_after_discovery"
	if err := as.db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
		if !bound && q.Statement.Table == "agent_turn_projections" && len(q.Statement.Selects) > 1 {
			if _, err := as.pool.Exec(ctx, `UPDATE agent_turn_projections SET harness_turn_id=$2 WHERE id=$1`, submitted.Turn.ID, "h-"+submitted.Turn.ID); err != nil {
				t.Fatal(err)
			}
			bound = true
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer as.db.Callback().Query().Remove(callback)
	n, pending, more, err := restagePendingTurns(ctx, as.svc, 25)
	if err != nil || n != 0 || pending != 1 || more || !bound {
		t.Fatalf("enqueued=%d pending=%d more=%v bound=%v err=%v", n, pending, more, bound, err)
	}
	var status string
	if err := as.pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE actor_name=$1 AND aggregate_id=$2`, queue.ActorAgentTurnSync, submitted.Turn.ID).Scan(&status); err != nil || status != queue.StatusConsumed {
		t.Fatalf("dispatch=%s err=%v", status, err)
	}
}
