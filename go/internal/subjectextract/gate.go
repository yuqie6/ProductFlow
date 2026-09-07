package subjectextract

import (
	"encoding/json"
	"sort"
)

// CheckAsMap 供 produce_route.subject_extract JSON 写入（不含原始 PNG 字节）。
func CheckAsMap(r Result) map[string]any {
	out := map[string]any{
		"schema_version": r.SchemaVersion,
		"engine":         r.Engine,
		"pass":           r.Pass,
		"detail":         r.Detail,
		"coverage":       r.Coverage,
		"source_sha256":  r.SourceSHA256,
		"lineage": map[string]any{
			"kind":                  r.Lineage.Kind,
			"source_content_sha256": r.Lineage.SourceContentSHA256,
		},
	}
	if r.Width > 0 {
		out["width"] = r.Width
		out["height"] = r.Height
	}
	if r.MaskSHA256 != "" {
		out["mask_sha256"] = r.MaskSHA256
		out["lineage"].(map[string]any)["mask_content_sha256"] = r.Lineage.MaskContentSHA256
	}
	if r.CutoutSHA256 != "" {
		out["cutout_sha256"] = r.CutoutSHA256
		out["lineage"].(map[string]any)["cutout_content_sha256"] = r.Lineage.CutoutContentSHA256
	}
	if r.Bounds.W > 0 && r.Bounds.H > 0 {
		out["bounds"] = map[string]any{
			"x": r.Bounds.X, "y": r.Bounds.Y, "w": r.Bounds.W, "h": r.Bounds.H,
		}
	}
	if len(r.UnresolvedItems) > 0 {
		out["unresolved_items"] = stringSliceAny(r.UnresolvedItems)
	}
	return out
}

// ApplySubjectPreserveGate 把提取结果写入 produce_route；失败强制 route_qualified=false。
// 成功不把原先不合格抬成合格。非 subject_preserve 路线原样返回。返回新 map。
func ApplySubjectPreserveGate(produceRoute map[string]any, result Result) map[string]any {
	out := cloneMap(produceRoute)
	if out == nil {
		out = map[string]any{}
	}
	route, _ := out["route"].(string)
	if route != "" && route != "subject_preserve" {
		return out
	}
	if route == "" {
		out["route"] = "subject_preserve"
	}
	out["subject_extract"] = CheckAsMap(result)
	if !result.Pass {
		out["route_qualified"] = false
		unresolved := stringListFromAny(out["unresolved_items"])
		unresolved = append(unresolved, "保留主体路线质检失败")
		for _, item := range result.UnresolvedItems {
			if item == "" {
				continue
			}
			unresolved = append(unresolved, item)
		}
		out["unresolved_items"] = stringSliceAny(uniqueSorted(unresolved))
	}
	return out
}

// CheckComposeAsMap 供 produce_route.subject_compose JSON 写入（不含原始 PNG 字节）。
func CheckComposeAsMap(r ComposeResult) map[string]any {
	out := map[string]any{
		"schema_version":  r.SchemaVersion,
		"engine":          r.Engine,
		"pass":            r.Pass,
		"detail":          r.Detail,
		"contact_shadow":  r.ContactShadow,
		"background_kind": r.BackgroundKind,
		"lineage": map[string]any{
			"kind":                   r.Lineage.Kind,
			"cutout_content_sha256":  r.Lineage.CutoutContentSHA256,
			"source_content_sha256":  r.Lineage.SourceContentSHA256,
		},
	}
	if r.Width > 0 {
		out["width"] = r.Width
		out["height"] = r.Height
	}
	if r.PNGSHA256 != "" {
		out["png_sha256"] = r.PNGSHA256
		out["lineage"].(map[string]any)["compose_content_sha256"] = r.Lineage.ComposeContentSHA256
	}
	if r.CutoutSHA256 != "" {
		out["cutout_sha256"] = r.CutoutSHA256
	}
	if r.Placement.W > 0 && r.Placement.H > 0 {
		out["placement"] = map[string]any{
			"x": r.Placement.X, "y": r.Placement.Y,
			"w": r.Placement.W, "h": r.Placement.H,
		}
	}
	if len(r.UnresolvedItems) > 0 {
		out["unresolved_items"] = stringSliceAny(r.UnresolvedItems)
	}
	return out
}

// ApplySubjectComposeGate 把合成结果写入 produce_route.subject_compose；失败强制 route_qualified=false。
// 成功不抬高原先不合格。非 subject_preserve 路线原样返回。
func ApplySubjectComposeGate(produceRoute map[string]any, result ComposeResult) map[string]any {
	out := cloneMap(produceRoute)
	if out == nil {
		out = map[string]any{}
	}
	route, _ := out["route"].(string)
	if route != "" && route != "subject_preserve" {
		return out
	}
	if route == "" {
		out["route"] = "subject_preserve"
	}
	out["subject_compose"] = CheckComposeAsMap(result)
	if !result.Pass {
		out["route_qualified"] = false
		unresolved := stringListFromAny(out["unresolved_items"])
		unresolved = append(unresolved, "保留主体合成质检失败")
		for _, item := range result.UnresolvedItems {
			if item == "" {
				continue
			}
			unresolved = append(unresolved, item)
		}
		out["unresolved_items"] = stringSliceAny(uniqueSorted(unresolved))
	}
	return out
}

// RouteQualifiedAfterExtract 综合声明合格与提取闸：提取失败则不得 pass。
func RouteQualifiedAfterExtract(declarationQualified bool, result Result) bool {
	return declarationQualified && result.Pass
}

// RouteQualifiedAfterCompose 综合声明合格与提取+合成闸。
func RouteQualifiedAfterCompose(declarationQualified bool, extract Result, compose ComposeResult) bool {
	return declarationQualified && extract.Pass && compose.Pass
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		out = make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
	}
	return out
}

func stringSliceAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

func stringListFromAny(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		if ss, ok := v.([]string); ok {
			return append([]string(nil), ss...)
		}
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, _ := item.(string)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func uniqueSorted(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
