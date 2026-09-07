package product

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestNormalizeFactPayloadAndDuplicates(t *testing.T) {
	got, err := normalizeFactPayload(map[string]any{"key": "  material  ", "value": "钢"})
	if err != nil {
		t.Fatal(err)
	}
	if got["key"] != "material" || got["source_type"] != "user" || got["status"] != "confirmed" || got["layer"] != "performance" {
		t.Fatalf("%+v", got)
	}
	if _, err := normalizeFactPayload(map[string]any{"key": ""}); err == nil {
		t.Fatal("empty key")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "source_type": "nope"}); err == nil {
		t.Fatal("invalid source_type")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "status": "maybe"}); err == nil {
		t.Fatal("invalid status")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "layer": "brand"}); err == nil {
		t.Fatal("invalid layer")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "source_type": nil}); err == nil {
		t.Fatal("explicit null source_type")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "status": nil}); err == nil {
		t.Fatal("explicit null status")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "layer": nil}); err == nil {
		t.Fatal("explicit null layer")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "requires_confirmation": nil}); err == nil {
		t.Fatal("explicit null requires_confirmation")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "evidence_asset_ids": nil}); err == nil {
		t.Fatal("explicit null evidence_asset_ids")
	}
	if _, err := normalizeFactPayload(map[string]any{"key": "material", "value": "钢", "conflicts": nil}); err == nil {
		t.Fatal("explicit null conflicts")
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

func TestFactsHTTPRejectsExplicitNullOnNonNullableFields(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "空值事实", map[string]string{"category": "收纳", "price": "12.00"}, 1)
	path := "/api/v3/products/" + created.Product.ID + "/facts"
	for _, fact := range []map[string]any{
		{"key": "material", "value": "钢", "source_type": nil},
		{"key": "material", "value": "钢", "status": nil},
		{"key": "material", "value": "钢", "layer": nil},
		{"key": "material", "value": "钢", "requires_confirmation": nil},
		{"key": "material", "value": "钢", "evidence_asset_ids": nil},
		{"key": "material", "value": "钢", "conflicts": nil},
	} {
		resp := ps.doJSON(t, http.MethodPut, path, map[string]any{"facts": []map[string]any{fact}})
		if resp.StatusCode != http.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("fact %+v got %d %s", fact, resp.StatusCode, raw)
		}
		resp.Body.Close()
	}
	ok := ps.doJSON(t, http.MethodPut, path, map[string]any{
		"facts": []map[string]any{{"key": "material", "value": "钢"}},
	})
	if ok.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(ok.Body)
		ok.Body.Close()
		t.Fatalf("absent defaults %d %s", ok.StatusCode, raw)
	}
	ok.Body.Close()
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

func TestProductListRejectsInvalidQuery(t *testing.T) {
	ps := newProductServer(t)
	_ = ps.createV2(t, "列表校验", nil, 1)
	cases := []string{
		"/api/v2/products?page=0",
		"/api/v2/products?page=abc",
		"/api/v2/products?page_size=0",
		"/api/v2/products?page_size=101",
		"/api/v2/products?sort=created_asc",
		"/api/v2/products?q=" + strings.Repeat("q", 101),
	}
	for _, path := range cases {
		resp := ps.do(t, http.MethodGet, path, nil, "")
		if resp.StatusCode != http.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("%s got %d %s", path, resp.StatusCode, raw)
		}
		resp.Body.Close()
	}
	ok := ps.do(t, http.MethodGet, "/api/v2/products?page=1&page_size=20&sort=name_asc", nil, "")
	if ok.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(ok.Body)
		ok.Body.Close()
		t.Fatalf("valid list %d %s", ok.StatusCode, raw)
	}
	ok.Body.Close()
}


