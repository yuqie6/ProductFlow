package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestQueuedRecoverySkipsLockedConversationPrefix(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("locked_after_discovery_%v", late), func(t *testing.T) {
			testQueuedRecoveryLockedConversation(t, late)
		})
	}
}

func TestQueuedRecoveryReportsCancellationAfterDiscovery(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	resp := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, resp, http.StatusCreated)
	var session SessionResponse
	as.decode(t, resp, &session)
	conv := session.Conversations[0].ConversationID
	if _, err := as.svc.CreateTask(context.Background(), session.ID, "cancel-recovery", "recover queued task", &conv); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const callback = "test:queued_recovery_cancel"
	if err := as.db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
		if q.Statement.Table == "agent_conversations" {
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer as.db.Callback().Query().Remove(callback)
	if n, _, err := recoverQueuedTaskTurns(ctx, as.svc, 25); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery count=%d err=%v", n, err)
	}
}

func testQueuedRecoveryLockedConversation(t *testing.T, late bool) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	ctx := context.Background()
	var sessions [2]SessionResponse
	for i := range sessions {
		resp := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
		as.mustStatus(t, resp, http.StatusCreated)
		as.decode(t, resp, &sessions[i])
	}
	var taskIDs []string
	for i := range 26 {
		session := sessions[0]
		if i == 25 {
			session = sessions[1]
		}
		conv := session.Conversations[0].ConversationID
		task, err := as.svc.CreateTask(ctx, session.ID, fmt.Sprintf("lock-prefix-%d", i), "recover queued task", &conv)
		if err != nil {
			t.Fatal(err)
		}
		taskIDs = append(taskIDs, task.ID)
		if _, err := as.pool.Exec(ctx, `UPDATE agent_tasks SET created_at=NOW()-INTERVAL '1 hour'+$2*INTERVAL '1 second' WHERE id=$1`, task.ID, i); err != nil {
			t.Fatal(err)
		}
	}
	locked, err := as.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(ctx)
	acquired := false
	lock := func() {
		if _, err := locked.Exec(ctx, `SELECT id FROM agent_conversations WHERE id=$1 FOR UPDATE`, sessions[0].Conversations[0].ConversationID); err != nil {
			t.Fatal(err)
		}
		acquired = true
	}
	if late {
		const callback = "test:queued_recovery_late_lock"
		if err := as.db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
			if !acquired && q.Statement.Table == "agent_conversations" {
				lock()
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer as.db.Callback().Query().Remove(callback)
	} else {
		lock()
	}
	for round := range 3 {
		runCtx, cancel := context.WithTimeout(ctx, time.Second)
		n, _, err := recoverQueuedTaskTurns(runCtx, as.svc, 25)
		cancel()
		want := 0
		if (!late && round == 0) || (late && round == 1) {
			want = 1
		}
		if err != nil || n != want {
			t.Fatalf("round %d recovered=%d err=%v, want %d", round, n, err, want)
		}
	}
	if !acquired {
		t.Fatal("conversation lock was not acquired")
	}
	var count int
	if err := as.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_turn_projections WHERE task_id=ANY($1)`, taskIDs[:25]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("locked prefix projections=%d err=%v", count, err)
	}
	if err := locked.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n, more, err := recoverQueuedTaskTurns(ctx, as.svc, 25); err != nil || n != 25 || more {
		t.Fatalf("after unlock recovered=%d more=%v err=%v", n, more, err)
	}
	if n, _, err := recoverQueuedTaskTurns(ctx, as.svc, 25); err != nil || n != 0 {
		t.Fatalf("replay recovered=%d err=%v", n, err)
	}
	for _, id := range taskIDs {
		var current int
		if err := as.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE t.current_turn_id=p.id)
	 FROM agent_turn_projections p JOIN agent_tasks t ON p.task_id=t.id WHERE t.id=$1`, id).Scan(&count, &current); err != nil || count != 1 || current != 1 {
			t.Fatalf("task=%s projections=%d current=%d err=%v", id, count, current, err)
		}
	}
}
