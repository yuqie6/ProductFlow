package graph

import (
	"sort"
	"strings"
)

const (
	ProduceRouteSubjectPreserve = "subject_preserve"
	ProduceRouteGenerative      = "generative"
	produceRouteSchemaVersion   = 1
)

// 生成式路线禁止用提示词/UI 把本路线标成像素保真（IQ-CF-05 / CF-B3）。
var forbiddenGenerativePixelPhrases = []string{
	"像素级一致",
	"像素级还原",
	"像素级保真",
	"保持像素一致",
	"像素完全一致",
	"绝对像素保真",
	"pixel-perfect",
	"pixel perfect",
	"pixel-level consistency",
}

// ProduceRouteRecord 记录图位产图路线与合格判据（CF-B3）。
type ProduceRouteRecord struct {
	SchemaVersion       int      `json:"schema_version"`
	Route               string   `json:"route"`
	ImageTypeKey        string   `json:"image_type_key,omitempty"`
	AppearanceMayChange bool     `json:"appearance_may_change"`
	RouteQualified      bool     `json:"route_qualified"`
	UnresolvedItems     []string `json:"unresolved_items,omitempty"`
	ForbiddenPhrases    []string `json:"forbidden_phrases,omitempty"`
}

// ProduceRouteInput 组装路线记录；SubjectPreserveFailed / FailureReasons 表示保留主体质检失败。
type ProduceRouteInput struct {
	Route                 string
	ImageTypeKey          string
	HasIdentityReference  bool
	SkipIdentityCheck     bool // prompt 产物只钉路线与文案禁令，身份在生图节点检查
	PromptTexts           []string
	SubjectPreserveFailed bool
	FailureReasons        []string
}

// DefaultProduceRoute 按图种给默认路线：主图/细节/SKU 保留主体，场景等走生成式。
func DefaultProduceRoute(imageTypeKey string) string {
	switch strings.TrimSpace(imageTypeKey) {
	case "hero", "detail", "sku", "packaging":
		return ProduceRouteSubjectPreserve
	default:
		return ProduceRouteGenerative
	}
}

// NormalizeProduceRoute 只接受闭集；非法或空串返回 ""。
func NormalizeProduceRoute(raw string) string {
	switch strings.TrimSpace(raw) {
	case ProduceRouteSubjectPreserve:
		return ProduceRouteSubjectPreserve
	case ProduceRouteGenerative:
		return ProduceRouteGenerative
	default:
		return ""
	}
}

// ResolveProduceRoute 读节点 config；缺省或非法时回落到图种默认。
func ResolveProduceRoute(config map[string]any, imageTypeKey string) string {
	if config != nil {
		if raw, ok := config["produce_route"].(string); ok {
			if route := NormalizeProduceRoute(raw); route != "" {
				return route
			}
		}
	}
	key := strings.TrimSpace(imageTypeKey)
	if key == "" && config != nil {
		key, _ = config["image_type_key"].(string)
		key = strings.TrimSpace(key)
	}
	return DefaultProduceRoute(key)
}

// BuildProduceRouteRecord 钉路线声明、生成式禁令与保留主体失败未解决项。
func BuildProduceRouteRecord(in ProduceRouteInput) ProduceRouteRecord {
	route := NormalizeProduceRoute(in.Route)
	if route == "" {
		route = DefaultProduceRoute(in.ImageTypeKey)
	}
	rec := ProduceRouteRecord{
		SchemaVersion:       produceRouteSchemaVersion,
		Route:               route,
		ImageTypeKey:        strings.TrimSpace(in.ImageTypeKey),
		AppearanceMayChange: route == ProduceRouteGenerative,
		RouteQualified:      true,
	}
	var unresolved []string
	if route == ProduceRouteGenerative {
		hits := FindForbiddenPixelFidelityPhrases(in.PromptTexts...)
		if len(hits) > 0 {
			rec.ForbiddenPhrases = hits
			unresolved = append(unresolved, "生成式路线提示词宣称像素保真："+strings.Join(hits, "、"))
		}
	}
	if route == ProduceRouteSubjectPreserve {
		if in.SubjectPreserveFailed {
			unresolved = append(unresolved, "保留主体路线质检失败")
		}
		for _, reason := range in.FailureReasons {
			reason = strings.TrimSpace(reason)
			if reason == "" {
				continue
			}
			unresolved = append(unresolved, reason)
		}
		if !in.SkipIdentityCheck && !in.HasIdentityReference {
			unresolved = append(unresolved, "保留主体路线缺少商品本体参考")
		}
		// 保留主体也不得宣称绝对像素保真。
		hits := FindForbiddenPixelFidelityPhrases(in.PromptTexts...)
		if len(hits) > 0 {
			rec.ForbiddenPhrases = hits
			unresolved = append(unresolved, "保留主体路线不得宣称绝对像素保真："+strings.Join(hits, "、"))
		}
	}
	sort.Strings(unresolved)
	rec.UnresolvedItems = unresolved
	rec.RouteQualified = len(unresolved) == 0
	return rec
}

