package graph

import (
	"fmt"
	"sort"
	"strings"
)

const textTraceSchemaVersion = 1

// TextTraceInput 组装图位文字追溯元数据（IQ-CF-02 / CF-B1）。
// Prompt 是已解析的 listing prompt 文档；Facts 是入边确认事实（含 identity 补齐）。
// UserImageOverride 为 true 表示本图 text_override 生效，文案槽视为显式本图覆盖。
type TextTraceInput struct {
	Prompt            map[string]any
	Facts             []map[string]any
	ImageTypeKey      string
	UserImageOverride bool
}

// TextTrace 记录成稿可核验文字如何追到 fact key 或本图覆盖。
type TextTrace struct {
	SchemaVersion       int                        `json:"schema_version"`
	ImageTypeKey        string                     `json:"image_type_key,omitempty"`
	UserImageOverride   bool                       `json:"user_image_override"`
	Entries             []TextTraceEntry           `json:"entries"`
	FactKeys            []string                   `json:"fact_keys"`
	SellingPointCheck   *SellingPointOneReasonCheck `json:"selling_point_check,omitempty"`
	TextQualified       bool                       `json:"text_qualified"`
	UntracedFields      []string                   `json:"untraced_fields,omitempty"`
}

// TextTraceEntry 是一条可核验文字与其依据。
type TextTraceEntry struct {
	Field             string   `json:"field"`
	Text              string   `json:"text"`
	FactKeys          []string `json:"fact_keys,omitempty"`
	UserImageOverride bool     `json:"user_image_override,omitempty"`
	Traced            bool     `json:"traced"`
}

// SellingPointOneReasonCheck 是卖点图「一图一主要购买理由」检查结果（可非 live）。
type SellingPointOneReasonCheck struct {
	Applicable  bool   `json:"applicable"`
	ReasonCount int    `json:"reason_count"`
	Pass        bool   `json:"pass"`
	Detail      string `json:"detail,omitempty"`
}

// BuildTextTrace 从 prompt 文案与商品事实推导追溯元数据；不调 provider、不 OCR。
func BuildTextTrace(in TextTraceInput) TextTrace {
	prompt := in.Prompt
	if prompt == nil {
		prompt = map[string]any{}
	}
	entries := collectVerifiableTextEntries(prompt, in.UserImageOverride)
	index := factValueIndex(in.Facts)
	used := map[string]struct{}{}
	var untraced []string
	for i := range entries {
		if entries[i].UserImageOverride {
			entries[i].Traced = true
			continue
		}
		keys := matchFactKeys(entries[i].Text, index)
		entries[i].FactKeys = keys
		entries[i].Traced = len(keys) > 0
		for _, key := range keys {
			used[key] = struct{}{}
		}
		if !entries[i].Traced {
			untraced = append(untraced, entries[i].Field)
		}
	}
	factKeys := make([]string, 0, len(used))
	for key := range used {
		factKeys = append(factKeys, key)
	}
	sort.Strings(factKeys)
	sort.Strings(untraced)
	selling := CheckSellingPointOneReason(in.ImageTypeKey, prompt, entries)
	qualified := len(untraced) == 0
	if selling.Applicable && !selling.Pass {
		qualified = false
	}
	return TextTrace{
		SchemaVersion:     textTraceSchemaVersion,
		ImageTypeKey:      strings.TrimSpace(in.ImageTypeKey),
		UserImageOverride: in.UserImageOverride,
		Entries:           entries,
		FactKeys:          factKeys,
		SellingPointCheck: selling,
		TextQualified:     qualified,
		UntracedFields:    untraced,
	}
}

// CheckSellingPointOneReason 检查卖点图是否至多一句主要购买理由，且该句可追溯。
// 非 selling_point 图种返回 Applicable=false。可先于 live OCR 单独单测。
func CheckSellingPointOneReason(imageTypeKey string, prompt map[string]any, entries []TextTraceEntry) *SellingPointOneReasonCheck {
	key := strings.TrimSpace(imageTypeKey)
	if key != "selling_point" {
		return &SellingPointOneReasonCheck{Applicable: false, Pass: true}
	}
	content := asMapOrNil(prompt["content"])
	reasons := usablePromptTexts(content["selling_points"])
	check := &SellingPointOneReasonCheck{
		Applicable:  true,
		ReasonCount: len(reasons),
	}
	switch {
	case len(reasons) == 0:
		check.Pass = true
		check.Detail = "卖点图未写购买理由"
	case len(reasons) > 1:
		check.Pass = false
		check.Detail = fmt.Sprintf("卖点图堆了 %d 句购买理由，默认只允许一句", len(reasons))
	default:
		traced := false
		for _, entry := range entries {
			if entry.Field == "content.selling_points[0]" && entry.Traced {
				traced = true
				break
			}
		}
		if !traced {
			// entries 可能因 UserImageOverride 整图覆盖而未带 selling_points 字段；再查正文覆盖。
			for _, entry := range entries {
				if strings.HasPrefix(entry.Field, "content.selling_points") && entry.Traced {
					traced = true
					break
				}
			}
		}
		if traced {
			check.Pass = true
			check.Detail = "卖点图仅一句且可追溯"
		} else {
			check.Pass = false
			check.Detail = "卖点图唯一购买理由无 fact key 且非本图覆盖"
		}
	}
	return check
}

