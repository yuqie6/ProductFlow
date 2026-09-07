package layout

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testFontBytes(t *testing.T) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "testdata", "fonts", "LiberationSans-Regular.ttf")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read font: %v", err)
	}
	return b
}

func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func cupFixture(t *testing.T, fontSize float64) (Document, FontCatalog, Assets, []byte) {
	t.Helper()
	subjectID := "asset-cup-body"
	subject := solidPNG(t, 200, 280, color.RGBA{R: 40, G: 120, B: 200, A: 255})
	doc := Document{
		SchemaVersion:  SchemaVersion,
		Width:          400,
		Height:         400,
		Background:     "#FFFFFF",
		SafeArea:       Insets{Top: 24, Right: 24, Bottom: 24, Left: 24},
		SubjectAssetID: subjectID,
		Layers: []Layer{
			{
				ID: "cup", Type: LayerImage,
				X: 100, Y: 60, Width: 200, Height: 280, ZIndex: 0,
				AssetID: subjectID,
			},
			{
				ID: "capacity", Type: LayerText,
				X: 40, Y: 320, Width: 320, Height: 48, ZIndex: 1,
				Text: "600ml", FontID: "liberation-sans", FontSize: fontSize,
				Color: "#111111", Align: AlignCenter, VAlign: VAlignMiddle,
			},
			{
				ID: "bar", Type: LayerShape, Shape: ShapeRect,
				X: 40, Y: 300, Width: 320, Height: 4, ZIndex: 1,
				Fill: "#333333",
			},
		},
	}
	fonts := FontCatalog{"liberation-sans": testFontBytes(t)}
	assets := Assets{subjectID: subject}
	return doc, fonts, assets, subject
}

func TestComposeDeterministic(t *testing.T) {
	doc, fonts, assets, _ := cupFixture(t, 28)
	a, err := Compose(doc, fonts, assets)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Compose(doc, fonts, assets)
	if err != nil {
		t.Fatal(err)
	}
	if a.PNGSHA256 != b.PNGSHA256 {
		t.Fatalf("png hash drift %s vs %s", a.PNGSHA256, b.PNGSHA256)
	}
	if a.Lineage.DocumentHash != b.Lineage.DocumentHash {
		t.Fatalf("document hash drift")
	}
	if !a.LayoutQualified {
		t.Fatalf("want qualified, unresolved=%v missing=%v", a.UnresolvedItems, a.MissingGlyphs)
	}
}

func TestFontSizeChangeKeepsSubjectBitmap(t *testing.T) {
	doc28, fonts, assets, subject := cupFixture(t, 28)
	doc36, _, _, _ := cupFixture(t, 36)
	doc36.SubjectAssetID = doc28.SubjectAssetID
	doc36.Layers = append([]Layer(nil), doc28.Layers...)
	doc36.Layers[1].FontSize = 36

	r28, err := Compose(doc28, fonts, assets)
	if err != nil {
		t.Fatal(err)
	}
	r36, err := Compose(doc36, fonts, assets)
	if err != nil {
		t.Fatal(err)
	}
	if r28.Lineage.SubjectAssetID != r36.Lineage.SubjectAssetID {
		t.Fatalf("subject id changed")
	}
	if r28.Lineage.SubjectContentSHA256 != ContentSHA256(subject) {
		t.Fatalf("subject sha mismatch")
	}
	if r28.Lineage.SubjectContentSHA256 != r36.Lineage.SubjectContentSHA256 {
		t.Fatalf("subject content changed after font size edit")
	}
	if r28.Lineage.ParentAssetID != doc28.SubjectAssetID {
		t.Fatalf("parent want subject")
	}
	if r28.PNGSHA256 == r36.PNGSHA256 {
		t.Fatalf("export png should change when font size changes")
	}
	if r28.Lineage.DocumentHash == r36.Lineage.DocumentHash {
		t.Fatalf("document hash should change when font size changes")
	}
	// 主体位图资产字节本身未被 Compose 改写
	if ContentSHA256(assets[doc28.SubjectAssetID]) != ContentSHA256(subject) {
		t.Fatalf("assets map mutated")
	}
}

func TestMissingGlyphNotQualified(t *testing.T) {
	doc, fonts, assets, _ := cupFixture(t, 28)
	doc.Layers[1].Text = "容量六百毫升" // Liberation Sans 无这些汉字
	r, err := Compose(doc, fonts, assets)
	if err != nil {
		t.Fatal(err)
	}
	if r.LayoutQualified {
		t.Fatalf("missing glyphs must not qualify; missing=%v", r.MissingGlyphs)
	}
	if len(r.MissingGlyphs) == 0 {
		t.Fatalf("expected missing glyphs")
	}
	found := false
	for _, item := range r.UnresolvedItems {
		if len(item) >= len("missing_glyph:") && item[:len("missing_glyph:")] == "missing_glyph:" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unresolved missing_glyph absent: %v", r.UnresolvedItems)
	}
}

func TestOutsideSafeNotQualified(t *testing.T) {
	doc, fonts, assets, _ := cupFixture(t, 28)
	doc.Layers[1].X = 0
	doc.Layers[1].Y = 0
	r, err := Compose(doc, fonts, assets)
	if err != nil {
		t.Fatal(err)
	}
	if r.LayoutQualified {
		t.Fatalf("outside safe must not qualify")
	}
}

func TestPreviewPlanMatchesExportFrames(t *testing.T) {
	doc, fonts, assets, _ := cupFixture(t, 28)
	r, err := Compose(doc, fonts, assets)
	if err != nil {
		t.Fatal(err)
	}
	preview := PreviewFrames(doc)
	if err := CompareFrames(preview, r.Plan, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPackageDoesNotImportGraph(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	needle := "productflow/internal/" + "graph"
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(needle)) {
			t.Fatalf("%s imports graph (forbidden for layout service)", name)
		}
	}
}

func TestValidateRejectsBadAlign(t *testing.T) {
	doc, _, _, _ := cupFixture(t, 28)
	doc.Layers[1].Align = "justify"
	if err := ValidateDocument(doc); err == nil {
		t.Fatal("expected align validation error")
	}
}
