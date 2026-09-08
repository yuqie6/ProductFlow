package graph

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestGraphClaimRoundYieldsToOtherGraphAndBothProgress(t *testing.T) {
	_, db := admissionDB(t)
	setAdmissionMax(t, db, 1)
	merchantA := auth.MustDevMerchantID(t, db)
	merchantB := insertAdmissionMerchant(t, db)
	runID, nodes := insertAdmissionGraphRun(t, db, merchantA, "queued", "queued")
	otherRunID, otherNodes := insertAdmissionGraphRun(t, db, merchantB, "queued", "queued")
	other := stageAdmissionDispatch(t, db, queue.ActorGraphRun, merchantB, time.Now().UTC().Add(-time.Second))

	ok, _, err := claimQueuedNodeRun(context.Background(), db, nodes[0], false)
	if err != nil || !ok {
		t.Fatalf("first graph node claim ok=%t err=%v", ok, err)
	}
	markAdmissionNodeTerminal(t, db, nodes[0])

	if ok, _, err := claimQueuedNodeRun(context.Background(), db, nodes[1], true); !errors.Is(err, errWaitingCapacity) || ok {
		t.Fatalf("second node must yield to other GraphRun: ok=%t err=%v", ok, err)
	}
	assertAdmissionDispatchStatus(t, db, other.ID, queue.StatusPending)

	ok, _, err = claimQueuedNodeRun(context.Background(), db, otherNodes[0], false)
	if err != nil || !ok {
		t.Fatalf("other GraphRun must progress after the yield run=%s: ok=%t err=%v", otherRunID, ok, err)
	}
	markAdmissionNodeTerminal(t, db, otherNodes[0])
	ok, _, err = claimQueuedNodeRun(context.Background(), db, otherNodes[1], true)
	if err != nil || !ok {
		t.Fatalf("other GraphRun must make progress in its next round: ok=%t err=%v", ok, err)
	}
	markAdmissionNodeTerminal(t, db, otherNodes[1])
	if err := db.Model(&schema.AsyncDispatches{}).Where("id = ?", other.ID).Update("status", queue.StatusConsumed).Error; err != nil {
		t.Fatal(err)
	}
	ok, _, err = claimQueuedNodeRun(context.Background(), db, nodes[1], false)
	if err != nil || !ok {
		t.Fatalf("next ExecuteRun grant must progress graph run=%s: ok=%t err=%v", runID, ok, err)
	}
}

func TestGraphClaimRoundLimitsLongGraphBeforeImageSession(t *testing.T) {
	_, db := admissionDB(t)
	setAdmissionMax(t, db, 3)
	merchantID := auth.MustDevMerchantID(t, db)
	_, nodes := insertAdmissionGraphRun(t, db, merchantID, "running", "running", "queued")
	other := stageAdmissionDispatch(t, db, queue.ActorImageSession, "merchant-b", time.Now().UTC().Add(-time.Second))

	if ok, _, err := claimQueuedNodeRun(context.Background(), db, nodes[2], true); !errors.Is(err, errWaitingCapacity) || ok {
		t.Fatalf("long graph must defer its third node for image session: ok=%t err=%v", ok, err)
	}
	if err := db.Model(&schema.AsyncDispatches{}).Where("id = ?", other.ID).Update("status", queue.StatusConsumed).Error; err != nil {
		t.Fatal(err)
	}
	ok, _, err := claimQueuedNodeRun(context.Background(), db, nodes[2], false)
	if err != nil || !ok {
		t.Fatalf("same graph may fill an unused slot when no competitor remains: ok=%t err=%v", ok, err)
	}
}

