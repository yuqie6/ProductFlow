package library

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

// Collect 按幂等键把全局素材收录为商品图片身份，复用 MediaObject，不复制 bytes。
// ID 无效、重复或超限返回 Validation；商品或素材不存在返回 NotFound；幂等键参数冲突返回 Conflict。
func (s Service) Collect(ctx context.Context, productID string, libraryIDs []string, idempotencyKey string) ([]product.ImageAsset, error) {
	var out []product.ImageAsset
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		out, err = s.collectTx(ctx, pgxTx, productID, libraryIDs, idempotencyKey)
		return err
	})
	return out, err
}

// collectTx 把全局素材收录进商品图库。Idempotency-Key 命中同一 hash 则复用；重复 ID 或超限返回 400。
func (s Service) collectTx(ctx context.Context, pgxTx *gorm.DB, productID string, libraryIDs []string, idempotencyKey string) ([]product.ImageAsset, error) {
	if len(libraryIDs) > maxCollect {
		return nil, apperr.Validationf("一次最多收录 %d 个素材", maxCollect)
	}
	if productID == "" || utf8.RuneCountInString(productID) > 36 {
		return nil, apperr.Validation("商品或素材库资产 ID 无效")
	}
	requestBytes := len(productID)
	for _, id := range libraryIDs {
		if id == "" || utf8.RuneCountInString(id) > 36 {
			return nil, apperr.Validation("商品或素材库资产 ID 无效")
		}
		requestBytes += len(id)
	}
	if requestBytes > maxCollectBytes {
		return nil, apperr.Validation("素材收录请求过大")
	}
	uniqueIDs := make([]string, 0, len(libraryIDs))
	seen := map[string]struct{}{}
	for _, id := range libraryIDs {
		if _, ok := seen[id]; ok {
			return nil, apperr.Validation("素材收录请求包含重复 ID")
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	var key, requestHash string
	if strings.TrimSpace(idempotencyKey) != "" {
		normalized, err := normalizeIdempotency(idempotencyKey, "素材收录 Idempotency-Key 不能为空")
		if err != nil {
			return nil, err
		}
		key = normalized
		hashed, err := collectRequestHash(productID, libraryIDs)
		if err != nil {
			return nil, err
		}
		requestHash = hashed
	}

	if _, err := product.Lock(ctx, pgxTx, productID); err != nil {
		return nil, err
	}

	if key != "" {
		var rec schema.MediaLibraryCollectionKeys
		scanErr := auth.ScopeMerchant(ctx, pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
			Where("product_id = ? AND idempotency_key = ?", productID, key), "merchant_id").
			Take(&rec).Error
		if scanErr == nil {
			if rec.RequestHash != requestHash {
				return nil, apperr.Conflict("相同 idempotency key 不能用于不同的素材收录参数")
			}
			replayed, err := product.LoadByLibrarySources(ctx, pgxTx, productID, uniqueIDs)
			if err != nil {
				return nil, err
			}
			if len(replayed) != len(uniqueIDs) {
				return nil, apperr.Conflict("素材收录幂等记录与商品图片不一致")
			}
			out := make([]product.ImageAsset, 0, len(uniqueIDs))
			for _, id := range uniqueIDs {
				out = append(out, replayed[id])
			}
			return out, nil
		}
		if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return nil, scanErr
		}
	}

	assets, err := lockLibraryAssets(ctx, pgxTx, uniqueIDs)
	if err != nil {
		return nil, err
	}
	byID := map[string]Asset{}
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	existing := map[string]product.ImageAsset{}
	created := map[string]product.ImageAsset{}
	ordered := make([]Asset, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		asset, ok := byID[id]
		if !ok {
			return nil, apperr.NotFound("素材库资产不存在")
		}
		ordered = append(ordered, asset)
	}
	for _, libraryAsset := range ordered {
		obj, err := validateForUse(libraryAsset)
		if err != nil {
			return nil, err
		}
		found, ok, err := product.LookupByLibrarySource(ctx, pgxTx, productID, libraryAsset.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			existing[libraryAsset.ID] = found
			continue
		}
		inserted, err := product.InsertCollected(ctx, pgxTx, product.CollectedInput{
			ProductID:            productID,
			MediaObjectID:        obj.ID,
			OriginType:           originForLibrary(libraryAsset),
			DisplayName:          libraryAsset.DisplayName,
			OriginalFilename:     libraryAsset.OriginalFilename,
			SourceLibraryAssetID: libraryAsset.ID,
		})
		if product.UniqueViolation(err) {
			found, ok, lookupErr := product.LookupByLibrarySource(ctx, pgxTx, productID, libraryAsset.ID)
			if lookupErr != nil || !ok {
				return nil, err
			}
			existing[libraryAsset.ID] = found
			continue
		}
		if err != nil {
			return nil, err
		}
		created[libraryAsset.ID] = inserted
	}
	if len(created) > 0 {
		if err := product.Touch(ctx, pgxTx, productID); err != nil {
			return nil, err
		}
	}
	if key != "" {
	merchantID := auth.ResolveMerchantID(ctx)
		err = pgxTx.WithContext(ctx).Create(&schema.MediaLibraryCollectionKeys{
			ID:             clockid.New(),
			MerchantID:     merchantID,
			ProductID:      productID,
			IdempotencyKey: key,
			RequestHash:    requestHash,
			CreatedAt:      time.Now().UTC(),
		}).Error
		if product.UniqueViolation(err) || uniqueViolation(err) {
			var rec schema.MediaLibraryCollectionKeys
			if scanErr := auth.ScopeMerchant(ctx, pgxTx.WithContext(ctx).Select("request_hash").
				Where("product_id = ? AND idempotency_key = ?", productID, key), "merchant_id").
				Take(&rec).Error; scanErr != nil {
				return nil, err
			}
			if rec.RequestHash != requestHash {
				return nil, apperr.Conflict("相同 idempotency key 不能用于不同的素材收录参数")
			}
		} else if err != nil {
			return nil, err
		}
	}
	out := make([]product.ImageAsset, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		if asset, ok := existing[id]; ok {
			out = append(out, asset)
			continue
		}
		out = append(out, created[id])
	}
	return out, nil
}
