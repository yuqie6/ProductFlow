package library

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
)

func verifiedMedia(obj media.Object) (media.Object, error) {
	if obj.ID == "" {
		return media.Object{}, apperr.Conflict("素材来源缺少 MediaObject")
	}
	if obj.VerificationStatus != media.StatusVerified {
		return media.Object{}, apperr.Validation("素材媒体尚未通过核验")
	}
	if obj.SHA256 == "" || obj.ByteSize <= 0 || obj.Width <= 0 || obj.Height <= 0 {
		return media.Object{}, apperr.Conflict("素材媒体缺少完整核验元数据")
	}
	return obj, nil
}

func provenanceFromMedia(sourceType, sourceID, filename string, obj media.Object, capturedAt time.Time, origin *string) Provenance {
	return Provenance{
		SchemaVersion:    1,
		SourceType:       sourceType,
		SourceID:         sourceID,
		SHA256:           obj.SHA256,
		MIMEType:         obj.MIMEType,
		ByteSize:         obj.ByteSize,
		Width:            obj.Width,
		Height:           obj.Height,
		OriginalFilename: filename,
		OriginType:       origin,
		CapturedAt:       capturedAt.UTC(),
	}
}

func mediaFromAsset(asset Asset) (media.Object, error) {
	if asset.MIMEType == "" {
		return media.Object{}, apperr.Conflict("素材来源缺少 MediaObject")
	}
	obj := media.Object{
		ID:                 asset.MediaObjectID,
		StoragePath:        asset.StoragePath,
		MIMEType:           asset.MIMEType,
		SHA256:             asset.SHA256,
		VerificationStatus: asset.VerificationStatus,
	}
	if asset.ByteSize != nil {
		obj.ByteSize = *asset.ByteSize
	}
	if asset.Width != nil {
		obj.Width = *asset.Width
	}
	if asset.Height != nil {
		obj.Height = *asset.Height
	}
	return verifiedMedia(obj)
}

func assertCoherent(asset Asset, obj media.Object) error {
	parsed, err := parseProvenance(asset.ProvenanceJSON)
	if err != nil {
		return apperr.Conflict("素材库 provenance 无法核验")
	}
	expected, err := provenanceHash(parsed)
	if err != nil || asset.ProvenanceHash != expected {
		return apperr.Conflict("素材库来源与媒体元数据不一致")
	}
	if parsed.SourceType != asset.SourceType || parsed.SourceID != asset.SourceID ||
		parsed.SHA256 != obj.SHA256 || parsed.MIMEType != obj.MIMEType ||
		parsed.ByteSize != obj.ByteSize || parsed.Width != obj.Width || parsed.Height != obj.Height ||
		parsed.OriginalFilename != asset.OriginalFilename {
		return apperr.Conflict("素材库来源与媒体元数据不一致")
	}
	if asset.SourceProductAssetID != nil && asset.SourceProductMediaID != nil && *asset.SourceProductMediaID != obj.ID {
		return apperr.Conflict("素材库商品来源与媒体不一致")
	}
	if asset.SourceSessionAssetID != nil && asset.SourceSessionMediaID != nil && *asset.SourceSessionMediaID != obj.ID {
		return apperr.Conflict("素材库会话来源与媒体不一致")
	}
	return nil
}

func validateIntegrity(asset Asset) (media.Object, error) {
	obj, err := mediaFromAsset(asset)
	if err != nil {
		return media.Object{}, err
	}
	if err := assertCoherent(asset, obj); err != nil {
		return media.Object{}, err
	}
	return obj, nil
}

func validateForUse(asset Asset) (media.Object, error) {
	if asset.IsArchived {
		return media.Object{}, apperr.Conflict("归档素材不能收录到商品")
	}
	return validateIntegrity(asset)
}

func originForLibrary(asset Asset) string {
	if asset.SourceType == SourceSession {
		return "image_session_attach"
	}
	if asset.SourceType == SourceProduct {
		if asset.SourceProductOrigin != nil && *asset.SourceProductOrigin != "" {
			return *asset.SourceProductOrigin
		}
		if asset.ProvenanceJSON != nil {
			if raw, ok := asset.ProvenanceJSON["origin_type"].(string); ok && raw != "" {
				switch raw {
				case "upload", "workflow_generation", "image_session_attach", "local_edit":
					return raw
				}
			}
		}
	}
	return "upload"
}

