package library

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

// B4：同商 from-product/session、workflow sync 成功；跨商绑定拒绝且无半写入（404 CrossMerchantDetail）。
func TestMerchantLibraryBindingIsolation(t *testing.T) {
	ls := newLibraryServer(t)
	merchantID := auth.MustDevMerchantID(t, ls.db)

	ownProduct := ls.createDirect(t, "本商图库绑定-"+clockid.New())
	graphID, _ := ownProduct.Graph["id"].(string)
	if graphID == "" {
		t.Fatalf("graph %+v", ownProduct.Graph)
	}
	ownProductAssetID := ownProduct.CreatedAssets[0].ID

	fromProduct := ls.doJSON(t, http.MethodPost, "/api/media-library/from-product", map[string]any{
		"product_image_asset_id": ownProductAssetID,
	})
	ls.mustStatus(t, fromProduct, http.StatusCreated)
	var ownLibrary AssetResponse
	ls.decode(t, fromProduct, &ownLibrary)

	sessionID := clockid.New()
	ownSessionAssetID := clockid.New()
	fixtureCtx := auth.WithMerchantID(context.Background(), merchantID)
	if _, err := ls.pool.Exec(fixtureCtx, `
		INSERT INTO image_sessions (id, merchant_id, title, created_at, updated_at)
		VALUES ($1, $2, '本商会话绑定', NOW(), NOW())
	`, sessionID, merchantID); err != nil {
		t.Fatal(err)
	}
	if _, err := ls.pool.Exec(context.Background(), `
		INSERT INTO image_session_assets (id, session_id, kind, original_filename, mime_type, storage_path, media_object_id, created_at)
		VALUES ($1, $2, 'generated_image', 'own-session.png', 'image/png', 'media/own-session.png', $3, NOW())
	`, ownSessionAssetID, sessionID, ownProduct.CreatedAssets[0].MediaObjectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ls.pool.Exec(context.Background(), `DELETE FROM image_session_assets WHERE id = $1`, ownSessionAssetID)
		_, _ = ls.pool.Exec(context.Background(), `DELETE FROM image_sessions WHERE id = $1`, sessionID)
	})

	fromSession := ls.doJSON(t, http.MethodPost, "/api/media-library/from-session", map[string]any{
		"image_session_asset_id": ownSessionAssetID,
	})
	ls.mustStatus(t, fromSession, http.StatusCreated)
	var ownSessionLibrary AssetResponse
	ls.decode(t, fromSession, &ownSessionLibrary)
	if ownSessionLibrary.SourceType != SourceSession || ownSessionLibrary.SourceID != ownSessionAssetID {
		t.Fatalf("%+v", ownSessionLibrary)
	}

	synced := ls.doJSON(t, http.MethodPost, "/api/media-library/workflows/"+graphID+"/media-library/sync?product_id="+ownProduct.Product.ID, map[string]any{
		"media_library_asset_ids": []string{ownLibrary.ID},
	})
	ls.mustStatus(t, synced, http.StatusOK)
	var syncPayload WorkflowList
	ls.decode(t, synced, &syncPayload)
	if len(syncPayload.Items) != 1 || syncPayload.Items[0].Asset.ID != ownLibrary.ID {
		t.Fatalf("%+v", syncPayload)
	}

	listed := ls.do(t, http.MethodGet, "/api/media-library/workflows/"+graphID+"/media-library?product_id="+ownProduct.Product.ID, nil, "", nil)
	ls.mustStatus(t, listed, http.StatusOK)
	listed.Body.Close()

	got := ls.do(t, http.MethodGet, "/api/media-library/"+ownLibrary.ID, nil, "", nil)
	ls.mustStatus(t, got, http.StatusOK)
	got.Body.Close()

	foreign := seedForeignLibraryBinding(t, ls)

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

	var beforeLibrary int64
	if err := ls.db.Model(&schema.MediaLibraryAssets{}).Where("merchant_id = ?", foreign.merchantID).Count(&beforeLibrary).Error; err != nil {
		t.Fatal(err)
	}
	var beforeLinks int64
	if err := ls.db.Model(&schema.WorkflowMediaLibraryAssets{}).Where("workflow_id = ?", foreign.workflowID).Count(&beforeLinks).Error; err != nil {
		t.Fatal(err)
	}
	var beforeOwnLinks int64
	if err := ls.db.Model(&schema.WorkflowMediaLibraryAssets{}).Where("workflow_id = ?", graphID).Count(&beforeOwnLinks).Error; err != nil {
		t.Fatal(err)
	}
	var beforeProductAssets int64
	if err := ls.db.Model(&schema.ProductImageAssets{}).Where("product_id = ?", foreign.productID).Count(&beforeProductAssets).Error; err != nil {
		t.Fatal(err)
	}

	assertCross404("foreign from-product", ls.doJSON(t, http.MethodPost, "/api/media-library/from-product", map[string]any{
		"product_image_asset_id": foreign.productAssetID,
	}))
	assertCross404("foreign from-session", ls.doJSON(t, http.MethodPost, "/api/media-library/from-session", map[string]any{
		"image_session_asset_id": foreign.sessionAssetID,
	}))
	assertCross404("foreign workflow list", ls.do(t, http.MethodGet,
		"/api/media-library/workflows/"+foreign.workflowID+"/media-library?product_id="+foreign.productID, nil, "", nil))
	assertCross404("foreign workflow sync", ls.doJSON(t, http.MethodPost,
		"/api/media-library/workflows/"+foreign.workflowID+"/media-library/sync?product_id="+foreign.productID, map[string]any{
			"media_library_asset_ids": []string{ownLibrary.ID},
		}))
	assertCross404("own workflow sync foreign library asset", ls.doJSON(t, http.MethodPost,
		"/api/media-library/workflows/"+graphID+"/media-library/sync?product_id="+ownProduct.Product.ID, map[string]any{
			"media_library_asset_ids": []string{foreign.libraryAssetID},
		}))
	assertCross404("foreign library get", ls.do(t, http.MethodGet, "/api/media-library/"+foreign.libraryAssetID, nil, "", nil))
	assertCross404("foreign library download", ls.do(t, http.MethodGet, "/api/media-library/"+foreign.libraryAssetID+"/download", nil, "", nil))
	assertCross404("collect to foreign product", ls.do(t, http.MethodPost, "/api/media-library/collect",
		strings.NewReader(`{"product_id":"`+foreign.productID+`","media_library_asset_ids":["`+ownLibrary.ID+`"]}`),
		"application/json", map[string]string{"Idempotency-Key": "b4-cross-" + clockid.New()}))

	var afterLibrary int64
	if err := ls.db.Model(&schema.MediaLibraryAssets{}).Where("merchant_id = ?", foreign.merchantID).Count(&afterLibrary).Error; err != nil {
		t.Fatal(err)
	}
	if afterLibrary != beforeLibrary {
		t.Fatalf("cross-merchant from-* wrote library assets: before=%d after=%d", beforeLibrary, afterLibrary)
	}
	var afterLinks int64
	if err := ls.db.Model(&schema.WorkflowMediaLibraryAssets{}).Where("workflow_id = ?", foreign.workflowID).Count(&afterLinks).Error; err != nil {
		t.Fatal(err)
	}
	if afterLinks != beforeLinks {
		t.Fatalf("cross-merchant workflow sync wrote links: before=%d after=%d", beforeLinks, afterLinks)
	}
	var afterOwnLinks int64
	if err := ls.db.Model(&schema.WorkflowMediaLibraryAssets{}).Where("workflow_id = ?", graphID).Count(&afterOwnLinks).Error; err != nil {
		t.Fatal(err)
	}
	if afterOwnLinks != beforeOwnLinks {
		t.Fatalf("foreign library sync mutated own workflow links: before=%d after=%d", beforeOwnLinks, afterOwnLinks)
	}
	var afterProductAssets int64
	if err := ls.db.Model(&schema.ProductImageAssets{}).Where("product_id = ?", foreign.productID).Count(&afterProductAssets).Error; err != nil {
		t.Fatal(err)
	}
	if afterProductAssets != beforeProductAssets {
		t.Fatalf("cross-merchant collect wrote product assets: before=%d after=%d", beforeProductAssets, afterProductAssets)
	}
}

