package subjectextract

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestExtractSuccessLeavesVerifiableMaskAndCutout(t *testing.T) {
	pngBytes := mustFixtureSubjectOnWhite(t, 120, 80, 40, 20, 40, 40)
	result := ExtractPNG(pngBytes)
	if !result.Pass {
		t.Fatalf("want pass, got fail: %+v unresolved=%v", result.Detail, result.UnresolvedItems)
	}
	if result.Engine != engineCorner {
		t.Fatalf("engine: %s", result.Engine)
	}
	if result.MaskSHA256 == "" || result.CutoutSHA256 == "" {
		t.Fatalf("missing hashes: mask=%q cutout=%q", result.MaskSHA256, result.CutoutSHA256)
	}
	if contentSHA256(result.MaskPNG) != result.MaskSHA256 {
		t.Fatal("mask sha256 mismatch vs bytes")
	}
	if contentSHA256(result.CutoutPNG) != result.CutoutSHA256 {
		t.Fatal("cutout sha256 mismatch vs bytes")
	}
	if result.Lineage.Kind != "subject_extract" || result.Lineage.SourceContentSHA256 != result.SourceSHA256 {
		t.Fatalf("lineage: %+v", result.Lineage)
	}
	if result.Coverage < minCoverage || result.Coverage > maxCoverage {
		t.Fatalf("coverage out of gate: %f", result.Coverage)
	}
	if result.Bounds.W < 30 || result.Bounds.H < 30 {
		t.Fatalf("bounds too small: %+v", result.Bounds)
	}
	// 蒙版须含主体与背景像素。
	maskImg, err := png.Decode(bytes.NewReader(result.MaskPNG))
	if err != nil {
		t.Fatal(err)
	}
	mb := maskImg.Bounds()
	on, off := 0, 0
	for y := mb.Min.Y; y < mb.Max.Y; y++ {
		for x := mb.Min.X; x < mb.Max.X; x++ {
			r, _, _, _ := maskImg.At(x, y).RGBA()
			if r>>8 > 128 {
				on++
			} else {
				off++
			}
		}
	}
	if on == 0 || off == 0 {
		t.Fatalf("mask must have subject and background: on=%d off=%d", on, off)
	}
	cutout, err := png.Decode(bytes.NewReader(result.CutoutPNG))
	if err != nil {
		t.Fatal(err)
	}
	cb := cutout.Bounds()
	opaque, transparent := 0, 0
	for y := cb.Min.Y; y < cb.Max.Y; y++ {
		for x := cb.Min.X; x < cb.Max.X; x++ {
			_, _, _, a := cutout.At(x, y).RGBA()
			if a > 0x8000 {
				opaque++
			} else {
				transparent++
			}
		}
	}
	if opaque == 0 || transparent == 0 {
		t.Fatalf("cutout must have opaque subject and transparent bg: o=%d t=%d", opaque, transparent)
	}
}

func TestExtractFailureNotRouteQualified(t *testing.T) {
	// 纯白图：角点背景与全图像素不可分 → 无主体。
	solid := mustSolidPNG(t, 64, 64, color.RGBA{240, 240, 240, 255})
	fail := ExtractPNG(solid)
	if fail.Pass {
		t.Fatalf("solid bg must fail: coverage=%f", fail.Coverage)
	}
	if len(fail.UnresolvedItems) == 0 {
		t.Fatal("want unresolved items")
	}

	route := map[string]any{
		"schema_version":  1,
		"route":           "subject_preserve",
		"route_qualified": true,
		"image_type_key":  "hero",
	}
	gated := ApplySubjectPreserveGate(route, fail)
	if gated["route_qualified"] != false {
		t.Fatalf("failure must force route_qualified=false: %#v", gated["route_qualified"])
	}
	items := stringListFromAny(gated["unresolved_items"])
	if len(items) == 0 {
		t.Fatalf("want unresolved on produce_route: %#v", gated)
	}
	check, ok := gated["subject_extract"].(map[string]any)
	if !ok || check["pass"] != false {
		t.Fatalf("subject_extract: %#v", gated["subject_extract"])
	}
	if RouteQualifiedAfterExtract(true, fail) {
		t.Fatal("declaration true + extract fail must not qualify")
	}
}

func TestExtractEmptyInputFails(t *testing.T) {
	result := ExtractPNG(nil)
	if result.Pass {
		t.Fatal("empty must fail")
	}
	gated := ApplySubjectPreserveGate(map[string]any{
		"route": "subject_preserve", "route_qualified": true,
	}, result)
	if gated["route_qualified"] != false {
		t.Fatal("empty extract must not route-qualify")
	}
}

func TestApplyGateSkipsGenerative(t *testing.T) {
	pngBytes := mustFixtureSubjectOnWhite(t, 80, 60, 20, 15, 30, 30)
	result := ExtractPNG(pngBytes)
	if !result.Pass {
		t.Fatalf("fixture: %v", result.UnresolvedItems)
	}
	gen := map[string]any{
		"route": "generative", "route_qualified": true,
	}
	out := ApplySubjectPreserveGate(gen, result)
	if _, ok := out["subject_extract"]; ok {
		t.Fatalf("generative must not attach subject_extract: %#v", out)
	}
	if out["route_qualified"] != true {
		t.Fatal("generative qualification unchanged")
	}
}

func TestApplyGateSuccessKeepsQualifiedAndMetadata(t *testing.T) {
	pngBytes := mustFixtureSubjectOnWhite(t, 100, 80, 30, 20, 40, 40)
	result := ExtractPNG(pngBytes)
	if !result.Pass {
		t.Fatal(result.UnresolvedItems)
	}
	route := map[string]any{
		"route": "subject_preserve", "route_qualified": true,
	}
	gated := ApplySubjectPreserveGate(route, result)
	if gated["route_qualified"] != true {
		t.Fatalf("success should keep qualified: %#v", gated)
	}
	check := gated["subject_extract"].(map[string]any)
	if check["pass"] != true || check["mask_sha256"] == nil || check["cutout_sha256"] == nil {
		t.Fatalf("metadata: %#v", check)
	}
	lineage := check["lineage"].(map[string]any)
	if lineage["kind"] != "subject_extract" {
		t.Fatalf("lineage: %#v", lineage)
	}
}

func mustFixtureSubjectOnWhite(t *testing.T, w, h, sx, sy, sw, sh int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	for y := sy; y < sy+sh && y < h; y++ {
		for x := sx; x < sx+sw && x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{30, 90, 180, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustSolidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
