package product

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func TestOperatorProductManagementUsesExplicitMerchantWithoutGeneration(t *testing.T) {
	ps := newProductServer(t)
	own := ps.createV2(t, "operator home product", nil, 1)
	foreign := seedForeignProductChain(t, ps, own.Product.ID)
	var target schema.Products
	if err := ps.db.Where("id = ?", foreign.productID).Take(&target).Error; err != nil {
		t.Fatal(err)
	}
	base := "/api/ops/merchants/" + target.MerchantID + "/products"
	for _, path := range []string{base, base + "/" + target.ID, base + "/" + target.ID + "/facts"} {
		resp := ps.do(t, http.MethodGet, path, nil, "")
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s %d %s", path, resp.StatusCode, raw)
		}
		resp.Body.Close()
	}
	bad := ps.do(t, http.MethodGet, base+"/"+own.Product.ID, nil, "")
	if bad.StatusCode != http.StatusNotFound {
		t.Fatalf("mixed merchant product accepted: %d", bad.StatusCode)
	}
	bad.Body.Close()
	var beforeJobs int64
	if err := ps.db.Model(&schema.AsyncDispatches{}).Count(&beforeJobs).Error; err != nil {
		t.Fatal(err)
	}
	edited := ps.doJSON(t, http.MethodPut, base+"/"+target.ID+"/facts", map[string]any{"name": "admin corrected name"})
	if edited.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(edited.Body)
		t.Fatalf("edit %d %s", edited.StatusCode, raw)
	}
	edited.Body.Close()
	var after schema.Products
	if err := ps.db.Where("id = ?", target.ID).Take(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.MerchantID != target.MerchantID || after.Name != "admin corrected name" {
		t.Fatalf("ownership/name %+v", after)
	}
	noGenerate := ps.doJSON(t, http.MethodPost, base+"/"+target.ID+"/generate", map[string]any{})
	if noGenerate.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected generation route %d", noGenerate.StatusCode)
	}
	noGenerate.Body.Close()
	var afterJobs int64
	if err := ps.db.Model(&schema.AsyncDispatches{}).Count(&afterJobs).Error; err != nil {
		t.Fatal(err)
	}
	if beforeJobs != afterJobs {
		t.Fatal("product management dispatched work")
	}
	// Ordinary accounts cannot acquire administrator authority by choosing this path.
	var ownRow schema.Products
	if err := ps.db.Where("id = ?", own.Product.ID).Take(&ownRow).Error; err != nil {
		t.Fatal(err)
	}
	var operator schema.Users
	if err := ps.db.Where("merchant_id = ? AND is_operator = TRUE", ownRow.MerchantID).Take(&operator).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Model(&schema.Users{}).Where("id = ?", operator.ID).Update("merchant_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ps.db.Model(&schema.Users{}).Where("id = ?", operator.ID).Update("merchant_id", ownRow.MerchantID)
	})
	withoutHome := ps.do(t, http.MethodGet, base, nil, "")
	if withoutHome.StatusCode != http.StatusOK {
		t.Fatalf("operator without home cannot manage target: %d", withoutHome.StatusCode)
	}
	withoutHome.Body.Close()
	ordinaryPath := ps.do(t, http.MethodGet, "/api/v2/products", nil, "")
	if ordinaryPath.StatusCode != http.StatusForbidden {
		t.Fatalf("operator without home gained ordinary product scope: %d", ordinaryPath.StatusCode)
	}
	ordinaryPath.Body.Close()
	if err := ps.db.Model(&schema.Users{}).Where("id = ?", operator.ID).Update("merchant_id", ownRow.MerchantID).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Model(&schema.Users{}).Where("merchant_id = ?", ownRow.MerchantID).Update("is_operator", false).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ps.db.Model(&schema.Users{}).Where("merchant_id = ?", ownRow.MerchantID).Update("is_operator", true)
	})
	denied := ps.do(t, http.MethodGet, base, nil, "")
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary account ops %d", denied.StatusCode)
	}
	denied.Body.Close()
	if err := ps.db.Model(&schema.Users{}).Where("merchant_id = ?", ownRow.MerchantID).Update("is_operator", true).Error; err != nil {
		t.Fatal(err)
	}
	ps.setSetting(t, "deletion_enabled", "true")
	deleted := ps.do(t, http.MethodDelete, base+"/"+target.ID, nil, "")
	if deleted.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(deleted.Body)
		t.Fatalf("delete %d %s", deleted.StatusCode, raw)
	}
	deleted.Body.Close()
	if _, err := ps.svc.Get(auth.WithMerchantID(context.Background(), ownRow.MerchantID), own.Product.ID); err != nil {
		t.Fatalf("operator deletion affected own product: %v", err)
	}
}
