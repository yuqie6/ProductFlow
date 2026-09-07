package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/ocr"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/product"
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

// applyAdoptionOCRGate 对要求文字的 text_trace 读成片并对照；失败拒绝采用。
// 选型：无成片字节 / 无可对照期望 → Validation 拒绝（不得静默当 pass）；无文字期望则跳过 OCR。
func (s Service) applyAdoptionOCRGate(
	ctx context.Context,
	tx *gorm.DB,
	productID string,
	slot preparedSlot,
	payload map[string]any,
	facts []map[string]any,
) error {
	rawTrace, _ := payload["text_trace"].(map[string]any)
	if !adoptionTextTraceNeedsOCR(rawTrace) {
		return nil
	}
	refs := factRefsFromMaps(facts)
	expected := ocr.ExpectedFromTextTrace(rawTrace, refs)
	if len(expected) == 0 {
		return apperr.Validationf("图位 %s 无可对照的文字期望，不能采用为交付", slot.slotKey)
	}

	asset, err := product.LoadAssetRow(ctx, tx, slot.sourceAssetID)
	if err != nil || asset.ProductID != productID {
		return apperr.Validationf("图位 %s 成片不可读，文字 OCR 无法核对", slot.slotKey)
	}
	content, err := s.Media.ReadVerified(ctx, tx, asset.MediaObjectID)
	if err != nil {
		if re, ok := media.AsReadError(err); ok {
			return apperr.Validationf("图位 %s 成片字节缺失或未核验，文字 OCR 无法核对（%s）", slot.slotKey, re.Detail)
		}
		return err
	}
	if len(content.Bytes) == 0 {
		return apperr.Validationf("图位 %s 成片字节为空，文字 OCR 无法核对", slot.slotKey)
	}

	gated, result, err := graph.ApplyImageOCRTraceForAdoption(ctx, content.Bytes, rawTrace, facts)
	if err != nil {
		return apperr.Validationf("图位 %s 文字 OCR 对照失败：%s", slot.slotKey, err.Error())
	}
	if !result.Pass || !ocr.TextQualifiedAfterOCR(true, result) {
		return apperr.Validationf("图位 %s 文字 OCR 对照不合格，不能采用为交付", slot.slotKey)
	}
	if q, ok := gated["text_qualified"].(bool); ok && !q {
		return apperr.Validationf("图位 %s 文字 OCR 对照不合格，不能采用为交付", slot.slotKey)
	}
	return nil
}
