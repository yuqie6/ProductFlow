package recipe_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/recipe"
)

func TestRecipeListIsolatesInvalidCurrentVersions(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, "recipe_list_"+strings.ReplaceAll(clockid.New(), "-", ""))
	rs := newRecipeServerWithDB(t, pool, gdb)
	productID, g := rs.createDirectGraph(t, "recipe list validation")
	create := func(title string) recipe.RecipeView {
		t.Helper()
		resp := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", map[string]any{
			"source_type": "workflow", "expected_graph_revision": g.Revision, "title": title,
		})
		rs.mustStatus(t, resp, http.StatusCreated)
		var out recipe.RecipeView
		rs.decode(t, resp, &out)
		return out
	}
	valid := create("valid recipe")
	invalid := create("invalid recipe")
	// Simulate a persisted version that predates the current node contract.
	_, err := rs.pool.Exec(context.Background(), `UPDATE workflow_recipe_versions SET payload_json = jsonb_set(payload_json::jsonb, '{nodes,0,config,retired_field}', 'true')::json WHERE id = $1`, invalid.CurrentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, archived := range []string{"false", "true"} {
		resp := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes?include_archived="+archived, nil, "")
		rs.mustStatus(t, resp, http.StatusOK)
		var items []recipe.RecipeView
		rs.decode(t, resp, &items)
		found := false
		for _, item := range items {
			if item.ID == invalid.ID {
				t.Fatal("invalid recipe exposed as usable")
			}
			if item.ID == valid.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("valid recipe missing")
		}
	}
	detail := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes/"+invalid.ID, nil, "")
	rs.mustStatus(t, detail, http.StatusConflict)
	rs.decode(t, detail, nil)
	preview := rs.doJSON(t, http.MethodPost, "/api/v3/workflow-recipes/"+invalid.ID+"/creation-preview", map[string]any{"expected_recipe_version": 1})
	rs.mustStatus(t, preview, http.StatusConflict)
	rs.decode(t, preview, nil)
	// Corrupt the remaining current version's hash: no usable items still returns an array.
	_, err = rs.pool.Exec(context.Background(), `UPDATE workflow_recipe_versions SET payload_hash = repeat('0', 64) WHERE id = $1`, valid.CurrentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	resp := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes", nil, "")
	rs.mustStatus(t, resp, http.StatusOK)
	var items []recipe.RecipeView
	rs.decode(t, resp, &items)
	if items == nil || len(items) != 0 {
		t.Fatalf("expected empty array, got %+v", items)
	}
	var count int
	if err := rs.pool.QueryRow(context.Background(), `SELECT count(*) FROM workflow_recipes WHERE id = ANY($1::varchar[])`, []string{valid.ID, invalid.ID}).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("listing removed persisted recipes")
	}
}
