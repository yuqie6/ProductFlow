package product

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

func decodeOps[T any](t *testing.T, response *http.Response, status int) T {
	t.Helper()
	defer response.Body.Close()
	var out T
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != status {
		t.Fatalf("HTTP %d want %d: %s", response.StatusCode, status, raw)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOperatorAuditCommitsWithFactsAndSurvivesDelete(t *testing.T) {
	ps := newProductServer(t)
	own := ps.createV2(t, "own", nil, 1)
	foreign := seedForeignProductChain(t, ps, own.Product.ID)
	base := "/api/ops/merchants/" + foreign.merchantID
	path := base + "/products/" + foreign.productID
	facts := decodeOps[FactsResponse](t, ps.doJSON(t, http.MethodPut, path+"/facts", map[string]any{"name": "reviewed name"}), 200)
	decodeOps[map[string]any](t, ps.doJSON(t, http.MethodPut, path+"/facts", map[string]any{"name": "stale name"}), 409)
	decodeOps[map[string]any](t, ps.doJSON(t, http.MethodPut, path+"/facts", map[string]any{"update_node_ids": []string{}, "expected_fact_version": facts.CurrentFactVersion}), 400)
	// A failure to complete the audit must roll back the product update.
	callback := "test:fail_operator_audit"
	if err := ps.db.Callback().Update().Before("gorm:update").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "operator_product_actions" {
			db.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ps.db.Callback().Update().Remove(callback) })
	decodeOps[map[string]any](t, ps.doJSON(t, http.MethodPut, path+"/facts", map[string]any{"name": "must rollback", "expected_fact_version": facts.CurrentFactVersion}), 500)
	ps.setSetting(t, "deletion_enabled", "true")
	mediaPath := filepath.Join(ps.svc.Media.Files.Root, "foreign", foreign.productID, "a.png")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("must survive rollback"), 0600); err != nil {
		t.Fatal(err)
	}
	decodeOps[map[string]any](t, ps.do(t, http.MethodDelete, path, nil, ""), 500)
	if _, err := os.Stat(mediaPath); err != nil {
		t.Fatalf("audit rollback removed media file: %v", err)
	}
	_ = ps.db.Callback().Update().Remove(callback)
	current := decodeOps[Detail](t, ps.do(t, http.MethodGet, path, nil, ""), 200)
	if current.Name != "reviewed name" {
		t.Fatalf("audit failure committed product: %+v", current)
	}
	ps.setSetting(t, "deletion_enabled", "true")
	deleted := ps.do(t, http.MethodDelete, path, nil, "")
	deleted.Body.Close()
	if deleted.StatusCode != 204 {
		t.Fatalf("delete %d", deleted.StatusCode)
	}
	if _, err := os.Stat(mediaPath); !os.IsNotExist(err) {
		t.Fatalf("successful delete did not clean file: %v", err)
	}
	actions := decodeOps[operatorPage[operatorAction]](t, ps.do(t, http.MethodGet, base+"/actions?product_id="+foreign.productID, nil, ""), 200)
	counts := map[string]int{}
	for _, event := range actions.Items {
		counts[event.Result]++
		if event.MerchantID != foreign.merchantID || event.ProductID != foreign.productID || event.ActorUserID == "" {
			t.Fatalf("wrong audit identity %+v", event)
		}
		if event.Result == "succeeded" && event.ProductName != "reviewed name" {
			t.Fatalf("lost target snapshot %+v", event)
		}
	}
	if actions.Total != 6 || counts["succeeded"] != 2 || counts["rejected"] != 2 || counts["unknown"] != 2 {
		t.Fatalf("audit outcomes: %+v %+v", counts, actions)
	}
	page := decodeOps[operatorPage[operatorAction]](t, ps.do(t, http.MethodGet, base+"/actions?page_size=1&page=2", nil, ""), 200)
	if page.Total != 6 || len(page.Items) != 1 || page.Items[0].ID == actions.Items[0].ID {
		t.Fatalf("pagination %+v", page)
	}
}

func TestOperatorMediaURLsKeepMerchantBoundary(t *testing.T) {
	ps := newProductServer(t)
	own := ps.createV2(t, "own", nil, 1)
	foreign := seedForeignProductChain(t, ps, own.Product.ID)
	base := "/api/ops/merchants/" + foreign.merchantID
	mediaPath := filepath.Join(ps.svc.Media.Files.Root, "foreign", foreign.productID, "a.png")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("operator media fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	assets := decodeOps[GalleryAssetPage](t, ps.do(t, http.MethodGet, base+"/products/"+foreign.productID+"/image-assets", nil, ""), 200)
	if len(assets.Items) != 1 || !strings.HasPrefix(assets.Items[0].DownloadURL, base+"/product-image-assets/") {
		t.Fatalf("media projection %+v", assets)
	}
	response := ps.do(t, http.MethodGet, assets.Items[0].DownloadURL, nil, "")
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("authorized media %d", response.StatusCode)
	}
	for _, path := range []string{base + "/product-image-assets/" + own.CreatedAssets[0].ID + "/download", "/api/v2/product-image-assets/" + foreign.assetID + "/download", base + "/products/" + own.Product.ID + "/image-assets"} {
		response = ps.do(t, http.MethodGet, path, nil, "")
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("cross merchant %s: %d", path, response.StatusCode)
		}
	}
	var target schema.Products
	if err := ps.db.Where("id = ?", foreign.productID).Take(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Model(&target).Update("cover_image_asset_id", foreign.assetID).Error; err != nil {
		t.Fatal(err)
	}
	list := decodeOps[ListResponse](t, ps.do(t, http.MethodGet, base+"/products", nil, ""), 200)
	if len(list.Items) != 1 || list.Items[0].CoverImageDownloadURL == nil || *list.Items[0].CoverImageDownloadURL != assets.Items[0].DownloadURL {
		t.Fatalf("cover %+v", list)
	}
}

