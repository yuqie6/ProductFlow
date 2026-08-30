package product

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestCoverAddImagesAndDelete(t *testing.T) {
	ps := newProductServer(t)
	ps.setDeletion(t, false)
	created := ps.createV2(t, "封面商品", nil, 1)
	firstID := created.CreatedAssets[0].ID

	body, contentType := multipartPNGs(t, map[string]string{}, 1)
	addResp := ps.do(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-assets", body, contentType)
	if addResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(addResp.Body)
		addResp.Body.Close()
		t.Fatalf("add %d %s", addResp.StatusCode, raw)
	}
	var added AssetListResponse
	ps.decode(t, addResp, &added)
	if len(added.Items) != 1 {
		t.Fatalf("%+v", added)
	}
	secondID := added.Items[0].ID

	coverResp := ps.doJSON(t, http.MethodPut, "/api/v2/products/"+created.Product.ID+"/cover", map[string]any{
		"asset_id": secondID,
	})
	if coverResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(coverResp.Body)
		coverResp.Body.Close()
		t.Fatalf("set cover %d %s", coverResp.StatusCode, raw)
	}
	var covered Detail
	ps.decode(t, coverResp, &covered)
	if covered.CoverImageAssetID == nil || *covered.CoverImageAssetID != secondID {
		t.Fatalf("%+v", covered)
	}

	clearResp := ps.do(t, http.MethodDelete, "/api/v2/products/"+created.Product.ID+"/cover", nil, "")
	if clearResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(clearResp.Body)
		clearResp.Body.Close()
		t.Fatalf("clear cover %d %s", clearResp.StatusCode, raw)
	}
	var cleared Detail
	ps.decode(t, clearResp, &cleared)
	if cleared.CoverImageAssetID != nil {
		t.Fatalf("%+v", cleared)
	}

	forbidden := ps.do(t, http.MethodDelete, "/api/v2/product-image-assets/"+firstID, nil, "")
	if forbidden.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(forbidden.Body)
		forbidden.Body.Close()
		t.Fatalf("forbidden %d %s", forbidden.StatusCode, raw)
	}
	forbidden.Body.Close()

	ps.enableDeletion(t)
	delCovered := ps.do(t, http.MethodDelete, "/api/v2/product-image-assets/"+secondID, nil, "")
	if delCovered.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(delCovered.Body)
		delCovered.Body.Close()
		t.Fatalf("delete second %d %s", delCovered.StatusCode, raw)
	}
	delCovered.Body.Close()

	delFirst := ps.do(t, http.MethodDelete, "/api/v2/product-image-assets/"+firstID, nil, "")
	if delFirst.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(delFirst.Body)
		delFirst.Body.Close()
		t.Fatalf("delete first %d %s", delFirst.StatusCode, raw)
	}
	delFirst.Body.Close()

	productDel := ps.do(t, http.MethodDelete, "/api/v2/products/"+created.Product.ID, nil, "")
	if productDel.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(productDel.Body)
		productDel.Body.Close()
		t.Fatalf("delete product %d %s", productDel.StatusCode, raw)
	}
	productDel.Body.Close()

	missing := ps.do(t, http.MethodGet, "/api/v2/products/"+created.Product.ID, nil, "")
	if missing.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(missing.Body)
		missing.Body.Close()
		t.Fatalf("get deleted %d %s", missing.StatusCode, raw)
	}
	missing.Body.Close()
}

func TestDeleteCoverAssetIsConflict(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "封面占用", nil, 1)
	ps.enableDeletion(t)
	resp := ps.do(t, http.MethodDelete, "/api/v2/product-image-assets/"+created.CreatedAssets[0].ID, nil, "")
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("cover delete %d %s", resp.StatusCode, raw)
	}
	var body map[string]any
	ps.decode(t, resp, &body)
	if body["detail"] != "商品图片仍被设为封面，不能删除" {
		t.Fatalf("%+v", body)
	}
}

func TestAddImagesSetsCoverWhenEmpty(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "空封面追加", nil, 1)
	clear := ps.do(t, http.MethodDelete, "/api/v2/products/"+created.Product.ID+"/cover", nil, "")
	clear.Body.Close()
	body, contentType := multipartPNGs(t, map[string]string{}, 1)
	add := ps.do(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-assets", body, contentType)
	var added AssetListResponse
	ps.decode(t, add, &added)
	got := ps.do(t, http.MethodGet, "/api/v2/products/"+created.Product.ID, nil, "")
	var detail Detail
	ps.decode(t, got, &detail)
	if detail.CoverImageAssetID == nil || *detail.CoverImageAssetID != added.Items[0].ID {
		t.Fatalf("cover %+v added %+v", detail.CoverImageAssetID, added.Items)
	}
	if added.Items[0].VerificationStatus != media.StatusVerified {
		t.Fatalf("%+v", added.Items[0])
	}
}

