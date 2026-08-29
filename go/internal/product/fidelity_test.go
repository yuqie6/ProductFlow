package product

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func fidelityPayload(expected int, key string) map[string]any {
	return map[string]any{
		"expected_latest_version": expected,
		"idempotency_key":         key,
		"shape_fidelity":          "pass",
		"color_material_fidelity": "fail",
		"logo_text_legibility":    "not_applicable",
		"text_policy_compliance":  "pass",
		"notes":                   "人工复核",
	}
}

func fidelityPath(productID, assetID string) string {
	return "/api/v3/products/" + productID + "/image-assets/" + assetID + "/fidelity-checks"
}

func TestFidelityCheckListCreateReplayAndConflicts(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "人工检查商品", nil, 1)
	path := fidelityPath(created.Product.ID, created.CreatedAssets[0].ID)

	empty := ps.do(t, http.MethodGet, path, nil, "")
	if empty.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(empty.Body)
		empty.Body.Close()
		t.Fatalf("list empty %d %s", empty.StatusCode, raw)
	}
	var listed FidelityCheckList
	ps.decode(t, empty, &listed)
	if listed.LatestVersion != 0 || listed.ProductID != created.Product.ID || listed.AssetID != created.CreatedAssets[0].ID || len(listed.Items) != 0 {
		t.Fatalf("%+v", listed)
	}

	first := ps.doJSON(t, http.MethodPost, path, fidelityPayload(0, "check-1"))
	if first.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(first.Body)
		first.Body.Close()
		t.Fatalf("create %d %s", first.StatusCode, raw)
	}
	var createdCheck FidelityCheck
	ps.decode(t, first, &createdCheck)
	if createdCheck.Version != 1 || createdCheck.CheckedBy != "administrator" || len(createdCheck.RequestHash) != 64 {
		t.Fatalf("%+v", createdCheck)
	}
	if createdCheck.ShapeFidelity != "pass" || createdCheck.ColorMaterialFidelity != "fail" ||
		createdCheck.LogoTextLegibility != "not_applicable" || createdCheck.TextPolicyCompliance != "pass" {
		t.Fatalf("%+v", createdCheck)
	}
	if createdCheck.CreatedAt.IsZero() {
		t.Fatal("created_at missing")
	}
	encoded, _ := json.Marshal(createdCheck.CreatedAt)
	if _, err := time.Parse(`"`+time.RFC3339Nano+`"`, string(encoded)); err != nil {
		if _, err2 := time.Parse(`"`+time.RFC3339+`"`, string(encoded)); err2 != nil {
			t.Fatalf("created_at json %s", encoded)
		}
	}

	got := ps.do(t, http.MethodGet, path, nil, "")
	if got.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(got.Body)
		got.Body.Close()
		t.Fatalf("list %d %s", got.StatusCode, raw)
	}
	ps.decode(t, got, &listed)
	if listed.LatestVersion != 1 || len(listed.Items) != 1 || listed.Items[0].Version != 1 || listed.Items[0].ID != createdCheck.ID {
		t.Fatalf("%+v", listed)
	}

	replay := ps.doJSON(t, http.MethodPost, path, fidelityPayload(0, "check-1"))
	if replay.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(replay.Body)
		replay.Body.Close()
		t.Fatalf("replay %d %s", replay.StatusCode, raw)
	}
	var replayed FidelityCheck
	ps.decode(t, replay, &replayed)
	if replayed.ID != createdCheck.ID {
		t.Fatalf("replay id %s want %s", replayed.ID, createdCheck.ID)
	}

	conflictBody := fidelityPayload(0, "check-1")
	conflictBody["shape_fidelity"] = "fail"
	conflict := ps.doJSON(t, http.MethodPost, path, conflictBody)
	if conflict.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(conflict.Body)
		conflict.Body.Close()
		t.Fatalf("hash conflict %d %s", conflict.StatusCode, raw)
	}
	if detail := decodeDetail(t, ps, conflict); detail != fidelityIdempotencyConflict {
		t.Fatalf("hash conflict detail %q", detail)
	}

	stale := ps.doJSON(t, http.MethodPost, path, fidelityPayload(0, "stale"))
	if stale.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(stale.Body)
		stale.Body.Close()
		t.Fatalf("stale %d %s", stale.StatusCode, raw)
	}
	if detail := decodeDetail(t, ps, stale); detail != fidelityVersionConflict {
		t.Fatalf("stale detail %q", detail)
	}
}

func TestFidelityCheckUnknownProduct(t *testing.T) {
	ps := newProductServer(t)
	resp := ps.do(t, http.MethodGet, fidelityPath("00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"), nil, "")
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("got %d %s", resp.StatusCode, raw)
	}
	if detail := decodeDetail(t, ps, resp); detail != "商品不存在" {
		t.Fatalf("detail %q", detail)
	}
}

func TestDeleteProductWithFidelityCheck(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "保真删除商品", nil, 1)
	path := fidelityPath(created.Product.ID, created.CreatedAssets[0].ID)
	posted := ps.doJSON(t, http.MethodPost, path, fidelityPayload(0, "delete-check"))
	if posted.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(posted.Body)
		posted.Body.Close()
		t.Fatalf("create %d %s", posted.StatusCode, raw)
	}
	posted.Body.Close()

	ps.enableDeletion(t)
	deleted := ps.do(t, http.MethodDelete, "/api/v2/products/"+created.Product.ID, nil, "")
	if deleted.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(deleted.Body)
		deleted.Body.Close()
		t.Fatalf("delete %d %s", deleted.StatusCode, raw)
	}
	deleted.Body.Close()

	missing := ps.do(t, http.MethodGet, "/api/v2/products/"+created.Product.ID, nil, "")
	if missing.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(missing.Body)
		missing.Body.Close()
		t.Fatalf("get deleted %d %s", missing.StatusCode, raw)
	}
	missing.Body.Close()
}

func decodeDetail(t *testing.T, ps *productServer, resp *http.Response) string {
	t.Helper()
	var body map[string]any
	ps.decode(t, resp, &body)
	detail, _ := body["detail"].(string)
	return detail
}
