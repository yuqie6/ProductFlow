package graph

import (
	"context"

	"github.com/yuqie6/productflow/internal/ocr"
)

// ApplyImageOCRTrace 对成片做 OCR 对照并写回 text_trace（IQ-CF-02 B0）。
// 默认残余墨迹硬失败（合成夹具）。采用路径请用 ApplyImageOCRTraceForAdoption。
func ApplyImageOCRTrace(
	ctx context.Context,
	pngBytes []byte,
	textTrace map[string]any,
	facts []map[string]any,
) (map[string]any, ocr.CompareResult, error) {
	return applyImageOCRTrace(ctx, pngBytes, textTrace, facts, true)
}

// ApplyImageOCRTraceForAdoption 采用硬闸：只核期望文字是否出现；不因商品主体残余墨迹拒绝。
func ApplyImageOCRTraceForAdoption(
	ctx context.Context,
	pngBytes []byte,
	textTrace map[string]any,
	facts []map[string]any,
) (map[string]any, ocr.CompareResult, error) {
	return applyImageOCRTrace(ctx, pngBytes, textTrace, facts, false)
}

func applyImageOCRTrace(
	ctx context.Context,
	pngBytes []byte,
	textTrace map[string]any,
	facts []map[string]any,
	rejectUnmatchedInk bool,
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
	result, err := ocr.ComparePNGWithOpts(ex, pngBytes, expected, ocr.ComparePNGOpts{
		RejectUnmatchedInk: rejectUnmatchedInk,
	})
	if err != nil {
		return nil, ocr.CompareResult{}, err
	}
	return ocr.ApplyOCRGate(textTrace, result), result, nil
}
