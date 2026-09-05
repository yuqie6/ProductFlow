package library

import (
	"context"
	"errors"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type FolderPage struct {
	Items       []Folder `json:"items"`
	NextAfterID *string  `json:"next_after_id"`
}

// ListOrganizationFolders is independently paged; an asset page need not contain the destination folder.
func (s Service) ListOrganizationFolders(ctx context.Context, query, afterID string, limit int) (FolderPage, error) {
	if len(query) > 255 || len(afterID) > 36 {
		return FolderPage{}, apperr.Validation("文件夹查询过长")
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	out := FolderPage{Items: []Folder{}}
	err := tx.WithGorm(ctx, s.DB, func(db *gorm.DB) error {
		q := db.WithContext(ctx).Table("media_library_folders AS f").Select("f.id, f.name, COUNT(a.id) AS count").
			Joins("LEFT JOIN media_library_assets a ON a.folder_id = f.id AND a.is_archived = FALSE").Group("f.id").Order("f.id").Limit(limit + 1)
		if query != "" {
			q = q.Where("f.name ILIKE ?", "%"+normalizeSearch(query)+"%")
		}
		if afterID != "" {
			q = q.Where("f.id > ?", afterID)
		}
		if err := q.Scan(&out.Items).Error; err != nil {
			return err
		}
		if len(out.Items) > limit {
			out.Items = out.Items[:limit]
			id := out.Items[limit-1].ID
			out.NextAfterID = &id
		}
		return nil
	})
	return out, err
}

type WorkflowLinkObservation struct {
	WorkflowID       string          `json:"workflow_id"`
	WorkflowTitle    string          `json:"workflow_title"`
	WorkflowRevision int             `json:"workflow_revision"`
	Linked           map[string]bool `json:"linked"`
}

// ObserveWorkflowLinks resolves membership only for the supplied bounded asset page.
func (s Service) ObserveWorkflowLinks(ctx context.Context, workflowID string, assetIDs []string) (WorkflowLinkObservation, error) {
	if len(assetIDs) > 100 {
		return WorkflowLinkObservation{}, apperr.Validation("关联检查最多 100 个素材")
	}
	out := WorkflowLinkObservation{Linked: map[string]bool{}}
	err := tx.WithGorm(ctx, s.DB, func(db *gorm.DB) error {
		var graph schema.WorkflowGraphs
		err := db.WithContext(ctx).Select("id, title, revision").Where("id = ? AND active = ?", workflowID, true).Take(&graph).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("工作流不存在")
		}
		if err != nil {
			return err
		}
		out.WorkflowID, out.WorkflowTitle, out.WorkflowRevision = graph.ID, graph.Title, graph.Revision
		for _, id := range assetIDs {
			out.Linked[id] = false
		}
		var linked []string
		if len(assetIDs) > 0 {
			if err := db.WithContext(ctx).Model(&schema.WorkflowMediaLibraryAssets{}).Where("workflow_id = ? AND media_library_asset_id IN ?", workflowID, assetIDs).Pluck("media_library_asset_id", &linked).Error; err != nil {
				return err
			}
		}
		for _, id := range linked {
			out.Linked[id] = true
		}
		return nil
	})
	return out, err
}
