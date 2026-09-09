package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

type deliveryThenError struct{ service delivery.Service }

func (d deliveryThenError) QueueAfterImageSuccess(ctx context.Context, db *gorm.DB, nodeID, assetID string) error {
	if err := d.service.QueueAfterImageSuccess(ctx, db, nodeID, assetID); err != nil {
		return err
	}
	return errors.New("delivery callback failed after staging")
}

type deliverySourceReadFailure struct{ service delivery.Service }

func (d deliverySourceReadFailure) QueueAfterImageSuccess(ctx context.Context, db *gorm.DB, nodeID, assetID string) error {
	if err := db.Exec("ALTER TABLE workflow_graph_artifacts RENAME TO unavailable_delivery_artifacts").Error; err != nil {
		return err
	}
	return d.service.QueueAfterImageSuccess(ctx, db, nodeID, assetID)
}

func TestOptionalDeliveryFailureRollsBackOnlyDelivery(t *testing.T) {
	for _, failure := range []string{"dispatch_sql", "after_staging", "outer_settlement", "source_read"} {
		t.Run(failure, func(t *testing.T) {
			gs := newIsolatedGraphServer(t)
			productID, graphID := gs.createDirectGraph(t)
			ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, gs.db))
			if err := gs.db.Exec(`UPDATE workflow_graph_nodes SET config_json=jsonb_set(config_json::jsonb, '{delivery_spec}', '{"width":64,"height":64,"format":"png","fit":"contain"}'::jsonb) WHERE graph_id=? AND node_type='image_generation'`, graphID).Error; err != nil {
				t.Fatal(err)
			}
			service := delivery.Service{DB: gs.db, Media: gs.media}
			var queuer graph.DeliveryQueuer = service
			if failure == "dispatch_sql" {
				if err := gs.db.Exec(`ALTER TABLE river_job ADD CONSTRAINT test_delivery_stage_failure CHECK(args ->> 'actor' <> 'run_delivery_rendition_job')`).Error; err != nil {
					t.Fatal(err)
				}
			} else if failure == "outer_settlement" {
				if err := gs.db.Exec("ALTER TABLE merchant_quota_holds ADD CONSTRAINT test_image_settlement_failure CHECK(status <> 'settled')").Error; err != nil {
					t.Fatal(err)
				}
			} else if failure == "source_read" {
				queuer = deliverySourceReadFailure{service: service}
			} else {
				queuer = deliveryThenError{service: service}
			}
			resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
			gs.mustStatus(t, resp, 201)
			var run graph.GraphRunResponse
			gs.decode(t, resp, &run)
			exec := graph.Executor{DB: gs.db, Deps: graph.Dependencies{Prompt: graph.MockPromptProvider{}, Image: graph.MockImageProvider{}, Assets: product.Service{DB: gs.db, Media: gs.media}, Delivery: queuer}}
			if err := gs.tryExecuteLocally(t, run.ID, exec); err != nil {
				t.Fatalf("optional queue poisoned image transaction: %v", err)
			}
			var status string
			if err := gs.pool.QueryRow(ctx, "SELECT status FROM workflow_graph_runs WHERE id=$1", run.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if failure != "outer_settlement" && status != "succeeded" {
				t.Fatalf("optional delivery changed image result: %s", status)
			}
			var artifacts, jobs, dispatches, settled int
			if err := gs.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_graph_artifacts a JOIN workflow_graph_node_runs n ON n.id=a.node_run_id WHERE n.graph_run_id=$1 AND a.artifact_type='image'`, run.ID).Scan(&artifacts); err != nil {
				t.Fatal(err)
			}
			if err := gs.pool.QueryRow(ctx, `SELECT count(*) FROM delivery_rendition_jobs WHERE product_id=$1`, productID).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			if err := gs.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args ->> 'actor'='run_delivery_rendition_job'`).Scan(&dispatches); err != nil {
				t.Fatal(err)
			}
			if err := gs.pool.QueryRow(ctx, `SELECT count(*) FROM merchant_quota_holds h JOIN workflow_graph_provider_effects e ON e.quota_key=h.idempotency_key JOIN workflow_graph_node_runs n ON n.id=e.node_run_id WHERE n.graph_run_id=$1 AND h.status='settled'`, run.ID).Scan(&settled); err != nil {
				t.Fatal(err)
			}
			if failure == "outer_settlement" {
				if status != "unknown" || artifacts != 0 || settled != 0 || jobs != 0 || dispatches != 0 {
					t.Fatalf("delivery escaped outer rollback: status=%s artifacts=%d settled=%d jobs=%d dispatches=%d", status, artifacts, settled, jobs, dispatches)
				}
				return
			}
			if artifacts < 1 || settled != artifacts || jobs != 0 || dispatches != 0 {
				t.Fatalf("partial effects: artifacts=%d settled=%d jobs=%d dispatches=%d", artifacts, settled, jobs, dispatches)
			}
			if failure == "dispatch_sql" {
				if err := gs.db.Exec("ALTER TABLE river_job DROP CONSTRAINT test_delivery_stage_failure").Error; err != nil {
					t.Fatal(err)
				}
			}
			// The existing delivery entrypoint can stage the retained original after the fault is removed.
			var nodeID, assetID string
			if err := gs.pool.QueryRow(ctx, `SELECT n.node_id,a.product_image_asset_id FROM workflow_graph_artifacts a JOIN workflow_graph_node_runs n ON n.id=a.node_run_id WHERE n.graph_run_id=$1 AND a.artifact_type='image' LIMIT 1`, run.ID).Scan(&nodeID, &assetID); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := tx.WithGorm(ctx, gs.db, func(db *gorm.DB) error { return service.QueueAfterImageSuccess(ctx, db, nodeID, assetID) }); err != nil {
					t.Fatal(err)
				}
			}
			if err := gs.pool.QueryRow(ctx, `SELECT count(*) FROM delivery_rendition_jobs j JOIN river_job d ON d.args ->> 'aggregate_id'=j.id WHERE j.source_asset_id=$1 AND d.args ->> 'actor'='run_delivery_rendition_job'`, assetID).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			if jobs != 1 {
				t.Fatalf("recovery staged %d deliveries", jobs)
			}
		})
	}
}
