package product

import (
	"encoding/json"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/storage"
)

func serializeDetail(p Product) Detail {
	intake := json.RawMessage("null")
	if len(p.IntakeJSON) > 0 {
		intake = p.IntakeJSON
	}
	return Detail{
		ID:                p.ID,
		Name:              p.Name,
		Category:          p.Category,
		Price:             p.Price,
		SourceNote:        p.SourceNote,
		CoverImageAssetID: p.CoverImageAssetID,
		Intake:            intake,
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         p.UpdatedAt,
	}
}

func serializeAsset(asset ImageAsset) AssetResponse {
	download, preview, thumb := assetURLs(asset.ID)
	return AssetResponse{
		ID:                        asset.ID,
		ProductID:                 asset.ProductID,
		MediaObjectID:             asset.MediaObjectID,
		OriginType:                asset.OriginType,
		DisplayName:               asset.DisplayName,
		OriginalFilename:          asset.OriginalFilename,
		ImageTypeKey:              asset.ImageTypeKey,
		UserFolderID:              asset.UserFolderID,
		ParentAssetID:             asset.ParentAssetID,
		SourceImageSessionAssetID: asset.SourceImageSessionAsset,
		SourceLibraryAssetID:      asset.SourceLibraryAsset,
		MIMEType:                  asset.MIMEType,
		ByteSize:                  asset.ByteSize,
		Width:                     asset.Width,
		Height:                    asset.Height,
		VerificationStatus:        asset.VerificationStatus,
		DownloadURL:               download,
		PreviewURL:                preview,
		ThumbnailURL:              thumb,
		CreatedAt:                 asset.CreatedAt,
		UpdatedAt:                 asset.UpdatedAt,
	}
}

func serializeAssets(assets []ImageAsset) []AssetResponse {
	out := make([]AssetResponse, 0, len(assets))
	for _, asset := range assets {
		out = append(out, serializeAsset(asset))
	}
	return out
}

func assetURLs(assetID string) (download, preview, thumbnail string) {
	base := "/api/v2/product-image-assets/" + assetID + "/download"
	return storage.ImageURLs(base)
}

func metadataFacts(p Product) []map[string]any {
	type pair struct {
		key string
		val *string
	}
	pairs := []pair{
		{"product_name", &p.Name},
		{"category", p.Category},
		{"price", p.Price},
		{"source_note", p.SourceNote},
	}
	out := []map[string]any{}
	for _, item := range pairs {
		if item.val == nil || strings.TrimSpace(*item.val) == "" {
			continue
		}
		out = append(out, map[string]any{
			"key":                   item.key,
			"value":                 *item.val,
			"source_type":           "user",
			"status":                "confirmed",
			"requires_confirmation": false,
			"evidence_asset_ids":    []any{},
			"conflicts":             []any{},
		})
	}
	return out
}
