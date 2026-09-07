package delivery

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/yuqie6/productflow/internal/ocr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestDeliveryAdoptionCreatesImmutableVersionsAndExportMatchesPreview(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	assetID := created.CreatedAssets[0].ID
	graphID, _, _ := ds.attachArtifactLineage(t, productID, assetID)

	spec := map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"}
	create := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "image_type_key": "hero",
			"source_asset_id": assetID, "source_node_id": "node-hero",
			"delivery_spec": spec, "quality_status": "pass",
		}},
	})
	ds.mustStatus(t, create, http.StatusCreated)
	var version1 AdoptionVersionResponse
	ds.decode(t, create, &version1)
	if version1.Version != 1 || !version1.IsCurrent || len(version1.Slots) != 1 {
		t.Fatalf("%+v", version1)
	}
	if !version1.Slots[0].Qualified || version1.Slots[0].SourceAssetID != assetID {
		t.Fatalf("slot %+v", version1.Slots[0])
	}

	rejected := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
			"delivery_spec": spec, "quality_status": "fail",
		}},
	})
	ds.mustStatus(t, rejected, http.StatusBadRequest)
	rejected.Body.Close()

	overflow := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
			"delivery_spec": spec, "quality_status": "pass", "text_overflow": true,
		}},
	})
	ds.mustStatus(t, overflow, http.StatusBadRequest)
	overflow.Body.Close()

	extra := ds.uploadExtraAsset(t, productID)
	ds.attachArtifactOnGraph(t, productID, graphID, extra)
	create2 := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "image_type_key": "hero",
			"source_asset_id": extra, "delivery_spec": spec, "quality_status": "pass",
		}},
	})
	ds.mustStatus(t, create2, http.StatusCreated)
	var version2 AdoptionVersionResponse
	ds.decode(t, create2, &version2)
	if version2.Version != 2 || version2.Slots[0].SourceAssetID != extra {
		t.Fatalf("%+v", version2)
	}

	old := ds.do(t, http.MethodGet, "/api/v3/products/"+productID+"/delivery-adoptions/"+version1.ID, nil, "")
	ds.mustStatus(t, old, http.StatusOK)
	var still AdoptionVersionResponse
	ds.decode(t, old, &still)
	if still.Slots[0].SourceAssetID != assetID || still.IsCurrent {
		t.Fatalf("rerun must not mutate adopted version: %+v", still)
	}

	current := ds.do(t, http.MethodGet, "/api/v3/products/"+productID+"/delivery-adoptions/current", nil, "")
	ds.mustStatus(t, current, http.StatusOK)
	var cur AdoptionVersionResponse
	ds.decode(t, current, &cur)
	if cur.ID != version2.ID {
		t.Fatalf("current %+v", cur)
	}

	ensure := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions/"+version1.ID+"/renditions", map[string]any{})
	ds.mustStatus(t, ensure, http.StatusAccepted)
	var ensured AdoptionRenditionsResponse
	ds.decode(t, ensure, &ensured)
	if len(ensured.Preview.Items) != 1 || ensured.Preview.Items[0].RenditionJobID == nil {
		t.Fatalf("%+v", ensured)
	}
	jobID := *ensured.Preview.Items[0].RenditionJobID
	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), jobID); err != nil {
		t.Fatal(err)
	}

	preview := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions/"+version1.ID+"/preview", map[string]any{})
	ds.mustStatus(t, preview, http.StatusOK)
	var previewBody AdoptionPreviewResponse
	ds.decode(t, preview, &previewBody)
	if !previewBody.ExportReady || len(previewBody.Items) != 1 || previewBody.Items[0].SourceAssetID != assetID {
		t.Fatalf("%+v", previewBody)
	}

	export := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions/"+version1.ID+"/export", map[string]any{})
	ds.mustStatus(t, export, http.StatusOK)
	raw, err := io.ReadAll(export.Body)
	export.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["manifest.json"] || !names[previewBody.Items[0].Filename] {
		t.Fatalf("zip names %v want %s", names, previewBody.Items[0].Filename)
	}
	manifestFile, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := io.ReadAll(manifestFile)
	manifestFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	items, _ := manifest["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("manifest items %v", items)
	}
	item, _ := items[0].(map[string]any)
	source, _ := item["source_asset"].(map[string]any)
	if source["id"] != assetID {
		t.Fatalf("export must keep adopted source %v", source)
	}
}

