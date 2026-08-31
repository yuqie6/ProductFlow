package media

import (
	"context"
	"errors"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// Deleted 是已无逻辑引用、事务内删 media_objects 行之后、待 commit 再清文件的媒体。
// 调用方必须在 tx 成功后再 Files.DeleteWithVariants(StoragePath)。不要在事务里删磁盘。
type Deleted struct {
	ID          string
	StoragePath string // 事务成功后再 Files.DeleteWithVariants；不要在 tx 里删磁盘
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

// mediaObjectReferenced 查商品图、会话图、全局素材、局部编辑 mask 是否还指着这个 MediaObject。
// 任一表查到行就 true；四张都 NotFound 才 false。查询 error 原样冒泡，不要当成「没引用」去删。
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
