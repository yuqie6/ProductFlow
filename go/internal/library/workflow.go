package library

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

func (s Service) ListWorkflow(ctx context.Context, productID, workflowID string, limit int) (WorkflowList, error) {
	if limit < 1 {
		limit = maxWorkflow
	}
	if limit > maxWorkflow {
		limit = maxWorkflow
	}
	var out WorkflowList
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, false); err != nil {
			return err
		}
		var err error
		out, err = s.listWorkflowTx(ctx, pgxTx, productID, workflowID, limit)
		return err
	})
	return out, err
}

func (s Service) listWorkflowTx(ctx context.Context, pgxTx pgx.Tx, productID, workflowID string, limit int) (WorkflowList, error) {
	rows, err := pgxTx.Query(ctx, `
		SELECT w.media_library_asset_id, w.created_at, lib.id, src.id
		FROM workflow_media_library_assets w
		JOIN media_library_assets a ON a.id = w.media_library_asset_id
		LEFT JOIN product_image_assets lib
			ON lib.source_library_asset_id = a.id AND lib.product_id = $2
		LEFT JOIN product_image_assets src
			ON src.id = a.source_product_asset_id AND src.product_id = $2
		WHERE w.workflow_id = $1
		ORDER BY w.created_at DESC, w.media_library_asset_id DESC
		LIMIT $3
	`, workflowID, productID, limit)
	if err != nil {
		return WorkflowList{}, err
	}
	defer rows.Close()
	type item struct {
		id        string
		linkedAt  time.Time
		collectID *string
		sourceID  *string
	}
	var items []item
	ids := []string{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.linkedAt, &it.collectID, &it.sourceID); err != nil {
			return WorkflowList{}, err
		}
		items = append(items, it)
		ids = append(ids, it.id)
	}
	if err := rows.Err(); err != nil {
		return WorkflowList{}, err
	}
	assets, err := reloadInOrder(ctx, pgxTx, uniqueKeepOrder(ids))
	if err != nil {
		return WorkflowList{}, err
	}
	byID := map[string]Asset{}
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	out := WorkflowList{WorkflowID: workflowID, Items: []WorkflowItem{}}
	for _, it := range items {
		serialized, err := serializeAsset(byID[it.id])
		if err != nil {
			return WorkflowList{}, err
		}
		productAssetID := it.collectID
		if productAssetID == nil {
			productAssetID = it.sourceID
		}
		out.Items = append(out.Items, WorkflowItem{
			Asset:               serialized,
			ProductImageAssetID: productAssetID,
			LinkedAt:            it.linkedAt,
		})
	}
	return out, nil
}

func (s Service) SyncWorkflow(ctx context.Context, productID, workflowID string, libraryIDs []string) (WorkflowList, error) {
	if len(libraryIDs) == 0 {
		return WorkflowList{}, apperr.Validation("至少选择一个素材")
	}
	if len(libraryIDs) > maxWorkflow {
		return WorkflowList{}, apperr.Validationf("一次最多关联 %d 个素材", maxWorkflow)
	}
	seen := map[string]struct{}{}
	uniqueIDs := make([]string, 0, len(libraryIDs))
	for _, id := range libraryIDs {
		if id == "" || utf8.RuneCountInString(id) > 36 {
			return WorkflowList{}, apperr.Validation("素材 ID 无效")
		}
		if _, ok := seen[id]; ok {
			return WorkflowList{}, apperr.Validation("关联请求包含重复素材")
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	var out WorkflowList
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, false); err != nil {
			return err
		}
		assets, err := loadAssets(ctx, pgxTx, assetSelect+` WHERE a.id = ANY($1)`, uniqueIDs)
		if err != nil {
			return err
		}
		if len(assets) != len(uniqueIDs) {
			return apperr.NotFound("素材库资产不存在")
		}
		for _, asset := range assets {
			if _, err := validateForUse(asset); err != nil {
				return err
			}
		}
		sameProduct := map[string]struct{}{}
		for _, asset := range assets {
			if asset.SourceProductProductID != nil && *asset.SourceProductProductID == productID {
				sameProduct[asset.ID] = struct{}{}
			}
		}
		toCollect := make([]string, 0)
		for _, id := range uniqueIDs {
			if _, ok := sameProduct[id]; !ok {
				toCollect = append(toCollect, id)
			}
		}
		if len(toCollect) > 0 {
			if _, err := s.collectTx(ctx, pgxTx, productID, toCollect, ""); err != nil {
				return err
			}
		}
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, true); err != nil {
			return err
		}
		existing := map[string]struct{}{}
		rows, err := pgxTx.Query(ctx, `
			SELECT media_library_asset_id FROM workflow_media_library_assets
			WHERE workflow_id = $1 AND media_library_asset_id = ANY($2)
		`, workflowID, uniqueIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			existing[id] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range uniqueIDs {
			if _, ok := existing[id]; ok {
				continue
			}
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO workflow_media_library_assets (workflow_id, media_library_asset_id, created_at)
				VALUES ($1, $2, NOW())
			`, workflowID, id); err != nil {
				return err
			}
		}
		out, err = s.listWorkflowTx(ctx, pgxTx, productID, workflowID, maxWorkflow)
		return err
	})
	return out, err
}

func (s Service) RemoveWorkflow(ctx context.Context, productID, workflowID, libraryAssetID string) error {
	return tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, false); err != nil {
			return err
		}
		tag, err := pgxTx.Exec(ctx, `
			DELETE FROM workflow_media_library_assets
			WHERE workflow_id = $1 AND media_library_asset_id = $2
		`, workflowID, libraryAssetID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("工作流素材关联不存在")
		}
		return nil
	})
}

func uniqueKeepOrder(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
