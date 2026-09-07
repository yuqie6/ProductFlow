package delivery

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
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
	if len(manifestRaw) == 0 || manifestRaw[len(manifestRaw)-1] != '\n' {
		t.Fatal("manifest.json must end with a newline")
	}
	renditionJob, _ := item["rendition_job"].(map[string]any)
	if renditionJob["spec_schema_version"] != float64(1) {
		t.Fatalf("spec_schema_version %+v", renditionJob["spec_schema_version"])
	}
	sourceAsset, _ := item["source_asset"].(map[string]any)
	resultAsset, _ := item["result_asset"].(map[string]any)
	for _, key := range []string{
		"id", "origin_type", "image_type_key", "display_name", "original_filename",
		"parent_asset_id", "created_at", "mime_type", "width", "height", "byte_size", "sha256",
	} {
		if _, ok := sourceAsset[key]; !ok {
			t.Fatalf("source_asset missing %s in %+v", key, sourceAsset)
		}
		if _, ok := resultAsset[key]; !ok {
			t.Fatalf("result_asset missing %s in %+v", key, resultAsset)
		}
	}
	if sourceAsset["id"] != assetID {
		t.Fatalf("source_asset.id %+v want %s", sourceAsset["id"], assetID)
	}
	if resultAsset["sha256"] != sha {
		t.Fatalf("result_asset.sha256 %+v want %s", resultAsset["sha256"], sha)
	}
}

func TestExportArchiveBytesAreDeterministic(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifactLineage(t, created.Product.ID, assetID)
	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)
	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}

	svc := Service{DB: ds.db, Media: ds.media}
	first, err := svc.Export(context.Background(), created.Product.ID, []string{job.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(first.Path) })
	second, err := svc.Export(context.Background(), created.Product.ID, []string{job.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(second.Path) })

	a, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("export zip bytes drifted: %d vs %d", len(a), len(b))
	}

	zr, err := zip.OpenReader(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) < 2 {
		t.Fatalf("zip entries %d", len(zr.File))
	}
	if zr.File[0].Name != "manifest.json" {
		t.Fatalf("first entry %s want manifest.json", zr.File[0].Name)
	}
	for _, f := range zr.File {
		if f.Method != zip.Deflate {
			t.Fatalf("%s method %d", f.Name, f.Method)
		}
		if f.CreatorVersion>>8 != 3 {
			t.Fatalf("%s create_system %d want 3", f.Name, f.CreatorVersion>>8)
		}
		if f.ExternalAttrs != 0o600<<16 {
			t.Fatalf("%s external_attr %d want %d", f.Name, f.ExternalAttrs, 0o600<<16)
		}
		if len(f.Extra) != 0 {
			t.Fatalf("%s extra %q", f.Name, f.Extra)
		}
		if f.Comment != "" {
			t.Fatalf("%s comment %q", f.Name, f.Comment)
		}
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
	if !strings.Contains(err.Error(), auth.CrossMerchantDetail) && !strings.Contains(err.Error(), "交付图任务不存在") {
		t.Fatalf("err %v", err)
	}
}

func TestExportRejectsResultFileSizeChange(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifact(t, created.Product.ID, assetID)
	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)
	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	got := ds.do(t, http.MethodGet, "/api/v2/delivery-rendition-jobs/"+job.ID, nil, "")
	ds.mustStatus(t, got, http.StatusOK)
	ds.decode(t, got, &job)
	if job.ResultAsset == nil {
		t.Fatal("missing result asset")
	}
	abs := resolveDeliveryMedia(t, ds, job.ResultAsset.MediaObjectID)
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, append(raw, 0x00), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = (Service{DB: ds.db, Media: ds.media}).Export(context.Background(), created.Product.ID, []string{job.ID}, false)
	if err == nil {
		t.Fatal("expected size-change conflict")
	}
	if !strings.Contains(err.Error(), "交付图结果文件大小已变化") {
		t.Fatalf("err %v", err)
	}
}
