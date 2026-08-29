package library

import (
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/storage"
)

func serializeAsset(asset Asset) (AssetResponse, error) {
	if asset.MediaObjectID == "" || asset.VerificationStatus == "" && asset.MIMEType == "" {
		return AssetResponse{}, apperr.NotFound("素材库媒体对象不存在")
	}
	if asset.MIMEType == "" {
		return AssetResponse{}, apperr.NotFound("素材库媒体对象不存在")
	}
	download, preview, thumb := storage.ImageURLs("/api/media-library/" + asset.ID + "/download")
	tags := asset.Tags
	if tags == nil {
		tags = []Tag{}
	}
	outTags := make([]Tag, 0, len(tags))
	for _, tag := range tags {
		outTags = append(outTags, Tag{ID: tag.ID, Name: tag.Name})
	}
	return AssetResponse{
		ID:                 asset.ID,
		MediaObjectID:      asset.MediaObjectID,
		SourceType:         asset.SourceType,
		SourceID:           asset.SourceID,
		DisplayName:        asset.DisplayName,
		FolderID:           asset.FolderID,
		FolderName:         asset.FolderName,
		Tags:               outTags,
		OriginalFilename:   asset.OriginalFilename,
		Revision:           asset.Revision,
		IsArchived:         asset.IsArchived,
		ArchivedAt:         asset.ArchivedAt,
		ProvenanceHash:     asset.ProvenanceHash,
		MIMEType:           asset.MIMEType,
		ByteSize:           asset.ByteSize,
		Width:              asset.Width,
		Height:             asset.Height,
		VerificationStatus: asset.VerificationStatus,
		DownloadURL:        download,
		PreviewURL:         preview,
		ThumbnailURL:       thumb,
		CreatedAt:          asset.CreatedAt,
		UpdatedAt:          asset.UpdatedAt,
	}, nil
}

func serializeAssets(assets []Asset) ([]AssetResponse, error) {
	out := make([]AssetResponse, 0, len(assets))
	for _, asset := range assets {
		item, err := serializeAsset(asset)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
