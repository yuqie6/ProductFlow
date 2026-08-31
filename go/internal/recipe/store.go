package recipe

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

type recipeRecord struct {
	ID               string
	Kind             string
	Origin           string
	OfficialKey      *string
	CurrentVersionID *string
	ArchivedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Current          *versionRecord
	Versions         []versionRecord
}

type versionRecord struct {
	ID                             string
	RecipeID                       string
	Version                        int
	SchemaVersion                  int
	CatalogVersion                 int
	CreationSource                 string
	Title                          string
	Description                    *string
	PayloadJSON                    []byte
	PayloadHash                    string
	GovernanceJSON                 []byte
	PreferredVisualSystemVersionID *string
	CreatedAt                      time.Time
}

type applicationRecord struct {
	ID                   string
	ProductID            string
	RecipeVersionID      string
	RecipeID             string
	RecipeVersion        int
	GraphID              string
	OperationGroupID     string
	Mode                 string
	IdempotencyKey       string
	RequestHash          string
	AddedNodeIDs         []string
	AddedEdgeIDs         []string
	PreviewGraphRevision *int
	PreviewDigest        *string
	UpdatedNodeIDs       []string
	RequiredBindings     []string
}

func recipeFromSchema(rec schema.WorkflowRecipes) recipeRecord {
	return recipeRecord{
		ID:               rec.ID,
		Kind:             rec.Kind,
		Origin:           rec.Origin,
		OfficialKey:      rec.OfficialKey,
		CurrentVersionID: rec.CurrentVersionID,
		ArchivedAt:       rec.ArchivedAt,
		CreatedAt:        rec.CreatedAt,
		UpdatedAt:        rec.UpdatedAt,
	}
}

// versionFromSchema 把版本行收成应用记录。GovernanceJSON 为 nil 时保持 nil，与「空对象 {}」区分。
func versionFromSchema(rec schema.WorkflowRecipeVersions) versionRecord {
	ver := versionRecord{
		ID:                             rec.ID,
		RecipeID:                       rec.RecipeID,
		Version:                        rec.Version,
		SchemaVersion:                  rec.SchemaVersion,
		CatalogVersion:                 rec.CatalogVersion,
		CreationSource:                 rec.CreationSource,
		Title:                          rec.Title,
		Description:                    rec.Description,
		PayloadJSON:                    []byte(rec.PayloadJSON),
		PayloadHash:                    rec.PayloadHash,
		PreferredVisualSystemVersionID: rec.PreferredVisualSystemVersionID,
		CreatedAt:                      rec.CreatedAt,
	}
	if rec.GovernanceJSON != nil {
		ver.GovernanceJSON = []byte(*rec.GovernanceJSON)
	}
	return ver
}

func getProductTarget(ctx context.Context, tx *gorm.DB, productID string, forUpdate bool) (productTarget, error) {
	q := tx.WithContext(ctx).Select("id, current_fact_set_version_id").Where("id = ?", productID)
	if forUpdate {
		q = q.Clauses(pfdb.ForUpdate())
	}
	var rec schema.Products
	err := q.Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return productTarget{}, apperr.NotFound("商品不存在")
	}
	return productTarget{ID: rec.ID, FactSetVersionID: rec.CurrentFactSetVersionID}, err
}

func visualSystemVersionExists(ctx context.Context, tx *gorm.DB, id string) error {
	var rec schema.VisualSystemVersions
	err := tx.WithContext(ctx).Select("id").Where("id = ?", id).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.NotFound("视觉系统版本不存在")
	}
	return err
}

// listRecipes 只列 origin=user 的配方（系统种子不出现在设置页）。默认去掉已归档，再批量补 current 版本。
func listRecipes(ctx context.Context, tx *gorm.DB, includeArchived bool) ([]recipeRecord, error) {
	q := tx.WithContext(ctx).Where("origin = ?", "user")
	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}
	var rows []schema.WorkflowRecipes
	if err := q.Order("updated_at DESC, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]recipeRecord, 0, len(rows))
	ids := []string{}
	for _, rec := range rows {
		item := recipeFromSchema(rec)
		out = append(out, item)
		if item.CurrentVersionID != nil {
			ids = append(ids, *item.CurrentVersionID)
		}
	}
	versions, err := loadVersionsByIDs(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].CurrentVersionID == nil {
			continue
		}
		if ver, ok := versions[*out[i].CurrentVersionID]; ok {
			copied := ver
			out[i].Current = &copied
		}
	}
	return out, nil
}