func TestDeliveryAdoptionConcurrentCreatesBumpVersions(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifact(t, productID, assetID)
	spec := map[string]any{"width": 32, "height": 32, "format": "png", "fit": "contain"}

	var wg sync.WaitGroup
	results := make(chan int, 2)
	errs := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
				"slots": []map[string]any{{
					"slot_key": fmt.Sprintf("slot-%d", i), "sort_order": 0,
					"source_asset_id": assetID, "delivery_spec": spec, "quality_status": "unchecked",
				}},
			})
			if resp.StatusCode != http.StatusCreated {
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				errs <- fmt.Sprintf("status %d %s", resp.StatusCode, raw)
				return
			}
			var body AdoptionVersionResponse
			ds.decode(t, resp, &body)
			results <- body.Version
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for msg := range errs {
		t.Fatal(msg)
	}
	seen := map[int]bool{}
	for v := range results {
		if seen[v] {
			t.Fatalf("duplicate version %d", v)
		}
		seen[v] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("versions %v", seen)
	}

	listed := ds.do(t, http.MethodGet, "/api/v3/products/"+productID+"/delivery-adoptions", nil, "")
	ds.mustStatus(t, listed, http.StatusOK)
	var list AdoptionListResponse
	ds.decode(t, listed, &list)
	if len(list.Items) != 2 || list.CurrentVersionID == nil {
		t.Fatalf("%+v", list)
	}
}

func (ds *deliveryServer) uploadExtraAsset(t *testing.T, productID string) string {
	t.Helper()
	return ds.uploadPNGAsset(t, productID, pngBytes(t, 24, 24), "extra.png")
}

func (ds *deliveryServer) uploadPNGAsset(t *testing.T, productID string, png []byte, filename string) string {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("images", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := ds.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-assets", &buf, w.FormDataContentType())
	ds.mustStatus(t, resp, http.StatusCreated)
	var body struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	ds.decode(t, resp, &body)
	if len(body.Items) != 1 {
		t.Fatalf("%+v", body)
	}
	return body.Items[0].ID
}

func (ds *deliveryServer) attachArtifactOnGraph(t *testing.T, productID, graphID, assetID string) {
	t.Helper()
	ds.attachArtifactOnGraphWithPayload(t, productID, graphID, assetID, qualifiedAdoptionArtifactPayload())
}

func (ds *deliveryServer) attachArtifactOnGraphWithPayload(
	t *testing.T, productID, graphID, assetID string, payload map[string]any,
) {
	t.Helper()
	nodeID := clockid.New()
	runID := clockid.New()
	nodeRunID := clockid.New()
	artifactID := clockid.New()
	digest := strings.Repeat("c", 64)
	hash := strings.Repeat("d", 64)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_nodes (
			id, graph_id, node_type, title, position_x, position_y, config_json, created_at, updated_at
		) VALUES ($1, $2, 'image_generation', '出图2', 40, 0, '{}'::jsonb, NOW(), NOW())
	`, nodeID, graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at, finished_at
		) VALUES ($1, $2, 'succeeded', 'graph', 1, '{}'::json, TRUE, NOW(), NOW())
	`, runID, graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_node_runs (
			id, graph_run_id, node_id, status, sort_order, started_at, finished_at
		) VALUES ($1, $2, $3, 'succeeded', 1, NOW(), NOW())
	`, nodeRunID, runID, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_artifacts (
			id, graph_id, node_id, node_run_id, artifact_type, schema_version, graph_revision,
			payload_json, payload_hash, input_digest, product_image_asset_id, created_at
		) VALUES ($1, $2, $3, $4, 'image', 3, 1, $5::jsonb, $6, $7, $8, NOW())
	`, artifactID, graphID, nodeID, nodeRunID, string(payloadJSON), hash, digest, assetID); err != nil {
		t.Fatal(err)
	}
	_ = productID
}

func TestDeliveryAdoptionRejectsUnqualifiedGraphArtifacts(t *testing.T) {
	ds := newDeliveryServer(t)
	spec := map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"}

	mustReject := func(name string, payload map[string]any, quality string, wantSubstr string) {
		t.Helper()
		created := ds.createProduct(t)
		productID := created.Product.ID
		assetID := created.CreatedAssets[0].ID
		ds.attachArtifactLineageWithPayload(t, productID, assetID, payload)
		resp := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
			"slots": []map[string]any{{
				"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
				"delivery_spec": spec, "quality_status": quality,
			}},
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s want 400 got %d %s", name, resp.StatusCode, raw)
		}
		raw, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(raw), wantSubstr) {
			t.Fatalf("%s body %s want substr %q", name, raw, wantSubstr)
		}
	}

	textFail := qualifiedAdoptionArtifactPayload()
	textFail["text_trace"] = map[string]any{"text_qualified": false}
	mustReject("fake pass + text_qualified=false", textFail, "pass", "文字追溯不合格")

	routeFail := qualifiedAdoptionArtifactPayload()
	routeFail["produce_route"] = map[string]any{
		"route": "subject_preserve", "route_qualified": false,
		"unresolved_items": []any{"保留主体路线质检失败"},
	}
	mustReject("fake pass + route_qualified=false", routeFail, "pass", "产图路线不合格")
	mustReject("unchecked + route_qualified=false", routeFail, "unchecked", "产图路线不合格")

	mustReject("pass without metadata", map[string]any{}, "pass", "缺少合格追溯元数据")

	created := ds.createProduct(t)
	productID := created.Product.ID
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifactLineageWithPayload(t, productID, assetID, qualifiedAdoptionArtifactPayload())
	ok := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
			"delivery_spec": spec, "quality_status": "pass",
		}},
	})
	ds.mustStatus(t, ok, http.StatusCreated)
	ok.Body.Close()

	uncheckedMissing := ds.createProduct(t)
	uncheckedAsset := uncheckedMissing.CreatedAssets[0].ID
	ds.attachArtifactLineageWithPayload(t, uncheckedMissing.Product.ID, uncheckedAsset, map[string]any{})
	uncheckedOK := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+uncheckedMissing.Product.ID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": uncheckedAsset,
			"delivery_spec": spec, "quality_status": "unchecked",
		}},
	})
	ds.mustStatus(t, uncheckedOK, http.StatusCreated)
	uncheckedOK.Body.Close()
}

