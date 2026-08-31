package graph

import (
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
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
	LegacyFallback   bool
}

// loadProductSourceSnapshot 经 ProductGuard 读商品与 fact 版本，编进 snapshot。
// Guard 返回 nil,nil 时这里变成 Validation（绑定了不存在的商品）。未写 source_product_id 绑本图商品。
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

	guard, err := requireProductGuard(ctx)
	if err != nil {
		return productSourceSnapshot{}, err
	}
	src, err := guard.LoadSource(ctx, tx, *sourceProductID)
	if err != nil {
		return productSourceSnapshot{}, err
	}
	if src == nil {
		return productSourceSnapshot{}, apperr.Validation("商品资料节点绑定的商品不存在")
	}
	product := productSummary{
		ID: src.ID, Name: src.Name, Category: src.Category, Price: src.Price, SourceNote: src.SourceNote,
	}
	currentFactSetID := src.CurrentFactSetID

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
		LegacyFallback:   !hasSourceBinding,
	}
	if factSetID == nil {
		return out, nil
	}
	set, err := guard.LoadFactSet(ctx, tx, *factSetID, *sourceProductID)
	if err != nil {
		return productSourceSnapshot{}, err
	}
	if set == nil {
		if rawFactSetID != nil {
			return productSourceSnapshot{}, apperr.Validation("fact_set_version_id 不属于绑定商品")
		}
		return out, nil
	}
	out.FactSetVersion = &factSetSnapshot{
		ID: set.ID, ProductID: set.ProductID, Version: set.Version, Facts: set.Facts,
	}
	out.Facts = set.Facts
	return out, nil
}

// mergeRuntimeFacts 用商品名称等身份字段补 facts 里没有的 key。已有同名 key（大小写不敏感）不覆盖。
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
