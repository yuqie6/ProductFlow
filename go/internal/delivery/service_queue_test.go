package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestQueueAfterImageSuccessReturnsInsertAndStageErrors(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifact(t, created.Product.ID, assetID)
	var nodeID string
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT n.id FROM workflow_graph_nodes n
		JOIN workflow_graphs g ON g.id = n.graph_id
		WHERE g.product_id = $1
	`, created.Product.ID).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	spec := map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"}
	config, err := json.Marshal(map[string]any{"delivery_spec": spec})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		UPDATE workflow_graph_nodes SET config_json = $1::jsonb WHERE id = $2
	`, config, nodeID); err != nil {
		t.Fatal(err)
	}

	svc := Service{DB: ds.db, Media: ds.media}
	err = tx.WithGorm(context.Background(), ds.db, func(pgxTx *gorm.DB) error {
		return svc.QueueAfterImageSuccess(context.Background(), pgxTx, nodeID, assetID)
	})
	if err != nil {
		t.Fatal(err)
	}
	var jobCount int
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM delivery_rendition_jobs WHERE source_asset_id = $1
	`, assetID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 {
		t.Fatalf("jobs %d", jobCount)
	}
	var jobID string
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT id FROM delivery_rendition_jobs WHERE source_asset_id = $1
	`, assetID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	var dispatchCount int
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM async_dispatches WHERE actor_name = 'run_delivery_rendition_job' AND aggregate_id = $1
	`, jobID).Scan(&dispatchCount); err != nil {
		t.Fatal(err)
	}
	if dispatchCount != 1 {
		t.Fatalf("dispatch %d", dispatchCount)
	}

	missing := tx.WithGorm(context.Background(), ds.db, func(pgxTx *gorm.DB) error {
		return svc.QueueAfterImageSuccess(context.Background(), pgxTx, clockid.New(), assetID)
	})
	if missing == nil {
		t.Fatal("missing node should return error")
	}
}
