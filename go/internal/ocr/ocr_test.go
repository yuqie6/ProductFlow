package ocr

import (
	"testing"
)

func TestOCRMissingExpectedBlocksQualified(t *testing.T) {
	ex := MustGlyphExtractor()
	// 成片无容量字，但 text_trace 声明 fact_keys=capacity。
	png, err := RenderTextPNG("Stainless Body", 400, 120, 28)
	if err != nil {
		t.Fatal(err)
	}
	trace := map[string]any{
		"schema_version":  1,
		"text_qualified":  true,
		"fact_keys":       []any{"capacity"},
		"user_image_override": false,
		"entries": []any{
			map[string]any{
				"field": "text.headline", "text": "600ml", "traced": true,
				"fact_keys": []any{"capacity"},
			},
		},
	}
	expected := ExpectedFromTextTrace(trace, []FactRef{{Key: "capacity", Value: "600ml"}})
	if len(expected) == 0 {
		t.Fatal("expected items empty")
	}
	result, err := ComparePNG(ex, png, expected)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Fatalf("want fail, got pass: %+v", result)
	}
	if len(result.Missing) == 0 {
		t.Fatalf("want missing 600ml: %+v", result)
	}
	gated := ApplyOCRGate(trace, result)
	if gated["text_qualified"] != false {
		t.Fatalf("OCR fail must force text_qualified=false: %#v", gated["text_qualified"])
	}
	rawCheck, ok := gated["ocr_check"].(map[string]any)
	if !ok || rawCheck["pass"] != false {
		t.Fatalf("ocr_check missing/pass: %#v", gated["ocr_check"])
	}
	if TextQualifiedAfterOCR(true, result) {
		t.Fatal("declaration true + OCR fail must not qualify")
	}
}

func TestOCRMatchingFactAllowsPass(t *testing.T) {
	ex := MustGlyphExtractor()
	png, err := RenderTextPNG("600ml", 320, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := ex.Contains(png, "600ml")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("glyph template must find rendered 600ml")
	}
	trace := map[string]any{
		"schema_version":     1,
		"text_qualified":     true,
		"fact_keys":          []any{"capacity"},
		"user_image_override": false,
		"entries": []any{
			map[string]any{
				"field": "text.headline", "text": "600ml", "traced": true,
				"fact_keys": []any{"capacity"},
			},
		},
	}
	expected := ExpectedFromTextTrace(trace, []FactRef{{Key: "capacity", Value: "600ml"}})
	result, err := ComparePNG(ex, png, expected)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pass {
		t.Fatalf("want pass: %+v extracted=%q", result, result.Extracted)
	}
	if len(result.Matched) == 0 || len(result.Missing) != 0 {
		t.Fatalf("match/missing: %+v", result)
	}
	gated := ApplyOCRGate(trace, result)
	if gated["text_qualified"] != true {
		t.Fatalf("OCR pass must keep declaration true: %#v", gated["text_qualified"])
	}
	if !TextQualifiedAfterOCR(true, result) {
		t.Fatal("want qualified")
	}
}

func TestOCRDoesNotUpgradeDeclarationFail(t *testing.T) {
	ex := MustGlyphExtractor()
	png, err := RenderTextPNG("600ml", 320, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	trace := map[string]any{
		"text_qualified": false,
		"fact_keys":      []any{"capacity"},
		"entries": []any{
			map[string]any{"text": "600ml", "traced": true, "fact_keys": []any{"capacity"}},
		},
	}
	expected := ExpectedFromTextTrace(trace, []FactRef{{Key: "capacity", Value: "600ml"}})
	result, err := ComparePNG(ex, png, expected)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pass {
		t.Fatalf("OCR itself should pass: %+v", result)
	}
	gated := ApplyOCRGate(trace, result)
	if gated["text_qualified"] != false {
		t.Fatalf("must not upgrade declaration fail: %#v", gated["text_qualified"])
	}
	if TextQualifiedAfterOCR(false, result) {
		t.Fatal("declaration false must stay unqualified")
	}
}

func TestOCRExtraInkFailsPass(t *testing.T) {
	ex := MustGlyphExtractor()
	// 成片同时有期望字与未声明字 → 残余墨迹记 extra，不得 pass。
	png, err := RenderTextPNG("600ml  SALE", 480, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	expected := []ExpectedItem{{Source: "fact_key", Key: "capacity", Text: "600ml"}}
	result, err := ComparePNG(ex, png, expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("600ml should match: %+v", result)
	}
	if result.Pass || len(result.Extra) == 0 {
		t.Fatalf("want extra ink fail: %+v", result)
	}
	if TextQualifiedAfterOCR(true, result) {
		t.Fatal("extra must block qualified")
	}
}

func TestOCRExpectedOnlyAllowsProductBodyInk(t *testing.T) {
	ex := MustGlyphExtractor()
	// 成片含期望字与未声明字（残余墨迹）：严格模式硬失败；采用模式只核期望字。
	// 不用大块色主体夹具——大模板窗会把远处墨迹算进 extra 导致 Contains 假阴性。
	png, err := RenderTextPNG("600ml  SALE", 480, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	expected := []ExpectedItem{{Source: "fact_key", Key: "capacity", Text: "600ml"}}

	strict, err := ComparePNG(ex, png, expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(strict.Missing) != 0 {
		t.Fatalf("600ml must match: %+v", strict)
	}
	if strict.Pass || len(strict.Extra) == 0 {
		t.Fatalf("strict mode should fail on unmatched ink: %+v", strict)
	}

	loose, err := ComparePNGExpectedOnly(ex, png, expected)
	if err != nil {
		t.Fatal(err)
	}
	if !loose.Pass || len(loose.Missing) != 0 {
		t.Fatalf("adoption mode must pass when expected text present: %+v", loose)
	}
	if len(loose.Extra) == 0 {
		t.Fatal("extra should still be recorded for diagnostics")
	}
	if !TextQualifiedAfterOCR(true, loose) {
		t.Fatal("adoption mode must allow text_qualified")
	}
}


func TestContainsRenderedText(t *testing.T) {
	ex := MustGlyphExtractor()
	png, err := RenderTextPNG("600ml", 320, 100, 32)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := ex.Contains(png, "600ml")
	if err != nil || !ok {
		t.Fatalf("Contains 600ml: ok=%v err=%v", ok, err)
	}
	ok, err = ex.Contains(png, "999ml")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("must not contain 999ml")
	}
}

func TestUserOverrideExpected(t *testing.T) {
	trace := map[string]any{
		"user_image_override": true,
		"fact_keys":           []any{},
		"entries": []any{
			map[string]any{"text": "节日限定", "traced": true, "user_image_override": true},
		},
	}
	expected := ExpectedFromTextTrace(trace, nil)
	if len(expected) != 1 || expected[0].Source != "override" {
		t.Fatalf("%+v", expected)
	}
}
