package graph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

type productSummary struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Category   *string `json:"category"`
	Price      *string `json:"price"`
	SourceNote *string `json:"source_note"`
}

type factSetSnapshot struct {
	ID        string           `json:"id"`
	ProductID string           `json:"product_id"`
	Version   int              `json:"version"`
	Facts     []map[string]any `json:"facts"`
}

type productSourceSnapshot struct {
	SourceProductID  *string
	FactSetVersionID *string
	SourceProduct    *productSummary
	FactSetVersion   *factSetSnapshot
	Facts            []map[string]any
}

func loadProductSourceSnapshot(ctx context.Context, tx *gorm.DB, graphProductID string, config map[string]any) (productSourceSnapshot, error) {
	payload := config
	if payload == nil {
		payload = map[string]any{}
	}
	_, hasSourceBinding := payload["source_product_id"]
	rawSourceID := payload["source_product_id"]
	if rawSourceID != nil {
		s, ok := rawSourceID.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return productSourceSnapshot{}, apperr.Validation("商品资料节点的 source_product_id 无效")
		}
	}
	var sourceProductID *string
	if s, ok := rawSourceID.(string); ok {
		trimmed := strings.TrimSpace(s)
		sourceProductID = &trimmed
	}
	if !hasSourceBinding {
		sourceProductID = &graphProductID
	}
	if sourceProductID == nil {
		if payload["fact_set_version_id"] != nil {
			return productSourceSnapshot{}, apperr.Validation("未绑定商品的商品资料节点不能绑定 fact_set_version_id")
		}
		return productSourceSnapshot{}, nil
	}

	var product productSummary
	var currentFactSetID *string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, name, category, price::text, source_note, current_fact_set_version_id
		FROM products WHERE id = $1
	`, *sourceProductID).Scan(&product.ID, &product.Name, &product.Category, &product.Price, &product.SourceNote, &currentFactSetID)
	if errors.Is(err, sqldb.ErrNoRows) {
		return productSourceSnapshot{}, apperr.Validation("商品资料节点绑定的商品不存在")
	}
	if err != nil {
		return productSourceSnapshot{}, err
	}

	rawFactSetID := payload["fact_set_version_id"]
	var factSetID *string
	if rawFactSetID != nil {
		s, ok := rawFactSetID.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return productSourceSnapshot{}, apperr.Validation("商品资料节点的 fact_set_version_id 无效")
		}
		trimmed := strings.TrimSpace(s)
		factSetID = &trimmed
	} else if currentFactSetID != nil && *currentFactSetID != "" {
		factSetID = currentFactSetID
	}

	out := productSourceSnapshot{
		SourceProductID:  sourceProductID,
		FactSetVersionID: factSetID,
		SourceProduct:    &product,
		Facts:            []map[string]any{},
	}
	if factSetID == nil {
		return out, nil
	}
	var set factSetSnapshot
	var payloadJSON []byte
	err = pfdb.QueryRow(ctx, tx, `
		SELECT id, product_id, version, payload_json
		FROM product_fact_set_versions
		WHERE id = $1 AND product_id = $2
	`, *factSetID, *sourceProductID).Scan(&set.ID, &set.ProductID, &set.Version, &payloadJSON)
	if errors.Is(err, sqldb.ErrNoRows) {
		if rawFactSetID != nil {
			return productSourceSnapshot{}, apperr.Validation("fact_set_version_id 不属于绑定商品")
		}
		return out, nil
	}
	if err != nil {
		return productSourceSnapshot{}, err
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
	set.Facts = parsed.Facts
	out.FactSetVersion = &set
	out.Facts = parsed.Facts
	return out, nil
}

func mergeRuntimeFacts(facts []map[string]any, source *productSourceSnapshot) []map[string]any {
	if source == nil || source.SourceProduct == nil {
		return facts
	}
	existing := map[string]struct{}{}
	for _, item := range facts {
		key, _ := item["key"].(string)
		existing[strings.ToLower(key)] = struct{}{}
	}
	product := source.SourceProduct
	identity := []struct {
		key   string
		value *string
	}{
		{"product_name", strPtr(product.Name)},
		{"category", product.Category},
		{"price", product.Price},
		{"source_note", product.SourceNote},
	}
	extras := []map[string]any{}
	for _, item := range identity {
		if item.value == nil || strings.TrimSpace(*item.value) == "" {
			continue
		}
		if _, ok := existing[strings.ToLower(item.key)]; ok {
			continue
		}
		extras = append(extras, map[string]any{"key": item.key, "value": *item.value})
	}
	return append(extras, facts...)
}

func strPtr(s string) *string { return &s }
