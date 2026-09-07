package library

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

func TestUploadCreatesVerifiedDirectUpload(t *testing.T) {
	ls := newLibraryServer(t)
	asset := ls.uploadOne(t, "test_direct_upload.png", nil)
	if asset.DisplayName != "test_direct_upload.png" || asset.OriginalFilename != "test_direct_upload.png" {
		t.Fatalf("%+v", asset)
	}
	if asset.SourceType != SourceUpload || asset.VerificationStatus != "verified" || asset.IsArchived {
		t.Fatalf("%+v", asset)
	}
	if !strings.Contains(asset.DownloadURL, "/api/media-library/"+asset.ID+"/download") {
		t.Fatalf("%s", asset.DownloadURL)
	}
}

func TestUploadIdempotencyKeyDedups(t *testing.T) {
	ls := newLibraryServer(t)
	body, contentType := multipartFiles(t, []struct {
		name string
		data []byte
	}{{"dup.png", pngBytes(t, 8, 6)}}, nil)
	headers := map[string]string{"Idempotency-Key": "upload-dup-" + clockid.New()}
	first := ls.do(t, http.MethodPost, "/api/media-library/upload", body, contentType, headers)
	ls.mustStatus(t, first, http.StatusCreated)
	var firstItems []AssetResponse
	ls.decode(t, first, &firstItems)

	body2, contentType2 := multipartFiles(t, []struct {
		name string
		data []byte
	}{{"dup.png", pngBytes(t, 8, 6)}}, nil)
	second := ls.do(t, http.MethodPost, "/api/media-library/upload", body2, contentType2, headers)
	ls.mustStatus(t, second, http.StatusCreated)
	var secondItems []AssetResponse
	ls.decode(t, second, &secondItems)
	if firstItems[0].ID != secondItems[0].ID {
		t.Fatalf("%s vs %s", firstItems[0].ID, secondItems[0].ID)
	}

	body3, contentType3 := multipartFiles(t, []struct {
		name string
		data []byte
	}{{"dup.png", pngBytes(t, 8, 6)}}, nil)
	third := ls.do(t, http.MethodPost, "/api/media-library/upload", body3, contentType3, map[string]string{
		"Idempotency-Key": "upload-dup-other-" + clockid.New(),
	})
	ls.mustStatus(t, third, http.StatusCreated)
	var thirdItems []AssetResponse
	ls.decode(t, third, &thirdItems)
	if thirdItems[0].ID == firstItems[0].ID {
		t.Fatal("different key should create a new asset")
	}

	conflictBody, conflictType := multipartFiles(t, []struct {
		name string
		data []byte
	}{{"other.png", pngBytes(t, 16, 16)}}, nil)
	conflict := ls.do(t, http.MethodPost, "/api/media-library/upload", conflictBody, conflictType, headers)
	ls.mustStatus(t, conflict, http.StatusConflict)
	conflict.Body.Close()
}

