package graph

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestApplySubjectPreserveExtractSuccessMetadata(t *testing.T) {
	ref := mustSubjectFixturePNG(t, 100, 80, 25, 20, 45, 40)
	route := ProduceRouteAsMap(BuildProduceRouteRecord(ProduceRouteInput{
		Route:                ProduceRouteSubjectPreserve,
		ImageTypeKey:         "hero",
		HasIdentityReference: true,
		PromptTexts:          []string{"棚拍主图"},
	}))
	if !route["route_qualified"].(bool) {
		t.Fatalf("precondition: %+v", route)
	}
	gated, result, err := ApplySubjectPreserveExtract(ref, route)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pass {
		t.Fatalf("extract fail: %v", result.UnresolvedItems)
	}
	if gated["route_qualified"] != true {
		t.Fatalf("success must stay route-qualified: %+v", gated)
	}
	check, ok := gated["subject_extract"].(map[string]any)
	if !ok {
		t.Fatalf("missing subject_extract: %+v", gated)
	}
	if check["pass"] != true {
		t.Fatalf("check: %+v", check)
	}
	maskHash, _ := check["mask_sha256"].(string)
	cutoutHash, _ := check["cutout_sha256"].(string)
	if maskHash == "" || cutoutHash == "" {
		t.Fatalf("verifiable hashes missing: %+v", check)
	}
	if maskHash != result.MaskSHA256 || cutoutHash != result.CutoutSHA256 {
		t.Fatal("metadata hashes must match extract result")
	}
	composeMeta, ok := gated["subject_compose"].(map[string]any)
	if !ok || composeMeta["pass"] != true {
		t.Fatalf("want subject_compose on success: %+v", gated)
	}
	pngHash, _ := composeMeta["png_sha256"].(string)
	if pngHash == "" {
		t.Fatalf("compose png hash missing: %+v", composeMeta)
	}
	placement, _ := composeMeta["placement"].(map[string]any)
	if placement["w"] == nil || placement["h"] == nil {
		t.Fatalf("compose placement missing: %+v", composeMeta)
	}
}

func TestApplySubjectPreserveExtractFailureUnqualified(t *testing.T) {
	solid := mustSolidFixturePNG(t, 48, 48, color.RGBA{200, 200, 200, 255})
	route := ProduceRouteAsMap(BuildProduceRouteRecord(ProduceRouteInput{
		Route:                ProduceRouteSubjectPreserve,
		ImageTypeKey:         "hero",
		HasIdentityReference: true,
	}))
	gated, result, err := ApplySubjectPreserveExtract(solid, route)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Fatal("solid image must fail extract")
	}
	if gated["route_qualified"] != false {
		t.Fatalf("failure must not be route-qualified: %+v", gated)
	}
	items, _ := gated["unresolved_items"].([]any)
	if len(items) == 0 {
		t.Fatalf("want unresolved: %+v", gated)
	}
	q := ParseArtifactDeliveryQualification(map[string]any{"produce_route": gated})
	if !q.HasProduceRoute || RouteAllowsDeliveryPass(q.Route) {
		t.Fatalf("failed extract must block delivery pass: %+v", q.Route)
	}
}

func TestApplySubjectPreserveExtractSkipsGenerative(t *testing.T) {
	ref := mustSubjectFixturePNG(t, 80, 60, 20, 15, 30, 30)
	route := map[string]any{
		"route": ProduceRouteGenerative, "route_qualified": true,
	}
	gated, result, err := ApplySubjectPreserveExtract(ref, route)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass || result.Engine != "" {
		// skipped → zero result
		if result.MaskSHA256 != "" {
			t.Fatalf("generative should skip extract: %+v", result)
		}
	}
	if _, ok := gated["subject_extract"]; ok {
		t.Fatalf("must not attach extract on generative: %+v", gated)
	}
}

