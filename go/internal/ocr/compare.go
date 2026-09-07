package ocr

import (
	"sort"
	"strings"
)

// ExpectedItem 是对照期望的一条可见文字（来自 fact 或本图覆盖）。
type ExpectedItem struct {
	Source string // fact_key / override / entry
	Key    string
	Text   string
}

// CompareResult 是 OCR 与 text_trace 期望的结构化对照。
type CompareResult struct {
	SchemaVersion int      `json:"schema_version"`
	Pass          bool     `json:"pass"`
	Engine        string   `json:"engine,omitempty"`
	Extracted     string   `json:"extracted,omitempty"`
	Matched       []string `json:"matched"`
	Missing       []string `json:"missing"`
	Extra         []string `json:"extra"`
	Detail        string   `json:"detail,omitempty"`
}

// FactRef 是 fact_keys 解析所需的最小事实。
type FactRef struct {
	Key   string
	Value string
}

// ExpectedFromTextTrace 从产物 text_trace 与事实表收集须在成片出现的文字。
// 优先 entries 文案；fact_keys 用事实 value 补齐；user_image_override 条目计入覆盖源。
func ExpectedFromTextTrace(textTrace map[string]any, facts []FactRef) []ExpectedItem {
	if textTrace == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ExpectedItem
	add := func(source, key, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		id := foldOCR(text)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, ExpectedItem{Source: source, Key: key, Text: text})
	}

	factIndex := map[string]string{}
	for _, f := range facts {
		k := strings.TrimSpace(f.Key)
		v := strings.TrimSpace(f.Value)
		if k != "" && v != "" {
			factIndex[k] = v
		}
	}

	if rawEntries, ok := textTrace["entries"].([]any); ok {
		for _, raw := range rawEntries {
			entry, ok := raw.(map[string]any)
			if !ok || entry == nil {
				continue
			}
			text := strings.TrimSpace(asString(entry["text"]))
			override := asBool(entry["user_image_override"])
			keys := stringList(entry["fact_keys"])
			traced := asBool(entry["traced"])
			switch {
			case override:
				add("override", strings.Join(keys, ","), text)
			case traced || len(keys) > 0:
				add("entry", strings.Join(keys, ","), text)
			}
		}
	}

	for _, key := range stringList(textTrace["fact_keys"]) {
		if v, ok := factIndex[key]; ok {
			add("fact_key", key, v)
		}
	}

	if asBool(textTrace["user_image_override"]) {
		// 根级覆盖：若尚无条目，不额外造字；条目已在上面计入。
		_ = textTrace
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Text == out[j].Text {
			return out[i].Key < out[j].Key
		}
		return out[i].Text < out[j].Text
	})
	return out
}

// CompareExtracted 对照提取文本与期望：匹配 / 缺失 / 多余。
func CompareExtracted(extracted ExtractResult, expected []ExpectedItem) CompareResult {
	out := CompareResult{
		SchemaVersion: defaultSchemaVersion,
		Engine:        extracted.Engine,
		Extracted:     extracted.Text,
		Matched:       []string{},
		Missing:       []string{},
		Extra:         []string{},
	}
	hay := foldOCR(extracted.Text)
	matchedFold := map[string]struct{}{}
	for _, exp := range expected {
		folded := foldOCR(exp.Text)
		if folded == "" {
			continue
		}
		if strings.Contains(hay, folded) {
			out.Matched = append(out.Matched, exp.Text)
			matchedFold[folded] = struct{}{}
		} else {
			out.Missing = append(out.Missing, exp.Text)
		}
	}
	// 多余：按空白切分 OCR 词元，折叠后不能被任一期望覆盖。
	for _, token := range tokenizeOCR(extracted.Text) {
		folded := foldOCR(token)
		if folded == "" {
			continue
		}
		covered := false
		for _, exp := range expected {
			ef := foldOCR(exp.Text)
			if ef == "" {
				continue
			}
			if folded == ef || strings.Contains(ef, folded) || strings.Contains(folded, ef) {
				covered = true
				break
			}
		}
		if !covered {
			if _, ok := matchedFold[folded]; !ok {
				out.Extra = append(out.Extra, token)
			}
		}
	}
	out.Matched = uniqStable(out.Matched)
	out.Missing = uniqStable(out.Missing)
	out.Extra = uniqStable(out.Extra)
	out.Pass = len(out.Missing) == 0 && len(out.Extra) == 0
	switch {
	case out.Pass:
		out.Detail = "成片文字与 text_trace 期望一致"
	case len(out.Missing) > 0 && len(out.Extra) > 0:
		out.Detail = "成片缺少期望文字且含多余文字"
	case len(out.Missing) > 0:
		out.Detail = "成片缺少 text_trace 期望文字"
	default:
		out.Detail = "成片含 text_trace 未声明的多余文字"
	}
	return out
}