func textTraceCapacityPayload() map[string]any {
	p := qualifiedAdoptionArtifactPayload()
	p["text_trace"] = map[string]any{
		"schema_version": 1, "text_qualified": true, "user_image_override": false,
		"fact_keys": []any{"capacity"},
		"entries": []any{
			map[string]any{"text": "600ml", "traced": true, "fact_keys": []any{"capacity"}},
		},
	}
	return p
}

func TestDeliveryAdoptionOCRMissingRejectsDeclaredPass(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	// 声明合格但成片无 600ml → OCR missing → 拒绝
	missingPNG, err := ocr.RenderTextPNG("Stainless Body", 400, 120, 28)
	if err != nil {
		t.Fatal(err)
	}
	assetID := ds.uploadPNGAsset(t, productID, missingPNG, "missing-capacity.png")
	ds.attachArtifactLineageWithPayload(t, productID, assetID, textTraceCapacityPayload())
	spec := map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"}
	resp := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
			"delivery_spec": spec, "quality_status": "pass",
		}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 400 got %d %s", resp.StatusCode, raw)
	}
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "文字 OCR 对照不合格") {
		t.Fatalf("body %s", raw)
	}
}

func TestDeliveryAdoptionOCRPassAllowsDeclaredPass(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	matchPNG, err := ocr.RenderTextPNG("600ml", 320, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	assetID := ds.uploadPNGAsset(t, productID, matchPNG, "capacity-ok.png")
	ds.attachArtifactLineageWithPayload(t, productID, assetID, textTraceCapacityPayload())
	spec := map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"}
	resp := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
			"delivery_spec": spec, "quality_status": "pass",
		}},
	})
	ds.mustStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
}

func TestDeliveryAdoptionOCRProductBodyInkAllowsPass(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	// 期望字 + 未声明字：旧硬闸会因残余墨迹拒绝；收窄后应可采用。
	pngBytes, err := ocr.RenderTextPNG("600ml  SALE", 480, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	assetID := ds.uploadPNGAsset(t, productID, pngBytes, "capacity-with-extra.png")
	ds.attachArtifactLineageWithPayload(t, productID, assetID, textTraceCapacityPayload())
	spec := map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"}
	resp := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
			"delivery_spec": spec, "quality_status": "pass",
		}},
	})
	ds.mustStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
}


func TestDeliveryAdoptionOCRRejectsEmptyExpectations(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	assetID := created.CreatedAssets[0].ID
	payload := qualifiedAdoptionArtifactPayload()
	payload["text_trace"] = map[string]any{
		"schema_version": 1, "text_qualified": true, "user_image_override": false,
		"fact_keys": []any{"capacity"}, "entries": []any{},
	}
	ds.attachArtifactLineageWithPayload(t, productID, assetID, payload)
	spec := map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"}
	resp := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": assetID,
			"delivery_spec": spec, "quality_status": "pass",
		}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 400 got %d %s", resp.StatusCode, raw)
	}
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "无可对照的文字期望") {
		t.Fatalf("body %s", raw)
	}
}

func TestAdoptionTextTraceNeedsOCR(t *testing.T) {
	if adoptionTextTraceNeedsOCR(nil) {
		t.Fatal("nil")
	}
	if adoptionTextTraceNeedsOCR(map[string]any{"fact_keys": []any{}, "entries": []any{}}) {
		t.Fatal("empty must skip OCR")
	}
	if !adoptionTextTraceNeedsOCR(map[string]any{"fact_keys": []any{"capacity"}}) {
		t.Fatal("fact_keys require OCR")
	}
	if !adoptionTextTraceNeedsOCR(map[string]any{
		"entries": []any{map[string]any{"text": "600ml"}},
	}) {
		t.Fatal("entry text require OCR")
	}
}
