package delivery

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestExportWritesManifestLineageAndSha256(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	graphID, runID, nodeRunID := ds.attachArtifactLineage(t, created.Product.ID, assetID)

	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)
	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}

	archive, err := (Service{DB: ds.db, Media: ds.media}).Export(context.Background(), created.Product.ID, []string{job.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(archive.Path) })
	if archive.Filename != "交付商品-delivery-export.zip" {
		t.Fatalf("filename %s", archive.Filename)
	}

	zr, err := zip.OpenReader(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = data
	}
	manifestRaw, ok := files["manifest.json"]
	if !ok {
		t.Fatalf("zip entries %v", keys(files))
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["kind"] != "productflow.delivery_export" {
		t.Fatalf("kind %+v", manifest["kind"])
	}
	if manifest["complete"] != true {
		t.Fatalf("complete %+v", manifest["complete"])
	}
	if manifest["schema_version"] != float64(1) {
		t.Fatalf("schema_version %+v", manifest["schema_version"])
	}
	missing, ok := manifest["missing_items"].([]any)
	if !ok {
		t.Fatalf("missing_items %+v", manifest["missing_items"])
	}
	if len(missing) != 0 {
		t.Fatalf("missing_items %v", missing)
	}
	items, ok := manifest["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items %+v", manifest["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item %+v", items[0])
	}
	filename, _ := item["filename"].(string)
	if !strings.HasPrefix(filename, "交付商品-") || !strings.HasSuffix(filename, ".png") {
		t.Fatalf("item filename %s", filename)
	}
	graph, _ := item["graph"].(map[string]any)
	if graph["id"] != graphID || graph["revision"] != float64(1) {
		t.Fatalf("graph %+v want id=%s rev=1", graph, graphID)
	}
	if nested, ok := graph["graph"]; ok {
		t.Fatalf("graph must be flattened, nested %+v", nested)
	}
	run, _ := item["run"].(map[string]any)
	if run["id"] != runID || run["revision"] != float64(1) {
		t.Fatalf("run %+v want id=%s rev=1", run, runID)
	}
	if item["node_run_id"] != nodeRunID {
		t.Fatalf("node_run_id %+v want %s", item["node_run_id"], nodeRunID)
	}
	measured, _ := item["measured"].(map[string]any)
	sha, _ := measured["sha256"].(string)
	imageBytes, ok := files[filename]
	if !ok {
		t.Fatalf("zip missing %s, have %v", filename, keys(files))
	}
	sum := sha256.Sum256(imageBytes)
	if sha != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 %s vs file %s", sha, hex.EncodeToString(sum[:]))
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestExportPartialIncludesMissingItems(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifactLineage(t, created.Product.ID, assetID)
	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 32, "height": 32, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)
	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}

	queued := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 48, "height": 48, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, queued, http.StatusAccepted)
	var pending JobResponse
	ds.decode(t, queued, &pending)

	archive, err := (Service{DB: ds.db, Media: ds.media}).Export(context.Background(), created.Product.ID, []string{job.ID, pending.ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(archive.Path) })

	zr, err := zip.OpenReader(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var manifest map[string]any
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
	}
	if manifest["complete"] != false {
		t.Fatalf("complete %+v", manifest["complete"])
	}
	missing, _ := manifest["missing_items"].([]any)
	if len(missing) != 1 {
		t.Fatalf("missing_items %+v", manifest["missing_items"])
	}
	row, _ := missing[0].(map[string]any)
	if row["job_id"] != pending.ID {
		t.Fatalf("missing job %+v", row)
	}
}

func TestExportRejectsUnknownJobWithoutWritingZip(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	_, err := (Service{DB: ds.db, Media: ds.media}).Export(context.Background(), created.Product.ID, []string{"missing-job"}, false)
	if err == nil {
		t.Fatal("expected missing job")
	}
	if !strings.Contains(err.Error(), "交付图任务不存在") {
		t.Fatalf("err %v", err)
	}
}
