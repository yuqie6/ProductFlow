package imageeval

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/providers"
)

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testPool(role, typeKey, url string, w, h, n int) PoolImage {
	return PoolImage{
		Path:   role + "-" + typeKey + ".png",
		SHA256: FileSHA256([]byte(url)),
		Width:  w, Height: h, ByteSize: n,
		TypeKey: typeKey, Confidence: 0.9, SourceURL: url, Role: role,
	}
}

func completeImages() []PoolImage {
	var out []PoolImage
	for i := 0; i < 5; i++ {
		key := "hero"
		if i == 1 {
			key = "scene"
		}
		if i >= 2 {
			key = "detail"
		}
		out = append(out, testPool("gallery", key, "https://img.example/g"+itoa(i), 800, 800, 20_000))
	}
	out = append(out, testPool("sku", "sku", "https://img.example/sku", 800, 800, 20_000))
	for i := 0; i < 8; i++ {
		out = append(out, testPool("detail", "selling_point", "https://img.example/d"+itoa(i), 800, 1400, 30_000))
	}
	out[5].TypeKey = "specifications"
	return out
}

func itoa(i int) string {
	return string(rune('0' + i))
}

func TestNaivePromptCoversGeneratingTypes(t *testing.T) {
	for _, key := range GeneratingTypes {
		if _, err := NaivePrompt(key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if _, err := NaivePrompt("factory"); err == nil {
		t.Fatal("expected error for evidence type")
	}
}

func TestAdmitRejectsVirtualAndThin(t *testing.T) {
	listing := ExtractListing{URL: "https://item.taobao.com/item.htm?id=1", Title: "Claude Pro 官方续费账号", Category: "3c"}
	if _, err := Admit(listing, completeImages()); err == nil {
		t.Fatal("virtual goods should reject")
	}
	listing.Title = "陶瓷马克杯"
	thin := completeImages()[:3]
	if _, err := Admit(listing, thin); err == nil {
		t.Fatal("thin gallery should reject")
	}
}

func TestAdmitCompleteListing(t *testing.T) {
	listing := ExtractListing{
		Source: "taobao", URL: "https://item.taobao.com/item.htm?id=99",
		Title: "陶瓷马克杯 旗舰店", Category: "home", IsTmall: true,
	}
	m, err := Admit(listing, completeImages())
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == "" || len(m.References) == 0 || len(m.Gold) == 0 {
		t.Fatalf("incomplete admit %+v", m)
	}
	if len(m.ImageTypes) < MinDistinctTypes {
		t.Fatalf("types %v", m.ImageTypes)
	}
	hasHero, hasSell := false, false
	for _, tc := range m.ImageTypes {
		if tc.Key == "hero" {
			hasHero = true
		}
		if tc.Key == "selling_point" {
			hasSell = true
		}
	}
	if !hasHero || !hasSell {
		t.Fatalf("missing required gold types: %+v", m.ImageTypes)
	}
}

func TestSampleStratifiedReproducible(t *testing.T) {
	pool := []Manifest{
		{ID: "a", Category: "home"},
		{ID: "b", Category: "3c"},
		{ID: "c", Category: "home"},
		{ID: "d", Category: "beauty"},
	}
	x, err := SampleStratified(pool, 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	y, err := SampleStratified(pool, 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(x) != 3 {
		t.Fatalf("n=%d", len(x))
	}
	for i := range x {
		if x[i].ID != y[i].ID {
			t.Fatalf("seed mismatch %s %s", x[i].ID, y[i].ID)
		}
	}
}

func TestGateSlotWorkbenchMustBeatNaiveAndHoldGold(t *testing.T) {
	ok := GateSlot(SlotJudgement{
		Workbench: Scores{4, 4, 4, 4},
		Gold:      &Scores{3, 3, 4, 3},
		Naive:     Scores{2, 2, 2, 2},
	})
	if !ok.Passed {
		t.Fatalf("expected pass %v", ok.FailReasons)
	}
	loseGold := GateSlot(SlotJudgement{
		Workbench: Scores{3, 3, 3, 3},
		Gold:      &Scores{4, 4, 4, 4},
		Naive:     Scores{2, 2, 2, 2},
	})
	if loseGold.Passed {
		t.Fatal("should fail vs gold")
	}
	tieNaive := GateSlot(SlotJudgement{
		Workbench: Scores{3, 3, 3, 3},
		Naive:     Scores{3, 3, 3, 3},
	})
	if tieNaive.Passed {
		t.Fatal("must strictly beat naive")
	}
	fid := GateSlot(SlotJudgement{
		Workbench: Scores{2, 5, 5, 5},
		Gold:      &Scores{4, 3, 3, 3},
		Naive:     Scores{1, 1, 1, 1},
	})
	if fid.Passed {
		t.Fatal("fidelity below gold should fail")
	}
}

func TestParseJudgeJSON(t *testing.T) {
	wb, gold, naive, notes, err := ParseJudgeJSON(`here {"workbench":{"fidelity":4,"fit":4,"utility":3,"aesthetics":4},"gold":{"fidelity":3,"fit":3,"utility":3,"aesthetics":3},"naive":{"fidelity":2,"fit":2,"utility":2,"aesthetics":2},"notes":"封面更清楚"}`)
	if err != nil {
		t.Fatal(err)
	}
	if wb.Fidelity != 4 || gold == nil || gold.Fidelity != 3 || naive.Fidelity != 2 || notes == "" {
		t.Fatalf("%v %v %v %q", wb, gold, naive, notes)
	}
}

func TestClassifySuggestedTypeWins(t *testing.T) {
	key, conf := ClassifyImage(ExtractImage{Role: "gallery", Index: 0, SuggestedType: "packaging"}, 800, 800)
	if key != "packaging" || conf < 0.9 {
		t.Fatalf("%s %v", key, conf)
	}
}

func TestClassifyGallerySecondIsScene(t *testing.T) {
	key, conf := ClassifyImage(ExtractImage{Role: "gallery", Index: 1}, 800, 800)
	if key != "scene" || conf < MinTypeConfidence {
		t.Fatalf("%s %v", key, conf)
	}
}

func TestEvalTypeCountsK1(t *testing.T) {
	m := Manifest{ImageTypes: []TypeCount{{Key: "hero", Quantity: 6}, {Key: "selling_point", Quantity: 6}}}
	got := evalTypeCounts(m)
	if len(got) != 2 || got[0].Quantity != JudgeK || got[1].Key != "selling_point" {
		t.Fatalf("%+v", got)
	}
}

func TestIsVirtualGoods(t *testing.T) {
	if !IsVirtualGoods("官方订阅独享稳定会员帐号") {
		t.Fatal("expected virtual")
	}
	if IsVirtualGoods("陶瓷马克杯 暖白釉") {
		t.Fatal("physical mug")
	}
}

func TestIsUGCImage(t *testing.T) {
	if !IsUGCImage("买家秀", "") {
		t.Fatal("expected ugc")
	}
	if IsUGCImage("商品主图", "SKU白底") {
		t.Fatal("studio shot")
	}
}

func TestUpgradeCDNURL(t *testing.T) {
	got := UpgradeCDNURL("//img.alicdn.com/imgextra/i1/x.jpg_400x400q90.jpg_.webp")
	if got != "https://img.alicdn.com/imgextra/i1/x.jpg" {
		t.Fatalf("%s", got)
	}
}

func TestParseResponsesText(t *testing.T) {
	text, err := parseResponsesText([]byte(`{"output_text":"{\"workbench\":{\"fidelity\":4,\"fit\":4,\"utility\":4,\"aesthetics\":4}}"}`))
	if err != nil || !strings.Contains(text, "workbench") {
		t.Fatalf("%q %v", text, err)
	}
	text, err = parseResponsesText([]byte(`{"output":[{"content":[{"type":"output_text","text":"ok"}]}]}`))
	if err != nil || text != "ok" {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := parseResponsesText([]byte("<html>nope</html>")); err == nil {
		t.Fatal("expected non-json error")
	}
}

func TestJudgeEndpointStripsV1(t *testing.T) {
	got := judgeEndpoint("https://example.com/v1", "/v1/responses")
	if got != "https://example.com/v1/responses" {
		t.Fatalf("%s", got)
	}
}

func TestRunSampledRequiresSwitch(t *testing.T) {
	t.Setenv(RunSwitch, "")
	_, err := RunSampled(context.Background(), RunConfig{N: 1, Seed: 1, Image: stubImage{}, Judge: stubJudge{}})
	if err == nil {
		t.Fatal("expected env gate")
	}
}

type stubImage struct{}

func (stubImage) Name() string { return "stub" }
func (stubImage) Generate(context.Context, providers.GenerateRequest) (providers.GenerateResult, error) {
	return providers.GenerateResult{}, fmt.Errorf("not live")
}
func (stubImage) Edit(context.Context, providers.EditRequest) (providers.EditResult, error) {
	return providers.EditResult{}, fmt.Errorf("not live")
}
func (stubImage) Capability() providers.EditCapability { return providers.EditCapability{} }
func (stubImage) ReconcileResponse(context.Context, string) (string, error) {
	return "", nil
}

type stubJudge struct{}

func (stubJudge) Judge(context.Context, JudgeInput) (SlotJudgement, error) {
	return SlotJudgement{}, fmt.Errorf("not live")
}
