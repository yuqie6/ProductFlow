package product

import (
	"context"
	"encoding/json"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/graph"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

func (GraphGuard) LoadSource(ctx context.Context, tx *gorm.DB, productID string) (*graph.SourceProduct, error) {
	var out graph.SourceProduct
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, name, category, price::text, source_note, current_fact_set_version_id
		FROM products WHERE id = $1
	`, productID).Scan(&out.ID, &out.Name, &out.Category, &out.Price, &out.SourceNote, &out.CurrentFactSetID)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (GraphGuard) LoadFactSet(ctx context.Context, tx *gorm.DB, factSetID, productID string) (*graph.FactSet, error) {
	q := `
		SELECT id, product_id, version, payload_json
		FROM product_fact_set_versions WHERE id = $1
	`
	args := []any{factSetID}
	if productID != "" {
		q += ` AND product_id = $2`
		args = append(args, productID)
	}
	var out graph.FactSet
	var payloadJSON []byte
	err := pfdb.QueryRow(ctx, tx, q, args...).Scan(&out.ID, &out.ProductID, &out.Version, &payloadJSON)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Facts []map[string]any `json:"facts"`
	}
	if len(payloadJSON) > 0 {
		_ = json.Unmarshal(payloadJSON, &parsed)
	}
	if parsed.Facts == nil {
		parsed.Facts = []map[string]any{}
	}
	out.Facts = parsed.Facts
	return &out, nil
}

func (GraphGuard) BoundAssetMeta(ctx context.Context, tx *gorm.DB, productID, assetID string) (string, string, error) {
	var display, mime string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT a.display_name, COALESCE(m.mime_type, '')
		FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.product_id = $1 AND a.id = $2
	`, productID, assetID).Scan(&display, &mime)
	if errors.Is(err, sqldb.ErrNoRows) {
		return "", "", nil
	}
	return display, mime, err
}
