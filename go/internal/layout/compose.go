package layout

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
)

// DocumentHash 对规范 JSON 做 SHA-256，供 lineage。
func DocumentHash(doc Document) (string, error) {
	return canonjson.SHA256Hex(doc)
}

// Compose 按 Document 确定性导出 PNG + LayoutPlan。不调模型、不调度 Graph。
func Compose(doc Document, fonts FontCatalog, assets Assets) (Result, error) {
	if err := ValidateDocument(doc); err != nil {
		return Result{}, apperr.Validation(err.Error())
	}
	subjectBytes := assets[doc.SubjectAssetID]
	if len(subjectBytes) == 0 {
		return Result{}, apperr.Validation("主体资产字节缺失")
	}
	lineage, err := BuildLineage(doc, subjectBytes)
	if err != nil {
		return Result{}, err
	}

	canvas := image.NewRGBA(image.Rect(0, 0, doc.Width, doc.Height))
	if bg, ok := parseColor(doc.Background); ok {
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	}

	plan := LayoutPlan{
		SchemaVersion: SchemaVersion,
		Width:         doc.Width,
		Height:        doc.Height,
		SafeArea:      doc.SafeArea,
	}
	var missing []string
	faces := map[string]font.Face{}

	for _, layer := range sortedLayers(doc.Layers) {
		placed := PlacedLayer{
			ID:     layer.ID,
			Type:   layer.Type,
			X:      layer.X,
			Y:      layer.Y,
			Width:  layer.Width,
			Height: layer.Height,
			ZIndex: layer.ZIndex,
		}
		placed.OutsideSafe = outsideSafe(doc, layer.X, layer.Y, layer.Width, layer.Height)

		switch layer.Type {
		case LayerImage:
			placed.AssetID = layer.AssetID
			raw := assets[layer.AssetID]
			if len(raw) == 0 {
				return Result{}, apperr.Validationf("图层 %q 资产字节缺失", layer.ID)
			}
			img, err := decodeImage(raw)
			if err != nil {
				return Result{}, apperr.Validationf("图层 %q 无法解码: %v", layer.ID, err)
			}
			dst := image.Rect(layer.X, layer.Y, layer.X+layer.Width, layer.Y+layer.Height)
			xdraw.CatmullRom.Scale(canvas, dst, img, img.Bounds(), draw.Over, nil)

		case LayerShape:
			fill, ok := parseColor(layer.Fill)
			if !ok {
				fill = color.RGBA{A: 255}
			}
			rect := image.Rect(layer.X, layer.Y, layer.X+layer.Width, layer.Y+layer.Height)
			draw.Draw(canvas, rect, &image.Uniform{C: fill}, image.Point{}, draw.Over)

		case LayerText:
			placed.Text = layer.Text
			placed.FontID = layer.FontID
			placed.FontSize = layer.FontSize
			placed.Color = layer.Color
			placed.Align = resolveAlign(layer.Align)
			placed.VAlign = resolveVAlign(layer.VAlign)
			face, err := loadFace(faces, fonts, layer.FontID, layer.FontSize)
			if err != nil {
				return Result{}, apperr.Validationf("图层 %q 字体: %v", layer.ID, err)
			}
			col, ok := parseColor(layer.Color)
			if !ok {
				col = color.RGBA{A: 255}
			}
			lines, miss, baseline := layoutText(face, layer)
			placed.Lines = lines
			placed.BaselineY = baseline
			placed.MissingRunes = miss
			missing = append(missing, miss...)
			for _, line := range lines {
				drawer := &font.Drawer{
					Dst:  canvas,
					Src:  &image.Uniform{C: col},
					Face: face,
					Dot:  fixed.P(line.X, line.Y),
				}
				drawer.DrawString(line.Text)
			}
		}
		plan.Layers = append(plan.Layers, placed)
	}

	qualified, items, glyphs := collectUnresolved(plan, missing)
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return Result{}, apperr.Internal("排版 PNG 编码失败")
	}
	pngBytes := buf.Bytes()
	return Result{
		PNG:             pngBytes,
		PNGSHA256:       ContentSHA256(pngBytes),
		Plan:            plan,
		Lineage:         lineage,
		LayoutQualified: qualified,
		UnresolvedItems: items,
		MissingGlyphs:   glyphs,
	}, nil
}

