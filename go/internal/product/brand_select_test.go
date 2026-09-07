package product

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func TestProductBrandSelectionSameMerchant(t *testing.T) {
	ps := newProductServer(t)
	productID := createBareProduct(t, ps, auth.MustDevMerchantID(t, ps.db), "选定品牌商品")
	brandID := createBareBrand(t, ps, auth.MustDevMerchantID(t, ps.db), "本商品牌")

	put := ps.doJSON(t, http.MethodPut, "/api/v3/products/"+productID+"/brand-selection", map[string]any{
		"brand_id": brandID,
	})
	if put.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(put.Body)
		put.Body.Close()
		t.Fatalf("put %d %s", put.StatusCode, raw)
	}
	var view BrandSelectionView
	ps.decode(t, put, &view)
	if view.BrandID == nil || *view.BrandID != brandID {
		t.Fatalf("selection %+v", view)
	}

	get := ps.do(t, http.MethodGet, "/api/v3/products/"+productID+"/brand-selection", nil, "")
	if get.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(get.Body)
		get.Body.Close()
		t.Fatalf("get %d %s", get.StatusCode, raw)
	}
	var got BrandSelectionView
	ps.decode(t, get, &got)
	if got.BrandID == nil || *got.BrandID != brandID {
		t.Fatalf("get selection %+v", got)
	}

	detailResp := ps.do(t, http.MethodGet, "/api/v2/products/"+productID, nil, "")
	var detail Detail
	ps.decode(t, detailResp, &detail)
	if detail.BrandID == nil || *detail.BrandID != brandID {
		t.Fatalf("detail brand_id %+v", detail.BrandID)
	}

	cleared := ps.doJSON(t, http.MethodPut, "/api/v3/products/"+productID+"/brand-selection", map[string]any{
		"brand_id": nil,
	})
	if cleared.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(cleared.Body)
		cleared.Body.Close()
		t.Fatalf("clear %d %s", cleared.StatusCode, raw)
	}
	var empty BrandSelectionView
	ps.decode(t, cleared, &empty)
	if empty.BrandID != nil {
		t.Fatalf("expected cleared brand %+v", empty)
	}
}

func TestProductBrandSelectionCrossMerchantDenied(t *testing.T) {
	ps := newProductServer(t)
	dual := auth.SeedDualMerchants(t, ps.db, ps.client, ps.srv.URL)
	productID := createBareProduct(t, ps, dual.MerchantAID, "商家A商品")
	brandB := createBareBrand(t, ps, dual.MerchantBID, "商家B品牌")

	rawBody, err := json.Marshal(map[string]any{"brand_id": brandB})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, ps.srv.URL+"/api/v3/products/"+productID+"/brand-selection", bytes.NewReader(rawBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, c := range dual.CookiesA {
		req.AddCookie(c)
	}
	cross, err := ps.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(cross.Body)
	cross.Body.Close()
	if cross.StatusCode != http.StatusNotFound {
		t.Fatalf("cross brand %d %s", cross.StatusCode, raw)
	}
	var body struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Detail != auth.CrossMerchantDetail {
		t.Fatalf("detail=%q", body.Detail)
	}
}

func createBareProduct(t *testing.T, ps *productServer, merchantID, name string) string {
	t.Helper()
	id := clockid.New()
	now := time.Now().UTC()
	if err := ps.db.Create(&schema.Products{
		ID: id, MerchantID: merchantID, Name: name, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func createBareBrand(t *testing.T, ps *productServer, merchantID, name string) string {
	t.Helper()
	id := clockid.New()
	now := time.Now().UTC()
	if err := ps.db.Create(&schema.Brands{
		ID: id, MerchantID: merchantID, Name: name, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return id
}
