package media

import (
	"context"
	"errors"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// Deleted 是已无逻辑引用、事务内删行之后待提交后清文件的媒体。
type Deleted struct {
	ID          string
	StoragePath string
}

// PruneUnreferenced 删除已无商品图/会话图/全局素材/局部编辑 mask 引用的 MediaObject 行。
// 文件删除发生在调用方 commit 之后。
func PruneUnreferenced(ctx context.Context, tx *gorm.DB, mediaIDs []string) ([]Deleted, error) {
	deleted := make([]Deleted, 0)
	seen := map[string]struct{}{}
	for _, mediaID := range mediaIDs {
		if mediaID == "" {
			continue
		}
		if _, ok := seen[mediaID]; ok {
			continue
		}
		seen[mediaID] = struct{}{}
		referenced, err := mediaObjectReferenced(ctx, tx, mediaID)
		if err != nil {
			return nil, err
		}
		if referenced {
			continue
		}
		var rec schema.MediaObjects
		err = tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id, storage_path").Where("id = ?", mediaID).Take(&rec).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if err := tx.WithContext(ctx).Where("id = ?", mediaID).Delete(&schema.MediaObjects{}).Error; err != nil {
			return nil, err
		}
		deleted = append(deleted, Deleted{ID: mediaID, StoragePath: rec.StoragePath})
	}
	return deleted, nil
}

func mediaObjectReferenced(ctx context.Context, tx *gorm.DB, mediaID string) (bool, error) {
	var asset schema.ProductImageAssets
	err := tx.WithContext(ctx).Select("id").Where("media_object_id = ?", mediaID).Take(&asset).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	var session schema.ImageSessionAssets
	err = tx.WithContext(ctx).Select("id").Where("media_object_id = ?", mediaID).Take(&session).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	var library schema.MediaLibraryAssets
	err = tx.WithContext(ctx).Select("id").Where("media_object_id = ?", mediaID).Take(&library).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	var task schema.LocalImageEditTasks
	err = tx.WithContext(ctx).Select("id").Where("mask_media_object_id = ?", mediaID).Take(&task).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return false, nil
}
