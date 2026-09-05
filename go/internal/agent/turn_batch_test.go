package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestTurnBatchPreservesScopeOrderAndHarness(t *testing.T) {
	pool, db := testdb.Open(t)
	ctx := context.Background()
	prefix := clockid.New()[:20]
	productID, sessionID := prefix+"-p", prefix+"-s"
	globalID, convID, taskID := prefix+"-g", prefix+"-c", prefix+"-task"
	taskHarness := prefix + "-harness"
	for _, seed := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO products (id,name,created_at,updated_at) VALUES ($1,'fixture',NOW(),NOW())`, []any{productID}},
		{`INSERT INTO agent_sessions (id,title,status,created_at,updated_at,activity_at) VALUES ($1,'fixture','active',NOW(),NOW(),NOW())`, []any{sessionID}},
		{`INSERT INTO agent_conversations (id,session_id,product_id,harness_run_id,status,scope_type,created_at,updated_at)
		 VALUES ($1,$3,NULL,$1,'collecting','global',NOW(),NOW()), ($2,$3,$4,$2,'collecting','product_workflow',NOW(),NOW())`, []any{globalID, convID, sessionID, productID}},
		{`INSERT INTO agent_tasks (id,session_id,conversation_id,product_id,harness_run_id,title,goal,status,created_at,updated_at)
		 VALUES ($1,$2,$3,$4,$5,'fixture','fixture','queued',NOW(),NOW())`, []any{taskID, sessionID, convID, productID, taskHarness}},
	} {
		if _, err := pool.Exec(ctx, seed.sql, seed.args...); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{prefix + "-2", prefix + "-1"}
	for i, id := range append(append([]string{}, ids...), prefix+"-global") {
		conversation := convID
		var task any
		if i == 0 {
			task = taskID
		}
		if i == 2 {
			conversation = globalID
		}
		if _, err := pool.Exec(ctx, `INSERT INTO agent_turn_projections
		 (id,conversation_id,task_id,idempotency_key,request_hash,input_text,input_asset_ids_json,status,resume_required,tool_steps_json,output_text,thinking_text,created_at,updated_at)
		 VALUES ($1,$2,$3,$1,$4,'input','[]','succeeded',FALSE,'[{"name":"fixture"}]','output','thinking',NOW(),NOW())`, id, conversation, task, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
	var reads int
	const callback = "test:turn_batch_reads"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(q *gorm.DB) {
		if !q.DryRun && strings.Contains(q.Statement.SQL.String(), "AS conversation_harness_run_id") {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	page, err := (Service{DB: db}).ListTurns(ctx, &productID, convID, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || reads != 1 {
		t.Fatalf("page items=%d projection_queries=%d, want 2/1", len(page.Items), reads)
	}
	rows, err := loadTurns(ctx, db, &productID, convID, ids)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		if row.ID != ids[i] {
			t.Fatal("batch changed caller order")
		}
		single, err := loadTurn(ctx, db, &productID, convID, ids[i])
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(serializeTurn(row, nil))
		want, _ := json.Marshal(serializeTurn(single, nil))
		if string(got) != string(want) {
			t.Fatalf("batch differs from single read: %s != %s", got, want)
		}
	}
	if serializeTurn(rows[0], nil).HarnessRunID != taskHarness || serializeTurn(rows[1], nil).HarnessRunID != convID {
		t.Fatal("task/conversation harness precedence changed")
	}
	wrongProduct := prefix + "-other"
	for i, tc := range []struct {
		product *string
		conv    string
		ids     []string
	}{
		{nil, convID, ids}, {&wrongProduct, convID, ids}, {&productID, globalID, []string{prefix + "-global"}},
		{&productID, convID, []string{ids[0], prefix + "-global"}}, {&productID, convID, []string{"missing"}},
	} {
		t.Run(fmt.Sprintf("reject_%d", i), func(t *testing.T) {
			if got, err := loadTurns(ctx, db, tc.product, tc.conv, tc.ids); err == nil || got != nil {
				t.Fatalf("invalid scope/missing row returned %+v err=%v", got, err)
			}
		})
	}
	if rows, err := loadTurns(ctx, db, &productID, convID, nil); err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("empty batch: %+v %v", rows, err)
	}
}