// loadRecipe 读一条用户配方及全部版本。official 种子对外伪装成 404，避免设置页改系统配方。
// forUpdate 锁 recipes 行，给 Append/Archive 用。
func loadRecipe(ctx context.Context, tx *gorm.DB, recipeID string, forUpdate bool) (recipeRecord, error) {
	q := tx.WithContext(ctx).Where("id = ?", recipeID)
	if forUpdate {
		q = q.Clauses(pfdb.ForUpdate())
	}
	var rec schema.WorkflowRecipes
	err := q.Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return recipeRecord{}, apperr.NotFound("工作流配方不存在")
	}
	if err != nil {
		return recipeRecord{}, err
	}
	out := recipeFromSchema(rec)
	if out.Origin == originOfficial {
		return recipeRecord{}, apperr.NotFound("工作流配方不存在")
	}
	versions, err := loadRecipeVersions(ctx, tx, out.ID)
	if err != nil {
		return recipeRecord{}, err
	}
	out.Versions = versions
	if out.CurrentVersionID != nil {
		for i := range versions {
			if versions[i].ID == *out.CurrentVersionID {
				copied := versions[i]
				out.Current = &copied
				break
			}
		}
	}
	return out, nil
}

func loadRecipeVersions(ctx context.Context, tx *gorm.DB, recipeID string) ([]versionRecord, error) {
	var rows []schema.WorkflowRecipeVersions
	err := tx.WithContext(ctx).Where("recipe_id = ?", recipeID).Order("version").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]versionRecord, 0, len(rows))
	for _, rec := range rows {
		out = append(out, versionFromSchema(rec))
	}
	return out, nil
}

