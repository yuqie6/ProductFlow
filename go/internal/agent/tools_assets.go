package agent

import (
	"context"
	"os"
	"time"

	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/product"
)

const assetMaxBytes = 20 * 1024 * 1024

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

// ListProductAssets 列出商品图库有界元数据，不把整库交给 Turn。conversation 不存在返回 NotFound。非商品工作流返回 Conflict；图库筛选非法返回 Validation。
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

// InspectProductAssets 按明确 id 检查商品图片元数据。asset id 非法或为空返回 Validation。图片不存在或不属于当前商品返回 NotFound；非商品工作流返回 Conflict。
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

// AssetContent 是内部工具面读图时的字节袋，不是 JSON DTO，也不进 SSE。
//
// Bytes 是已核验原图；与行内 MIME / 尺寸对不上会拒绝。浏览器请走媒体 URL，不要把本结构塞进 Turn 投影或 Goal。
type AssetContent struct {
	Bytes       []byte // 已核验原图 bytes
	MediaType   string // 与核验 MIME 对齐
	DisplayName string // 展示名，随 bytes 返回
}

// ReadProductAssetContent 读取商品图片 bytes；超过上限或与核验元数据不一致时拒绝。超过单张上限返回 Validation。文件不存在返回 NotFound；与核验元数据不一致返回 Conflict。
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

// ListLibraryAssets 给全局 Agent 列有界图库元数据（无 bytes），对应内部 GET .../media-library。
//
// conversation 必须是 global，否则校验失败。cursor 是图库 opaque 游标不是页码。query 可选。归档素材不出现。
// limit<1 回落到默认 50。只读，不写工具账本、lease、Goal。
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

// InspectLibraryAssets 按明确 id 检查未归档全局素材元数据。asset id 非法或为空返回 Validation。素材不存在或已归档返回 NotFound；非全局 conversation 返回 Conflict。
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

// ReadLibraryAssetContent 读取全局素材 bytes；已归档返回 NotFound。
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

// readAssetBytes 从已核验存储路径读图片 bytes，并与库内 mime/尺寸/大小对账。不一致返回 Conflict；超过 Agent 单张上限返回 Validation。
//
// 商品图与全局图库的 Read*Content 共用。不把未核验文件交给模型。
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