func TestFoldersTagsMoveAndListFilters(t *testing.T) {
	ls := newLibraryServer(t)
	marker := "pf-lib-" + clockid.New()
	create := ls.doJSON(t, http.MethodPost, "/api/media-library/folders", map[string]any{"name": marker})
	ls.mustStatus(t, create, http.StatusCreated)
	var folder Folder
	ls.decode(t, create, &folder)
	replay := ls.doJSON(t, http.MethodPost, "/api/media-library/folders", map[string]any{"name": "  " + marker + "  "})
	ls.mustStatus(t, replay, http.StatusOK)
	var replayed Folder
	ls.decode(t, replay, &replayed)
	if replayed.ID != folder.ID {
		t.Fatalf("%s vs %s", replayed.ID, folder.ID)
	}

	tagResp := ls.doJSON(t, http.MethodPost, "/api/media-library/tags", map[string]any{"name": marker + "-tag"})
	ls.mustStatus(t, tagResp, http.StatusCreated)
	var tag Tag
	ls.decode(t, tagResp, &tag)

	asset := ls.uploadOne(t, marker+".png", nil)
	moved := ls.doJSON(t, http.MethodPost, "/api/media-library/organize/move", map[string]any{
		"asset_ids":          []string{asset.ID},
		"folder_id":          folder.ID,
		"expected_revisions": map[string]int{asset.ID: asset.Revision},
	})
	ls.mustStatus(t, moved, http.StatusOK)
	var movedItems []AssetResponse
	ls.decode(t, moved, &movedItems)
	if movedItems[0].FolderID == nil || *movedItems[0].FolderID != folder.ID || movedItems[0].Revision != asset.Revision+1 {
		t.Fatalf("%+v", movedItems[0])
	}

	tagged := ls.doJSON(t, http.MethodPost, "/api/media-library/organize/tags", map[string]any{
		"asset_ids":          []string{asset.ID},
		"tag_names":          []string{marker + "-tag"},
		"expected_revisions": map[string]int{asset.ID: movedItems[0].Revision},
	})
	ls.mustStatus(t, tagged, http.StatusOK)
	var taggedItems []AssetResponse
	ls.decode(t, tagged, &taggedItems)
	if len(taggedItems[0].Tags) != 1 || taggedItems[0].Tags[0].Name != marker+"-tag" {
		t.Fatalf("%+v", taggedItems[0].Tags)
	}

	listed := ls.do(t, http.MethodGet, "/api/media-library?q="+marker+"&folder_id="+folder.ID+"&tag="+marker+"-tag", nil, "", nil)
	ls.mustStatus(t, listed, http.StatusOK)
	var page ListResponse
	ls.decode(t, listed, &page)
	if len(page.Items) != 1 || page.Items[0].ID != asset.ID {
		t.Fatalf("%+v", page)
	}

	deleted := ls.do(t, http.MethodDelete, "/api/media-library/folders/"+folder.ID, nil, "", nil)
	ls.mustStatus(t, deleted, http.StatusOK)
	var delPayload map[string]any
	ls.decode(t, deleted, &delPayload)
	if delPayload["unorganized_count"] != float64(1) {
		t.Fatalf("%v", delPayload)
	}
	got := ls.do(t, http.MethodGet, "/api/media-library/"+asset.ID, nil, "", nil)
	ls.mustStatus(t, got, http.StatusOK)
	var after AssetResponse
	ls.decode(t, got, &after)
	if after.FolderID != nil {
		t.Fatalf("folder should be cleared: %+v", after)
	}
}

func TestFromProductCollectAndIdempotency(t *testing.T) {
	ls := newLibraryServer(t)
	created := ls.createProduct(t, "素材收录商品-"+clockid.New())
	sourceID := created.CreatedAssets[0].ID
	saved := ls.doJSON(t, http.MethodPost, "/api/media-library/from-product", map[string]any{
		"product_image_asset_id": sourceID,
	})
	ls.mustStatus(t, saved, http.StatusCreated)
	var libraryAsset AssetResponse
	ls.decode(t, saved, &libraryAsset)
	if libraryAsset.SourceType != SourceProduct || libraryAsset.SourceID != sourceID {
		t.Fatalf("%+v", libraryAsset)
	}
	replaySave := ls.doJSON(t, http.MethodPost, "/api/media-library/from-product", map[string]any{
		"product_image_asset_id": sourceID,
	})
	ls.mustStatus(t, replaySave, http.StatusOK)
	var replayed AssetResponse
	ls.decode(t, replaySave, &replayed)
	if replayed.ID != libraryAsset.ID {
		t.Fatalf("%s vs %s", replayed.ID, libraryAsset.ID)
	}

	other := ls.createProduct(t, "另一商品-"+clockid.New())
	otherSaved := ls.doJSON(t, http.MethodPost, "/api/media-library/from-product", map[string]any{
		"product_image_asset_id": other.CreatedAssets[0].ID,
	})
	ls.mustStatus(t, otherSaved, http.StatusCreated)
	var otherAsset AssetResponse
	ls.decode(t, otherSaved, &otherAsset)

	key := "collect-" + clockid.New()
	payload := map[string]any{
		"product_id":              created.Product.ID,
		"media_library_asset_ids": []string{libraryAsset.ID},
	}
	raw, _ := json.Marshal(payload)
	first := ls.do(t, http.MethodPost, "/api/media-library/collect", bytes.NewReader(raw), "application/json", map[string]string{
		"Idempotency-Key": key,
	})
	ls.mustStatus(t, first, http.StatusOK)
	var firstAssets []map[string]any
	ls.decode(t, first, &firstAssets)
	if len(firstAssets) != 1 {
		t.Fatalf("%v", firstAssets)
	}

	raw2, _ := json.Marshal(payload)
	replay := ls.do(t, http.MethodPost, "/api/media-library/collect", bytes.NewReader(raw2), "application/json", map[string]string{
		"Idempotency-Key": key,
	})
	ls.mustStatus(t, replay, http.StatusOK)
	var replayAssets []map[string]any
	ls.decode(t, replay, &replayAssets)
	if replayAssets[0]["id"] != firstAssets[0]["id"] {
		t.Fatalf("%v vs %v", replayAssets[0]["id"], firstAssets[0]["id"])
	}

	changed, _ := json.Marshal(map[string]any{
		"product_id":              created.Product.ID,
		"media_library_asset_ids": []string{otherAsset.ID},
	})
	conflict := ls.do(t, http.MethodPost, "/api/media-library/collect", bytes.NewReader(changed), "application/json", map[string]string{
		"Idempotency-Key": key,
	})
	ls.mustStatus(t, conflict, http.StatusConflict)
	conflict.Body.Close()
}