func loadFace(cache map[string]font.Face, fonts FontCatalog, fontID string, size float64) (font.Face, error) {
	key := fmt.Sprintf("%s@%.3f", fontID, size)
	if face, ok := cache[key]; ok {
		return face, nil
	}
	raw := fonts[fontID]
	if len(raw) == 0 {
		return nil, fmt.Errorf("未知 font_id %q", fontID)
	}
	ft, err := opentype.Parse(raw)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		return nil, err
	}
	cache[key] = face
	return face, nil
}

func layoutText(face font.Face, layer Layer) (lines []PlanLine, missing []string, baselineY int) {
	text := layer.Text
	metrics := face.Metrics()
	lineHeight := metrics.Height.Ceil()
	if lineHeight < 1 {
		lineHeight = int(layer.FontSize)
	}
	ascent := metrics.Ascent.Ceil()

	type measured struct {
		text  string
		width int
	}
	var wrapped []measured
	remaining := text
	for remaining != "" {
		line, rest, miss := wrapLine(face, remaining, layer.Width)
		missing = append(missing, miss...)
		wrapped = append(wrapped, measured{text: line, width: measureString(face, line)})
		remaining = rest
	}
	if len(wrapped) == 0 {
		wrapped = []measured{{text: "", width: 0}}
	}

	totalH := lineHeight * len(wrapped)
	startY := layer.Y + ascent
	switch resolveVAlign(layer.VAlign) {
	case VAlignMiddle:
		startY = layer.Y + (layer.Height-totalH)/2 + ascent
	case VAlignBottom:
		startY = layer.Y + layer.Height - totalH + ascent
	}
	baselineY = startY

	for i, m := range wrapped {
		x := layer.X
		switch resolveAlign(layer.Align) {
		case AlignCenter:
			x = layer.X + (layer.Width-m.width)/2
		case AlignRight:
			x = layer.X + layer.Width - m.width
		}
		y := startY + i*lineHeight
		lines = append(lines, PlanLine{Text: m.text, X: x, Y: y, Width: m.width})
	}
	return lines, uniqStrings(missing), baselineY
}

func wrapLine(face font.Face, text string, maxWidth int) (line, rest string, missing []string) {
	if maxWidth <= 0 {
		return text, "", nil
	}
	var b strings.Builder
	width := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == '\n' {
			return b.String(), text[i+size:], missing
		}
		adv, ok := face.GlyphAdvance(r)
		if !ok {
			missing = append(missing, string(r))
			// 缺字仍占位推进，避免静默空白当合格（合格位另标）
			adv = fixed.I(int(face.Metrics().Height.Ceil() / 2))
			if adv < fixed.I(1) {
				adv = fixed.I(8)
			}
		}
		w := adv.Ceil()
		if b.Len() > 0 && width+w > maxWidth {
			return b.String(), text[i:], missing
		}
		b.WriteRune(r)
		width += w
		i += size
	}
	return b.String(), "", missing
}

func measureString(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

func decodeImage(raw []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	return img, err
}

func parseColor(raw string) (color.RGBA, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return color.RGBA{}, false
	}
	if s[0] == '#' {
		s = s[1:]
	}
	var r, g, b uint8
	switch len(s) {
	case 6:
		var v uint32
		if _, err := fmt.Sscanf(s, "%06x", &v); err != nil {
			return color.RGBA{}, false
		}
		r = uint8(v >> 16)
		g = uint8(v >> 8)
		b = uint8(v)
	default:
		return color.RGBA{}, false
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}, true
}

func uniqStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
