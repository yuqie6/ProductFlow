package product

import (
	"context"
	"net/http"
	"testing"
)

func TestUnauthorizedFacts(t *testing.T) {
	ps := newProductServer(t)
	req, err := http.NewRequest(http.MethodGet, ps.srv.URL+"/api/v3/products/missing/facts", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ps.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestFactsProductMissing(t *testing.T) {
	ps := newProductServer(t)
	resp := ps.do(t, http.MethodGet, "/api/v3/products/00000000-0000-4000-8000-000000000001/facts", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCoverRejectsForeignAsset(t *testing.T) {
	ps := newProductServer(t)
	a := ps.createV2(t, "商品甲", nil, 1)
	b := ps.createV2(t, "商品乙", nil, 1)
	resp := ps.doJSON(t, http.MethodPut, "/api/v2/products/"+a.Product.ID+"/cover", map[string]any{
		"asset_id": b.CreatedAssets[0].ID,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	ps.decode(t, resp, &body)
	if body["detail"] != "封面图片不属于当前商品" {
		t.Fatalf("%+v", body)
	}
}

func TestStaleAssetRenameAndEmptyFolderName(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "改名商品", nil, 1)
	stale := ps.doJSON(t, http.MethodPatch, "/api/v2/products/"+created.Product.ID+"/image-assets/"+created.CreatedAssets[0].ID, map[string]any{
		"expected_display_name": "旧名字",
		"display_name":          "新名字",
	})
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("got %d", stale.StatusCode)
	}
	stale.Body.Close()
	empty := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-folders", map[string]any{"name": "   "})
	if empty.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", empty.StatusCode)
	}
	empty.Body.Close()
}

func TestSourceDirectoryAndGalleryAssetDetail(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "来源目录", nil, 1)
	page, err := ps.svc.ListGalleryAssets(context.Background(), created.Product.ID, GalleryListInput{DirectoryKind: "source", DirectoryKey: "upload", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("%+v", page)
	}
	detail, err := ps.svc.GetGalleryAsset(context.Background(), created.Product.ID, created.CreatedAssets[0].ID)
	if err != nil || detail.ID != created.CreatedAssets[0].ID {
		t.Fatalf("%+v %v", detail, err)
	}
	httpDetail := ps.do(t, http.MethodGet, "/api/v2/products/"+created.Product.ID+"/image-assets/"+created.CreatedAssets[0].ID, nil, "")
	if httpDetail.StatusCode != http.StatusOK {
		t.Fatalf("got %d", httpDetail.StatusCode)
	}
	httpDetail.Body.Close()
}

func TestArchiveRejectsEmptySelection(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "打包校验", nil, 1)
	resp := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-assets/download-archive", map[string]any{
		"asset_ids": []string{},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestParseUpdateFactsDetectsProvidedVersionKeys(t *testing.T) {
	in, err := parseUpdateFacts([]byte(`{"expected_fact_set_version_id":null,"name":"x"}`))
	if err != nil || !in.ExpectedVersionProvided || in.Name == nil || *in.Name != "x" {
		t.Fatalf("%+v %v", in, err)
	}
	if in.ExpectedFactSetVersionID != nil {
		t.Fatal("null id should stay nil")
	}
}

func TestParseUpdateFactsRejectsUnknownFieldsAndInvalidEnums(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"name":"x","foo":1}`,
		`{"facts":[{"key":"a","value":1,"extra":true}]}`,
		`{"facts":[{"key":"a","value":1,"source_type":"nope"}]}`,
		`{"facts":[{"key":"a","value":1,"status":"maybe"}]}`,
		`{"facts":[{"key":"a"}]}`,
		`{"expected_fact_version":0}`,
		`{"expected_fact_set_version_id":""}`,
		`{"name":"x"}{}`,
		``,
	}
	for _, raw := range cases {
		if _, err := parseUpdateFacts([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	in, err := parseUpdateFacts([]byte(`{"facts":[{"key":"material","value":"钢","source_type":"agent_inference","status":"observed"}]}`))
	if err != nil || in.Facts == nil || len(*in.Facts) != 1 {
		t.Fatalf("%+v %v", in, err)
	}
	got := (*in.Facts)[0]
	if got["source_type"] != "agent_inference" || got["status"] != "observed" {
		t.Fatalf("%+v", got)
	}
}

func TestNormalizeFolderAndDisplayNames(t *testing.T) {
	if _, err := normalizeFolderName(""); err == nil {
		t.Fatal("empty folder")
	}
	if _, err := normalizeDisplayName("  "); err == nil {
		t.Fatal("empty display")
	}
	name, err := normalizeFolderName("  精选  ")
	if err != nil || name != "精选" {
		t.Fatalf("%q %v", name, err)
	}
}

func TestCleanFilenameStripsInvalidChars(t *testing.T) {
	if got := cleanFilename(`a/b:c*.png`, "image"); got == `a/b:c*.png` {
		t.Fatalf("%q", got)
	}
	if got := cleanFilename("   ", "image"); got != "image" {
		t.Fatalf("%q", got)
	}
}