func TestCollectRejectsArchivedAsset(t *testing.T) {
	ls := newLibraryServer(t)
	created := ls.createProduct(t, "归档收录-"+clockid.New())
	saved := ls.doJSON(t, http.MethodPost, "/api/media-library/from-product", map[string]any{
		"product_image_asset_id": created.CreatedAssets[0].ID,
	})
	ls.mustStatus(t, saved, http.StatusCreated)
	var libraryAsset AssetResponse
	ls.decode(t, saved, &libraryAsset)
	archived := ls.do(t, http.MethodPost, "/api/media-library/"+libraryAsset.ID+"/archive", nil, "", nil)
	ls.mustStatus(t, archived, http.StatusOK)
	archived.Body.Close()
	raw, _ := json.Marshal(map[string]any{
		"product_id":              created.Product.ID,
		"media_library_asset_ids": []string{libraryAsset.ID},
	})
	resp := ls.do(t, http.MethodPost, "/api/media-library/collect", bytes.NewReader(raw), "application/json", map[string]string{
		"Idempotency-Key": "collect-archived-" + clockid.New(),
	})
	ls.mustStatus(t, resp, http.StatusConflict)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "归档素材") {
		t.Fatalf("%s", body)
	}
}

func TestArchiveRestoreAndWorkflowLibrary(t *testing.T) {
	ls := newLibraryServer(t)
	direct := ls.createDirect(t, "工作流素材商品-"+clockid.New())
	graphID, _ := direct.Graph["id"].(string)
	if graphID == "" {
		t.Fatalf("graph %+v", direct.Graph)
	}
	saved := ls.doJSON(t, http.MethodPost, "/api/media-library/from-product", map[string]any{
		"product_image_asset_id": direct.CreatedAssets[0].ID,
	})
	ls.mustStatus(t, saved, http.StatusCreated)
	var libraryAsset AssetResponse
	ls.decode(t, saved, &libraryAsset)

	path := "/api/media-library/workflows/" + graphID + "/media-library?product_id=" + direct.Product.ID
	initial := ls.do(t, http.MethodGet, path, nil, "", nil)
	ls.mustStatus(t, initial, http.StatusOK)
	var empty WorkflowList
	ls.decode(t, initial, &empty)
	if len(empty.Items) != 0 || empty.WorkflowID != graphID {
		t.Fatalf("%+v", empty)
	}

	synced := ls.doJSON(t, http.MethodPost, "/api/media-library/workflows/"+graphID+"/media-library/sync?product_id="+direct.Product.ID, map[string]any{
		"media_library_asset_ids": []string{libraryAsset.ID},
	})
	ls.mustStatus(t, synced, http.StatusOK)
	var payload WorkflowList
	ls.decode(t, synced, &payload)
	if len(payload.Items) != 1 || payload.Items[0].Asset.ID != libraryAsset.ID {
		t.Fatalf("%+v", payload)
	}
	if payload.Items[0].ProductImageAssetID == nil || *payload.Items[0].ProductImageAssetID != direct.CreatedAssets[0].ID {
		t.Fatalf("%+v", payload.Items[0])
	}

	blocked := ls.do(t, http.MethodPost, "/api/media-library/"+libraryAsset.ID+"/archive", nil, "", nil)
	ls.mustStatus(t, blocked, http.StatusConflict)
	raw, _ := io.ReadAll(blocked.Body)
	blocked.Body.Close()
	if !strings.Contains(string(raw), "工作流素材库") {
		t.Fatalf("%s", raw)
	}

	removed := ls.do(t, http.MethodDelete, "/api/media-library/workflows/"+graphID+"/media-library/"+libraryAsset.ID+"?product_id="+direct.Product.ID, nil, "", nil)
	ls.mustStatus(t, removed, http.StatusNoContent)
	removed.Body.Close()

	listed := ls.do(t, http.MethodGet, path, nil, "", nil)
	ls.mustStatus(t, listed, http.StatusOK)
	var after WorkflowList
	ls.decode(t, listed, &after)
	if len(after.Items) != 0 {
		t.Fatalf("%+v", after)
	}

	archived := ls.do(t, http.MethodPost, "/api/media-library/"+libraryAsset.ID+"/archive", nil, "", nil)
	ls.mustStatus(t, archived, http.StatusOK)
	var archivedAsset AssetResponse
	ls.decode(t, archived, &archivedAsset)
	if !archivedAsset.IsArchived || archivedAsset.Revision != libraryAsset.Revision+1 {
		t.Fatalf("%+v", archivedAsset)
	}
	restored := ls.do(t, http.MethodPost, "/api/media-library/"+libraryAsset.ID+"/restore", nil, "", nil)
	ls.mustStatus(t, restored, http.StatusOK)
	var restoredAsset AssetResponse
	ls.decode(t, restored, &restoredAsset)
	if restoredAsset.IsArchived || restoredAsset.Revision != libraryAsset.Revision+2 {
		t.Fatalf("%+v", restoredAsset)
	}
}