func TestGraphSingleRunUsesAllAdmissionSlots(t *testing.T) {
	_, db := admissionDB(t)
	setAdmissionMax(t, db, 3)
	merchantID := auth.MustDevMerchantID(t, db)
	_, nodes := insertAdmissionGraphRun(t, db, merchantID, "queued", "queued", "queued", "queued")

	claimed := false
	for i := 0; i < 3; i++ {
		ok, _, err := claimQueuedNodeRun(context.Background(), db, nodes[i], claimed)
		if err != nil || !ok {
			t.Fatalf("single graph node %d claim ok=%t err=%v", i, ok, err)
		}
		claimed = true
	}
	if ok, _, err := claimQueuedNodeRun(context.Background(), db, nodes[3], claimed); !errors.Is(err, errWaitingCapacity) || ok {
		t.Fatalf("fourth node must respect max=3: ok=%t err=%v", ok, err)
	}
}

func admissionDB(t *testing.T) (*pgxpool.Pool, *gorm.DB) {
	t.Helper()
	name := fmt.Sprintf("pf_graph_fair_0909_%d", time.Now().UnixNano())
	return testdb.IsolatedMigrated(t, name)
}

func setAdmissionMax(t *testing.T, db *gorm.DB, max int) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&schema.AppSettings{
		Key: generation.MaxConcurrentSettingKey, Value: fmt.Sprint(max), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func insertAdmissionGraphRun(t *testing.T, db *gorm.DB, merchantID string, statuses ...string) (string, []string) {
	t.Helper()
	now := time.Now().UTC()
	productID, graphID, runID := clockid.New(), clockid.New(), clockid.New()
	if err := db.Exec(`
		INSERT INTO products (id, name, created_at, updated_at, merchant_id)
		VALUES (?, 'admission-fairness', ?, ?, ?)
	`, productID, now, now, merchantID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES (?, ?, 'admission-fairness', TRUE, 3, 1, ?, ?)
	`, graphID, productID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		INSERT INTO workflow_graph_runs
		(id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at)
		VALUES (?, ?, 'running', 'graph', 1, '{}', TRUE, ?)
	`, runID, graphID, now).Error; err != nil {
		t.Fatal(err)
	}
	nodes := make([]string, 0, len(statuses))
	for i, status := range statuses {
		nodeID := clockid.New()
		if err := db.Exec(`
			INSERT INTO workflow_graph_node_runs
			(id, graph_run_id, status, sort_order, started_at, attempt_count)
			VALUES (?, ?, ?, ?, ?, 0)
		`, nodeID, runID, status, i, now).Error; err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, nodeID)
	}
	return runID, nodes
}

func insertAdmissionMerchant(t *testing.T, db *gorm.DB) string {
	t.Helper()
	id := clockid.New()
	now := time.Now().UTC()
	if err := db.Exec(`
		INSERT INTO merchants (id, name, status, created_at, updated_at)
		VALUES (?, 'admission-fairness-secondary', 'active', ?, ?)
	`, id, now, now).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func markAdmissionNodeTerminal(t *testing.T, db *gorm.DB, nodeID string) {
	t.Helper()
	if err := db.Model(&schema.WorkflowGraphNodeRuns{}).Where("id = ?", nodeID).Updates(map[string]any{
		"status": "succeeded", "finished_at": time.Now().UTC(), "active_attempt_id": nil,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func stageAdmissionDispatch(t *testing.T, db *gorm.DB, actor, merchantID string, availableAt time.Time) queue.Dispatch {
	t.Helper()
	ctx := context.Background()
	aggregateID := clockid.New()
	var dispatch queue.Dispatch
	if err := db.Transaction(func(dbTx *gorm.DB) error {
		var err error
		dispatch, err = queue.Stage(ctx, dbTx, queue.DeliveryKey(actor, aggregateID), actor, aggregateID, nil, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schema.AsyncDispatches{}).Where("id = ?", dispatch.ID).Updates(map[string]any{
		"merchant_id":  merchantID,
		"available_at": availableAt,
		"created_at":   availableAt,
		"updated_at":   availableAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	dispatch.MerchantID = merchantID
	dispatch.AvailableAt = availableAt
	return dispatch
}

func assertAdmissionDispatchStatus(t *testing.T, db *gorm.DB, id, want string) {
	t.Helper()
	var got string
	if err := db.Model(&schema.AsyncDispatches{}).Where("id = ?", id).Pluck("status", &got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("dispatch %s status=%s want=%s", id, got, want)
	}
}
