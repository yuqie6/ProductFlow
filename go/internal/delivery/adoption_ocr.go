package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/ocr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// adoptionTextTraceNeedsOCR 判定 text_trace 是否要求成片文字对照（政策要求文字）。
// 有 fact_keys 或条目文案时必须 OCR；空 trace（无期望）跳过，不得把缺期望当 OCR pass。
func adoptionTextTraceNeedsOCR(textTrace map[string]any) bool {
	if textTrace == nil {
		return false
	}
	for _, key := range adoptionStringList(textTrace["fact_keys"]) {
		if strings.TrimSpace(key) != "" {
			return true
		}
	}
	rawEntries, ok := textTrace["entries"].([]any)
	if !ok {
		return false
	}
	for _, raw := range rawEntries {
		entry, ok := raw.(map[string]any)
		if !ok || entry == nil {
			continue
		}
		text, _ := entry["text"].(string)
		if strings.TrimSpace(text) != "" {
			return true
		}
	}
	return false
}

func adoptionStringList(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func loadAdoptionFactMaps(
	ctx context.Context,
	tx *gorm.DB,
	productID string,
	preferredFactSetID *string,
) ([]map[string]any, error) {
	versionID := ""
	if preferredFactSetID != nil {
		versionID = strings.TrimSpace(*preferredFactSetID)
	}
	if versionID == "" {
		var prod schema.Products
		if err := tx.WithContext(ctx).Select("current_fact_set_version_id").
			Where("id = ?", productID).Take(&prod).Error; err != nil {
			return nil, err
		}
		if prod.CurrentFactSetVersionID == nil {
			return nil, nil
		}
		versionID = strings.TrimSpace(*prod.CurrentFactSetVersionID)
		if versionID == "" {
			return nil, nil
		}
	}
	var rec schema.ProductFactSetVersions
	err := tx.WithContext(ctx).
		Where("id = ? AND product_id = ?", versionID, productID).
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(rec.PayloadJSON)
	if raw == "" {
		return nil, nil
	}
	var parsed struct {
		Facts []map[string]any `json:"facts"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	return parsed.Facts, nil
}

func factRefsFromMaps(facts []map[string]any) []ocr.FactRef {
	out := make([]ocr.FactRef, 0, len(facts))
	for _, f := range facts {
		if f == nil {
			continue
		}
		key, _ := f["key"].(string)
		key = strings.TrimSpace(key)
		if key == "" || f["value"] == nil {
			continue
		}
		value, ok := f["value"].(string)
		if !ok {
			value = strings.TrimSpace(fmt.Sprint(f["value"]))
		} else {
			value = strings.TrimSpace(value)
		}
		if value == "" || value == "<nil>" {
			continue
		}
		out = append(out, ocr.FactRef{Key: key, Value: value})
	}
	return out
}

// checkAdoptionOCR 只返回检查结论。图片字节由采用入口完成归属与完整性校验。
func checkAdoptionOCR(ctx context.Context, imageBytes []byte, payload map[string]any, facts []map[string]any) (string, string, error) {
	rawTrace, _ := payload["text_trace"].(map[string]any)
	if !adoptionTextTraceNeedsOCR(rawTrace) {
		return "pass", "", nil
	}
	expected := ocr.ExpectedFromTextTrace(rawTrace, factRefsFromMaps(facts))
	if len(expected) == 0 {
		return "unchecked", "无可对照的文字期望", nil
	}
	gated, result, err := graph.ApplyImageOCRTraceForAdoption(ctx, imageBytes, rawTrace, facts)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "unchecked", "文字检查无法完成，请人工核对", nil
	}
	if !result.Pass || !ocr.TextQualifiedAfterOCR(true, result) {
		return "fail", "文字检查未识别到预期内容，请人工核对", nil
	}
	if q, ok := gated["text_qualified"].(bool); ok && !q {
		return "fail", "文字追溯检查未通过", nil
	}
	return "pass", "", nil
}
