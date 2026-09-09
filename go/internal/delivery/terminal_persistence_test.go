package delivery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
)

func TestFailurePersistenceDoesNotConsumeDelivery(t *testing.T) {
	ctx := context.Background()
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	jobID := insertQueuedDeliveryJob(t, ds, created.Product.ID, created.CreatedAssets[0].ID, strings.Repeat("e", 64), time.Now().UTC())
	if err := os.Remove(resolveDeliveryMedia(t, ds, created.CreatedAssets[0].MediaObjectID)); err != nil {
		t.Fatal(err)
	}
	const constraint = "test_delivery_failure_write"
	if _, err := ds.pool.Exec(ctx, "ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT "+constraint+" CHECK (id <> '"+jobID+"' OR status <> 'failed')"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := ds.pool.Exec(context.Background(), "ALTER TABLE delivery_rendition_jobs DROP CONSTRAINT "+constraint); err != nil {
			t.Error(err)
		}
	})
	if _, err := restageDeliveryJob(ctx, ds.db, jobID); err != nil {
		t.Fatal(err)
	}
	executor := Executor{DB: ds.db, Media: ds.media}
	err := runDeliveryRiverWorker(t, ctx, ds.pool, jobID, executor)
	if err == nil || !strings.Contains(err.Error(), constraint) {
		t.Fatalf("want write error, got %v", err)
	}
	var status string
	if err := ds.pool.QueryRow(ctx, "SELECT status FROM delivery_rendition_jobs WHERE id=$1", jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Fatalf("job=%s", status)
	}
}

func TestResultPersistencePreservesDeliveryCause(t *testing.T) {
	for _, failTerminal := range []bool{false, true} {
		t.Run(fmt.Sprint(failTerminal), func(t *testing.T) {
			ctx := context.Background()
			ds := newDeliveryServer(t)
			created := ds.createProduct(t)
			jobID := insertQueuedDeliveryJob(t, ds, created.Product.ID, created.CreatedAssets[0].ID, strings.Repeat("d", 64), time.Now().UTC())
			for _, phase := range []string{"succeeded", "failed"} {
				if phase == "failed" && !failTerminal {
					continue
				}
				constraint := "test_delivery_result_" + phase
				if _, err := ds.pool.Exec(ctx, "ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT "+constraint+" CHECK (id <> '"+jobID+"' OR status <> '"+phase+"')"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := ds.pool.Exec(context.Background(), "ALTER TABLE delivery_rendition_jobs DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
						t.Error(err)
					}
				})
			}
			if _, err := restageDeliveryJob(ctx, ds.db, jobID); err != nil {
				t.Fatal(err)
			}
			executor := Executor{DB: ds.db, Media: ds.media}
			err := runDeliveryRiverWorker(t, ctx, ds.pool, jobID, executor)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.ConstraintName != "test_delivery_result_succeeded" {
				t.Fatalf("original result cause lost: %v", err)
			}
			if failTerminal && !strings.Contains(err.Error(), "test_delivery_result_failed") {
				t.Fatalf("terminal cause lost: %v", err)
			}
			var status string
			var retryable bool
			var resultID *string
			if err := ds.pool.QueryRow(ctx, "SELECT status,is_retryable,result_asset_id FROM delivery_rendition_jobs WHERE id=$1", jobID).Scan(&status, &retryable, &resultID); err != nil {
				t.Fatal(err)
			}
			expected := "failed"
			if failTerminal {
				expected = "running"
			}
			if status != expected || resultID != nil || (!failTerminal && !retryable) {
				t.Fatalf("status=%s result=%v retryable=%t", status, resultID, retryable)
			}
			var assets int
			if err := ds.pool.QueryRow(ctx, "SELECT count(*) FROM product_image_assets WHERE parent_asset_id=$1", created.CreatedAssets[0].ID).Scan(&assets); err != nil {
				t.Fatal(err)
			}
			if assets != 0 {
				t.Fatalf("rolled back result left %d assets", assets)
			}
			for _, phase := range []string{"succeeded", "failed"} {
				if _, err := ds.pool.Exec(ctx, "ALTER TABLE delivery_rendition_jobs DROP CONSTRAINT IF EXISTS test_delivery_result_"+phase); err != nil {
					t.Fatal(err)
				}
			}
			if failTerminal {
				if outcome, err := recoverDeliveryJobState(ctx, ds.db, jobID, time.Now().UTC().Add(time.Minute)); err != nil || outcome != "requeued" {
					t.Fatalf("recovery=%s err=%v", outcome, err)
				}
			} else {
				if _, err := (Service{DB: ds.db}).Retry(auth.WithMerchantID(ctx, auth.MustDevMerchantID(t, ds.db)), jobID); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				if err := executor.Execute(ctx, jobID); err != nil {
					t.Fatal(err)
				}
			}
			if err := ds.pool.QueryRow(ctx, "SELECT status,result_asset_id FROM delivery_rendition_jobs WHERE id=$1", jobID).Scan(&status, &resultID); err != nil {
				t.Fatal(err)
			}
			if err := ds.pool.QueryRow(ctx, "SELECT count(*) FROM product_image_assets WHERE parent_asset_id=$1", created.CreatedAssets[0].ID).Scan(&assets); err != nil {
				t.Fatal(err)
			}
			if status != "succeeded" || resultID == nil || assets != 1 {
				t.Fatalf("retry status=%s result=%v assets=%d", status, resultID, assets)
			}

		})
	}
}
