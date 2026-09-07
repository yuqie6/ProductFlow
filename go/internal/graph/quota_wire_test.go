package graph_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func mustSeedMerchantQuota(t *testing.T, db *gorm.DB, merchantID string, units int64) {
	t.Helper()
	if units <= 0 {
		return
	}
	svc := &quota.Service{DB: db}
	if _, err := svc.Adjust(context.Background(), merchantID, "seed-graph-"+clockid.New(), units, "test fixture", ""); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
}

func loadQuotaAccount(t *testing.T, db *gorm.DB, merchantID string) quota.Account {
	t.Helper()
	acct, err := (&quota.Service{DB: db}).GetAccount(context.Background(), merchantID)
	if err != nil {
		t.Fatal(err)
	}
	return acct
}

func loadQuotaHold(t *testing.T, db *gorm.DB, merchantID, key string) schema.MerchantQuotaHolds {
	t.Helper()
	var row schema.MerchantQuotaHolds
	if err := db.Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Take(&row).Error; err != nil {
		t.Fatalf("load hold %s: %v", key, err)
	}
	return row
}

func countQuotaEvents(t *testing.T, db *gorm.DB, merchantID, eventType, key string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, eventType, key).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func findAnyImageHoldKey(t *testing.T, db *gorm.DB, merchantID string) string {
	t.Helper()
	var row schema.MerchantQuotaHolds
	err := db.Where("merchant_id = ? AND idempotency_key LIKE ?", merchantID, "graph-image-node:%").
		Order("created_at DESC").
		Take(&row).Error
	if err != nil {
		t.Fatalf("find image hold: %v", err)
	}
	return row.IdempotencyKey
}

func TestImageNodeRejectsInsufficientQuota(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	merchantID := auth.MustDevMerchantID(t, gs.db)
	acct := loadQuotaAccount(t, gs.db, merchantID)
	if acct.AvailableUnits > 0 {
		if _, err := (&quota.Service{DB: gs.db}).Adjust(context.Background(), merchantID, "drain-"+clockid.New(), -acct.AvailableUnits, "drain", ""); err != nil {
			t.Fatal(err)
		}
	}

	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)

	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)

	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "failed" {
		t.Fatalf("status %s reason %+v", finished.Status, finished.FailureReason)
	}
	var imageFailed bool
	for _, node := range finished.NodeRuns {
		if node.FailureReason != nil && *node.FailureReason == "可用额度不足" {
			imageFailed = true
			break
		}
	}
	if !imageFailed {
		t.Fatalf("expected image node conflict 可用额度不足, nodes=%+v", finished.NodeRuns)
	}
	var holdCount int64
	if err := gs.db.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key LIKE ?", merchantID, "graph-image-node:%").
		Count(&holdCount).Error; err != nil {
		t.Fatal(err)
	}
	if holdCount != 0 {
		t.Fatalf("insufficient quota must not leave holds, got %d", holdCount)
	}
}

func TestImageNodeSuccessSettlesQuotaHold(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	merchantID := auth.MustDevMerchantID(t, gs.db)
	before := loadQuotaAccount(t, gs.db, merchantID)

	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)

	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)

	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "succeeded" {
		t.Fatalf("status %s", finished.Status)
	}
	key := findAnyImageHoldKey(t, gs.db, merchantID)
	hold := loadQuotaHold(t, gs.db, merchantID, key)
	if hold.Status != quota.StatusSettled {
		t.Fatalf("hold after success=%+v", hold)
	}
	if countQuotaEvents(t, gs.db, merchantID, quota.EventReserve, key) != 1 {
		t.Fatal("missing reserve event")
	}
	if countQuotaEvents(t, gs.db, merchantID, quota.EventSettle, key) != 1 {
		t.Fatal("missing settle event")
	}
	after := loadQuotaAccount(t, gs.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits-1 || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("after settle before=%+v after=%+v", before, after)
	}
}

func TestImageNodeUnknownMarksQuotaPending(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	merchantID := auth.MustDevMerchantID(t, gs.db)

	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)

	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{Err: errors.New("provider crashed")},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)

	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "unknown" {
		t.Fatalf("status %s reason %+v", finished.Status, finished.FailureReason)
	}
	key := findAnyImageHoldKey(t, gs.db, merchantID)
	hold := loadQuotaHold(t, gs.db, merchantID, key)
	if hold.Status != quota.StatusPendingReconciliation {
		t.Fatalf("hold after unknown=%+v", hold)
	}
	if countQuotaEvents(t, gs.db, merchantID, quota.EventMarkUnknown, key) != 1 {
		t.Fatal("missing mark_unknown event")
	}
	_, _, err := (&quota.Service{DB: gs.db}).Release(context.Background(), merchantID, key)
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		t.Fatalf("release unknown want conflict, got %v", err)
	}
}
