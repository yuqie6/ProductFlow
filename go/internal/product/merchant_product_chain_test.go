package product

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// B2：商品子链本商读写/下载成功；跨商 product/fact/folder/asset/conversation/recipe 与 ZIP 混装统一 404。
func TestMerchantProductChainIsolation(t *testing.T) {
	ps := newProductServer(t)
	mine := ps.createV2(t, "本商商品链", nil, 1)
	ownAssetID := mine.CreatedAssets[0].ID

	foreign := seedForeignProductChain(t, ps, mine.Product.ID)

	// —— 正测：本商 ——
	factsGet := ps.do(t, http.MethodGet, "/api/v3/products/"+mine.Product.ID+"/facts", nil, "")
	if factsGet.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(factsGet.Body)
		factsGet.Body.Close()
		t.Fatalf("own facts get %d %s", factsGet.StatusCode, raw)
	}
	factsGet.Body.Close()

	factsPut := ps.doJSON(t, http.MethodPut, "/api/v3/products/"+mine.Product.ID+"/facts", map[string]any{
		"facts": []map[string]any{{"key": "material", "value": "钢"}},
	})
	if factsPut.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(factsPut.Body)
		factsPut.Body.Close()
		t.Fatalf("own facts put %d %s", factsPut.StatusCode, raw)
	}
	factsPut.Body.Close()

	boot := ps.do(t, http.MethodGet, "/api/v2/products/"+mine.Product.ID+"/image-library", nil, "")
	if boot.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(boot.Body)
		boot.Body.Close()
		t.Fatalf("own gallery bootstrap %d %s", boot.StatusCode, raw)
	}
	boot.Body.Close()

	folderResp := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+mine.Product.ID+"/image-folders", map[string]any{
		"name": "本商目录",
	})
	if folderResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(folderResp.Body)
		folderResp.Body.Close()
		t.Fatalf("own folder create %d %s", folderResp.StatusCode, raw)
	}
	var ownFolder GalleryFolderMutation
	ps.decode(t, folderResp, &ownFolder)

	coverResp := ps.doJSON(t, http.MethodPut, "/api/v2/products/"+mine.Product.ID+"/cover", map[string]any{
		"asset_id": ownAssetID,
	})
	if coverResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(coverResp.Body)
		coverResp.Body.Close()
		t.Fatalf("own set cover %d %s", coverResp.StatusCode, raw)
	}
	coverResp.Body.Close()

	dl := ps.do(t, http.MethodGet, "/api/v2/product-image-assets/"+ownAssetID+"/download", nil, "")
	if dl.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(dl.Body)
		dl.Body.Close()
		t.Fatalf("own download %d %s", dl.StatusCode, raw)
	}
	dl.Body.Close()

	zipOK := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+mine.Product.ID+"/image-assets/download-archive", map[string]any{
		"asset_ids": []string{ownAssetID},
	})
	if zipOK.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(zipOK.Body)
		zipOK.Body.Close()
		t.Fatalf("own zip %d %s", zipOK.StatusCode, raw)
	}
	zipOK.Body.Close()

	wsOwn := ps.do(t, http.MethodGet, "/api/v2/agent-product-workspaces/"+foreign.ownConversationID, nil, "")
	if wsOwn.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(wsOwn.Body)
		wsOwn.Body.Close()
		t.Fatalf("own workspace get %d %s", wsOwn.StatusCode, raw)
	}
	wsOwn.Body.Close()

	// —— 反测：跨商 UUID / ZIP 混装 → 404；根/直链对外文案钉死 CrossMerchantDetail ——
	assertStatus404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
	}
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

	assertCross404("foreign facts get", ps.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/facts", nil, ""))
	assertCross404("foreign facts put", ps.doJSON(t, http.MethodPut, "/api/v3/products/"+foreign.productID+"/facts", map[string]any{
		"facts": []map[string]any{{"key": "material", "value": "铜"}},
	}))
	assertCross404("foreign gallery", ps.do(t, http.MethodGet, "/api/v2/products/"+foreign.productID+"/image-library", nil, ""))
	assertCross404("foreign folder rename", ps.doJSON(t, http.MethodPatch, "/api/v2/products/"+foreign.productID+"/image-folders/"+foreign.folderID, map[string]any{
		"expected_name": "他商目录",
		"name":          "改名",
	}))
	assertStatus404("foreign folder on own product", ps.doJSON(t, http.MethodPatch, "/api/v2/products/"+mine.Product.ID+"/image-folders/"+foreign.folderID, map[string]any{
		"expected_name": "他商目录",
		"name":          "改名",
	}))
	assertCross404("foreign asset as cover", ps.doJSON(t, http.MethodPut, "/api/v2/products/"+mine.Product.ID+"/cover", map[string]any{
		"asset_id": foreign.assetID,
	}))
	assertCross404("foreign asset download", ps.do(t, http.MethodGet, "/api/v2/product-image-assets/"+foreign.assetID+"/download", nil, ""))
	assertCross404("foreign product zip", ps.doJSON(t, http.MethodPost, "/api/v2/products/"+foreign.productID+"/image-assets/download-archive", map[string]any{
		"asset_ids": []string{foreign.assetID},
	}))
	assertStatus404("zip mix foreign asset", ps.doJSON(t, http.MethodPost, "/api/v2/products/"+mine.Product.ID+"/image-assets/download-archive", map[string]any{
		"asset_ids": []string{ownAssetID, foreign.assetID},
	}))

	assertCross404("foreign workspace", ps.do(t, http.MethodGet, "/api/v2/agent-product-workspaces/"+foreign.conversationID, nil, ""))

	ps.enableDeletion(t)
	assertCross404("foreign asset delete", ps.do(t, http.MethodDelete, "/api/v2/product-image-assets/"+foreign.assetID, nil, ""))

	ctx := ps.merchantCtx(t)
	_, err := ps.svc.CreateFromRecipe(ctx, RecipeCreateInput{
		Product: CreateInput{
			Name:    "跨商配方产物",
			Uploads: []Upload{{Filename: "t.png", MIMEType: "image/png", Content: pngFile(t, 4, 4)}},
		},
		RecipeID:              foreign.recipeID,
		ExpectedRecipeVersion: 1,
		PreviewDigest:         strings.Repeat("a", 64),
		IdempotencyKey:        clockid.New(),
	})
	if !apperr.IsNotFound(err) || err.Error() != auth.CrossMerchantDetail {
		t.Fatalf("foreign recipe create: %v", err)
	}

	_, err = ps.svc.GetAsset(ctx, foreign.assetID)
	if !apperr.IsNotFound(err) || err.Error() != auth.CrossMerchantDetail {
		t.Fatalf("GetAsset foreign: %v", err)
	}

	_ = ownFolder
}

