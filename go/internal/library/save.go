package library

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
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

// mediaFromAsset 从库行拼 MediaObject。缺 MIME 视为来源行不完整，返回 409。
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

// assertCoherent 核对 provenance 哈希与媒体尺寸/摘要。任一字段漂移返回 409，防止复用被改过的源。
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

// SaveFromSession 把连续生图结果写入全局素材身份，复用 MediaObject。
// 非生成结果或媒体未核验返回 Validation；会话图片不存在返回 NotFound；媒体元数据不全返回 Conflict。
func (s Service) SaveFromSession(ctx context.Context, imageSessionAssetID string) (SaveResult, error) {
	var result SaveResult
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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

// SaveFromProduct 把商品图片写入全局素材身份，复用 MediaObject。
// 商品图不存在时冒泡 NotFound；媒体未核验返回 Validation；缺少 MediaObject 或核验元数据不全返回 Conflict。
func (s Service) SaveFromProduct(ctx context.Context, productImageAssetID string) (SaveResult, error) {
	var result SaveResult
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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

// normalizeUploadNames 填默认文件名并截到 maxFilename。display 为空时回落到 filename。
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

// Upload 把已校验文件写成全局素材（source_type=direct_upload）。
// 调用时机：HTTP POST /upload。有 Idempotency-Key 时同键同哈希回放已有行；键相同参数不同 Conflict。
// folderID 为空指针或空白视为未整理。失败会 Rollback 已 Stage 的文件。
// 禁区：不要在这里收录到商品图库（那是 Collect）。
func (s Service) Upload(ctx context.Context, items []UploadItem, folderID *string, idempotencyKey string) ([]SaveResult, error) {
	if folderID != nil && strings.TrimSpace(*folderID) == "" {
		folderID = nil
	}
	var results []SaveResult
	var compensation storage.Compensation
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
			var rec schema.MediaLibraryUploadKeys
			scanErr := pgxTx.WithContext(ctx).Where("idempotency_key = ?", key).Take(&rec).Error
			if scanErr == nil {
				if rec.RequestHash != requestHash {
					return apperr.Conflict("相同 idempotency key 不能用于不同的上传参数")
				}
				var ids []string
				if err := json.Unmarshal([]byte(rec.AssetIdsJSON), &ids); err != nil {
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
			if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
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
			if err := pgxTx.WithContext(ctx).Create(&schema.MediaLibraryUploadKeys{
				ID:             clockid.New(),
				IdempotencyKey: key,
				RequestHash:    requestHash,
				AssetIdsJSON:   string(raw),
				CreatedAt:      time.Now().UTC(),
			}).Error; err != nil {
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

// Archive 归档全局素材；不删除 MediaObject 或工作流引用。
// 素材不存在返回 NotFound；revision 已变或仍被工作流子图库引用返回 Conflict。
func (s Service) Archive(ctx context.Context, assetID string, expectedRevision *int) (Asset, error) {
	return s.setArchive(ctx, assetID, true, expectedRevision)
}

// Restore 取消归档（is_archived=false），不恢复已删的 MediaObject。
// 调用时机：HTTP POST /:asset_id/restore。expectedRevision 非 nil 且对不上则 Conflict。
// 找不到 NotFound。不改工作流子图库关联。
func (s Service) Restore(ctx context.Context, assetID string, expectedRevision *int) (Asset, error) {
	return s.setArchive(ctx, assetID, false, expectedRevision)
}

// setArchive 按 revision 乐观锁改归档位。仍被 workflow_media_library_assets 引用时禁止归档。
func (s Service) setArchive(ctx context.Context, assetID string, archived bool, expectedRevision *int) (Asset, error) {
	var out Asset
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		asset, err := s.loadAsset(ctx, pgxTx, assetID)
		if err != nil {
			return err
		}
		if expectedRevision != nil && asset.Revision != *expectedRevision {
			return apperr.Conflict("素材库资产 revision 已变化")
		}
		if archived {
			var rec schema.WorkflowMediaLibraryAssets
			scanErr := pgxTx.WithContext(ctx).Select("workflow_id").Where("media_library_asset_id = ?", asset.ID).Take(&rec).Error
			if scanErr == nil {
				return apperr.Conflict("素材仍被工作流素材库使用，解除关联后才能归档")
			}
			if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
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
		res := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).
			Where("id = ? AND revision = ? AND is_archived = ?", asset.ID, asset.Revision, !archived).
			Updates(map[string]any{
				"is_archived": archived,
				"archived_at": archivedAt,
				"revision":    gorm.Expr("revision + 1"),
				"updated_at":  now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return apperr.Conflict("素材库资产 revision 已变化")
		}
		out, err = s.loadAsset(ctx, pgxTx, asset.ID)
		return err
	})
	return out, err
}