// TextTraceAsMap 供 artifact / compiled_context JSON 写入。
func TextTraceAsMap(trace TextTrace) map[string]any {
	entries := make([]any, 0, len(trace.Entries))
	for _, entry := range trace.Entries {
		item := map[string]any{
			"field":  entry.Field,
			"text":   entry.Text,
			"traced": entry.Traced,
		}
		if len(entry.FactKeys) > 0 {
			item["fact_keys"] = stringListToAny(entry.FactKeys)
		}
		if entry.UserImageOverride {
			item["user_image_override"] = true
		}
		entries = append(entries, item)
	}
	out := map[string]any{
		"schema_version":      trace.SchemaVersion,
		"user_image_override": trace.UserImageOverride,
		"entries":             entries,
		"fact_keys":           stringListToAny(trace.FactKeys),
		"text_qualified":      trace.TextQualified,
	}
	if trace.ImageTypeKey != "" {
		out["image_type_key"] = trace.ImageTypeKey
	}
	if len(trace.UntracedFields) > 0 {
		out["untraced_fields"] = stringListToAny(trace.UntracedFields)
	}
	if trace.SellingPointCheck != nil {
		out["selling_point_check"] = map[string]any{
			"applicable":   trace.SellingPointCheck.Applicable,
			"reason_count": trace.SellingPointCheck.ReasonCount,
			"pass":         trace.SellingPointCheck.Pass,
			"detail":       trace.SellingPointCheck.Detail,
		}
	}
	return out
}

func collectVerifiableTextEntries(prompt map[string]any, userOverride bool) []TextTraceEntry {
	var entries []TextTraceEntry
	text := asMapOrNil(prompt["text"])
	for _, field := range []string{"headline", "subtitle", "body"} {
		value := usablePromptText(text[field])
		if value == "" {
			continue
		}
		entries = append(entries, TextTraceEntry{
			Field:             "text." + field,
			Text:              value,
			UserImageOverride: userOverride,
		})
	}
	content := asMapOrNil(prompt["content"])
	for i, point := range usablePromptTexts(content["selling_points"]) {
		// text_override 只改 text/copy_regions，卖点句仍须追到 fact key。
		entries = append(entries, TextTraceEntry{
			Field: fmt.Sprintf("content.selling_points[%d]", i),
			Text:  point,
		})
	}
	return entries
}

type factValueRef struct {
	Key   string
	Value string
}

func factValueIndex(facts []map[string]any) []factValueRef {
	out := make([]factValueRef, 0, len(facts))
	seen := map[string]struct{}{}
	for _, fact := range facts {
		key := strings.TrimSpace(asString(fact["key"]))
		value := strings.TrimSpace(asString(fact["value"]))
		if key == "" || value == "" {
			continue
		}
		id := strings.ToLower(key) + "\x00" + value
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, factValueRef{Key: key, Value: value})
	}
	return out
}

// matchFactKeys 用事实 value 子串对齐文案；最短 2 字，避免单字符误匹配。
func matchFactKeys(text string, index []factValueRef) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	foldedText := strings.ToLower(text)
	keys := map[string]struct{}{}
	for _, fact := range index {
		if len([]rune(fact.Value)) < 2 {
			continue
		}
		foldedValue := strings.ToLower(fact.Value)
		if strings.Contains(foldedText, foldedValue) || strings.Contains(foldedValue, foldedText) {
			keys[fact.Key] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// factsUpstreamOfImage 沿 RolePrompt 入边收集上游 image_prompt 的确认事实。
func factsUpstreamOfImage(graph AppliedGraph, imageNodeID string, sources map[string]SourceRecord) []map[string]any {
	for _, edge := range incomingSorted(graph, imageNodeID) {
		if edge.Role != RolePrompt {
			continue
		}
		facts, _, _, _, err := collectPromptInputs(graph, edge.SourceNodeID, sources)
		if err != nil {
			return nil
		}
		return facts
	}
	return nil
}

func nodeHasTextOverride(config map[string]any) bool {
	if config == nil {
		return false
	}
	raw, ok := config["text_override"]
	if !ok || raw == nil {
		return false
	}
	_, isMap := asMap(raw)
	return isMap
}