func TestFromSessionRequiresGeneratedImage(t *testing.T) {
	ls := newLibraryServer(t)
	productCreated := ls.createProduct(t, "会话来源商品-"+clockid.New())
	mediaID := productCreated.CreatedAssets[0].MediaObjectID
	sessionID := clockid.New()
	assetID := clockid.New()
	merchantID := auth.MustDevMerchantID(t, ls.db)
	fixtureCtx := auth.WithMerchantID(context.Background(), merchantID)
	if _, err := ls.pool.Exec(fixtureCtx, `
		INSERT INTO image_sessions (id, merchant_id, title, created_at, updated_at)
		VALUES ($1, $2, '素材库测试会话', NOW(), NOW())
	`, sessionID, merchantID); err != nil {
		t.Fatal(err)
	}
	if _, err := ls.pool.Exec(context.Background(), `
		INSERT INTO image_session_assets (id, session_id, kind, original_filename, mime_type, storage_path, media_object_id, created_at)
		VALUES ($1, $2, 'reference_upload', 'session.png', 'image/png', 'media/x.png', $3, NOW())
	`, assetID, sessionID, mediaID); err != nil {
		t.Fatal(err)
	}
	rejected := ls.doJSON(t, http.MethodPost, "/api/media-library/from-session", map[string]any{
		"image_session_asset_id": assetID,
	})
	ls.mustStatus(t, rejected, http.StatusBadRequest)
	raw, _ := io.ReadAll(rejected.Body)
	rejected.Body.Close()
	if !strings.Contains(string(raw), "只有生成结果") {
		t.Fatalf("%s", raw)
	}

	generatedID := clockid.New()
	if _, err := ls.pool.Exec(context.Background(), `
		INSERT INTO image_session_assets (id, session_id, kind, original_filename, mime_type, storage_path, media_object_id, created_at)
		VALUES ($1, $2, 'generated_image', 'session.png', 'image/png', 'media/x.png', $3, NOW())
	`, generatedID, sessionID, mediaID); err != nil {
		t.Fatal(err)
	}
	created := ls.doJSON(t, http.MethodPost, "/api/media-library/from-session", map[string]any{
		"image_session_asset_id": generatedID,
	})
	ls.mustStatus(t, created, http.StatusCreated)
	var asset AssetResponse
	ls.decode(t, created, &asset)
	if asset.SourceType != SourceSession || asset.SourceID != generatedID {
		t.Fatalf("%+v", asset)
	}
}

