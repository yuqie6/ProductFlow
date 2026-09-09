package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/riverqueue/river"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"gorm.io/gorm"
)

func TestSyncDoesNotStartSettledOrParkedTurn(t *testing.T) {
	for _, entry := range []string{"worker", "bind"} {
		for _, status := range []string{"succeeded", "failed", "canceled", "unknown", "awaiting_confirmation", "requires_input", "resume_required"} {
			t.Run(entry+"/"+status, func(t *testing.T) {
				gw := &questionGateway{startErr: errors.New("start unavailable")}
				as := newAgentServer(t, gw, "")
				conv, turnID := createUnboundStartGuardTurn(t, as)
				resume := status == "resume_required"
				storedStatus := status
				if resume {
					storedStatus = "running"
				}
				if _, err := as.pool.Exec(context.Background(), `UPDATE agent_turn_projections SET status=$2,resume_required=$3 WHERE id=$1`, turnID, storedStatus, resume); err != nil {
					t.Fatal(err)
				}
				gw.startErr = nil
				gw.startCount = 0
				var err error
				if entry == "worker" {
					err = as.svc.SyncTurn(context.Background(), turnID)
				} else {
					_, err = as.svc.bindGatewayTurn(context.Background(), nil, conv, turnID, true)
				}
				if err != nil || gw.startCount != 0 {
					t.Fatalf("status=%s starts=%d err=%v", status, gw.startCount, err)
				}
				var gotStatus string
				var harness *string
				if err := as.pool.QueryRow(context.Background(), `SELECT status,harness_turn_id FROM agent_turn_projections WHERE id=$1`, turnID).Scan(&gotStatus, &harness); err != nil || gotStatus != storedStatus || harness != nil {
					t.Fatalf("status=%s harness=%v err=%v", gotStatus, harness, err)
				}
			})
		}
	}
}

func TestSyncRechecksTerminalBeforeBinding(t *testing.T) {
	gw := &questionGateway{startErr: errors.New("start unavailable")}
	as := newAgentServer(t, gw, "")
	_, turnID := createUnboundStartGuardTurn(t, as)
	gw.startErr = nil
	gw.startCount = 0
	settled := false
	const callback = "test:sync_settled_after_read"
	if err := as.db.Callback().Query().After("gorm:query").Register(callback, func(q *gorm.DB) {
		if !settled && q.Statement.Table == "agent_turn_projections" && q.Error == nil {
			if _, err := as.pool.Exec(context.Background(), `UPDATE agent_turn_projections SET status='canceled' WHERE id=$1`, turnID); err != nil {
				t.Fatal(err)
			}
			settled = true
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer as.db.Callback().Query().Remove(callback)
	if err := as.svc.SyncTurn(context.Background(), turnID); err != nil || !settled || gw.startCount != 0 {
		t.Fatalf("settled=%v starts=%d err=%v", settled, gw.startCount, err)
	}
}

func TestSyncStillStartsUnboundActiveTurn(t *testing.T) {
	for _, status := range []string{"queued", "running", "cancel_requested"} {
		t.Run(status, func(t *testing.T) {
			gw := &questionGateway{startErr: errors.New("start unavailable")}
			as := newAgentServer(t, gw, "")
			_, turnID := createUnboundStartGuardTurn(t, as)
			if _, err := as.pool.Exec(context.Background(), `UPDATE agent_turn_projections SET status=$2 WHERE id=$1`, turnID, status); err != nil {
				t.Fatal(err)
			}
			gw.startErr = nil
			gw.startCount = 0
			if err := as.svc.SyncTurn(context.Background(), turnID); err != nil && !errors.Is(err, queue.ErrLater) {
				t.Fatal(err)
			}
			if gw.startCount != 1 {
				t.Fatalf("status=%s starts=%d, want 1", status, gw.startCount)
			}
		})
	}
}

func TestLateSyncEnvelopeConsumesWithoutRestartingCanceledTurn(t *testing.T) {
	gw := &questionGateway{startErr: errors.New("start unavailable")}
	as := newAgentServer(t, gw, "")
	_, turnID := createUnboundStartGuardTurn(t, as)
	ctx := context.Background()
	if _, err := as.pool.Exec(ctx, `UPDATE agent_turn_projections SET status='canceled' WHERE id=$1`, turnID); err != nil {
		t.Fatal(err)
	}
	var argsJSON []byte
	if err := as.pool.QueryRow(ctx, `SELECT args FROM river_job WHERE args ->> 'actor'=$1 AND args ->> 'aggregate_id'=$2`, queue.ActorAgentTurnSync, turnID).Scan(&argsJSON); err != nil {
		t.Fatal(err)
	}
	var args queue.TaskArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		t.Fatal(err)
	}
	gw.startErr = nil
	gw.startCount = 0
	worker := queue.NewWorker(map[string]queue.ActorFunc{queue.ActorAgentTurnSync: as.svc.SyncTurn})
	if err := worker.Work(ctx, &river.Job[queue.TaskArgs]{Args: args}); err != nil {
		t.Fatal(err)
	}
	if gw.startCount != 0 {
		t.Fatalf("canceled Turn restarted %d times", gw.startCount)
	}

}

func createUnboundStartGuardTurn(t *testing.T, as *agentServer) (string, string) {
	t.Helper()
	resp := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, resp, http.StatusCreated)
	var session SessionResponse
	as.decode(t, resp, &session)
	conv := session.Conversations[0].ConversationID
	resp = as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+conv+"/turns", map[string]any{
		"input_text": "start guard", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, resp, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, resp, &submitted)
	if submitted.Turn.HarnessTurnID != nil {
		t.Fatal("fixture must be unbound")
	}
	return conv, submitted.Turn.ID
}
