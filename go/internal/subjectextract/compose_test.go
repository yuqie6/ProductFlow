package subjectextract

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestComposeSuccessDimensionsAndOpaqueSubject(t *testing.T) {
	pngBytes := mustFixtureSubjectOnWhite(t, 120, 80, 40, 20, 40, 40)
	extract := ExtractPNG(pngBytes)
	if !extract.Pass {
		t.Fatalf("extract: %v", extract.UnresolvedItems)
	}
	// 抠图须保留透明背景，供合成覆盖。
	cutout, err := png.Decode(bytes.NewReader(extract.CutoutPNG))
	if err != nil {
		t.Fatal(err)
	}
	cb := cutout.Bounds()
	transparent := 0
	for y := cb.Min.Y; y < cb.Max.Y; y++ {
		for x := cb.Min.X; x < cb.Max.X; x++ {
			_, _, _, a := cutout.At(x, y).RGBA()
			if a < 0x8000 {
				transparent++
			}
		}
	}
	if transparent == 0 {
		t.Fatal("cutout must keep transparent background before compose")
	}

	spec := DefaultComposeSpec()
	spec.Width, spec.Height = 400, 400
	spec.SafeArea = Insets{Top: 32, Right: 32, Bottom: 32, Left: 32}
	spec.Background = BackgroundSpec{Kind: "solid", Color: "#EEEEEE"}
	spec.ContactShadow = true

	composed := ComposeFromExtract(extract, spec)
	if !composed.Pass {
		t.Fatalf("compose fail: %s unresolved=%v", composed.Detail, composed.UnresolvedItems)
	}
	if composed.Width != 400 || composed.Height != 400 {
		t.Fatalf("dimensions: %dx%d", composed.Width, composed.Height)
	}
	if composed.PNGSHA256 == "" || contentSHA256(composed.PNG) != composed.PNGSHA256 {
		t.Fatal("compose png sha mismatch")
	}
	if composed.Lineage.Kind != composeLineageKind {
		t.Fatalf("lineage kind: %s", composed.Lineage.Kind)
	}
	if composed.Lineage.CutoutContentSHA256 != extract.CutoutSHA256 {
		t.Fatal("lineage must chain cutout sha")
	}
	if composed.Placement.W < 1 || composed.Placement.H < 1 {
		t.Fatalf("placement: %+v", composed.Placement)
	}
	if outsideSafe(spec, composed.Placement) {
		t.Fatalf("placement outside safe: %+v", composed.Placement)
	}

	outImg, err := png.Decode(bytes.NewReader(composed.PNG))
	if err != nil {
		t.Fatal(err)
	}
	ob := outImg.Bounds()
	if ob.Dx() != 400 || ob.Dy() != 400 {
		t.Fatalf("decoded size %dx%d", ob.Dx(), ob.Dy())
	}
	// 主体放置区应有有色不透明像素；角落应接近背景灰。
	if !subjectHasOpaquePixels(mustRGBA(t, outImg), composed.Placement) {
		t.Fatal("composed subject region missing opaque subject pixels")
	}
	cr, cg, cb2, ca := outImg.At(0, 0).RGBA()
	if ca < 0xF000 || cr>>8 < 220 || cg>>8 < 220 || cb2>>8 < 220 {
		t.Fatalf("corner should be light bg, got %d,%d,%d,%d", cr>>8, cg>>8, cb2>>8, ca>>8)
	}

	route := map[string]any{
		"route": "subject_preserve", "route_qualified": true,
	}
	gated := ApplySubjectComposeGate(ApplySubjectPreserveGate(route, extract), composed)
	if gated["route_qualified"] != true {
		t.Fatalf("success must stay qualified: %#v", gated)
	}
	meta, ok := gated["subject_compose"].(map[string]any)
	if !ok || meta["pass"] != true {
		t.Fatalf("subject_compose: %#v", gated["subject_compose"])
	}
	if meta["png_sha256"] != composed.PNGSHA256 {
		t.Fatalf("metadata sha: %#v", meta)
	}
	if !RouteQualifiedAfterCompose(true, extract, composed) {
		t.Fatal("declaration+extract+compose pass must qualify")
	}
}

