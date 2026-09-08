package recipe_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/recipe"
)

func TestRecipeVisualReadFailureIsNotMissingVersion(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, "pf_recipe_visual_"+strings.ReplaceAll(clockid.New(), "-", ""))
	rs := newRecipeServerWithDB(t, pool, db)
	id, initial := rs.createDirectGraph(t, "visual failure")
	var row schema.Products
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithMerchantID(context.Background(), row.MerchantID)
	var visualID string
	for _, node := range initial.Nodes {
		if node.NodeType == string(graph.NodeVisualSystem) {
			visualID = node.ID
		}
	}
	if visualID == "" {
		t.Fatal("missing visual node")
	}
	gs := graph.Service{DB: db, Products: product.GraphGuard{}}
	updated, err := gs.ApplyChangeSet(ctx, id, initial.ID, graph.ChangeSet{
		BaseGraphRevision: initial.Revision,
		Operations:        []graph.Operation{graph.UpdateNodeConfigOp{NodeRef: visualID, Config: map[string]any{"visual_system_version_id": clockid.New()}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := recipe.Service{DB: db, Products: product.GraphGuard{}}
	in := recipe.CreateInput{ProductID: id, WorkflowID: initial.ID, SourceType: "workflow", ExpectedGraphRevision: updated.Revision, Title: "visual inheritance"}
	if err := db.Exec("ALTER TABLE visual_system_versions RENAME TO test_visual_versions_unavailable").Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.Create(ctx, in)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("original visual read error lost: %v", err)
	}
	var count int64
	if err := db.Model(&schema.WorkflowRecipes{}).Where("merchant_id = ?", row.MerchantID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed visual read saved %d recipes", count)
	}
	if err := db.Exec("ALTER TABLE test_visual_versions_unavailable RENAME TO visual_system_versions").Error; err != nil {
		t.Fatal(err)
	}
	// A genuinely absent inherited version remains optional after recovery.
	created, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("missing optional version no longer accepted: %v", err)
	}
	if created.CurrentVersion.PreferredVisualSystemVersionID != nil {
		t.Fatal("missing inherited version was persisted")
	}
	if err := db.Exec("ALTER TABLE visual_system_versions RENAME TO test_visual_versions_unavailable").Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.Append(ctx, recipe.AppendInput{CreateInput: in, RecipeID: created.ID, ExpectedRecipeVersion: 1})
	pgErr = nil
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("append lost visual read cause: %v", err)
	}
	var stored schema.WorkflowRecipes
	if err := db.Where("id = ?", created.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.CurrentVersionID == nil || *stored.CurrentVersionID != created.CurrentVersionID {
		t.Fatal("failed append changed current version")
	}
	if err := db.Model(&schema.WorkflowRecipeVersions{}).Where("recipe_id = ?", created.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("failed append left %d versions", count)
	}
}
