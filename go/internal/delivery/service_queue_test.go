package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
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
	merchantCtx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, ds.db))
	// A readable source which is not a generated image remains an optional skip.
	if err := ds.db.Exec("UPDATE workflow_graph_artifacts SET artifact_type='prompt' WHERE product_image_asset_id=?", assetID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.WithGorm(merchantCtx, ds.db, func(db *gorm.DB) error {
		return svc.QueueAfterImageSuccess(merchantCtx, db, nodeID, assetID)
	}); err != nil {
		t.Fatalf("non-generated source no longer skipped: %v", err)
	}
	var skippedJobs int
	if err := ds.pool.QueryRow(context.Background(), "SELECT count(*) FROM delivery_rendition_jobs WHERE source_asset_id=$1", assetID).Scan(&skippedJobs); err != nil {
		t.Fatal(err)
	}
	if skippedJobs != 0 {
		t.Fatalf("invalid source queued %d jobs", skippedJobs)
	}
	if err := ds.db.Exec("UPDATE workflow_graph_artifacts SET artifact_type='image' WHERE product_image_asset_id=?", assetID).Error; err != nil {
		t.Fatal(err)
	}
	err = tx.WithGorm(merchantCtx, ds.db, func(pgxTx *gorm.DB) error {
		return svc.QueueAfterImageSuccess(merchantCtx, pgxTx, nodeID, assetID)
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
	if got := riverTaskCount(t, ds.pool, queue.ActorDelivery, jobID); got != 1 {
		t.Fatalf("river jobs %d", got)
	}

	missing := tx.WithGorm(merchantCtx, ds.db, func(pgxTx *gorm.DB) error {
		return svc.QueueAfterImageSuccess(merchantCtx, pgxTx, clockid.New(), assetID)
	})
	if missing == nil {
		t.Fatal("missing node should return error")
	}
}
