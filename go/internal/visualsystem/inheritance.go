package visualsystem

import (
	"encoding/json"
	"strings"
)

// StyleChainKeys 是 IQ-CF-07 风格继承链允许的字段；事实/身份参考不得进入。
// 与 graph.CatalogVisualOverlay 的 style/colors 对齐，本包不 import graph。
var StyleChainKeys = map[string]struct{}{
	"style":  {},
	"colors": {},
}

// ForbiddenStyleChainKeys 若出现在继承层载荷中须剥离，不得进入 EffectivePayload。
var ForbiddenStyleChainKeys = map[string]struct{}{
	"source_product_id":        {},
	"fact_set_version_id":      {},
	"visual_system_version_id": {},
	"fact_keys":                {},
	"evidence_asset_ids":       {},
	"capacity":                 {},
	"facts":                    {},
	"product_identity":         {},
	"reference_asset_ids":      {},
	"images":                   {},
}

// ResolveInheritance 按 IQ-CF-07 合并：本商品覆盖 > 选定方案版本 > 品牌占位 > 产品默认。
// 事实与身份参考不参与本函数。Brand 未就绪时品牌层恒为占位且不合并任何品牌载荷。
func ResolveInheritance(input ResolveInput) InheritanceView {
	brand := DefaultBrandPlaceholder()
	defaults := filterStyleChain(input.ProductDefault)
	if defaults == nil {
		defaults = map[string]any{}
	}
	selectedPayload := filterStyleChain(input.SelectedPayload)
	override := filterStyleChain(input.ProductOverride)

	effective := cloneMap(defaults)
	if selectedPayload != nil {
		for key, value := range selectedPayload {
			effective[key] = cloneValue(value)
		}
	}
	// 品牌层：Brand 表未就绪时永不合并，即使调用方误传 BrandPayload。
	if override != nil {
		for key, value := range override {
			effective[key] = cloneValue(value)
		}
	}
	if len(effective) == 0 {
		effective = map[string]any{}
	}

	layers := []LayerContribution{
		{
			Layer:   LayerProductOverride,
			Active:  len(override) > 0,
			Payload: override,
			Note:    noteOverride(len(override) > 0),
		},
		{
			Layer:      LayerSelectedVisual,
			Active:     input.SelectedVersionID != nil && strings.TrimSpace(*input.SelectedVersionID) != "",
			VersionID:  input.SelectedVersionID,
			SystemID:   input.SelectedSystemID,
			SystemName: input.SelectedSystemName,
			Payload:    selectedPayload,
			Note:       noteSelected(input.SelectedVersionID != nil && strings.TrimSpace(*input.SelectedVersionID) != ""),
		},
		{
			Layer:       LayerBrandVersion,
			Active:      false,
			Placeholder: &brand,
			Note:        brand.Detail,
		},
		{
			Layer:   LayerProductDefault,
			Active:  true,
			Payload: defaults,
			Note:    "产品默认视觉底稿；上层未覆盖的字段由此提供",
		},
	}

	out := InheritanceView{
		ProductID:         input.ProductID,
		SelectedVersionID: input.SelectedVersionID,
		EffectivePayload:  effective,
		Layers:            layers,
		BrandPlaceholder:  brand,
	}
	if input.NewerVersion != nil {
		out.NewerVersionAvailable = input.NewerVersion
	}
	return out
}

// ResolveInput 是继承解析的内存输入；调用方负责从 DB / 图投影装载。
type ResolveInput struct {
	ProductID          string
	ProductOverride    map[string]any
	SelectedVersionID  *string
	SelectedSystemID   *string
	SelectedSystemName *string
	SelectedPayload    map[string]any
	ProductDefault     map[string]any
	NewerVersion       *VersionView
	// BrandPayload 预留；Brand 表未就绪时 ResolveInheritance 忽略，不伪造品牌层。
	BrandPayload map[string]any
}

// filterStyleChain 只保留 style/colors，并剥离子身份/事实键。nil 入参保持 nil。
func filterStyleChain(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range in {
		if _, forbidden := ForbiddenStyleChainKeys[key]; forbidden {
			continue
		}
		if _, ok := StyleChainKeys[key]; !ok {
			continue
		}
		out[key] = cloneValue(value)
	}
	if len(out) == 0 {
		return map[string]any{}
	}
	return out
}

func noteOverride(active bool) string {
	if active {
		return "本商品显式覆盖优先生效"
	}
	return "无本商品覆盖；继续看下层"
}

func noteSelected(active bool) string {
	if active {
		return "商品已显式选定该视觉方案版本；追加新版本不会静默替换"
	}
	return "尚未选定视觉方案版本"
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

func decodePayloadJSON(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
