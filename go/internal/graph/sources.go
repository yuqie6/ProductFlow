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

type productSourceBinding struct {
	sourceProductID *string
	legacyFallback  bool
}

type productSourceLookup struct {
	nodeID  string
	config  map[string]any
	binding productSourceBinding
	source  *SourceProduct
	factSet *string
}

// parseProductSourceBinding 校验 source_product_id，并保留 v2 未绑定时的空源语义。
// fact_set_version_id 的类型校验要等商品存在后执行，保持单节点读取的错误优先级。
func parseProductSourceBinding(graphProductID string, config map[string]any) (productSourceBinding, error) {
	payload := config
	if payload == nil {
		payload = map[string]any{}
	}
	_, hasSourceBinding := payload["source_product_id"]
	rawSourceID := payload["source_product_id"]
	if rawSourceID != nil {
		s, ok := rawSourceID.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return productSourceBinding{}, apperr.Validation("商品资料节点的 source_product_id 无效")
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
			return productSourceBinding{}, apperr.Validation("未绑定商品的商品资料节点不能绑定 fact_set_version_id")
		}
		return productSourceBinding{}, nil
	}
	return productSourceBinding{sourceProductID: sourceProductID, legacyFallback: !hasSourceBinding}, nil
}

func parseFactSetID(config map[string]any) (*string, error) {
	if config == nil || config["fact_set_version_id"] == nil {
		return nil, nil
	}
	s, ok := config["fact_set_version_id"].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil, apperr.Validation("商品资料节点的 fact_set_version_id 无效")
	}
	trimmed := strings.TrimSpace(s)
	return &trimmed, nil
}

func sourceSnapshotFromGuard(binding productSourceBinding, source *SourceProduct, factSetID *string, set *FactSet) productSourceSnapshot {
	out := productSourceSnapshot{
		SourceProductID:  binding.sourceProductID,
		FactSetVersionID: factSetID,
		Facts:            []map[string]any{},
		LegacyFallback:   binding.legacyFallback,
	}
	if source == nil {
		return out
	}
	out.SourceProduct = &productSummary{
		ID: source.ID, Name: source.Name, Category: source.Category,
		Price: source.Price, SourceNote: source.SourceNote,
	}
	if set != nil {
		out.FactSetVersion = &factSetSnapshot{
			ID: set.ID, ProductID: set.ProductID, Version: set.Version, Facts: set.Facts,
		}
		out.Facts = set.Facts
	}
	return out
}

// loadProductSourceSnapshots 经 ProductGuard 批量读商品与 fact 版本，编进每个节点的 snapshot。
// Guard 返回缺行时仍按单节点读取的规则转成 Validation 或空的 legacy source。
func loadProductSourceSnapshots(ctx context.Context, tx *gorm.DB, graphProductID string, nodes []AppliedNode) (map[string]productSourceSnapshot, error) {
	out := map[string]productSourceSnapshot{}
	lookups := make([]productSourceLookup, 0)
	sourceIDs := make([]string, 0)
	for _, node := range nodes {
		if node.NodeType != NodeProductSource {
			continue
		}
		binding, err := parseProductSourceBinding(graphProductID, node.Config)
		if err != nil {
			return nil, err
		}
		lookup := productSourceLookup{nodeID: node.ID, config: node.Config, binding: binding}
		lookups = append(lookups, lookup)
		if binding.sourceProductID != nil {
			sourceIDs = append(sourceIDs, *binding.sourceProductID)
		}
	}
	if len(lookups) == 0 || len(sourceIDs) == 0 {
		return out, nil
	}
	guard, err := requireProductGuard(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := guard.LoadSources(ctx, tx, uniqueStrings(sourceIDs))
	if err != nil {
		return nil, err
	}
	factSetIDs := make([]string, 0)
	for index := range lookups {
		lookup := &lookups[index]
		if lookup.binding.sourceProductID == nil {
			out[lookup.nodeID] = productSourceSnapshot{}
			continue
		}
		lookup.source = sources[*lookup.binding.sourceProductID]
		if lookup.source == nil {
			return nil, apperr.Validation("商品资料节点绑定的商品不存在")
		}
		factSetID, err := parseFactSetID(lookup.config)
		if err != nil {
			return nil, err
		}
		if factSetID == nil && lookup.source.CurrentFactSetID != nil && *lookup.source.CurrentFactSetID != "" {
			factSetID = lookup.source.CurrentFactSetID
		}
		lookup.factSet = factSetID
		if factSetID != nil {
			factSetIDs = append(factSetIDs, *factSetID)
		}
	}
	factSets, err := guard.LoadFactSets(ctx, tx, uniqueStrings(factSetIDs))
	if err != nil {
		return nil, err
	}
	for _, lookup := range lookups {
		if lookup.binding.sourceProductID == nil {
			continue
		}
		if lookup.factSet == nil {
			out[lookup.nodeID] = sourceSnapshotFromGuard(lookup.binding, lookup.source, nil, nil)
			continue
		}
		set := factSets[*lookup.factSet]
		if set == nil || set.ProductID != *lookup.binding.sourceProductID {
			if lookup.config != nil && lookup.config["fact_set_version_id"] != nil {
				return nil, apperr.Validation("fact_set_version_id 不属于绑定商品")
			}
			out[lookup.nodeID] = sourceSnapshotFromGuard(lookup.binding, lookup.source, lookup.factSet, nil)
			continue
		}
		out[lookup.nodeID] = sourceSnapshotFromGuard(lookup.binding, lookup.source, lookup.factSet, set)
	}
	return out, nil
}

// mergeRuntimeFacts 用商品名称等身份字段补 facts 里没有的 key。已有同名 key（大小写不敏感）不覆盖。
func mergeRuntimeFacts(facts []map[string]any, source *productSourceSnapshot) []map[string]any {
	confirmed := make([]map[string]any, 0, len(facts))
	for _, fact := range facts {
		status, _ := fact["status"].(string)
		pending, _ := fact["requires_confirmation"].(bool)
		if pending || status == "observed" || status == "conflicted" {
			continue
		}
		confirmed = append(confirmed, fact)
	}
	facts = confirmed
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
