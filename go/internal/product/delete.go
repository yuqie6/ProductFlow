package product

import (
	"context"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// DeleteProduct 在无 running GraphRun 时删商品；视觉体系若被其他商品占用则 409。
func (s Service) DeleteProduct(ctx context.Context, productID string) error {
	var files []media.Deleted
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		var running int
		err := pfdb.QueryRow(ctx, pgxTx, `
			SELECT 1 FROM workflow_graph_runs r
			JOIN workflow_graphs g ON g.id = r.graph_id
			WHERE g.product_id = $1 AND r.status = 'running'
			LIMIT 1
		`, productID).Scan(&running)
		if err == nil {
			return apperr.Validation("商品工作流运行中，稍后删除")
		}
		if !errors.Is(err, sqldb.ErrNoRows) {
			return err
		}
		rows, err := pfdb.Query(ctx, pgxTx, `SELECT media_object_id FROM product_image_assets WHERE product_id = $1`, productID)
		if err != nil {
			return err
		}
		var mediaIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			mediaIDs = append(mediaIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		removable, err := prepareVisualSystemCleanup(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `DELETE FROM products WHERE id = $1`, productID); err != nil {
			return err
		}
		if err := deleteOwnedVisualVersions(ctx, pgxTx, removable); err != nil {
			return err
		}
		files, err = media.PruneUnreferenced(ctx, pgxTx, mediaIDs)
		return err
	})
	if err != nil {
		return err
	}
	for _, file := range files {
		_ = s.Media.Files.DeleteWithVariants(file.StoragePath)
	}
	s.Media.Files.RemoveEmptyProductDirs(productID)
	return nil
}

type visualVersionRef struct {
	versionID      string
	visualSystemID string
}

func prepareVisualSystemCleanup(ctx context.Context, tx *gorm.DB, productID string) ([]visualVersionRef, error) {
	owned, err := collectOwnedVisualVersionIDs(ctx, tx, productID)
	if err != nil {
		return nil, err
	}
	referenced, err := collectReferencedVisualVersionIDs(ctx, tx, productID)
	if err != nil {
		return nil, err
	}
	for versionID := range referenced {
		if _, ok := owned[versionID]; !ok {
			return nil, apperr.Conflict("商品图片仍被其他视觉体系版本引用，不能删除商品")
		}
	}
	removable := make([]visualVersionRef, 0)
	for versionID := range owned {
		var visualSystemID string
		err := pfdb.QueryRow(ctx, tx, `SELECT visual_system_id FROM visual_system_versions WHERE id = $1`, versionID).Scan(&visualSystemID)
		if errors.Is(err, sqldb.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		external, err := visualVersionHasExternalConsumer(ctx, tx, productID, versionID)
		if err != nil {
			return nil, err
		}
		if external {
			if _, usedHere := referenced[versionID]; usedHere {
				return nil, apperr.Conflict("商品视觉体系仍被其他商品使用，不能删除其参考图片")
			}
			continue
		}
		if _, err := pfdb.Exec(ctx, tx, `DELETE FROM visual_system_version_references WHERE visual_system_version_id = $1`, versionID); err != nil {
			return nil, err
		}
		removable = append(removable, visualVersionRef{versionID: versionID, visualSystemID: visualSystemID})
	}
	return removable, nil
}

func deleteOwnedVisualVersions(ctx context.Context, tx *gorm.DB, versions []visualVersionRef) error {
	systemIDs := map[string]struct{}{}
	for _, version := range versions {
		if _, err := pfdb.Exec(ctx, tx, `DELETE FROM visual_system_versions WHERE id = $1`, version.versionID); err != nil {
			return err
		}
		systemIDs[version.visualSystemID] = struct{}{}
	}
	for systemID := range systemIDs {
		var remaining string
		err := pfdb.QueryRow(ctx, tx, `SELECT id FROM visual_system_versions WHERE visual_system_id = $1 LIMIT 1`, systemID).Scan(&remaining)
		if errors.Is(err, sqldb.ErrNoRows) {
			if _, err := pfdb.Exec(ctx, tx, `DELETE FROM visual_systems WHERE id = $1`, systemID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func collectOwnedVisualVersionIDs(ctx context.Context, tx *gorm.DB, productID string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	queries := []string{
		`SELECT DISTINCT n.config_json->>'visual_system_version_id'
		 FROM workflow_graph_nodes n
		 JOIN workflow_graphs g ON g.id = n.graph_id
		 WHERE g.product_id = $1 AND NULLIF(n.config_json->>'visual_system_version_id', '') IS NOT NULL`,
		`SELECT DISTINCT a.payload_json->>'visual_system_version_id'
		 FROM workflow_graph_artifacts a
		 JOIN workflow_graphs g ON g.id = a.graph_id
		 WHERE g.product_id = $1 AND NULLIF(a.payload_json->>'visual_system_version_id', '') IS NOT NULL`,
	}
	for _, sql := range queries {
		rows, err := pfdb.Query(ctx, tx, sql, productID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if id != "" {
				out[id] = struct{}{}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func collectReferencedVisualVersionIDs(ctx context.Context, tx *gorm.DB, productID string) (map[string]struct{}, error) {
	rows, err := pfdb.Query(ctx, tx, `
		SELECT DISTINCT r.visual_system_version_id
		FROM visual_system_version_references r
		JOIN product_image_assets a ON a.id = r.asset_id
		WHERE a.product_id = $1
	`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

func visualVersionHasExternalConsumer(ctx context.Context, tx *gorm.DB, productID, versionID string) (bool, error) {
	var one int
	err := pfdb.QueryRow(ctx, tx, `
		SELECT 1 FROM workflow_graph_nodes n
		JOIN workflow_graphs g ON g.id = n.graph_id
		WHERE n.config_json->>'visual_system_version_id' = $1 AND g.product_id <> $2
		LIMIT 1
	`, versionID, productID).Scan(&one)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sqldb.ErrNoRows) {
		return false, err
	}
	err = pfdb.QueryRow(ctx, tx, `
		SELECT 1 FROM workflow_graph_artifacts a
		JOIN workflow_graphs g ON g.id = a.graph_id
		WHERE a.payload_json->>'visual_system_version_id' = $1 AND g.product_id <> $2
		LIMIT 1
	`, versionID, productID).Scan(&one)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sqldb.ErrNoRows) {
		return false, err
	}
	err = pfdb.QueryRow(ctx, tx, `
		SELECT 1 FROM workflow_recipe_versions WHERE preferred_visual_system_version_id = $1 LIMIT 1
	`, versionID).Scan(&one)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sqldb.ErrNoRows) {
		return false, err
	}
	return false, nil
}
