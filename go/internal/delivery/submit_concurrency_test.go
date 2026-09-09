package delivery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestConcurrentDeliveryEntriesShareJobAndRiverTask(t *testing.T) {
	for _, pair := range []string{"submit_submit", "submit_auto", "auto_auto", "submit_rollback"} {
		t.Run(pair, func(t *testing.T) {
			ds := newDeliveryServer(t)
			created := ds.createProduct(t)
			assetID := created.CreatedAssets[0].ID
			ds.attachArtifact(t, created.Product.ID, assetID)
			var nodeID string
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			merchantCtx := auth.WithMerchantID(ctx, auth.MustDevMerchantID(t, ds.db))
			if err := ds.pool.QueryRow(ctx, `SELECT n.id FROM workflow_graph_nodes n JOIN workflow_graphs g ON g.id=n.graph_id WHERE g.product_id=$1`, created.Product.ID).Scan(&nodeID); err != nil {
				t.Fatal(err)
			}
			if err := ds.db.Exec(`UPDATE workflow_graph_nodes SET config_json='{"delivery_spec":{"width":64,"height":64,"format":"png","fit":"contain"}}' WHERE id=?`, nodeID).Error; err != nil {
				t.Fatal(err)
			}
			ready := make(chan struct{})
			var reads atomic.Int32
			const callback = "test:delivery_concurrent_missing"
			if err := ds.db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
				if db.Statement.Table != "delivery_rendition_jobs" || !errors.Is(db.Error, gorm.ErrRecordNotFound) {
					return
				}
				if reads.Add(1) == 2 {
					close(ready)
				}
				select {
				case <-ready:
				case <-ctx.Done():
					db.AddError(ctx.Err())
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { ds.db.Callback().Query().Remove(callback) })
			if pair == "submit_rollback" {
				installOneShotRiverInsertFailure(t, ds)
			}
			service := Service{DB: ds.db, Media: ds.media}
			type outcome struct {
				result SubmitResult
				err    error
			}
			results := make(chan outcome, 2)
			for i := 0; i < 2; i++ {
				auto := pair == "auto_auto" || (pair == "submit_auto" && i == 1)
				go func() {
					if auto {
						err := tx.WithGorm(merchantCtx, ds.db, func(db *gorm.DB) error { return service.QueueAfterImageSuccess(merchantCtx, db, nodeID, assetID) })
						results <- outcome{err: err}
						return
					}
					result, err := service.Submit(merchantCtx, assetID, map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"})
					results <- outcome{result: result, err: err}
				}()
			}
			createdCount := 0
			errorCount := 0
			var rollbackErr error
			returnedIDs := []string{}
			for i := 0; i < 2; i++ {
				r := <-results
				if r.err != nil {
					errorCount++
					rollbackErr = r.err
					if pair != "submit_rollback" {
						t.Errorf("concurrent entry: %v", r.err)
					}
				}
				if r.result.Created {
					createdCount++
				}
				if r.result.Job.ID != "" {
					returnedIDs = append(returnedIDs, r.result.Job.ID)
				}
			}
			if reads.Load() != 2 {
				t.Fatalf("missing row reads=%d", reads.Load())
			}
			var jobID string
			if err := ds.pool.QueryRow(ctx, "SELECT id FROM delivery_rendition_jobs WHERE source_asset_id=$1", assetID).Scan(&jobID); err != nil {
				t.Fatal(err)
			}
			for _, id := range returnedIDs {
				if id != jobID {
					t.Fatalf("returned %s instead of shared %s", id, jobID)
				}
			}
			wantCreated := 0
			if pair == "submit_submit" || pair == "submit_rollback" {
				wantCreated = 1
			}
			if pair == "submit_auto" {
				if createdCount > 1 {
					t.Fatalf("created results=%d", createdCount)
				}
			} else if createdCount != wantCreated {
				t.Fatalf("created results=%d want=%d", createdCount, wantCreated)
			}
			var jobs, riverTasks int
			if err := ds.pool.QueryRow(ctx, "SELECT count(*) FROM delivery_rendition_jobs WHERE source_asset_id=$1", assetID).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			riverTasks = riverTaskCount(t, ds.pool, queue.ActorDelivery, jobID)
			if jobs != 1 || riverTasks != 1 {
				t.Fatalf("jobs=%d river_tasks=%d errors=%d", jobs, riverTasks, errorCount)
			}
			if pair == "submit_rollback" {
				var pgErr *pgconn.PgError
				if errorCount != 1 || !errors.As(rollbackErr, &pgErr) || pgErr.Code != "22012" {
					t.Fatalf("River insert failure=%v code=%v errors=%d", rollbackErr, pgErr, errorCount)
				}
			} else if errorCount != 0 {
				t.Fatalf("unexpected concurrent errors=%d", errorCount)
			}
		})
	}
}

func installOneShotRiverInsertFailure(t *testing.T, ds *deliveryServer) {
	t.Helper()
	const sequenceName = "test_delivery_river_fail_once_seq"
	const functionName = "test_delivery_river_fail_once"
	const triggerName = "test_delivery_river_fail_once_trigger"
	ctx := context.Background()
	for _, statement := range []string{
		"DROP TRIGGER IF EXISTS " + triggerName + " ON river_job",
		"DROP FUNCTION IF EXISTS " + functionName + "()",
		"DROP SEQUENCE IF EXISTS " + sequenceName,
	} {
		if _, err := ds.pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ds.pool.Exec(ctx, "CREATE SEQUENCE "+sequenceName); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(ctx, `
		CREATE FUNCTION test_delivery_river_fail_once() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.kind = 'productflow_task' AND nextval('test_delivery_river_fail_once_seq') = 1 THEN
				RAISE EXCEPTION 'one-shot River insert failure' USING ERRCODE = '22012';
			END IF;
			RETURN NEW;
		END;
		$$
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(ctx, `
		CREATE TRIGGER test_delivery_river_fail_once_trigger
		BEFORE INSERT ON river_job FOR EACH ROW
		EXECUTE FUNCTION test_delivery_river_fail_once()
	`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{
			"DROP TRIGGER IF EXISTS " + triggerName + " ON river_job",
			"DROP FUNCTION IF EXISTS " + functionName + "()",
			"DROP SEQUENCE IF EXISTS " + sequenceName,
		} {
			if _, err := ds.pool.Exec(context.Background(), statement); err != nil {
				t.Error(err)
			}
		}
	})
}
