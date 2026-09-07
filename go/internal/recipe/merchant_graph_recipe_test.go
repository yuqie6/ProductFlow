package recipe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// B3：本商 list/get/save/preview/apply 成功；交叉 recipe/product 统一 404；跨商 apply 零写入。
func TestMerchantRecipeIsolation(t *testing.T) {
	rs := newRecipeServer(t)
	productID, g := rs.createDirectGraph(t, "本商配方隔离")

	save := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "workflow",
		"expected_graph_revision": g.Revision,
		"title":                   "本商配方",
	})
	rs.mustStatus(t, save, http.StatusCreated)
	var own map[string]any
	rs.decode(t, save, &own)
	ownID := own["id"].(string)

	getOwn := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes/"+ownID, nil, "")
	rs.mustStatus(t, getOwn, http.StatusOK)
	getOwn.Body.Close()

	listed := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes", nil, "")
	rs.mustStatus(t, listed, http.StatusOK)
	var summaries []map[string]any
	rs.decode(t, listed, &summaries)
	found := false
	for _, item := range summaries {
		if item["id"] == ownID {
			found = true
		}
	}
	if !found {
		t.Fatal("list missing own recipe")
	}

	var promptID, imageID string
	for _, node := range g.Nodes {
		switch node.NodeType {
		case "image_prompt":
			promptID = node.ID
		case "image_generation":
			imageID = node.ID
		}
	}
	fragment := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "selection",
		"node_ids":                []string{promptID, imageID},
		"expected_graph_revision": g.Revision,
		"title":                   "本商片段",
	})
	rs.mustStatus(t, fragment, http.StatusCreated)
	var frag map[string]any
	rs.decode(t, fragment, &frag)
	fragID := frag["id"].(string)

	targetID, _ := rs.createDirectGraph(t, "本商应用目标")
	preview := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+targetID+"/workflow-recipes/"+fragID+"/preview", map[string]any{
		"expected_recipe_version": 1,
	})
	rs.mustStatus(t, preview, http.StatusOK)
	var previewBody map[string]any
	rs.decode(t, preview, &previewBody)
	apply := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+targetID+"/workflow-recipes/"+fragID+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": previewBody["base_graph_revision"],
		"preview_digest":          previewBody["preview_digest"],
		"idempotency_key":         clockid.New(),
	})
	rs.mustStatus(t, apply, http.StatusCreated)
	apply.Body.Close()

	foreign := seedForeignRecipe(t, rs)

	assertCross404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
		var body struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Detail != auth.CrossMerchantDetail {
			t.Fatalf("%s detail=%q want %q body=%s", name, body.Detail, auth.CrossMerchantDetail, raw)
		}
	}

	assertCross404("foreign recipe get", rs.do(t, http.MethodGet, "/api/v3/workflow-recipes/"+foreign.recipeID, nil, ""))
	assertCross404("foreign recipe archive", rs.do(t, http.MethodDelete, "/api/v3/workflow-recipes/"+foreign.recipeID+"?expected_recipe_version=1", nil, ""))
	assertCross404("foreign creation-preview", rs.doJSON(t, http.MethodPost, "/api/v3/workflow-recipes/"+foreign.recipeID+"/creation-preview", map[string]any{
		"expected_recipe_version": 1,
	}))
	assertCross404("foreign recipe on own product preview", rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflow-recipes/"+foreign.recipeID+"/preview", map[string]any{
		"expected_recipe_version": 1,
	}))
	assertCross404("foreign recipe on own product apply", rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflow-recipes/"+foreign.recipeID+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": g.Revision,
		"preview_digest":          strings.Repeat("a", 64),
		"idempotency_key":         clockid.New(),
	}))
	assertCross404("own recipe on foreign product preview", rs.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflow-recipes/"+ownID+"/preview", map[string]any{
		"expected_recipe_version": 1,
	}))
	assertCross404("own recipe on foreign product apply", rs.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflow-recipes/"+ownID+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": 0,
		"preview_digest":          strings.Repeat("b", 64),
		"idempotency_key":         clockid.New(),
	}))
	assertCross404("extract from foreign workflow", rs.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/recipes", map[string]any{
		"source_type":             "workflow",
		"expected_graph_revision": 1,
		"title":                   "跨商提取",
	}))

	listedAfter := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes", nil, "")
	rs.mustStatus(t, listedAfter, http.StatusOK)
	var after []map[string]any
	rs.decode(t, listedAfter, &after)
	for _, item := range after {
		if item["id"] == foreign.recipeID {
			t.Fatal("list leaked foreign recipe")
		}
	}

	var appCount int64
	if err := rs.db.WithContext(context.Background()).Table("workflow_recipe_applications").
		Where("product_id = ?", foreign.productID).Count(&appCount).Error; err != nil {
		t.Fatal(err)
	}
	if appCount != 0 {
		t.Fatalf("foreign product applications=%d want 0", appCount)
	}
}

type foreignRecipeFixture struct {
	merchantID string
	productID  string
	graphID    string
	recipeID   string
	versionID  string
}

func seedForeignRecipe(t *testing.T, rs *recipeServer) foreignRecipeFixture {
	t.Helper()
	now := time.Now().UTC()
	foreignMerchant := schema.Merchants{
		ID: clockid.New(), Name: "夹具他商-B3-recipe", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := rs.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}
	productID := clockid.New()
	graphID := clockid.New()
	recipeID := clockid.New()
	versionID := clockid.New()
	if err := rs.db.Create(&schema.Products{
		ID: productID, MerchantID: foreignMerchant.ID, Name: "他商配方商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := rs.db.Create(&schema.WorkflowGraphs{
		ID: graphID, ProductID: productID, Title: "他商图", Active: true,
		SchemaVersion: 3, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := rs.db.Create(&schema.WorkflowRecipes{
		ID: recipeID, MerchantID: foreignMerchant.ID, Kind: "workflow_recipe",
		Origin: "user", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	payload := `{"schema_version":3,"catalog_version":1,"kind":"workflow_recipe","nodes":[],"edges":[],"groups":[]}`
	hash := strings.Repeat("c", 64)
	if err := rs.db.Create(&schema.WorkflowRecipeVersions{
		ID: versionID, RecipeID: recipeID, Version: 1, SchemaVersion: 3, CatalogVersion: 1,
		CreationSource: "user_extract", Title: "他商配方", PayloadJSON: payload, PayloadHash: hash,
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := rs.db.Model(&schema.WorkflowRecipes{}).Where("id = ?", recipeID).Updates(map[string]any{
		"current_version_id": versionID,
		"updated_at":         now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = rs.db.Where("recipe_id = ?", recipeID).Delete(&schema.WorkflowRecipeVersions{}).Error
		_ = rs.db.Where("id = ?", recipeID).Delete(&schema.WorkflowRecipes{}).Error
		_ = rs.db.Where("id = ?", graphID).Delete(&schema.WorkflowGraphs{}).Error
		_ = rs.db.Where("id = ?", productID).Delete(&schema.Products{}).Error
		_ = rs.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})
	return foreignRecipeFixture{
		merchantID: foreignMerchant.ID,
		productID:  productID,
		graphID:    graphID,
		recipeID:   recipeID,
		versionID:  versionID,
	}
}