func TestComposeGradientBackground(t *testing.T) {
	pngBytes := mustFixtureSubjectOnWhite(t, 100, 80, 30, 20, 40, 40)
	extract := ExtractPNG(pngBytes)
	if !extract.Pass {
		t.Fatal(extract.UnresolvedItems)
	}
	spec := DefaultComposeSpec()
	spec.Width, spec.Height = 320, 320
	spec.Background = BackgroundSpec{
		Kind: "gradient_v", Top: "#FFFFFF", Bottom: "#CCDDEE",
	}
	spec.ContactShadow = false
	composed := ComposeFromExtract(extract, spec)
	if !composed.Pass {
		t.Fatalf("gradient compose: %v", composed.UnresolvedItems)
	}
	if composed.BackgroundKind != "gradient_v" {
		t.Fatalf("bg kind: %s", composed.BackgroundKind)
	}
}

func TestComposeFailureWhenExtractFails(t *testing.T) {
	solid := mustSolidPNG(t, 64, 64, color.RGBA{240, 240, 240, 255})
	extract := ExtractPNG(solid)
	if extract.Pass {
		t.Fatal("solid must fail extract")
	}
	composed := ComposeFromExtract(extract, DefaultComposeSpec())
	if composed.Pass {
		t.Fatal("compose must fail when extract fails")
	}
	if len(composed.UnresolvedItems) == 0 {
		t.Fatal("want unresolved")
	}
	route := map[string]any{
		"route": "subject_preserve", "route_qualified": true,
	}
	gated := ApplySubjectPreserveGate(route, extract)
	gated = ApplySubjectComposeGate(gated, composed)
	if gated["route_qualified"] != false {
		t.Fatalf("failure must force route_qualified=false: %#v", gated)
	}
	items := stringListFromAny(gated["unresolved_items"])
	if len(items) == 0 {
		t.Fatalf("want unresolved on produce_route: %#v", gated)
	}
	check, ok := gated["subject_compose"].(map[string]any)
	if !ok || check["pass"] != false {
		t.Fatalf("subject_compose: %#v", gated["subject_compose"])
	}
	if RouteQualifiedAfterCompose(true, extract, composed) {
		t.Fatal("must not qualify after extract/compose failure")
	}
}

func TestComposeFailureInvalidSpec(t *testing.T) {
	pngBytes := mustFixtureSubjectOnWhite(t, 80, 60, 20, 15, 30, 30)
	extract := ExtractPNG(pngBytes)
	if !extract.Pass {
		t.Fatal(extract.UnresolvedItems)
	}
	spec := DefaultComposeSpec()
	spec.SafeArea = Insets{Top: 500, Right: 500, Bottom: 500, Left: 500}
	composed := ComposeFromExtract(extract, spec)
	if composed.Pass {
		t.Fatal("oversized safe area must fail")
	}
}

func TestComposeGateSkipsGenerative(t *testing.T) {
	pngBytes := mustFixtureSubjectOnWhite(t, 80, 60, 20, 15, 30, 30)
	extract := ExtractPNG(pngBytes)
	if !extract.Pass {
		t.Fatal(extract.UnresolvedItems)
	}
	composed := ComposeFromExtract(extract, DefaultComposeSpec())
	if !composed.Pass {
		t.Fatal(composed.UnresolvedItems)
	}
	gen := map[string]any{"route": "generative", "route_qualified": true}
	out := ApplySubjectComposeGate(gen, composed)
	if _, ok := out["subject_compose"]; ok {
		t.Fatalf("generative must not attach subject_compose: %#v", out)
	}
}

func mustRGBA(t *testing.T, img image.Image) *image.RGBA {
	t.Helper()
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}