func TestGalleryArchiveDuplicateDisplayNames(t *testing.T) {
	names := map[string]struct{}{}
	first, err := archiveEntryName(ImageAsset{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", DisplayName: "同名图片", MIMEType: "image/png"}, names)
	if err != nil || first != "同名图片.png" {
		t.Fatalf("%q %v", first, err)
	}
	second, err := archiveEntryName(ImageAsset{ID: "11111111-2222-3333-4444-555555555555", DisplayName: "同名图片", MIMEType: "image/png"}, names)
	if err != nil || second != "同名图片-11111111.png" {
		t.Fatalf("%q %v", second, err)
	}
	if _, err := archiveEntryName(ImageAsset{DisplayName: "x", MIMEType: "image/gif"}, map[string]struct{}{}); err == nil {
		t.Fatal("gif should fail")
	}
}

func TestGalleryHTTPOrganizesAndDownloadsArchive(t *testing.T) {
	ps := newProductServer(t)
	created := ps.createV2(t, "图库 API 商品", nil, 2)
	first := created.CreatedAssets[0]
	second := created.CreatedAssets[1]

	folderResp := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-folders", map[string]any{"name": "精选"})
	if folderResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(folderResp.Body)
		folderResp.Body.Close()
		t.Fatalf("folder %d %s", folderResp.StatusCode, raw)
	}
	var folder GalleryFolderMutation
	ps.decode(t, folderResp, &folder)

	dup := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-folders", map[string]any{"name": "精选"})
	if dup.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(dup.Body)
		dup.Body.Close()
		t.Fatalf("dup folder %d %s", dup.StatusCode, raw)
	}
	dup.Body.Close()

	renamedFolder := ps.doJSON(t, http.MethodPatch, "/api/v2/products/"+created.Product.ID+"/image-folders/"+folder.ID, map[string]any{
		"expected_name": "精选",
		"name":          "首选",
	})
	if renamedFolder.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(renamedFolder.Body)
		renamedFolder.Body.Close()
		t.Fatalf("rename folder %d %s", renamedFolder.StatusCode, raw)
	}
	renamedFolder.Body.Close()

	ps.decode(t, ps.doJSON(t, http.MethodPatch, "/api/v2/products/"+created.Product.ID+"/image-assets/"+first.ID, map[string]any{
		"expected_display_name": first.DisplayName,
		"display_name":          "同名图片",
	}), &GalleryAssetResponse{})
	ps.decode(t, ps.doJSON(t, http.MethodPatch, "/api/v2/products/"+created.Product.ID+"/image-assets/"+second.ID, map[string]any{
		"expected_display_name": second.DisplayName,
		"display_name":          "同名图片",
	}), &GalleryAssetResponse{})

	moved := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-assets/move", map[string]any{
		"items": []map[string]any{
			{"asset_id": first.ID, "expected_folder_id": nil},
			{"asset_id": second.ID, "expected_folder_id": nil},
		},
		"folder_id": folder.ID,
	})
	if moved.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(moved.Body)
		moved.Body.Close()
		t.Fatalf("move %d %s", moved.StatusCode, raw)
	}
	var movedPage GalleryAssetPage
	ps.decode(t, moved, &movedPage)
	if len(movedPage.Items) != 2 {
		t.Fatalf("%+v", movedPage)
	}

	listed := ps.do(t, http.MethodGet, "/api/v2/products/"+created.Product.ID+"/image-assets?directory_kind=user_folder&directory_key="+folder.ID, nil, "")
	var listedPage GalleryAssetPage
	ps.decode(t, listed, &listedPage)
	if len(listedPage.Items) != 2 {
		t.Fatalf("%+v", listedPage)
	}

	archiveResp := ps.doJSON(t, http.MethodPost, "/api/v2/products/"+created.Product.ID+"/image-assets/download-archive", map[string]any{
		"asset_ids": []string{first.ID, second.ID},
	})
	raw, _ := io.ReadAll(archiveResp.Body)
	archiveResp.Body.Close()
	if archiveResp.StatusCode != http.StatusOK {
		t.Fatalf("archive %d %s", archiveResp.StatusCode, raw)
	}
	if archiveResp.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("content-type %s", archiveResp.Header.Get("Content-Type"))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 || zr.File[0].Name != "同名图片.png" {
		t.Fatalf("%v", namesOf(zr))
	}
	if zr.File[1].Name != "同名图片-"+second.ID[:8]+".png" {
		t.Fatalf("%v", namesOf(zr))
	}

	deleted := ps.do(t, http.MethodDelete, "/api/v2/products/"+created.Product.ID+"/image-folders/"+folder.ID+"?expected_name=首选", nil, "")
	var deletedFolder DeleteGalleryFolderResponse
	ps.decode(t, deleted, &deletedFolder)
	if deletedFolder.MovedToUnorganizedCount != 2 {
		t.Fatalf("%+v", deletedFolder)
	}
	bootstrap := ps.do(t, http.MethodGet, "/api/v2/products/"+created.Product.ID+"/image-library", nil, "")
	var boot GalleryBootstrap
	ps.decode(t, bootstrap, &boot)
	if boot.UnorganizedCount != 2 {
		t.Fatalf("%+v", boot)
	}
}

func namesOf(zr *zip.Reader) []string {
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}

func TestReadAssetBytesRequiresProductMatch(t *testing.T) {
	ps := newProductServer(t)
	owned := ps.createV2(t, "参考图所属商品", nil, 1)
	other := ps.createV2(t, "另一件商品", nil, 1)
	_, _, _, err := ps.svc.ReadAssetBytes(context.Background(), nil, owned.Product.ID, other.CreatedAssets[0].ID)
	if err == nil {
		t.Fatal("cross-product asset must be rejected")
	}
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Detail != "参考图不属于该商品" {
		t.Fatalf("got %v", err)
	}
	data, mime, _, err := ps.svc.ReadAssetBytes(context.Background(), nil, owned.Product.ID, owned.CreatedAssets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || mime == "" {
		t.Fatalf("owned asset mime %s bytes %d", mime, len(data))
	}
}
