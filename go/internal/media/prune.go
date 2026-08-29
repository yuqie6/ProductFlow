package media

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Deleted 是已无逻辑引用、事务内删行之后待提交后清文件的媒体。
type Deleted struct {
	ID          string
	StoragePath string
}

// PruneUnreferenced 删除已无商品图/会话图/全局素材/局部编辑 mask 引用的 MediaObject 行。
// 文件删除发生在调用方 commit 之后。
func PruneUnreferenced(ctx context.Context, tx pgx.Tx, mediaIDs []string) ([]Deleted, error) {
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
		var referenced bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM product_image_assets WHERE media_object_id = $1
				UNION ALL
				SELECT 1 FROM image_session_assets WHERE media_object_id = $1
				UNION ALL
				SELECT 1 FROM media_library_assets WHERE media_object_id = $1
				UNION ALL
				SELECT 1 FROM local_image_edit_tasks WHERE mask_media_object_id = $1
			)
		`, mediaID).Scan(&referenced)
		if err != nil {
			return nil, err
		}
		if referenced {
			continue
		}
		var path string
		err = tx.QueryRow(ctx, `
			SELECT storage_path FROM media_objects WHERE id = $1 FOR UPDATE
		`, mediaID).Scan(&path)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM media_objects WHERE id = $1`, mediaID); err != nil {
			return nil, err
		}
		deleted = append(deleted, Deleted{ID: mediaID, StoragePath: path})
	}
	return deleted, nil
}