func TestGateImageProduceRouteSubjectPreserveSuccessMetadata(t *testing.T) {
	ref := mustSubjectFixturePNG(t, 100, 80, 25, 20, 45, 40)
	route := ProduceRouteAsMap(BuildProduceRouteRecord(ProduceRouteInput{
		Route:                ProduceRouteSubjectPreserve,
		ImageTypeKey:         "hero",
		HasIdentityReference: true,
		PromptTexts:          []string{"棚拍主图"},
	}))
	gated := gateImageProduceRouteWithSubjectExtract([]ReferenceImage{{
		Role: "product_identity", Bytes: ref, MIME: "image/png",
	}}, route)
	if gated["route_qualified"] != true {
		t.Fatalf("success must stay qualified: %+v", gated)
	}
	check, ok := gated["subject_extract"].(map[string]any)
	if !ok || check["pass"] != true {
		t.Fatalf("want subject_extract metadata: %+v", gated)
	}
	if check["mask_sha256"] == "" || check["cutout_sha256"] == "" {
		t.Fatalf("verifiable hashes missing: %+v", check)
	}
	composeMeta, ok := gated["subject_compose"].(map[string]any)
	if !ok || composeMeta["pass"] != true || composeMeta["png_sha256"] == "" {
		t.Fatalf("want subject_compose metadata: %+v", gated)
	}
}

func TestGateImageProduceRouteSubjectPreserveFailureUnqualified(t *testing.T) {
	solid := mustSolidFixturePNG(t, 48, 48, color.RGBA{200, 200, 200, 255})
	route := ProduceRouteAsMap(BuildProduceRouteRecord(ProduceRouteInput{
		Route:                ProduceRouteSubjectPreserve,
		ImageTypeKey:         "hero",
		HasIdentityReference: true,
	}))
	gated := gateImageProduceRouteWithSubjectExtract([]ReferenceImage{{
		Role: "product_identity", Bytes: solid, MIME: "image/png",
	}}, route)
	if gated["route_qualified"] != false {
		t.Fatalf("failure must force unqualified: %+v", gated)
	}
	if _, ok := gated["subject_extract"].(map[string]any); !ok {
		t.Fatalf("want subject_extract on failure: %+v", gated)
	}
	q := ParseArtifactDeliveryQualification(map[string]any{"produce_route": gated})
	if RouteAllowsDeliveryPass(q.Route) {
		t.Fatalf("failed extract must block delivery: %+v", q.Route)
	}
}

func TestGateImageProduceRouteGenerativeSkipped(t *testing.T) {
	ref := mustSubjectFixturePNG(t, 80, 60, 20, 15, 30, 30)
	route := ProduceRouteAsMap(BuildProduceRouteRecord(ProduceRouteInput{
		Route:        ProduceRouteGenerative,
		ImageTypeKey: "scene",
		PromptTexts:  []string{"场景摄影"},
	}))
	gated := gateImageProduceRouteWithSubjectExtract([]ReferenceImage{{
		Role: "product_identity", Bytes: ref, MIME: "image/png",
	}}, route)
	if gated["route_qualified"] != true {
		t.Fatalf("generative must not be forced unqualified: %+v", gated)
	}
	if _, ok := gated["subject_extract"]; ok {
		t.Fatalf("generative must skip extract: %+v", gated)
	}
}

func TestGateImageProduceRouteMissingIdentityUnqualified(t *testing.T) {
	route := ProduceRouteAsMap(BuildProduceRouteRecord(ProduceRouteInput{
		Route:                ProduceRouteSubjectPreserve,
		ImageTypeKey:         "hero",
		HasIdentityReference: true, // 声明有身份，但入边无字节 → 提取失败
	}))
	gated := gateImageProduceRouteWithSubjectExtract(nil, route)
	if gated["route_qualified"] != false {
		t.Fatalf("empty refs must force unqualified: %+v", gated)
	}
}

func mustSubjectFixturePNG(t *testing.T, w, h, sx, sy, sw, sh int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{252, 252, 252, 255})
		}
	}
	for y := sy; y < sy+sh && y < h; y++ {
		for x := sx; x < sx+sw && x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{20, 70, 160, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustSolidFixturePNG(t *testing.T, w, h int, c color.RGBA) []byte {
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