func (s Service) SaveFromSession(ctx context.Context, imageSessionAssetID string) (SaveResult, error) {
	var result SaveResult
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		row, err := loadSessionAsset(ctx, pgxTx, imageSessionAssetID)
		if err != nil {
			return err
		}
		if row.Kind != "generated_image" {
			return apperr.Validation("只有生成结果可以保存到素材库")
		}
		obj, err := verifiedMedia(row.Media)
		if err != nil {
			return err
		}
		if existing, ok, err := findBySource(ctx, pgxTx, SourceSession, row.ID); err != nil {
			return err
		} else if ok {
			asset, err := s.loadAsset(ctx, pgxTx, existing)
			if err != nil {
				return err
			}
			result = SaveResult{Asset: asset, Created: false}
			return nil
		}
		sourceID := row.ID
		p := provenanceFromMedia(SourceSession, sourceID, row.OriginalFilename, obj, row.CreatedAt, nil)
		id, err := insertLibraryAsset(ctx, pgxTx, Asset{
			MediaObjectID:        obj.ID,
			SourceType:           SourceSession,
			SourceID:             sourceID,
			SourceSessionAssetID: &sourceID,
			DisplayName:          row.OriginalFilename,
			OriginalFilename:     row.OriginalFilename,
		}, p)
		if uniqueViolation(err) {
			existing, ok, findErr := findBySource(ctx, pgxTx, SourceSession, row.ID)
			if findErr != nil {
				return findErr
			}
			if !ok {
				return err
			}
			asset, loadErr := s.loadAsset(ctx, pgxTx, existing)
			if loadErr != nil {
				return loadErr
			}
			result = SaveResult{Asset: asset, Created: false}
			return nil
		}
		if err != nil {
			return err
		}
		asset, err := s.loadAsset(ctx, pgxTx, id)
		if err != nil {
			return err
		}
		result = SaveResult{Asset: asset, Created: true}
		return nil
	})
	return result, err
}

func (s Service) SaveFromProduct(ctx context.Context, productImageAssetID string) (SaveResult, error) {
	var result SaveResult
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		asset, err := product.LoadImageForUpdate(ctx, pgxTx, productImageAssetID)
		if err != nil {
			return err
		}
		obj, err := s.Media.Get(ctx, pgxTx, asset.MediaObjectID)
		if err != nil {
			return apperr.Conflict("素材来源缺少 MediaObject")
		}
		obj, err = verifiedMedia(obj)
		if err != nil {
			return err
		}
		if existing, ok, err := findBySource(ctx, pgxTx, SourceProduct, asset.ID); err != nil {
			return err
		} else if ok {
			loaded, err := s.loadAsset(ctx, pgxTx, existing)
			if err != nil {
				return err
			}
			result = SaveResult{Asset: loaded, Created: false}
			return nil
		}
		origin := asset.OriginType
		p := provenanceFromMedia(SourceProduct, asset.ID, asset.OriginalFilename, obj, asset.CreatedAt, &origin)
		sourceID := asset.ID
		id, err := insertLibraryAsset(ctx, pgxTx, Asset{
			MediaObjectID:        obj.ID,
			SourceType:           SourceProduct,
			SourceID:             sourceID,
			SourceProductAssetID: &sourceID,
			DisplayName:          asset.DisplayName,
			OriginalFilename:     asset.OriginalFilename,
		}, p)
		if uniqueViolation(err) {
			existing, ok, findErr := findBySource(ctx, pgxTx, SourceProduct, asset.ID)
			if findErr != nil || !ok {
				return err
			}
			loaded, loadErr := s.loadAsset(ctx, pgxTx, existing)
			if loadErr != nil {
				return loadErr
			}
			result = SaveResult{Asset: loaded, Created: false}
			return nil
		}
		if err != nil {
			return err
		}
		loaded, err := s.loadAsset(ctx, pgxTx, id)
		if err != nil {
			return err
		}
		result = SaveResult{Asset: loaded, Created: true}
		return nil
	})
	return result, err
}

func normalizeUploadNames(filename, displayName string) (string, string) {
	normalized := strings.TrimSpace(filename)
	if normalized == "" {
		normalized = "upload.png"
	}
	display := strings.TrimSpace(displayName)
	if display == "" {
		display = normalized
	}
	if utf8.RuneCountInString(normalized) > maxFilename {
		normalized = string([]rune(normalized)[:maxFilename])
	}
	if utf8.RuneCountInString(display) > maxFilename {
		display = string([]rune(display)[:maxFilename])
	}
	if display == "" {
		display = normalized
	}
	return normalized, display
}

