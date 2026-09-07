package product

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/recipe"
)

func recipeCreationFixture(t *testing.T) (*productServer, recipe.Service, RecipeCreateInput) {
	t.Helper()
	ps := newProductServer(t)
	ctx := ps.merchantCtx(t)
	created, err := ps.svc.CreateDirect(ctx, CreateInput{Name: "recipe source", Uploads: []Upload{{Filename: "source.png", MIMEType: "image/png", Content: pngFile(t, 8, 6)}}}, []graph.DirectCreateImageType{{Key: "hero", Quantity: 1}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	live, err := (graph.Service{DB: ps.db, Products: GraphGuard{}}).Current(ctx, created.Product.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc := recipe.Service{DB: ps.db, Products: GraphGuard{}}
	rec, err := svc.Create(ctx, recipe.CreateInput{ProductID: created.Product.ID, WorkflowID: live.ID, SourceType: "workflow", ExpectedGraphRevision: live.Revision, Title: "complete recipe"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewCreation(ctx, rec.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return ps, svc, RecipeCreateInput{Product: CreateInput{Name: "new recipe product", SourceNote: "target details", Uploads: []Upload{{Filename: "target.png", MIMEType: "image/png", Content: pngFile(t, 9, 7)}}}, RecipeID: rec.ID, ExpectedRecipeVersion: 1, PreviewDigest: preview.PreviewDigest, IdempotencyKey: clockid.New()}
}

func productCount(t *testing.T, ps *productServer) int {
	t.Helper()
	var count int
	if err := ps.pool.QueryRow(context.Background(), "SELECT count(*) FROM products").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func storedFiles(t *testing.T, ps *productServer) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(ps.svc.Media.Files.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRecipeCreationPreviewAndAtomicConfirmation(t *testing.T) {
	ps, recipes, in := recipeCreationFixture(t)
	ctx := ps.merchantCtx(t)
	count := productCount(t, ps)
	files := storedFiles(t, ps)
	for i := 0; i < 2; i++ {
		preview, err := recipes.PreviewCreation(ctx, in.RecipeID, 1)
		if err != nil || preview.PreviewDigest != in.PreviewDigest {
			t.Fatalf("preview %v %v", preview, err)
		}
	}
	if productCount(t, ps) != count || fmt.Sprint(storedFiles(t, ps)) != fmt.Sprint(files) {
		t.Fatal("preview wrote product/media")
	}
	for _, bad := range []RecipeCreateInput{
		{Product: in.Product, RecipeID: in.RecipeID, ExpectedRecipeVersion: 2, PreviewDigest: in.PreviewDigest, IdempotencyKey: clockid.New()},
		{Product: in.Product, RecipeID: in.RecipeID, ExpectedRecipeVersion: 1, PreviewDigest: strings.Repeat("0", 64), IdempotencyKey: clockid.New()},
	} {
		if _, err := ps.svc.CreateFromRecipe(ctx, bad); err == nil {
			t.Fatal("invalid preview accepted")
		}
		if productCount(t, ps) != count || fmt.Sprint(storedFiles(t, ps)) != fmt.Sprint(files) {
			t.Fatal("failed confirmation leaked product/media")
		}
	}
	out, err := ps.svc.CreateFromRecipe(ctx, in)
	if err != nil || !out.Created {
		t.Fatalf("create %v %v", out, err)
	}
	if productCount(t, ps) != count+1 {
		t.Fatal("wrong product count")
	}
	for _, node := range out.Graph.Nodes {
		if node.BoundAssetID != nil || node.CurrentArtifactID != nil {
			t.Fatal("source asset/output leaked")
		}
		if node.NodeType == graph.NodeProductSource && node.Config["source_product_id"] != out.Product.ID {
			t.Fatal("wrong target product identity")
		}
	}
	var apps, conversations int
	_ = ps.pool.QueryRow(ctx, "SELECT count(*) FROM workflow_recipe_applications WHERE product_id=$1", out.Product.ID).Scan(&apps)
	_ = ps.pool.QueryRow(ctx, "SELECT count(*) FROM agent_conversations WHERE product_id=$1", out.Product.ID).Scan(&conversations)
	if apps != 1 || conversations != 0 {
		t.Fatalf("applications=%d conversations=%d", apps, conversations)
	}
	replay, err := ps.svc.CreateFromRecipe(ctx, in)
	if err != nil || replay.Created || replay.Product.ID != out.Product.ID || replay.Graph.ID != out.Graph.ID {
		t.Fatalf("replay %v %v", replay, err)
	}
	in.Product.Name = "changed confirmation"
	if _, err := ps.svc.CreateFromRecipe(ctx, in); err == nil {
		t.Fatal("same key allowed different product")
	}
	if productCount(t, ps) != count+1 {
		t.Fatal("duplicate product")
	}
	if _, err := recipes.Preview(ctx, out.Product.ID, in.RecipeID, 1); err == nil {
		t.Fatal("complete recipe overwrote graph")
	}
}

func TestRecipeCreationConcurrentConfirmation(t *testing.T) {
	ps, _, in := recipeCreationFixture(t)
	count := productCount(t, ps)
	results := make([]RecipeCreateResponse, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], errs[i] = ps.svc.CreateFromRecipe(ps.merchantCtx(t), in) }()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0].Product.ID != results[1].Product.ID || results[0].Created == results[1].Created || productCount(t, ps) != count+1 {
		t.Fatal("concurrent confirmation duplicated creation")
	}
}

func TestRecipeCreationHTTPContract(t *testing.T) {
	ps, _, in := recipeCreationFixture(t)
	request := func(key string) *http.Response {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for field, value := range map[string]string{"name": in.Product.Name, "source_note": in.Product.SourceNote, "recipe_id": in.RecipeID, "expected_recipe_version": "1", "preview_digest": in.PreviewDigest} {
			_ = w.WriteField(field, value)
		}
		file, err := w.CreateFormFile("images", "target.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write(in.Product.Uploads[0].Content)
		_ = w.Close()
		req, err := http.NewRequest(http.MethodPost, ps.srv.URL+"/api/v3/products/from-recipe", &buf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Idempotency-Key", key)
		for _, cookie := range ps.cookies {
			req.AddCookie(cookie)
		}
		resp, err := ps.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for _, trial := range []struct {
		key    string
		status int
	}{{"", 400}, {in.IdempotencyKey, 201}, {in.IdempotencyKey, 200}} {
		resp := request(trial.key)
		if resp.StatusCode != trial.status {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, trial.status, raw)
		}
		resp.Body.Close()
	}
}

func TestRecipeCreationCommitFailureRollsBackFiles(t *testing.T) {
	ps, _, in := recipeCreationFixture(t)
	ctx := ps.merchantCtx(t)
	count, files := productCount(t, ps), storedFiles(t, ps)
	_, err := ps.pool.Exec(ctx, `
CREATE FUNCTION fail_recipe_creation_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'commit failure probe'; END $$;
CREATE CONSTRAINT TRIGGER fail_recipe_creation_commit AFTER INSERT ON products DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_recipe_creation_commit();
`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ps.pool.Exec(ctx, `DROP TRIGGER fail_recipe_creation_commit ON products; DROP FUNCTION fail_recipe_creation_commit();`)
	})
	if _, err := ps.svc.CreateFromRecipe(ctx, in); err == nil || !strings.Contains(err.Error(), "commit failure probe") {
		t.Fatalf("expected deferred failure: %v", err)
	}
	if productCount(t, ps) != count || fmt.Sprint(storedFiles(t, ps)) != fmt.Sprint(files) {
		t.Fatal("commit failure leaked product/media")
	}
}
