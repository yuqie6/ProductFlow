package ocr

import (
	"encoding/json"
)

// CheckAsMap 供 artifact text_trace.ocr_check JSON 写入。
func CheckAsMap(c CompareResult) map[string]any {
	return map[string]any{
		"schema_version": c.SchemaVersion,
		"pass":           c.Pass,
		"engine":         c.Engine,
		"extracted":      c.Extracted,
		"matched":        stringSliceAny(c.Matched),
		"missing":        stringSliceAny(c.Missing),
		"extra":          stringSliceAny(c.Extra),
		"detail":         c.Detail,
	}
}

// ApplyOCRGate 把 OCR 对照写入 text_trace；失败时强制 text_qualified=false。
// 成功不把原先不合格抬成合格（声明式追溯仍独立）。返回新 map，不改入参。
func ApplyOCRGate(textTrace map[string]any, check CompareResult) map[string]any {
	out := cloneMap(textTrace)
	if out == nil {
		out = map[string]any{}
	}
	out["ocr_check"] = CheckAsMap(check)
	if !check.Pass {
		out["text_qualified"] = false
	}
	return out
}

// TextQualifiedAfterOCR 综合声明合格与 OCR 闸：OCR 失败则不得 pass。
func TextQualifiedAfterOCR(declarationQualified bool, check CompareResult) bool {
	return declarationQualified && check.Pass
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
