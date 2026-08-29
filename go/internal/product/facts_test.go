package product

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestNormalizeFactPayloadAndDuplicates(t *testing.T) {
	got, err := normalizeFactPayload(map[string]any{"key": "  material  ", "value": "钢"})
	if err != nil {
		t.Fatal(err)
	}
	if got["key"] != "material" || got["source_type"] != "user" || got["status"] != "confirmed" {
		t.Fatalf("%+v", got)
	}
	if _, err := normalizeFactPayload(map[string]any{"key": ""}); err == nil {
		t.Fatal("empty key")
	}
	_, err = normalizeFactMaps([]map[string]any{
		{"key": "Material", "value": "1"},
		{"key": "material", "value": "2"},
	})
	if err == nil {
		t.Fatal("duplicate keys")
	}
	var e apperr.Error
	if !errorAs(err, &e) || e.Detail != "商品事实 key 不能重复" {
		t.Fatalf("%v", err)
	}
}

func errorAs(err error, target *apperr.Error) bool {
	if err == nil {
		return false
	}
	got, ok := err.(apperr.Error)
	if !ok {
		return false
	}
	*target = got
	return true
}

func TestFactsHTTPCreatesImmutableVersions(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "事实商品", map[string]string{"category": "收纳", "price": "12.00"}, 1)

	resp := ps.do(t, http.MethodGet, "/api/v3/products/"+created.Product.ID+"/facts", nil, "")
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get facts %d %s", resp.StatusCode, raw)
	}
	var initial FactsResponse
	ps.decode(t, resp, &initial)
	if initial.CurrentFactSetVersionID != nil {
		t.Fatalf("v2 出生不应写 fact: %+v", initial)
	}

	first := ps.doJSON(t, http.MethodPut, "/api/v3/products/"+created.Product.ID+"/facts", map[string]any{
		"name":        "事实商品新名",
		"category":    "厨房",
		"price":       "13.50",
		"source_note": "用户维护",
		"facts":       []map[string]any{{"key": "material", "value": "钢"}},
	})
	if first.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(first.Body)
		first.Body.Close()
		t.Fatalf("first put %d %s", first.StatusCode, raw)
	}
	var firstPayload FactsResponse
	ps.decode(t, first, &firstPayload)
	if firstPayload.CurrentFactVersion == nil || *firstPayload.CurrentFactVersion != 1 {
		t.Fatalf("%+v", firstPayload)
	}
	if len(firstPayload.Facts) != 1 || firstPayload.Facts[0].Key != "material" {
		t.Fatalf("%+v", firstPayload.Facts)
	}
	if firstPayload.Product.Name != "事实商品新名" {
		t.Fatalf("name %s", firstPayload.Product.Name)
	}

	second := ps.doJSON(t, http.MethodPut, "/api/v3/products/"+created.Product.ID+"/facts", map[string]any{
		"expected_fact_set_version_id": firstPayload.CurrentFactSetVersionID,
		"facts":                        []map[string]any{{"key": "material", "value": "不锈钢"}},
	})
	if second.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(second.Body)
		second.Body.Close()
		t.Fatalf("second put %d %s", second.StatusCode, raw)
	}
	var secondPayload FactsResponse
	ps.decode(t, second, &secondPayload)
	if secondPayload.CurrentFactVersion == nil || *secondPayload.CurrentFactVersion != 2 {
		t.Fatalf("%+v", secondPayload)
	}
	if secondPayload.CurrentFactSetVersionID == nil || *secondPayload.CurrentFactSetVersionID == *firstPayload.CurrentFactSetVersionID {
		t.Fatal("version id should change")
	}

	stale := ps.doJSON(t, http.MethodPut, "/api/v3/products/"+created.Product.ID+"/facts", map[string]any{
		"expected_fact_set_version_id": firstPayload.CurrentFactSetVersionID,
		"facts":                        []map[string]any{{"key": "material", "value": "铝"}},
	})
	if stale.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(stale.Body)
		stale.Body.Close()
		t.Fatalf("stale %d %s", stale.StatusCode, raw)
	}
	stale.Body.Close()

	missingExpected := ps.doJSON(t, http.MethodPut, "/api/v3/products/"+created.Product.ID+"/facts", map[string]any{
		"facts": []map[string]any{{"key": "material", "value": "铝"}},
	})
	if missingExpected.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(missingExpected.Body)
		missingExpected.Body.Close()
		t.Fatalf("missing expected %d %s", missingExpected.StatusCode, raw)
	}
	missingExpected.Body.Close()

	latest := ps.do(t, http.MethodGet, "/api/v3/products/"+created.Product.ID+"/facts", nil, "")
	var latestPayload FactsResponse
	ps.decode(t, latest, &latestPayload)
	if latestPayload.Facts[0].Value != "不锈钢" {
		t.Fatalf("%+v", latestPayload.Facts[0].Value)
	}
}

func TestProductListUsesSnakeCaseJSON(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "列表商品", nil, 1)
	resp := ps.do(t, http.MethodGet, "/api/v2/products", nil, "")
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list %d %s", resp.StatusCode, raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	items, _ := payload["items"].([]any)
	if len(items) == 0 {
		t.Fatal("empty list")
	}
	found := false
	for _, item := range items {
		row, _ := item.(map[string]any)
		if row["id"] == created.Product.ID {
			found = true
			if _, ok := row["cover_image_asset_id"]; !ok {
				t.Fatalf("missing snake_case keys: %s", raw)
			}
			if _, ok := row["ID"]; ok {
				t.Fatalf("exported Go names leaked: %s", raw)
			}
		}
	}
	if !found {
		t.Fatalf("created product missing: %s", raw)
	}
}