func TestListCursorBoundToFilter(t *testing.T) {
	ls := newLibraryServer(t)
	prefix := "cursor-" + clockid.New()
	first := ls.uploadOne(t, prefix+"-a.png", nil)
	_ = ls.uploadOne(t, prefix+"-b.png", nil)
	_ = ls.uploadOne(t, prefix+"-c.png", nil)
	page1 := ls.do(t, http.MethodGet, "/api/media-library?limit=1&q="+prefix, nil, "", nil)
	ls.mustStatus(t, page1, http.StatusOK)
	var firstPage ListResponse
	ls.decode(t, page1, &firstPage)
	if firstPage.NextCursor == nil || len(firstPage.Items) != 1 {
		t.Fatalf("%+v", firstPage)
	}
	page2 := ls.do(t, http.MethodGet, "/api/media-library?limit=1&q="+prefix+"&cursor="+*firstPage.NextCursor, nil, "", nil)
	ls.mustStatus(t, page2, http.StatusOK)
	var secondPage ListResponse
	ls.decode(t, page2, &secondPage)
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatalf("%+v", secondPage)
	}
	mismatch := ls.do(t, http.MethodGet, "/api/media-library?limit=1&q=different&cursor="+*firstPage.NextCursor, nil, "", nil)
	ls.mustStatus(t, mismatch, http.StatusBadRequest)
	mismatch.Body.Close()
	_ = first
}

func TestListRejectsOversizedQuery(t *testing.T) {
	ls := newLibraryServer(t)
	longQ := ls.do(t, http.MethodGet, "/api/media-library?q="+strings.Repeat("q", 256), nil, "", nil)
	ls.mustStatus(t, longQ, http.StatusBadRequest)
	longQ.Body.Close()
	longFolder := ls.do(t, http.MethodGet, "/api/media-library?folder_id="+strings.Repeat("f", 37), nil, "", nil)
	ls.mustStatus(t, longFolder, http.StatusBadRequest)
	longFolder.Body.Close()
	longTag := ls.do(t, http.MethodGet, "/api/media-library?tag="+strings.Repeat("t", 81), nil, "", nil)
	ls.mustStatus(t, longTag, http.StatusBadRequest)
	longTag.Body.Close()
}

func TestWorkflowDeleteRouteRegistered(t *testing.T) {
	engine := httpx.NewEngine(nil)
	HTTP{}.Register(engine)
	found := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodDelete && strings.Contains(route.Path, "workflows") {
			found = true
			t.Log(route.Method, route.Path)
		}
	}
	if !found {
		t.Fatal("missing DELETE workflow media-library route")
	}
}

func TestDownloadServesOriginal(t *testing.T) {
	ls := newLibraryServer(t)
	asset := ls.uploadOne(t, "dl.png", nil)
	resp := ls.do(t, http.MethodGet, asset.DownloadURL, nil, "", nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("download %d %s", resp.StatusCode, raw)
	}
	resp.Body.Close()
}

func TestDownloadMissingOriginalMarksVerification(t *testing.T) {
	ls := newLibraryServer(t)
	asset := ls.uploadOne(t, "gone.png", nil)
	var rel string
	if err := ls.pool.QueryRow(context.Background(), `
		SELECT m.storage_path
		FROM media_library_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.id = $1
	`, asset.ID).Scan(&rel); err != nil {
		t.Fatal(err)
	}
	if rel == "" {
		t.Fatal("missing storage path")
	}
	if err := os.Remove(filepath.Join(ls.root, rel)); err != nil {
		t.Fatal(err)
	}
	resp := ls.do(t, http.MethodGet, asset.DownloadURL, nil, "", nil)
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("download %d %s", resp.StatusCode, raw)
	}
	resp.Body.Close()
	var status string
	if err := ls.pool.QueryRow(context.Background(), `
		SELECT m.verification_status
		FROM media_library_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.id = $1
	`, asset.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != media.StatusMissing {
		t.Fatalf("verification %s, want missing", status)
	}
	again := ls.do(t, http.MethodGet, asset.DownloadURL, nil, "", nil)
	if again.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(again.Body)
		again.Body.Close()
		t.Fatalf("second download %d %s", again.StatusCode, raw)
	}
	again.Body.Close()
}
