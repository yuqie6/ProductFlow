package localedit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

type countingResultProvider struct {
	MockProvider
	calls int
}

func (p *countingResultProvider) Edit(ctx context.Context, req EditRequest) (EditResult, error) {
	p.calls++
	return p.MockProvider.Edit(ctx, req)
}

func TestResultPersistenceFailurePreservesCauseWithoutProviderReplay(t *testing.T) {
	for _, terminalFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(terminalFailure), func(t *testing.T) {
			ctx := context.Background()
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_editresult_%d", time.Now().UnixNano()))
			provider := &countingResultProvider{MockProvider: MockProvider{Cap: SupportedCapability("mock-local")}}
			es := newEditServerWithDatabase(t, provider, pool, db)
			taskID := createQueuedLocalEdit(t, es, es.createProduct(t), "result-failure")
			if _, err := restageLocalEditTask(ctx, db, taskID); err != nil {
				t.Fatal(err)
			}
			var dispatchID string
			if err := pool.QueryRow(ctx, "UPDATE async_dispatches SET status='sent',attempts=1 WHERE aggregate_id=$1 RETURNING id", taskID).Scan(&dispatchID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "ALTER TABLE product_image_assets ADD CONSTRAINT test_result_asset_failure CHECK (origin_type <> 'local_edit')"); err != nil {
				t.Fatal(err)
			}
			if terminalFailure {
				if _, err := pool.Exec(ctx, "ALTER TABLE local_image_edit_tasks ADD CONSTRAINT test_result_terminal_failure CHECK (status <> 'unknown')"); err != nil {
					t.Fatal(err)
				}
			}
			executor := Executor{DB: db, Media: es.media, Provider: provider}
			err := queue.Consume(ctx, pool, dispatchID, taskID, map[string]queue.ActorFunc{queue.ActorLocalEdit: executor.Execute})
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.ConstraintName != "test_result_asset_failure" {
				t.Fatalf("result persistence cause lost: %v", err)
			}
			if terminalFailure && !strings.Contains(err.Error(), "test_result_terminal_failure") {
				t.Fatalf("terminal failure lost: %v", err)
			}
			var status, dispatchStatus string
			if err := pool.QueryRow(ctx, "SELECT t.status,d.status FROM local_image_edit_tasks t JOIN async_dispatches d ON d.aggregate_id=t.id WHERE t.id=$1", taskID).Scan(&status, &dispatchStatus); err != nil {
				t.Fatal(err)
			}
			expected := "unknown"
			if terminalFailure {
				expected = "running"
			}
			if status != expected || dispatchStatus != "pending" || provider.calls != 1 {
				t.Fatalf("status=%s dispatch=%s calls=%d", status, dispatchStatus, provider.calls)
			}
			if terminalFailure {
				if _, err := pool.Exec(ctx, "ALTER TABLE local_image_edit_tasks DROP CONSTRAINT test_result_terminal_failure"); err != nil {
					t.Fatal(err)
				}
				if outcome, err := recoverLocalEditState(ctx, db, taskID, time.Minute, time.Now().UTC().Add(2*time.Minute)); err != nil || outcome != "unknown" {
					t.Fatalf("recover=%s err=%v", outcome, err)
				}
			}
			var holdStatus string
			if err := pool.QueryRow(ctx, "SELECT status FROM merchant_quota_holds WHERE idempotency_key LIKE $1", "local-edit:"+taskID+":%").Scan(&holdStatus); err != nil {
				t.Fatal(err)
			}
			if holdStatus != quota.StatusPendingReconciliation {
				t.Fatalf("hold=%s", holdStatus)
			}
			if _, err := pool.Exec(ctx, "UPDATE async_dispatches SET status='sent' WHERE id=$1", dispatchID); err != nil {
				t.Fatal(err)
			}
			if err := queue.Consume(ctx, pool, dispatchID, taskID, map[string]queue.ActorFunc{queue.ActorLocalEdit: executor.Execute}); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, "SELECT status FROM async_dispatches WHERE id=$1", dispatchID).Scan(&dispatchStatus); err != nil {
				t.Fatal(err)
			}
			if dispatchStatus != "consumed" {
				t.Fatalf("redelivery=%s", dispatchStatus)
			}
			if provider.calls != 1 {
				t.Fatalf("unknown result replayed provider %d times", provider.calls)
			}
			var assets int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM product_image_assets WHERE origin_type='local_edit'").Scan(&assets); err != nil {
				t.Fatal(err)
			}
			if assets != 0 {
				t.Fatalf("failed persistence left %d assets", assets)
			}
		})
	}
}
