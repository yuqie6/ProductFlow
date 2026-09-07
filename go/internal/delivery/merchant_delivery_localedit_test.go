package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// B6：本商 job/export/adopt 成功；跨商 job/product/asset 拒绝（404 CrossMerchantDetail）。
func TestMerchantDeliveryIsolation(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifact(t, productID, assetID)

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
	if job.Status != "succeeded" || job.ResultAsset == nil {
		t.Fatalf("%+v", job)
	}

	listed := ds.do(t, http.MethodGet, "/api/v2/product-image-assets/"+assetID+"/renditions", nil, "")
	ds.mustStatus(t, listed, http.StatusOK)
	listed.Body.Close()

	exported := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-exports", map[string]any{
		"rendition_job_ids": []string{job.ID},
	})
	ds.mustStatus(t, exported, http.StatusOK)
	exported.Body.Close()

	adopt := ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-adoptions", map[string]any{
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "image_type_key": "hero",
			"source_asset_id": assetID, "delivery_spec": map[string]any{
				"width": 64, "height": 64, "format": "png", "fit": "contain",
			}, "quality_status": "pass",
		}},
	})
	ds.mustStatus(t, adopt, http.StatusCreated)
	var version AdoptionVersionResponse
	ds.decode(t, adopt, &version)

	current := ds.do(t, http.MethodGet, "/api/v3/products/"+productID+"/delivery-adoptions/current", nil, "")
	ds.mustStatus(t, current, http.StatusOK)
	current.Body.Close()

	foreign := seedForeignDelivery(t, ds)

	assertCross404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
		var body struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Detail != auth.CrossMerchantDetail {
			t.Fatalf("%s detail=%q want %q body=%s", name, body.Detail, auth.CrossMerchantDetail, raw)
		}
	}

	var beforeJobs int64
	if err := ds.db.Model(&schema.DeliveryRenditionJobs{}).Where("product_id = ?", foreign.productID).Count(&beforeJobs).Error; err != nil {
		t.Fatal(err)
	}
	var beforeAdoptions int64
	if err := ds.db.Model(&schema.DeliveryAdoptionVersions{}).Where("product_id = ?", foreign.productID).Count(&beforeAdoptions).Error; err != nil {
		t.Fatal(err)
	}

	assertCross404("foreign asset create", ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+foreign.assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	}))
	assertCross404("foreign asset list", ds.do(t, http.MethodGet, "/api/v2/product-image-assets/"+foreign.assetID+"/renditions", nil, ""))
	assertCross404("foreign job get", ds.do(t, http.MethodGet, "/api/v2/delivery-rendition-jobs/"+foreign.jobID, nil, ""))
	assertCross404("foreign job retry", ds.do(t, http.MethodPost, "/api/v2/delivery-rendition-jobs/"+foreign.jobID+"/retry", nil, ""))
	assertCross404("foreign export", ds.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/delivery-exports", map[string]any{
		"rendition_job_ids": []string{foreign.jobID},
	}))
	assertCross404("foreign adoption list", ds.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/delivery-adoptions", nil, ""))
	assertCross404("foreign adoption create", ds.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/delivery-adoptions", map[string]any{
		"acknowledge_quality_warnings": true,
		"slots": []map[string]any{{
			"slot_key": "hero-1", "sort_order": 0, "source_asset_id": foreign.assetID,
			"delivery_spec":  map[string]any{"width": 64, "height": 64, "format": "png", "fit": "contain"},
			"quality_status": "pass",
		}},
	}))
	assertCross404("foreign adoption get", ds.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/delivery-adoptions/"+foreign.adoptionID, nil, ""))
	assertCross404("own export foreign job", ds.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/delivery-exports", map[string]any{
		"rendition_job_ids": []string{foreign.jobID},
	}))

	var afterJobs int64
	if err := ds.db.Model(&schema.DeliveryRenditionJobs{}).Where("product_id = ?", foreign.productID).Count(&afterJobs).Error; err != nil {
		t.Fatal(err)
	}
	if afterJobs != beforeJobs {
		t.Fatalf("cross-merchant create wrote jobs: before=%d after=%d", beforeJobs, afterJobs)
	}
	var afterAdoptions int64
	if err := ds.db.Model(&schema.DeliveryAdoptionVersions{}).Where("product_id = ?", foreign.productID).Count(&afterAdoptions).Error; err != nil {
		t.Fatal(err)
	}
	if afterAdoptions != beforeAdoptions {
		t.Fatalf("cross-merchant adopt wrote versions: before=%d after=%d", beforeAdoptions, afterAdoptions)
	}
}

type foreignDeliveryFixture struct {
	merchantID string
	productID  string
	assetID    string
	jobID      string
	adoptionID string
}

func seedForeignDelivery(t *testing.T, ds *deliveryServer) foreignDeliveryFixture {
	t.Helper()
	now := time.Now().UTC()
	foreignMerchant := schema.Merchants{
		ID: clockid.New(), Name: "夹具他商-B6-delivery", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := ds.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}
	productID := clockid.New()
	if err := ds.db.Create(&schema.Products{
		ID: productID, MerchantID: foreignMerchant.ID, Name: "他商交付商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	byteSize := int64(64)
	width, height := 32, 32
	sha := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	mediaID := clockid.New()
	assetID := clockid.New()
	storagePath := "delivery-foreign/" + mediaID + ".png"
	verifiedAt := now
	if err := ds.db.Create(&schema.MediaObjects{
		ID: mediaID, StoragePath: storagePath, MIMEType: "image/png", ByteSize: &byteSize,
		Width: &width, Height: &height, SHA256: &sha, VerificationStatus: "verified",
		CreatedAt: now, VerifiedAt: &verifiedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ds.db.Create(&schema.ProductImageAssets{
		ID: assetID, ProductID: productID, MediaObjectID: mediaID, OriginType: "upload",
		DisplayName: "foreign", OriginalFilename: "foreign.png", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	jobID := clockid.New()
	specJSON := `{"width":64,"height":64,"format":"png","fit":"contain","crop_anchor":"center","background":"transparent","quality":null,"schema_version":1}`
	failReason := "夹具失败"
	if err := ds.db.Create(&schema.DeliveryRenditionJobs{
		ID: jobID, ProductID: productID, SourceAssetID: assetID,
		SpecSchemaVersion: 1, SpecJSON: specJSON, SpecHash: strings.Repeat("e", 64),
		Status: "failed", Attempts: 1, IsRetryable: true, FailureReason: &failReason,
		CreatedAt: now, UpdatedAt: now, FinishedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	adoptionID := clockid.New()
	if err := ds.db.Create(&schema.DeliveryAdoptionVersions{
		ID: adoptionID, ProductID: productID, Version: 1, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = ds.db.Where("version_id = ?", adoptionID).Delete(&schema.DeliveryAdoptionSlots{}).Error
		_ = ds.db.Where("id = ?", adoptionID).Delete(&schema.DeliveryAdoptionVersions{}).Error
		_ = ds.db.Where("id = ?", jobID).Delete(&schema.DeliveryRenditionJobs{}).Error
		_ = ds.db.Where("id = ?", assetID).Delete(&schema.ProductImageAssets{}).Error
		_ = ds.db.Where("id = ?", mediaID).Delete(&schema.MediaObjects{}).Error
		_ = ds.db.Where("id = ?", productID).Delete(&schema.Products{}).Error
		_ = ds.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})

	return foreignDeliveryFixture{
		merchantID: foreignMerchant.ID,
		productID:  productID,
		assetID:    assetID,
		jobID:      jobID,
		adoptionID: adoptionID,
	}
}