func TestOperatorTasksReadNativeStatesAndExcludeOtherMerchants(t *testing.T) {
	ps := newProductServer(t)
	own := ps.createV2(t, "own", nil, 1)
	foreign := seedForeignProductChain(t, ps, own.Product.ID)
	now := time.Now().UTC()
	sid := clockid.New()
	if err := ps.db.Create(&schema.ImageSessions{ID: sid, MerchantID: foreign.merchantID, Title: "Image session", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	secret := "provider private payload"
	if err := ps.db.Create(&schema.ImageSessionGenerationTasks{ID: clockid.New(), SessionID: sid, Status: "unknown", Prompt: "private prompt", Size: "1024x1024", GenerationCount: 1, CreatedAt: now, FailureReason: &secret}).Error; err != nil {
		t.Fatal(err)
	}
	agentSessionID, graphID := clockid.New(), clockid.New()
	var source schema.ProductImageAssets
	if err := ps.db.Where("id = ?", foreign.assetID).Take(&source).Error; err != nil {
		t.Fatal(err)
	}
	localTaskID := clockid.New()
	rows := []any{
		&schema.AgentSessions{ID: agentSessionID, MerchantID: foreign.merchantID, Title: "Agent session", Status: "active", CreatedAt: now, UpdatedAt: now},
		&schema.AgentTasks{ID: clockid.New(), MerchantID: foreign.merchantID, SessionID: agentSessionID, ProductID: &foreign.productID, HarnessRunID: clockid.New(), Title: "Agent task", Goal: "private goal", Status: "waiting_user", CreatedAt: now.Add(-time.Second), UpdatedAt: now},
		&schema.WorkflowGraphs{ID: graphID, ProductID: foreign.productID, Title: "Workflow", SchemaVersion: 3, Revision: 1, Active: true, CreatedAt: now, UpdatedAt: now},
		&schema.WorkflowGraphRuns{ID: clockid.New(), GraphID: graphID, Status: "succeeded", RunScope: "graph", GraphRevision: 1, SnapshotJSON: "{}", StartedAt: now.Add(-2 * time.Second), FinishedAt: &now},
		&schema.LocalImageEditTasks{ID: localTaskID, ProductID: foreign.productID, SourceAssetID: foreign.assetID, SourceMediaSHA256: strings.Repeat("b", 64), MaskMediaObjectID: source.MediaObjectID, Operation: "remove", MaskGeometryJSON: "{}", Status: "draft", Revision: 1, CreatedAt: now.Add(-3 * time.Second), UpdatedAt: now},
	}
	for _, row := range rows {
		if err := ps.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { ps.db.Where("id = ?", localTaskID).Delete(&schema.LocalImageEditTasks{}) })
	base := "/api/ops/merchants/" + foreign.merchantID
	tasks := decodeOps[operatorPage[operatorTask]](t, ps.do(t, http.MethodGet, base+"/tasks", nil, ""), 200)
	if tasks.Total != 4 || tasks.Items[0].Kind != "image_session" || tasks.Items[0].ProductID != nil || tasks.Items[0].Status != "unknown" || tasks.Items[0].FailureReason == nil || *tasks.Items[0].FailureReason == secret {
		t.Fatalf("tasks %+v", tasks)
	}
	scoped := decodeOps[operatorPage[operatorTask]](t, ps.do(t, http.MethodGet, base+"/tasks?product_id="+foreign.productID, nil, ""), 200)
	if scoped.Total != 3 || len(scoped.Items) != 3 {
		t.Fatalf("invented product ownership %+v", scoped)
	}
	expected := map[string]string{"agent_task": "waiting_user", "workflow_run": "succeeded", "local_edit": "draft"}
	for _, task := range scoped.Items {
		if task.ProductID == nil || *task.ProductID != foreign.productID || expected[task.Kind] != task.Status {
			t.Fatalf("native task projection %+v", task)
		}
	}
	page := decodeOps[operatorPage[operatorTask]](t, ps.do(t, http.MethodGet, base+"/tasks?page=2&page_size=1", nil, ""), 200)
	if page.Total != 4 || len(page.Items) != 1 || page.Items[0].Kind != "agent_task" {
		t.Fatalf("task pagination %+v", page)
	}
	decodeOps[map[string]any](t, ps.do(t, http.MethodGet, base+"/tasks?product_id="+own.Product.ID, nil, ""), 404)
	decodeOps[map[string]any](t, ps.do(t, http.MethodGet, base+"/tasks?page_size=101", nil, ""), 400)
}