func TestFactLayerGatePositiveAndNegativeFixtures(t *testing.T) {
	t.Parallel()

	confirmedCapacity, err := normalizeFactPayload(map[string]any{
		"key": "capacity", "value": "600ml", "source_type": "user", "status": "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmedCapacity["source_type"] != "user" || confirmedCapacity["status"] != "confirmed" || confirmedCapacity["layer"] != "performance" {
		t.Fatalf("user capacity: %+v", confirmedCapacity)
	}
	if boolOr(confirmedCapacity["requires_confirmation"]) {
		t.Fatal("user confirmed capacity should not require confirmation")
	}

	imageMaterial, err := normalizeFactPayload(map[string]any{
		"key": "material", "value": "不锈钢", "source_type": "image_observation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if imageMaterial["source_type"] != "image_observation" || imageMaterial["status"] != "observed" {
		t.Fatalf("image material defaults: %+v", imageMaterial)
	}
	if !boolOr(imageMaterial["requires_confirmation"]) {
		t.Fatal("image observation must stay pending")
	}

	if _, err := normalizeFactPayload(map[string]any{
		"key": "insulation", "value": "保温 24h", "source_type": "agent_inference", "status": "confirmed",
	}); err == nil {
		t.Fatal("agent inference confirmed performance must reject")
	} else {
		var e apperr.Error
		if !errorAs(err, &e) || e.Detail != "未确认的推断或图观事实不能升为已确认性能事实" {
			t.Fatalf("%v", err)
		}
	}

	if _, err := normalizeFactPayload(map[string]any{
		"key": "material", "value": "明星同款", "source_type": "user", "status": "confirmed",
	}); err == nil {
		t.Fatal("marketing tone in performance must reject")
	} else {
		var e apperr.Error
		if !errorAs(err, &e) || e.Detail != "营销口吻不能写入性能事实" {
			t.Fatalf("%v", err)
		}
	}

	marketing, err := normalizeFactPayload(map[string]any{
		"key": "selling_point", "value": "明星同款", "source_type": "user", "status": "user_declared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if marketing["layer"] != "marketing" {
		t.Fatalf("selling_point layer: %+v", marketing)
	}

	pendingInference, err := normalizeFactPayload(map[string]any{
		"key": "insulation", "value": "保温 24h", "source_type": "agent_inference", "status": "observed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !boolOr(pendingInference["requires_confirmation"]) || pendingInference["status"] != "observed" {
		t.Fatalf("pending inference: %+v", pendingInference)
	}

	conflicted, err := normalizeFactPayload(map[string]any{
		"key": "capacity", "value": "500ml", "status": "conflicted",
		"conflicts": []any{map[string]any{"value": "600ml", "source_type": "user"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !boolOr(conflicted["requires_confirmation"]) {
		t.Fatal("conflicted must require confirmation")
	}
	if _, err := normalizeFactPayload(map[string]any{
		"key": "capacity", "value": "500ml", "status": "conflicted",
	}); err == nil {
		t.Fatal("conflicted without conflicts must reject")
	}
}

func TestFactsHTTPLayerGateFixtures(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "分层闸", map[string]string{"category": "杯壶", "price": "39.00"}, 1)
	path := "/api/v3/products/" + created.Product.ID + "/facts"

	ok := ps.doJSON(t, http.MethodPut, path, map[string]any{
		"facts": []map[string]any{
			{"key": "capacity", "value": "600ml", "source_type": "user", "status": "confirmed"},
			{"key": "material", "value": "不锈钢", "source_type": "image_observation"},
			{"key": "selling_point", "value": "轻量杯身", "layer": "marketing", "source_type": "user", "status": "user_declared"},
		},
	})
	if ok.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(ok.Body)
		ok.Body.Close()
		t.Fatalf("positive put %d %s", ok.StatusCode, raw)
	}
	var payload FactsResponse
	ps.decode(t, ok, &payload)
	byKey := map[string]Fact{}
	for _, fact := range payload.Facts {
		byKey[fact.Key] = fact
	}
	if byKey["capacity"].Status != "confirmed" || byKey["capacity"].SourceType != "user" || byKey["capacity"].Layer != "performance" {
		t.Fatalf("capacity %+v", byKey["capacity"])
	}
	if byKey["material"].SourceType != "image_observation" || byKey["material"].Status != "observed" || !byKey["material"].RequiresConfirmation {
		t.Fatalf("material %+v", byKey["material"])
	}
	if byKey["selling_point"].Layer != "marketing" {
		t.Fatalf("selling_point %+v", byKey["selling_point"])
	}

	rejectInference := ps.doJSON(t, http.MethodPut, path, map[string]any{
		"expected_fact_set_version_id": payload.CurrentFactSetVersionID,
		"facts": []map[string]any{
			{"key": "insulation", "value": "保温 24h", "source_type": "agent_inference", "status": "confirmed"},
		},
	})
	if rejectInference.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(rejectInference.Body)
		rejectInference.Body.Close()
		t.Fatalf("inference confirmed %d %s", rejectInference.StatusCode, raw)
	}
	rejectInference.Body.Close()

	rejectMarketing := ps.doJSON(t, http.MethodPut, path, map[string]any{
		"expected_fact_set_version_id": payload.CurrentFactSetVersionID,
		"facts": []map[string]any{
			{"key": "material", "value": "明星同款", "source_type": "user", "status": "confirmed"},
		},
	})
	if rejectMarketing.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(rejectMarketing.Body)
		rejectMarketing.Body.Close()
		t.Fatalf("marketing performance %d %s", rejectMarketing.StatusCode, raw)
	}
	rejectMarketing.Body.Close()
}

func TestFactsImpactPreviewHTTP(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "影响预览", map[string]string{"category": "杯壶", "price": "39.00"}, 1)
	path := "/api/v3/products/" + created.Product.ID + "/facts"
	first := ps.doJSON(t, http.MethodPut, path, map[string]any{
		"facts": []map[string]any{{"key": "capacity", "value": "500ml", "source_type": "user", "status": "confirmed"}},
	})
	if first.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(first.Body)
		first.Body.Close()
		t.Fatalf("seed %d %s", first.StatusCode, raw)
	}
	var seeded FactsResponse
	ps.decode(t, first, &seeded)

	preview := ps.doJSON(t, http.MethodPost, path+"/impact-preview", map[string]any{
		"facts": []map[string]any{{"key": "capacity", "value": "600ml", "source_type": "user", "status": "confirmed"}},
	})
	if preview.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(preview.Body)
		preview.Body.Close()
		t.Fatalf("preview %d %s", preview.StatusCode, raw)
	}
	var impact FactsImpactPreviewResponse
	ps.decode(t, preview, &impact)
	if len(impact.ChangedFactKeys) != 1 || impact.ChangedFactKeys[0] != "capacity" {
		t.Fatalf("changed %+v", impact.ChangedFactKeys)
	}
	// v2 无图出生：无工作流时 nodes 为空，不得假装全图受影响。
	if len(impact.Nodes) != 0 {
		t.Fatalf("no-graph nodes %+v", impact.Nodes)
	}

	adopt := ps.doJSON(t, http.MethodPut, path, map[string]any{
		"expected_fact_set_version_id": seeded.CurrentFactSetVersionID,
		"facts":                        []map[string]any{{"key": "capacity", "value": "600ml", "source_type": "user", "status": "confirmed"}},
		"update_node_ids":              []string{},
	})
	if adopt.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(adopt.Body)
		adopt.Body.Close()
		t.Fatalf("adopt %d %s", adopt.StatusCode, raw)
	}
	var adopted FactsUpdateResponse
	ps.decode(t, adopt, &adopted)
	if adopted.CurrentFactVersion == nil || *adopted.CurrentFactVersion != 2 {
		t.Fatalf("%+v", adopted)
	}
	if !adopted.DidAdoptFactSet && adopted.AdoptedFactSetID == nil {
		// 无图时 DidAdoptFactSet 可为 false，但仍应写出新版本。
	}
}

