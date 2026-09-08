package delivery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestConcurrentDeliveryEntriesShareJobAndDispatch(t *testing.T) {
	for _, pair := range []string{"submit_submit", "submit_auto", "auto_auto", "submit_rollback"} {
		t.Run(pair, func(t *testing.T) {
			ds := newDeliveryServer(t)
			created := ds.createProduct(t)
			assetID := created.CreatedAssets[0].ID
			ds.attachArtifact(t, created.Product.ID, assetID)
			var nodeID string
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
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
			var stages atomic.Int32
			if pair == "submit_rollback" {
				const failStage = "test:delivery_first_stage_sql_failure"
				if err := ds.db.Callback().Create().Before("gorm:create").Register(failStage, func(db *gorm.DB) {
					if db.Statement.Table == "async_dispatches" && stages.Add(1) == 1 {
						db.AddError(db.Session(&gorm.Session{NewDB: true}).Exec("SELECT 1/0").Error)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { ds.db.Callback().Create().Remove(failStage) })
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
						err := tx.WithGorm(ctx, ds.db, func(db *gorm.DB) error { return service.QueueAfterImageSuccess(ctx, db, nodeID, assetID) })
						results <- outcome{err: err}
						return
					}
					result, err := service.Submit(ctx, assetID, map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"})
					results <- outcome{result: result, err: err}
				}()
			}
			createdCount := 0
			errorCount := 0
			returnedIDs := []string{}
			for i := 0; i < 2; i++ {
				r := <-results
				if r.err != nil {
					errorCount++
					var pgErr *pgconn.PgError
					if pair != "submit_rollback" || !errors.As(r.err, &pgErr) || pgErr.Code != "22012" {
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
			if (pair == "submit_submit" || pair == "submit_rollback") && createdCount != 1 {
				t.Fatalf("created results=%d", createdCount)
			}
			if pair == "submit_rollback" && errorCount != 1 {
				t.Fatalf("failed stage errors=%d", errorCount)
			}
			var jobs, dispatches int
			if err := ds.pool.QueryRow(ctx, "SELECT count(*) FROM delivery_rendition_jobs WHERE source_asset_id=$1", assetID).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			if err := ds.pool.QueryRow(ctx, "SELECT count(*) FROM async_dispatches WHERE actor_name='run_delivery_rendition_job' AND aggregate_id=$1", jobID).Scan(&dispatches); err != nil {
				t.Fatal(err)
			}
			if jobs != 1 || dispatches != 1 {
				t.Fatalf("jobs=%d dispatches=%d", jobs, dispatches)
			}
		})
	}
}
