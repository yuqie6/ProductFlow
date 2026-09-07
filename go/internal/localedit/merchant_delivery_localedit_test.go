package localedit

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// B6：本商 create/submit/execute 成功；跨商 task/product/source 拒绝（404 CrossMerchantDetail）。
func TestMerchantLocalEditIsolation(t *testing.T) {
	provider := MockProvider{Cap: SupportedCapability("mock-local")}
	es := newEditServer(t, provider)
	created := es.createProduct(t)
	productID := created.Product.ID
	sourceID := created.CreatedAssets[0].ID

	form := createForm(t, sourceID, false)
	draft := es.do(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits", form.body, form.contentType)
	es.mustStatus(t, draft, http.StatusCreated)
	var task TaskResponse
	es.decode(t, draft, &task)

	listed := es.do(t, http.MethodGet, "/api/v3/products/"+productID+"/image-edits", nil, "")
	es.mustStatus(t, listed, http.StatusOK)
	listed.Body.Close()

	submitted := es.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits/"+task.ID+"/submit", map[string]any{
		"idempotency_key": "b6-own-submit",
	})
	es.mustStatus(t, submitted, http.StatusAccepted)
	es.decode(t, submitted, &task)
	es.executeLocally(t, task.ID, Executor{DB: es.db, Media: es.media, Provider: provider})
	got := es.do(t, http.MethodGet, "/api/v3/products/"+productID+"/image-edits/"+task.ID, nil, "")
	es.mustStatus(t, got, http.StatusOK)
	es.decode(t, got, &task)
	if task.Status != "succeeded" || task.ResultAsset == nil {
		t.Fatalf("%+v", task)
	}

	foreign := seedForeignLocalEdit(t, es)

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

	var beforeTasks int64
	if err := es.db.Model(&schema.LocalImageEditTasks{}).Where("product_id = ?", foreign.productID).Count(&beforeTasks).Error; err != nil {
		t.Fatal(err)
	}

	foreignForm := createForm(t, foreign.assetID, false)
	assertCross404("foreign product create", es.do(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/image-edits", foreignForm.body, foreignForm.contentType))
	ownWithForeignSource := createForm(t, foreign.assetID, false)
	assertCross404("own product foreign source", es.do(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits", ownWithForeignSource.body, ownWithForeignSource.contentType))
	assertCross404("foreign list", es.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/image-edits", nil, ""))
	assertCross404("foreign get", es.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/image-edits/"+foreign.taskID, nil, ""))
	assertCross404("foreign submit", es.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/image-edits/"+foreign.taskID+"/submit", map[string]any{
		"idempotency_key": "b6-cross-submit",
	}))
	assertCross404("foreign retry", es.do(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/image-edits/"+foreign.taskID+"/retry", nil, ""))
	assertCross404("foreign cancel", es.do(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/image-edits/"+foreign.taskID+"/cancel", nil, ""))
	assertCross404("foreign adopt", es.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/image-edits/"+foreign.taskID+"/adopt", map[string]any{
		"expected_current_artifact_id": "missing",
	}))

	var afterTasks int64
	if err := es.db.Model(&schema.LocalImageEditTasks{}).Where("product_id = ?", foreign.productID).Count(&afterTasks).Error; err != nil {
		t.Fatal(err)
	}
	if afterTasks != beforeTasks {
		t.Fatalf("cross-merchant create wrote tasks: before=%d after=%d", beforeTasks, afterTasks)
	}
}

type foreignLocalEditFixture struct {
	merchantID string
	productID  string
	assetID    string
	taskID     string
}

func seedForeignLocalEdit(t *testing.T, es *editServer) foreignLocalEditFixture {
	t.Helper()
	now := time.Now().UTC()
	foreignMerchant := schema.Merchants{
		ID: clockid.New(), Name: "夹具他商-B6-localedit", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := es.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}
	productID := clockid.New()
	if err := es.db.Create(&schema.Products{
		ID: productID, MerchantID: foreignMerchant.ID, Name: "他商局部编辑商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	byteSize := int64(64)
	width, height := 8, 6
	sha := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	mediaID := clockid.New()
	maskMediaID := clockid.New()
	assetID := clockid.New()
	storagePath := "localedit-foreign/" + mediaID + ".png"
	maskPath := "localedit-foreign/" + maskMediaID + "-mask.png"
	verifiedAt := now
	if err := es.db.Create(&schema.MediaObjects{
		ID: mediaID, StoragePath: storagePath, MIMEType: "image/png", ByteSize: &byteSize,
		Width: &width, Height: &height, SHA256: &sha, VerificationStatus: "verified",
		CreatedAt: now, VerifiedAt: &verifiedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := es.db.Create(&schema.MediaObjects{
		ID: maskMediaID, StoragePath: maskPath, MIMEType: "image/png", ByteSize: &byteSize,
		Width: &width, Height: &height, SHA256: &sha, VerificationStatus: "verified",
		CreatedAt: now, VerifiedAt: &verifiedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := es.db.Create(&schema.ProductImageAssets{
		ID: assetID, ProductID: productID, MediaObjectID: mediaID, OriginType: "upload",
		DisplayName: "foreign", OriginalFilename: "foreign.png", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	taskID := clockid.New()
	geo := `{"source_width":8,"source_height":6,"viewport_width":8,"viewport_height":6,"viewport_to_source":[1,0,0,1,0,0]}`
	failReason := "夹具失败"
	if err := es.db.Create(&schema.LocalImageEditTasks{
		ID: taskID, ProductID: productID, SourceAssetID: assetID, SourceMediaSHA256: sha,
		MaskMediaObjectID: maskMediaID, Operation: "inpaint", MaskGeometryJSON: geo,
		Status: "failed", Revision: 1, Attempts: 1, IsRetryable: true, FailureReason: &failReason,
		CreatedAt: now, UpdatedAt: now, FinishedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = es.db.Where("task_id = ?", taskID).Delete(&schema.LocalImageEditTaskReferences{}).Error
		_ = es.db.Where("id = ?", taskID).Delete(&schema.LocalImageEditTasks{}).Error
		_ = es.db.Where("id = ?", assetID).Delete(&schema.ProductImageAssets{}).Error
		_ = es.db.Where("id = ?", mediaID).Delete(&schema.MediaObjects{}).Error
		_ = es.db.Where("id = ?", maskMediaID).Delete(&schema.MediaObjects{}).Error
		_ = es.db.Where("id = ?", productID).Delete(&schema.Products{}).Error
		_ = es.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})

	return foreignLocalEditFixture{
		merchantID: foreignMerchant.ID,
		productID:  productID,
		assetID:    assetID,
		taskID:     taskID,
	}
}
