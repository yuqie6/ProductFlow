package graph

import (
	"testing"

	"github.com/yuqie6/productflow/internal/ocr"
)

func TestApplyImageOCRTraceMissingTextForcesUnqualified(t *testing.T) {
	png, err := ocr.RenderTextPNG("no capacity here", 400, 100, 28)
	if err != nil {
		t.Fatal(err)
	}
	trace := map[string]any{
		"text_qualified": true,
		"fact_keys":      []any{"capacity"},
		"entries": []any{
			map[string]any{"text": "600ml", "traced": true, "fact_keys": []any{"capacity"}},
		},
	}
	facts := []map[string]any{{"key": "capacity", "value": "600ml"}}
	gated, result, err := ApplyImageOCRTrace(t.Context(), png, trace, facts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass || gated["text_qualified"] != false {
		t.Fatalf("want OCR fail → unqualified: result=%+v gated=%v", result, gated["text_qualified"])
	}
}

func TestApplyImageOCRTraceMatchKeepsQualified(t *testing.T) {
	png, err := ocr.RenderTextPNG("600ml", 320, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	trace := map[string]any{
		"text_qualified": true,
		"fact_keys":      []any{"capacity"},
		"entries": []any{
			map[string]any{"text": "600ml", "traced": true, "fact_keys": []any{"capacity"}},
		},
	}
	facts := []map[string]any{{"key": "capacity", "value": "600ml"}}
	gated, result, err := ApplyImageOCRTrace(t.Context(), png, trace, facts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pass || gated["text_qualified"] != true {
		t.Fatalf("want pass: result=%+v gated=%v", result, gated["text_qualified"])
	}
}
