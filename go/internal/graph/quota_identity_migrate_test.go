package graph

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestGraphEffectQuotaIdentityMigration(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_graphbilling_%d", time.Now().UnixNano()))
	ctx := context.Background()
	merchantID := auth.MustDevMerchantID(t, db)
	attempt, phase := clockid.New(), "claimed"
	runID := insertGraphRunForMerchant(t, pool, merchantID, time.Now().UTC(), "running", &phase, &attempt)
	var nodeID string
	if err := pool.QueryRow(ctx, "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", runID).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	key := imageNodeQuotaKey(nodeID, attempt)
	e := Executor{DB: db}
	if err := e.prepareProviderCall(ctx, runID, nodeID, attempt, "fixture", map[string]any{}, &key); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE workflow_graph_provider_effects DROP COLUMN quota_key").Error; err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(db); err != nil {
		t.Fatal(err)
	}
	var stored schema.WorkflowGraphProviderEffects
	if err := db.Where("node_run_id=?", nodeID).Take(&stored).Error; err != nil || stored.QuotaKey != nil {
		t.Fatalf("new nullable column=%v err=%v", stored.QuotaKey, err)
	}
	if err := db.Model(&stored).Update("quota_key", key).Error; err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id=?", stored.ID).Take(&stored).Error; err != nil || stored.QuotaKey == nil || *stored.QuotaKey != key {
		t.Fatalf("persisted quota key=%v err=%v", stored.QuotaKey, err)
	}
}