// ComparePNG 用字形模板搜索判定匹配/缺失；掩盖已匹配模板后的残余墨迹判定多余。
// extracted 汇总已匹配可见串；残余墨迹记入 extra（真实像素对照，非旁路假 OCR）。
func ComparePNG(ex *GlyphExtractor, pngBytes []byte, expected []ExpectedItem) (CompareResult, error) {
	result := CompareResult{
		SchemaVersion: defaultSchemaVersion,
		Engine:        engineGlyphTemplate,
		Matched:       []string{},
		Missing:       []string{},
		Extra:         []string{},
	}
	for _, exp := range expected {
		ok, err := ex.Contains(pngBytes, exp.Text)
		if err != nil {
			return CompareResult{}, err
		}
		if ok {
			result.Matched = append(result.Matched, exp.Text)
		} else {
			result.Missing = append(result.Missing, exp.Text)
		}
	}
	result.Matched = uniqStable(result.Matched)
	result.Missing = uniqStable(result.Missing)

	extra, err := ex.residualExtras(pngBytes, result.Matched)
	if err != nil {
		return CompareResult{}, err
	}
	result.Extra = extra
	result.Extracted = strings.Join(result.Matched, " ")
	if len(result.Extra) > 0 {
		if result.Extracted != "" {
			result.Extracted += " "
		}
		result.Extracted += strings.Join(result.Extra, " ")
	}
	result.Pass = len(result.Missing) == 0 && len(result.Extra) == 0
	switch {
	case result.Pass:
		result.Detail = "成片文字与 text_trace 期望一致"
	case len(result.Missing) > 0 && len(result.Extra) > 0:
		result.Detail = "成片缺少期望文字且含多余文字"
	case len(result.Missing) > 0:
		result.Detail = "成片缺少 text_trace 期望文字"
	default:
		result.Detail = "成片含 text_trace 未声明的多余文字"
	}
	return result, nil
}

// residualExtras 在抹去已匹配期望模板后，若仍有显著墨迹则记为多余。
func (e *GlyphExtractor) residualExtras(pngBytes []byte, matched []string) ([]string, error) {
	gray, err := decodeInkMask(pngBytes)
	if err != nil {
		return nil, err
	}
	before := countInk(gray)
	if before == 0 {
		return nil, nil
	}
	for _, text := range matched {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		bleached := false
		for _, size := range e.Sizes {
			tmpl, err := e.renderInkMask(text, size)
			if err != nil {
				return nil, err
			}
			if loc, ok := findTemplate(gray, tmpl); ok {
				bleachTemplate(gray, tmpl, loc)
				bleached = true
				break
			}
		}
		_ = bleached
	}
	remain := countInk(gray)
	// 残余超过原墨迹 12% 且不少于 24 像素 → 视为未声明可见字。
	thresh := before / 8
	if thresh < 24 {
		thresh = 24
	}
	if remain >= thresh {
		return []string{"(unmatched_visible_ink)"}, nil
	}
	return nil, nil
}

func tokenizeOCR(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\t' || r == ' ' || r == ',' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func uniqStable(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		k := foldOCR(s)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func stringList(v any) []string {
	switch typed := v.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, s := range typed {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s := strings.TrimSpace(asString(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
