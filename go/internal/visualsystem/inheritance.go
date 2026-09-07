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

// ResolveInheritance 按 IQ-CF-07 合并：本商品覆盖 > 选定方案版本 > 品牌层 > 产品默认。
// 事实与身份参考不参与本函数。品牌层：未选定 → brand_not_selected；已选定但无可解析 style/colors →
// brand_exists_no_style_merge；已选定且 BrandPayload 可解析时合并并 Active。
func ResolveInheritance(input ResolveInput) InheritanceView {
	defaults := filterStyleChain(input.ProductDefault)
	if defaults == nil {
		defaults = map[string]any{}
	}
	brandPayload := filterStyleChain(input.BrandPayload)
	selectedPayload := filterStyleChain(input.SelectedPayload)
	override := filterStyleChain(input.ProductOverride)

	brandID := strings.TrimSpace(input.BrandID)
	brandActive := brandID != "" && len(brandPayload) > 0
	brand := brandPlaceholderFor(brandID, brandActive)

	effective := cloneMap(defaults)
	if brandActive {
		for key, value := range brandPayload {
			effective[key] = cloneValue(value)
		}
	}
	if selectedPayload != nil {
		for key, value := range selectedPayload {
			effective[key] = cloneValue(value)
		}
	}
	if override != nil {
		for key, value := range override {
			effective[key] = cloneValue(value)
		}
	}
	if len(effective) == 0 {
		effective = map[string]any{}
	}

	brandLayer := LayerContribution{
		Layer:       LayerBrandVersion,
		Active:      brandActive,
		VersionID:   input.BrandVersionID,
		SystemID:    input.BrandSystemID,
		SystemName:  input.BrandSystemName,
		Payload:     brandPayload,
		Note:        noteBrand(brandActive, brand),
	}
	if !brandActive {
		brandLayer.Placeholder = &brand
		brandLayer.Payload = nil
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
		brandLayer,
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
	// BrandID 非空表示商品已选定品牌；合并要求 BrandPayload 含可解析 style/colors。
	BrandID string
	// BrandPayload 来自 Brand.visual_system_id 指向方案的当前（最新）版本；仅 style/colors 进入链。
	BrandPayload    map[string]any
	BrandVersionID  *string
	BrandSystemID   *string
	BrandSystemName *string
}

func brandPlaceholderFor(brandID string, brandActive bool) BrandPlaceholder {
	if brandActive {
		return MergedBrandPlaceholder()
	}
	if brandID != "" {
		return BrandExistsPlaceholder()
	}
	return DefaultBrandPlaceholder()
}

func noteBrand(active bool, brand BrandPlaceholder) string {
	if active {
		return "品牌挂接视觉方案的当前版本已并入风格色；选定方案与本商品覆盖优先"
	}
	return brand.Detail
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
