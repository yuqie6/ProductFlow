package product

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

func (GraphGuard) LoadSource(ctx context.Context, tx *gorm.DB, productID string) (*graph.SourceProduct, error) {
	var rec schema.Products
	err := tx.WithContext(ctx).Select("id, name, category, price, source_note, current_fact_set_version_id").
		Where("id = ?", productID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &graph.SourceProduct{
		ID:               rec.ID,
		Name:             rec.Name,
		Category:         rec.Category,
		Price:            rec.Price,
		SourceNote:       rec.SourceNote,
		CurrentFactSetID: rec.CurrentFactSetVersionID,
	}, nil
}

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
	}, nil
}

func (GraphGuard) BoundAssetMeta(ctx context.Context, tx *gorm.DB, productID, assetID string) (string, string, error) {
	var row struct {
		DisplayName string `gorm:"column:display_name"`
		MIMEType    string `gorm:"column:mime_type"`
	}
	err := tx.WithContext(ctx).Table("product_image_assets AS a").
		Select("a.display_name, COALESCE(m.mime_type, '') AS mime_type").
		Joins("JOIN media_objects m ON m.id = a.media_object_id").
		Where("a.product_id = ? AND a.id = ?", productID, assetID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", nil
	}
	return row.DisplayName, row.MIMEType, err
}