// RouteAllowsDeliveryPass 未解决项存在时不得进入已交付合格集；CreateAdoption 强制消费。
func RouteAllowsDeliveryPass(rec ProduceRouteRecord) bool {
	return rec.RouteQualified
}

// FindForbiddenPixelFidelityPhrases 返回文案中命中的禁词（去重排序）。
func FindForbiddenPixelFidelityPhrases(texts ...string) []string {
	joined := strings.ToLower(strings.Join(texts, "\n"))
	seen := map[string]struct{}{}
	var hits []string
	for _, phrase := range forbiddenGenerativePixelPhrases {
		needle := strings.ToLower(phrase)
		if needle == "" {
			continue
		}
		if strings.Contains(joined, needle) {
			if _, ok := seen[phrase]; ok {
				continue
			}
			seen[phrase] = struct{}{}
			hits = append(hits, phrase)
		}
	}
	sort.Strings(hits)
	return hits
}

// ScrubForbiddenPixelFidelityPhrases 从生成式提示词中去掉禁词，避免把像素保真合同发给模型。
func ScrubForbiddenPixelFidelityPhrases(text string) string {
	out := text
	for _, phrase := range forbiddenGenerativePixelPhrases {
		if phrase == "" {
			continue
		}
		out = strings.ReplaceAll(out, phrase, "")
		out = strings.ReplaceAll(out, strings.ToLower(phrase), "")
		out = strings.ReplaceAll(out, strings.ToUpper(phrase), "")
	}
	return strings.Join(strings.Fields(out), " ")
}

// ProduceRouteAsMap 供 artifact / 运行记录 JSON 写入。
func ProduceRouteAsMap(rec ProduceRouteRecord) map[string]any {
	out := map[string]any{
		"schema_version":        rec.SchemaVersion,
		"route":                 rec.Route,
		"appearance_may_change": rec.AppearanceMayChange,
		"route_qualified":       rec.RouteQualified,
	}
	if rec.ImageTypeKey != "" {
		out["image_type_key"] = rec.ImageTypeKey
	}
	if len(rec.UnresolvedItems) > 0 {
		out["unresolved_items"] = stringListToAny(rec.UnresolvedItems)
	}
	if len(rec.ForbiddenPhrases) > 0 {
		out["forbidden_phrases"] = stringListToAny(rec.ForbiddenPhrases)
	}
	return out
}

func collectPromptAuditTexts(prompt map[string]any, variation string) []string {
	var texts []string
	if variation = strings.TrimSpace(variation); variation != "" {
		texts = append(texts, variation)
	}
	if prompt == nil {
		return texts
	}
	if goal := usablePromptText(prompt["design_goal"]); goal != "" {
		texts = append(texts, goal)
	}
	for _, key := range []string{"shared_rules", "creative_boundary"} {
		texts = append(texts, usablePromptTexts(prompt[key])...)
	}
	fidelity := asMapOrNil(prompt["product_fidelity"])
	texts = append(texts, usablePromptTexts(fidelity["requirements"])...)
	content := asMapOrNil(prompt["content"])
	if bg := usablePromptText(content["background"]); bg != "" {
		texts = append(texts, bg)
	}
	texts = append(texts, usablePromptTexts(content["focus"])...)
	texts = append(texts, usablePromptTexts(content["selling_points"])...)
	return texts
}

func hasProductIdentityReference(refs []ReferenceImage) bool {
	for _, ref := range refs {
		if strings.TrimSpace(ref.Role) == "product_identity" {
			return true
		}
	}
	return false
}
