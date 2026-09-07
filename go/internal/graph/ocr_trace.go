package graph

import (
	"context"

	"github.com/yuqie6/productflow/internal/ocr"
)

// ApplyImageOCRTrace 对成片 PNG 做 OCR 对照并写回 text_trace（IQ-CF-02 B0）。
// 失败强制 text_qualified=false。不接入采用 HTTP；供内部/后续采用路径消费。
func ApplyImageOCRTrace(
	ctx context.Context,
	pngBytes []byte,
	textTrace map[string]any,
	facts []map[string]any,
) (map[string]any, ocr.CompareResult, error) {
	if textTrace == nil {
		textTrace = map[string]any{}
	}
	refs := make([]ocr.FactRef, 0, len(facts))
	for _, f := range facts {
		refs = append(refs, ocr.FactRef{
			Key:   asString(f["key"]),
			Value: asString(f["value"]),
		})
	}
	expected := ocr.ExpectedFromTextTrace(textTrace, refs)
	ex, err := ocr.NewGlyphExtractor(nil)
	if err != nil {
		return nil, ocr.CompareResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, ocr.CompareResult{}, err
	}
	// 确定性字形引擎；live 视觉由 ocr.DefaultExtractor / PRODUCTFLOW_OCR_LIVE 另用。
	result, err := ocr.ComparePNG(ex, pngBytes, expected)
	if err != nil {
		return nil, ocr.CompareResult{}, err
	}
	return ocr.ApplyOCRGate(textTrace, result), result, nil
}