func loadVersionsByIDs(ctx context.Context, tx *gorm.DB, ids []string) (map[string]versionRecord, error) {
	out := map[string]versionRecord{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []schema.WorkflowRecipeVersions
	if err := tx.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, rec := range rows {
		out[rec.ID] = versionFromSchema(rec)
	}
	return out, nil
}

func insertRecipe(ctx context.Context, tx *gorm.DB, rec recipeRecord) error {
	now := time.Now().UTC()
	return tx.WithContext(ctx).Create(&schema.WorkflowRecipes{
		ID:          rec.ID,
		Kind:        rec.Kind,
		Origin:      rec.Origin,
		OfficialKey: rec.OfficialKey,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error
}

// insertVersion 写入一版 payload。GovernanceJSON 空切片不当空对象写入，保持列 NULL。
func insertVersion(ctx context.Context, tx *gorm.DB, ver versionRecord) error {
	row := schema.WorkflowRecipeVersions{
		ID:                             ver.ID,
		RecipeID:                       ver.RecipeID,
		Version:                        ver.Version,
		SchemaVersion:                  ver.SchemaVersion,
		CatalogVersion:                 ver.CatalogVersion,
		CreationSource:                 ver.CreationSource,
		Title:                          ver.Title,
		Description:                    ver.Description,
		PayloadJSON:                    string(ver.PayloadJSON),
		PayloadHash:                    ver.PayloadHash,
		PreferredVisualSystemVersionID: ver.PreferredVisualSystemVersionID,
		CreatedAt:                      time.Now().UTC(),
	}
	if len(ver.GovernanceJSON) > 0 {
		s := string(ver.GovernanceJSON)
		row.GovernanceJSON = &s
	}
	return tx.WithContext(ctx).Create(&row).Error
}

func setCurrentVersion(ctx context.Context, tx *gorm.DB, recipeID, versionID string) error {
	return tx.WithContext(ctx).Model(&schema.WorkflowRecipes{}).Where("id = ?", recipeID).Updates(map[string]any{
		"current_version_id": versionID,
		"updated_at":         time.Now().UTC(),
	}).Error
}

func archiveRecipeRow(ctx context.Context, tx *gorm.DB, recipeID string) error {
	now := time.Now().UTC()
	return tx.WithContext(ctx).Model(&schema.WorkflowRecipes{}).Where("id = ?", recipeID).Updates(map[string]any{
		"archived_at": now,
		"updated_at":  now,
	}).Error
}

type applicationScan struct {
	ID                   string  `gorm:"column:id"`
	ProductID            string  `gorm:"column:product_id"`
	RecipeVersionID      string  `gorm:"column:recipe_version_id"`
	RecipeID             string  `gorm:"column:recipe_id"`
	RecipeVersion        int     `gorm:"column:version"`
	GraphID              string  `gorm:"column:graph_id"`
	Mode                 string  `gorm:"column:mode"`
	IdempotencyKey       string  `gorm:"column:idempotency_key"`
	RequestHash          string  `gorm:"column:request_hash"`
	AddedNodeIdsJSON     string  `gorm:"column:added_node_ids_json"`
	AddedEdgeIdsJSON     string  `gorm:"column:added_edge_ids_json"`
	PreviewGraphRevision *int    `gorm:"column:preview_graph_revision"`
	PreviewDigest        *string `gorm:"column:preview_digest"`
	UpdatedNodeIdsJSON   *string `gorm:"column:updated_node_ids_json"`
	RequiredBindingsJSON *string `gorm:"column:required_bindings_json"`
}

// applicationByKey 按商品 + Idempotency-Key 找已确认的应用。没有行返回 (nil, nil)，不是 404。
func applicationByKey(ctx context.Context, tx *gorm.DB, productID, key string) (*applicationRecord, error) {
	var row applicationScan
	err := tx.WithContext(ctx).Table("workflow_recipe_applications AS a").
		Select(`a.id, a.product_id, a.recipe_version_id, v.recipe_id, v.version, a.graph_id, a.mode,
			a.idempotency_key, a.request_hash, a.added_node_ids_json, a.added_edge_ids_json,
			a.preview_graph_revision, a.preview_digest, a.updated_node_ids_json, a.required_bindings_json`).
		Joins("JOIN workflow_recipe_versions v ON v.id = a.recipe_version_id").
		Where("a.product_id = ? AND a.idempotency_key = ?", productID, key).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec := applicationRecord{
		ID:                   row.ID,
		ProductID:            row.ProductID,
		RecipeVersionID:      row.RecipeVersionID,
		RecipeID:             row.RecipeID,
		RecipeVersion:        row.RecipeVersion,
		GraphID:              row.GraphID,
		Mode:                 row.Mode,
		IdempotencyKey:       row.IdempotencyKey,
		RequestHash:          row.RequestHash,
		PreviewGraphRevision: row.PreviewGraphRevision,
		PreviewDigest:        row.PreviewDigest,
	}
	rec.AddedNodeIDs = decodeStringList([]byte(row.AddedNodeIdsJSON))
	rec.AddedEdgeIDs = decodeStringList([]byte(row.AddedEdgeIdsJSON))
	if row.UpdatedNodeIdsJSON != nil {
		rec.UpdatedNodeIDs = decodeStringList([]byte(*row.UpdatedNodeIdsJSON))
	} else {
		rec.UpdatedNodeIDs = decodeStringList(nil)
	}
	if row.RequiredBindingsJSON != nil {
		rec.RequiredBindings = decodeStringList([]byte(*row.RequiredBindingsJSON))
	} else {
		rec.RequiredBindings = decodeStringList(nil)
	}
	return &rec, nil
}

// insertApplication 记下一次确认。nil 切片编成 []，避免下次回放把 null 和空数组当成不同 hash。
func insertApplication(ctx context.Context, tx *gorm.DB, rec applicationRecord) error {
	addedNodes, err := json.Marshal(nonNilStrings(rec.AddedNodeIDs))
	if err != nil {
		return err
	}
	addedEdges, err := json.Marshal(nonNilStrings(rec.AddedEdgeIDs))
	if err != nil {
		return err
	}
	updated, err := json.Marshal(nonNilStrings(rec.UpdatedNodeIDs))
	if err != nil {
		return err
	}
	bindings, err := json.Marshal(nonNilStrings(rec.RequiredBindings))
	if err != nil {
		return err
	}
	updatedStr := string(updated)
	bindingsStr := string(bindings)
	return tx.WithContext(ctx).Create(&schema.WorkflowRecipeApplications{
		ID:                   rec.ID,
		ProductID:            rec.ProductID,
		RecipeVersionID:      rec.RecipeVersionID,
		GraphID:              rec.GraphID,
		OperationGroupID:     rec.OperationGroupID,
		Mode:                 rec.Mode,
		SchemaVersion:        2,
		IdempotencyKey:       rec.IdempotencyKey,
		RequestHash:          rec.RequestHash,
		AddedNodeIdsJSON:     string(addedNodes),
		AddedEdgeIdsJSON:     string(addedEdges),
		PreviewGraphRevision: rec.PreviewGraphRevision,
		PreviewDigest:        rec.PreviewDigest,
		UpdatedNodeIdsJSON:   &updatedStr,
		RequiredBindingsJSON: &bindingsStr,
		CreatedAt:            time.Now().UTC(),
	}).Error
}

func decodeStringList(raw []byte) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
