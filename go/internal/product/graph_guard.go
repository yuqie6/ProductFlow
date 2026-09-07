package product

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// LoadSource 实现 graph.ProductGuard：找不到商品时返回 nil, nil。
func (GraphGuard) LoadSource(ctx context.Context, tx *gorm.DB, productID string) (*graph.SourceProduct, error) {
	var rec schema.Products
	err := auth.ScopeMerchant(ctx, tx.WithContext(ctx).Select("id, name, category, price, source_note, current_fact_set_version_id").
		Where("id = ?", productID), "merchant_id").Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sourceProductFromSchema(rec), nil
}

// LoadSources 批量返回商品资料。缺行不会写入 map，交由 graph 维持单商品读取的校验语义。
func (GraphGuard) LoadSources(ctx context.Context, tx *gorm.DB, productIDs []string) (map[string]*graph.SourceProduct, error) {
	out := map[string]*graph.SourceProduct{}
	if len(productIDs) == 0 {
		return out, nil
	}
	var recs []schema.Products
	err := auth.ScopeMerchant(ctx, tx.WithContext(ctx).
		Select("id, name, category, price, source_note, current_fact_set_version_id").
		Where("id IN ?", productIDs), "merchant_id").
		Find(&recs).Error
	if err != nil {
		return nil, err
	}
	for _, rec := range recs {
		out[rec.ID] = sourceProductFromSchema(rec)
	}
	return out, nil
}

func sourceProductFromSchema(rec schema.Products) *graph.SourceProduct {
	return &graph.SourceProduct{
		ID:               rec.ID,
		Name:             rec.Name,
		Category:         rec.Category,
		Price:            rec.Price,
		SourceNote:       rec.SourceNote,
		CurrentFactSetID: rec.CurrentFactSetVersionID,
	}
}

// LoadFactSet 实现 graph.ProductGuard：找不到版本时返回 nil, nil。
func (GraphGuard) LoadFactSet(ctx context.Context, tx *gorm.DB, factSetID, productID string) (*graph.FactSet, error) {
	q := tx.WithContext(ctx).Where("id = ?", factSetID)
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	var rec schema.ProductFactSetVersions
	err := q.Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return factSetFromSchema(rec), nil
}

// LoadFactSets 批量返回 fact 版本。payload_json 仍只在 product guard 内解析。
func (GraphGuard) LoadFactSets(ctx context.Context, tx *gorm.DB, factSetIDs []string) (map[string]*graph.FactSet, error) {
	out := map[string]*graph.FactSet{}
	if len(factSetIDs) == 0 {
		return out, nil
	}
	var recs []schema.ProductFactSetVersions
	if err := tx.WithContext(ctx).
		Select("id, product_id, version, payload_json").
		Where("id IN ?", factSetIDs).
		Find(&recs).Error; err != nil {
		return nil, err
	}
	for _, rec := range recs {
		set := factSetFromSchema(rec)
		out[rec.ID] = set
	}
	return out, nil
}

func factSetFromSchema(rec schema.ProductFactSetVersions) *graph.FactSet {
	var parsed struct {
		Facts []map[string]any `json:"facts"`
	}
	if rec.PayloadJSON != "" {
		_ = json.Unmarshal([]byte(rec.PayloadJSON), &parsed)
	}
	if parsed.Facts == nil {
		parsed.Facts = []map[string]any{}
	}
	return &graph.FactSet{
		ID:        rec.ID,
		ProductID: rec.ProductID,
		Version:   rec.Version,
		Facts:     parsed.Facts,
	}
}

// BoundAssetMetas 返回绑定图的显示名与 MIME。找不到时不写入 map，不报 NotFound。
func (GraphGuard) BoundAssetMetas(ctx context.Context, tx *gorm.DB, productID string, assetIDs []string) (map[string]graph.BoundAssetMetadata, error) {
	out := map[string]graph.BoundAssetMetadata{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          string `gorm:"column:id"`
		DisplayName string `gorm:"column:display_name"`
		MIMEType    string `gorm:"column:mime_type"`
	}
	err := tx.WithContext(ctx).Table("product_image_assets AS a").
		Select("a.id, a.display_name, COALESCE(m.mime_type, '') AS mime_type").
		Joins("JOIN media_objects m ON m.id = a.media_object_id").
		Where("a.product_id = ? AND a.id IN ?", productID, assetIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = graph.BoundAssetMetadata{DisplayName: row.DisplayName, MIMEType: row.MIMEType}
	}
	return out, nil
}