func (s Service) Upload(ctx context.Context, items []UploadItem, folderID *string, idempotencyKey string) ([]SaveResult, error) {
	if folderID != nil && strings.TrimSpace(*folderID) == "" {
		folderID = nil
	}
	var results []SaveResult
	var compensation storage.Compensation
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if folderID != nil {
			if _, err := getFolder(ctx, pgxTx, *folderID); err != nil {
				return err
			}
		}
		var requestHash string
		var key string
		if strings.TrimSpace(idempotencyKey) != "" {
			normalized, err := normalizeIdempotency(idempotencyKey, "素材库上传 Idempotency-Key 不能为空")
			if err != nil {
				return err
			}
			key = normalized
			hashed, err := uploadRequestHash(folderID, items)
			if err != nil {
				return err
			}
			requestHash = hashed
			var existingHash, assetIDsJSON string
			scanErr := pgxTx.QueryRow(ctx, `
				SELECT request_hash, asset_ids_json FROM media_library_upload_keys WHERE idempotency_key = $1
			`, key).Scan(&existingHash, &assetIDsJSON)
			if scanErr == nil {
				if existingHash != requestHash {
					return apperr.Conflict("相同 idempotency key 不能用于不同的上传参数")
				}
				var ids []string
				if err := json.Unmarshal([]byte(assetIDsJSON), &ids); err != nil {
					return err
				}
				for _, id := range ids {
					asset, err := s.loadAsset(ctx, pgxTx, id)
					if err != nil {
						return err
					}
					results = append(results, SaveResult{Asset: asset, Created: false})
				}
				return nil
			}
			if scanErr != nil && !errors.Is(scanErr, pgx.ErrNoRows) {
				return scanErr
			}
		}
		createdIDs := make([]string, 0, len(items))
		for _, item := range items {
			filename, display := normalizeUploadNames(item.Filename, "")
			obj, err := s.Media.Stage(ctx, pgxTx, item.Content, item.MIMEType, &compensation)
			if err != nil {
				compensation.Rollback()
				return err
			}
			captured := s.now()
			p := provenanceFromMedia(SourceUpload, obj.ID, filename, obj, captured, nil)
			id, err := insertLibraryAsset(ctx, pgxTx, Asset{
				MediaObjectID:    obj.ID,
				SourceType:       SourceUpload,
				SourceID:         obj.ID,
				DisplayName:      display,
				OriginalFilename: filename,
				FolderID:         folderID,
			}, p)
			if err != nil {
				compensation.Rollback()
				return err
			}
			createdIDs = append(createdIDs, id)
		}
		if key != "" {
			raw, _ := json.Marshal(createdIDs)
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO media_library_upload_keys (id, idempotency_key, request_hash, asset_ids_json, created_at)
				VALUES ($1, $2, $3, $4, NOW())
			`, clockid.New(), key, requestHash, string(raw)); err != nil {
				compensation.Rollback()
				return err
			}
		}
		for _, id := range createdIDs {
			asset, err := s.loadAsset(ctx, pgxTx, id)
			if err != nil {
				compensation.Rollback()
				return err
			}
			results = append(results, SaveResult{Asset: asset, Created: true})
		}
		compensation.Release()
		return nil
	})
	if err != nil {
		compensation.Rollback()
	}
	return results, err
}

func (s Service) Archive(ctx context.Context, assetID string, expectedRevision *int) (Asset, error) {
	return s.setArchive(ctx, assetID, true, expectedRevision)
}

func (s Service) Restore(ctx context.Context, assetID string, expectedRevision *int) (Asset, error) {
	return s.setArchive(ctx, assetID, false, expectedRevision)
}

func (s Service) setArchive(ctx context.Context, assetID string, archived bool, expectedRevision *int) (Asset, error) {
	var out Asset
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		asset, err := s.loadAsset(ctx, pgxTx, assetID)
		if err != nil {
			return err
		}
		if expectedRevision != nil && asset.Revision != *expectedRevision {
			return apperr.Conflict("素材库资产 revision 已变化")
		}
		if archived {
			var workflowID string
			scanErr := pgxTx.QueryRow(ctx, `
				SELECT workflow_id FROM workflow_media_library_assets WHERE media_library_asset_id = $1 LIMIT 1
			`, asset.ID).Scan(&workflowID)
			if scanErr == nil {
				return apperr.Conflict("素材仍被工作流素材库使用，解除关联后才能归档")
			}
			if !errors.Is(scanErr, pgx.ErrNoRows) {
				return scanErr
			}
		}
		if asset.IsArchived == archived {
			out = asset
			return nil
		}
		now := s.now()
		var archivedAt any
		if archived {
			archivedAt = now
		}
		tag, err := pgxTx.Exec(ctx, `
			UPDATE media_library_assets
			SET is_archived = $1, archived_at = $2, revision = revision + 1, updated_at = $3
			WHERE id = $4 AND revision = $5 AND is_archived = $6
		`, archived, archivedAt, now, asset.ID, asset.Revision, !archived)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return apperr.Conflict("素材库资产 revision 已变化")
		}
		out, err = s.loadAsset(ctx, pgxTx, asset.ID)
		return err
	})
	return out, err
}
