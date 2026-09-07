package product

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// B1：本商 list 可见；他商 UUID 详情统一 404。
func TestMerchantRootOwnershipProductIsolation(t *testing.T) {
	ps := newProductServer(t)
	mine := ps.createV2(t, "本商家商品", nil, 1)

	foreignMerchant := schema.Merchants{
		ID:        clockid.New(),
		Name:      "夹具他商",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := ps.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ps.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})

	foreignProductID := clockid.New()
	now := time.Now().UTC()
	if err := ps.db.Create(&schema.Products{
		ID:         foreignProductID,
		MerchantID: foreignMerchant.ID,
		Name:       "他商家商品",
		CreatedAt:  now,
		UpdatedAt:  now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ps.db.Where("id = ?", foreignProductID).Delete(&schema.Products{}).Error
	})

	listResp := ps.do(t, http.MethodGet, "/api/v2/products?page=1&page_size=50", nil, "")
	if listResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(listResp.Body)
		listResp.Body.Close()
		t.Fatalf("list %d %s", listResp.StatusCode, raw)
	}
	var listed ListResponse
	ps.decode(t, listResp, &listed)
	for _, item := range listed.Items {
		if item.ID == foreignProductID {
			t.Fatal("list leaked foreign merchant product")
		}
	}
	foundMine := false
	for _, item := range listed.Items {
		if item.ID == mine.Product.ID {
			foundMine = true
			break
		}
	}
	if !foundMine {
		t.Fatal("list missing own product")
	}

	cross := ps.do(t, http.MethodGet, "/api/v2/products/"+foreignProductID, nil, "")
	defer cross.Body.Close()
	if cross.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(cross.Body)
		t.Fatalf("cross-merchant get want 404 got %d %s", cross.StatusCode, raw)
	}

	own := ps.do(t, http.MethodGet, "/api/v2/products/"+mine.Product.ID, nil, "")
	if own.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(own.Body)
		own.Body.Close()
		t.Fatalf("own get %d %s", own.StatusCode, raw)
	}
	own.Body.Close()

	devMerchant := auth.MustDevMerchantID(t, ps.db)
	var row schema.Products
	if err := ps.db.WithContext(context.Background()).Select("merchant_id").Where("id = ?", mine.Product.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.MerchantID != devMerchant {
		t.Fatalf("created product merchant_id=%s want %s", row.MerchantID, devMerchant)
	}
}