type foreignLibraryBinding struct {
	merchantID     string
	productID      string
	productAssetID string
	sessionAssetID string
	workflowID     string
	libraryAssetID string
}

func seedForeignLibraryBinding(t *testing.T, ls *libraryServer) foreignLibraryBinding {
	t.Helper()
	now := time.Now().UTC()
	foreignMerchant := schema.Merchants{
		ID: clockid.New(), Name: "夹具他商-B4-library", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := ls.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}

	productID := clockid.New()
	mediaID := clockid.New()
	productAssetID := clockid.New()
	sessionID := clockid.New()
	sessionAssetID := clockid.New()
	workflowID := clockid.New()
	libraryAssetID := clockid.New()
	byteSize := int64(64)
	w, h := 8, 6
	sha := strings.Repeat("c", 64)
	storagePath := "library-foreign/" + mediaID + ".png"

	if err := ls.db.Create(&schema.Products{
		ID: productID, MerchantID: foreignMerchant.ID, Name: "他商图库商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ls.db.Create(&schema.MediaObjects{
		ID: mediaID, StoragePath: storagePath, MIMEType: "image/png",
		ByteSize: &byteSize, Width: &w, Height: &h, SHA256: &sha,
		VerificationStatus: "verified", CreatedAt: now, VerifiedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ls.db.Create(&schema.ProductImageAssets{
		ID: productAssetID, ProductID: productID, MediaObjectID: mediaID, OriginType: "upload",
		DisplayName: "他商图", OriginalFilename: "foreign.png", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ls.db.Create(&schema.ImageSessions{
		ID: sessionID, MerchantID: foreignMerchant.ID, Title: "他商会话", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ls.db.Create(&schema.ImageSessionAssets{
		ID: sessionAssetID, SessionID: sessionID, Kind: "generated_image", OriginalFilename: "foreign-session.png",
		MIMEType: "image/png", StoragePath: storagePath, CreatedAt: now, MediaObjectID: mediaID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ls.db.Create(&schema.WorkflowGraphs{
		ID: workflowID, ProductID: productID, Title: "他商工作流", Active: true,
		SchemaVersion: 3, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	sourceID := clockid.New()
	prov := Provenance{
		SchemaVersion:    1,
		SourceType:       SourceUpload,
		SourceID:         sourceID,
		SHA256:           sha,
		MIMEType:         "image/png",
		ByteSize:         int(byteSize),
		Width:            w,
		Height:           h,
		OriginalFilename: "foreign-lib.png",
		CapturedAt:       now,
	}
	hash, err := provenanceHash(prov)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(storeProvenance(prov))
	if err != nil {
		t.Fatal(err)
	}
	if err := ls.db.Create(&schema.MediaLibraryAssets{
		ID: libraryAssetID, MerchantID: foreignMerchant.ID, MediaObjectID: mediaID,
		SourceType: SourceUpload, SourceID: sourceID, ProvenanceJSON: string(raw), ProvenanceHash: hash,
		Revision: 1, DisplayName: "他商素材", OriginalFilename: "foreign-lib.png",
		IsArchived: false, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = ls.db.Where("id = ?", libraryAssetID).Delete(&schema.MediaLibraryAssets{}).Error
		_ = ls.db.Where("id = ?", workflowID).Delete(&schema.WorkflowGraphs{}).Error
		_ = ls.db.Where("id = ?", sessionAssetID).Delete(&schema.ImageSessionAssets{}).Error
		_ = ls.db.Where("id = ?", sessionID).Delete(&schema.ImageSessions{}).Error
		_ = ls.db.Where("id = ?", productAssetID).Delete(&schema.ProductImageAssets{}).Error
		_ = ls.db.Where("id = ?", mediaID).Delete(&schema.MediaObjects{}).Error
		_ = ls.db.Where("id = ?", productID).Delete(&schema.Products{}).Error
		_ = ls.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})

	return foreignLibraryBinding{
		merchantID:     foreignMerchant.ID,
		productID:      productID,
		productAssetID: productAssetID,
		sessionAssetID: sessionAssetID,
		workflowID:     workflowID,
		libraryAssetID: libraryAssetID,
	}
}
