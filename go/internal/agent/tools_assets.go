package agent

import (
	"context"
	"os"
	"time"

	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/product"
)

const (
	renameAssetTool  = "rename_product_image_asset_v1"
	createFolderTool = "create_product_image_folder_v1"
	renameFolderTool = "rename_product_image_folder_v1"
	moveAssetsTool   = "move_product_image_assets_v1"
	assetMaxBytes    = 20 * 1024 * 1024
)

func galleryMeta(item product.GalleryAssetResponse) AssetMetadata {
	var gen map[string]string
	if item.Generation != nil {
		gen = map[string]string{
			"workflow_id": item.Generation.WorkflowID,
			"node_id":     item.Generation.NodeID,
			"node_run_id": item.Generation.NodeRunID,
		}
	}
	return AssetMetadata{
		ID: item.ID, DisplayName: item.DisplayName, OriginalFilename: item.OriginalFilename,
		OriginType: item.OriginType, ImageTypeKey: item.ImageTypeKey, ImageTypeTitle: item.ImageTypeTitle,
		UserFolderID: item.UserFolderID, UserFolderName: item.UserFolderName, MIMEType: item.MIMEType,
		ByteSize: item.ByteSize, Width: item.Width, Height: item.Height,
		VerificationStatus: item.VerificationStatus, ParentAssetID: item.ParentAssetID,
		Generation: gen, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func libraryMeta(item library.AssetResponse) AssetMetadata {
	return AssetMetadata{
		ID: item.ID, DisplayName: item.DisplayName, OriginalFilename: item.OriginalFilename,
		OriginType: item.SourceType, UserFolderID: item.FolderID, UserFolderName: item.FolderName,
		MIMEType: item.MIMEType, ByteSize: item.ByteSize, Width: item.Width, Height: item.Height,
		VerificationStatus: item.VerificationStatus, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s Service) ListProductAssets(ctx context.Context, conversationID, directoryKind, directoryKey, query, sort, after string, limit int) (AssetListResponse, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return AssetListResponse{}, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return AssetListResponse{}, err
	}
	page, err := s.Product.ListGalleryAssets(ctx, *conv.ProductID, product.GalleryListInput{
		DirectoryKind: directoryKind, DirectoryKey: directoryKey, Query: query, Sort: sort, After: after, Limit: limit,
	})
	if err != nil {
		return AssetListResponse{}, err
	}
	items := make([]AssetMetadata, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, galleryMeta(item))
	}
	return AssetListResponse{Items: items, NextCursor: page.NextCursor}, nil
}

func (s Service) InspectProductAssets(ctx context.Context, conversationID string, assetIDs []string) ([]AssetMetadata, error) {
	ids, err := normalizeAssetIDs(assetIDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, apperr.Validation("请明确提供要查看的商品图片 ID")
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	out := make([]AssetMetadata, 0, len(ids))
	for _, id := range ids {
		asset, err := s.Product.GetAsset(ctx, id)
		if err != nil {
			return nil, err
		}
		if asset.ProductID != *conv.ProductID {
			return nil, apperr.NotFound("商品图片不存在")
		}
		out = append(out, AssetMetadata{
			ID: asset.ID, DisplayName: asset.DisplayName, OriginalFilename: asset.OriginalFilename,
			OriginType: asset.OriginType, ImageTypeKey: asset.ImageTypeKey, UserFolderID: asset.UserFolderID,
			MIMEType: asset.MIMEType, ByteSize: asset.ByteSize, Width: asset.Width, Height: asset.Height,
			VerificationStatus: asset.VerificationStatus, ParentAssetID: asset.ParentAssetID,
			CreatedAt: asset.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out, nil
}

type AssetContent struct {
	Bytes       []byte
	MediaType   string
	DisplayName string
}

func (s Service) ReadProductAssetContent(ctx context.Context, conversationID, assetID string) (AssetContent, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return AssetContent{}, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return AssetContent{}, err
	}
	asset, err := s.Product.GetAsset(ctx, assetID)
	if err != nil {
		return AssetContent{}, err
	}
	if asset.ProductID != *conv.ProductID {
		return AssetContent{}, apperr.NotFound("商品图片不存在")
	}
	return s.readAssetBytes(asset.StoragePath, asset.MIMEType, asset.DisplayName, asset.ByteSize, asset.Width, asset.Height)
}

func (s Service) ListLibraryAssets(ctx context.Context, conversationID, query, cursor string, limit int) (AssetListResponse, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return AssetListResponse{}, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return AssetListResponse{}, err
	}
	if limit < 1 {
		limit = assetListDefaultLimit
	}
	page, err := s.Library.List(ctx, library.ListFilter{Limit: limit, Cursor: cursor, Search: query})
	if err != nil {
		return AssetListResponse{}, err
	}
	items := make([]AssetMetadata, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, libraryMeta(item))
	}
	return AssetListResponse{Items: items, NextCursor: page.NextCursor}, nil
}

func (s Service) InspectLibraryAssets(ctx context.Context, conversationID string, assetIDs []string) ([]AssetMetadata, error) {
	ids, err := normalizeAssetIDs(assetIDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, apperr.Validation("请明确提供要查看的商品图片 ID")
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return nil, err
	}
	out := make([]AssetMetadata, 0, len(ids))
	for _, id := range ids {
		asset, err := s.Library.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if asset.IsArchived {
			return nil, apperr.NotFound("全局素材不存在或已归档")
		}
		out = append(out, libraryMeta(library.AssetResponse{
			ID: asset.ID, DisplayName: asset.DisplayName, OriginalFilename: asset.OriginalFilename,
			SourceType: asset.SourceType, FolderID: asset.FolderID, FolderName: asset.FolderName,
			MIMEType: asset.MIMEType, ByteSize: asset.ByteSize, Width: asset.Width, Height: asset.Height,
			VerificationStatus: asset.VerificationStatus, CreatedAt: asset.CreatedAt,
		}))
	}
	return out, nil
}

func (s Service) ReadLibraryAssetContent(ctx context.Context, conversationID, assetID string) (AssetContent, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return AssetContent{}, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return AssetContent{}, err
	}
	asset, err := s.Library.Get(ctx, assetID)
	if err != nil {
		return AssetContent{}, err
	}
	if asset.IsArchived {
		return AssetContent{}, apperr.NotFound("全局素材不存在或已归档")
	}
	return s.readAssetBytes(asset.StoragePath, asset.MIMEType, asset.DisplayName, asset.ByteSize, asset.Width, asset.Height)
}

func (s Service) readAssetBytes(storagePath, mimeType, displayName string, byteSize, width, height *int) (AssetContent, error) {
	if byteSize != nil && *byteSize > assetMaxBytes {
		return AssetContent{}, apperr.Validation("图片超过 Agent 单张图片大小上限")
	}
	abs, err := s.Media.Files.Resolve(storagePath)
	if err != nil {
		return AssetContent{}, apperr.NotFound("商品图片文件不存在")
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return AssetContent{}, apperr.NotFound("商品图片文件不存在")
	}
	if len(content) > assetMaxBytes {
		return AssetContent{}, apperr.Validation("图片超过 Agent 单张图片大小上限")
	}
	verified, err := media.Inspect(content, mimeType)
	if err != nil {
		return AssetContent{}, apperr.Conflict("商品图片文件与已核验媒体元数据不一致")
	}
	if byteSize != nil && verified.ByteSize != *byteSize {
		return AssetContent{}, apperr.Conflict("商品图片文件与已核验媒体元数据不一致")
	}
	if width != nil && verified.Width != *width {
		return AssetContent{}, apperr.Conflict("商品图片文件与已核验媒体元数据不一致")
	}
	if height != nil && verified.Height != *height {
		return AssetContent{}, apperr.Conflict("商品图片文件与已核验媒体元数据不一致")
	}
	return AssetContent{Bytes: content, MediaType: verified.MIMEType, DisplayName: displayName}, nil
}

func (s Service) PrepareAssetRename(ctx context.Context, conversationID, assetID, targetName string) (map[string]any, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	asset, err := s.Product.GetAsset(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if asset.ProductID != *conv.ProductID {
		return nil, apperr.NotFound("商品图片不存在")
	}
	return map[string]any{
		"asset_id": asset.ID, "expected_display_name": asset.DisplayName, "target_display_name": targetName,
	}, nil
}

func (s Service) ApplyAssetRename(ctx context.Context, conversationID, idempotencyKey, assetID, expected, target string) (map[string]any, error) {
	before := map[string]any{"asset_id": assetID, "display_name": expected}
	tgt := map[string]any{"asset_id": assetID, "display_name": target}
	replay, found, err := s.lookupMutation(ctx, conversationID, renameAssetTool, idempotencyKey, "rename_asset", before, tgt)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	renamed, err := s.Product.RenameGalleryAsset(ctx, *conv.ProductID, assetID, expected, target)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"asset_id": renamed.ID, "display_name": target, "applied": expected != target}
	extra := map[string]any{"asset_id": assetID, "expected_display_name": expected, "target_display_name": target}
	if err := s.recordMutation(ctx, conversationID, renameAssetTool, idempotencyKey, "rename_asset", before, tgt, result, extra); err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) PrepareFolderCreate(ctx context.Context, conversationID, name string) (map[string]any, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	var folder schema.ProductAssetFolders
	err = s.DB.WithContext(ctx).Select("id, name").
		Where("product_id = ? AND name = ?", *conv.ProductID, name).
		Take(&folder).Error
	if err == nil {
		return map[string]any{"folder_id": folder.ID, "name": folder.Name}, nil
	}
	return map[string]any{"folder_id": newID(), "name": name}, nil
}

func (s Service) ApplyFolderCreate(ctx context.Context, conversationID, idempotencyKey, folderID, name string) (map[string]any, error) {
	before := map[string]any{"folder_id": folderID, "exists": false}
	target := map[string]any{"folder_id": folderID, "name": name}
	replay, found, err := s.lookupMutation(ctx, conversationID, createFolderTool, idempotencyKey, "create_folder", before, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	folder, err := s.Product.CreateGalleryFolderWithID(ctx, *conv.ProductID, folderID, name)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"folder_id": folder.ID, "name": folder.Name, "sort_order": folder.SortOrder, "applied": true}
	if err := s.recordMutation(ctx, conversationID, createFolderTool, idempotencyKey, "create_folder", before, target, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) PrepareFolderRename(ctx context.Context, conversationID, folderID, targetName string) (map[string]any, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	var folder schema.ProductAssetFolders
	err = s.DB.WithContext(ctx).Select("name").
		Where("product_id = ? AND id = ?", *conv.ProductID, folderID).
		Take(&folder).Error
	if err != nil {
		return nil, apperr.NotFound("文件夹不存在")
	}
	expected := folder.Name
	return map[string]any{"folder_id": folderID, "expected_name": expected, "target_name": targetName}, nil
}

func (s Service) ApplyFolderRename(ctx context.Context, conversationID, idempotencyKey, folderID, expected, target string) (map[string]any, error) {
	before := map[string]any{"folder_id": folderID, "name": expected}
	tgt := map[string]any{"folder_id": folderID, "name": target}
	replay, found, err := s.lookupMutation(ctx, conversationID, renameFolderTool, idempotencyKey, "rename_folder", before, tgt)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	folder, err := s.Product.RenameGalleryFolder(ctx, *conv.ProductID, folderID, expected, target)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"folder_id": folder.ID, "name": folder.Name, "applied": expected != target}
	if err := s.recordMutation(ctx, conversationID, renameFolderTool, idempotencyKey, "rename_folder", before, tgt, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) PrepareAssetMove(ctx context.Context, conversationID string, assetIDs []string, targetFolderID *string) (map[string]any, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	moves := make([]map[string]any, 0, len(assetIDs))
	for _, id := range assetIDs {
		asset, err := s.Product.GetAsset(ctx, id)
		if err != nil {
			return nil, err
		}
		if asset.ProductID != *conv.ProductID {
			return nil, apperr.NotFound("商品图片不存在")
		}
		moves = append(moves, map[string]any{"asset_id": asset.ID, "expected_folder_id": asset.UserFolderID})
	}
	return map[string]any{"moves": moves, "target_folder_id": targetFolderID}, nil
}

func (s Service) ApplyAssetMove(ctx context.Context, conversationID, idempotencyKey string, moves []product.GalleryAssetMove, targetFolderID *string) (map[string]any, error) {
	rawMoves := make([]map[string]any, 0, len(moves))
	ids := make([]string, 0, len(moves))
	for _, move := range moves {
		rawMoves = append(rawMoves, map[string]any{"asset_id": move.AssetID, "expected_folder_id": move.ExpectedFolderID})
		ids = append(ids, move.AssetID)
	}
	before := map[string]any{"moves": rawMoves}
	target := map[string]any{"target_folder_id": targetFolderID, "moves": rawMoves}
	replay, found, err := s.lookupMutation(ctx, conversationID, moveAssetsTool, idempotencyKey, "move_assets", before, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	if _, err := s.Product.MoveGalleryAssets(ctx, *conv.ProductID, moves, targetFolderID); err != nil {
		return nil, err
	}
	result := map[string]any{"asset_ids": ids, "target_folder_id": targetFolderID, "applied": true}
	if err := s.recordMutation(ctx, conversationID, moveAssetsTool, idempotencyKey, "move_assets", before, target, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}