type foreignProductChain struct {
	merchantID        string
	productID         string
	folderID          string
	assetID           string
	conversationID    string
	ownConversationID string
	recipeID          string
}

func seedForeignProductChain(t *testing.T, ps *productServer, ownProductID string) foreignProductChain {
	t.Helper()
	now := time.Now().UTC()
	foreignMerchant := schema.Merchants{
		ID: clockid.New(), Name: "夹具他商-B2", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := ps.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ps.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})

	productID := clockid.New()
	folderID := clockid.New()
	mediaID := clockid.New()
	assetID := clockid.New()
	conversationID := clockid.New()
	ownConversationID := clockid.New()
	recipeID := clockid.New()
	devMerchant := auth.MustDevMerchantID(t, ps.db)

	if err := ps.db.Create(&schema.Products{
		ID: productID, MerchantID: foreignMerchant.ID, Name: "他商商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&schema.ProductAssetFolders{
		ID: folderID, ProductID: productID, Name: "他商目录", SortOrder: 0, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	byteSize := int64(64)
	w, h := 8, 6
	sha := strings.Repeat("b", 64)
	if err := ps.db.Create(&schema.MediaObjects{
		ID: mediaID, StoragePath: "foreign/" + productID + "/a.png", MIMEType: "image/png",
		ByteSize: &byteSize, Width: &w, Height: &h, SHA256: &sha,
		VerificationStatus: "verified", CreatedAt: now, VerifiedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&schema.ProductImageAssets{
		ID: assetID, ProductID: productID, MediaObjectID: mediaID, OriginType: "upload",
		DisplayName: "他商图", OriginalFilename: "a.png", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&schema.AgentConversations{
		ID: conversationID, MerchantID: foreignMerchant.ID, ProductID: &productID,
		HarnessRunID: "harness-foreign-" + conversationID, Status: "collecting",
		ScopeType: "product_workflow", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&schema.AgentConversations{
		ID: ownConversationID, MerchantID: devMerchant, ProductID: &ownProductID,
		HarnessRunID: "harness-own-" + ownConversationID, Status: "collecting",
		ScopeType: "product_workflow", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&schema.WorkflowRecipes{
		ID: recipeID, MerchantID: foreignMerchant.ID, Kind: "workflow_recipe",
		Origin: "user", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_ = ps.db.WithContext(ctx).Where("id = ?", conversationID).Delete(&schema.AgentConversations{}).Error
		_ = ps.db.WithContext(ctx).Where("id = ?", ownConversationID).Delete(&schema.AgentConversations{}).Error
		_ = ps.db.WithContext(ctx).Where("id = ?", assetID).Delete(&schema.ProductImageAssets{}).Error
		_ = ps.db.WithContext(ctx).Where("id = ?", mediaID).Delete(&schema.MediaObjects{}).Error
		_ = ps.db.WithContext(ctx).Where("id = ?", folderID).Delete(&schema.ProductAssetFolders{}).Error
		_ = ps.db.WithContext(ctx).Where("id = ?", recipeID).Delete(&schema.WorkflowRecipes{}).Error
		_ = ps.db.WithContext(ctx).Where("id = ?", productID).Delete(&schema.Products{}).Error
	})

	return foreignProductChain{
		merchantID:        foreignMerchant.ID,
		productID:         productID,
		folderID:          folderID,
		assetID:           assetID,
		conversationID:    conversationID,
		ownConversationID: ownConversationID,
		recipeID:          recipeID,
	}
}
