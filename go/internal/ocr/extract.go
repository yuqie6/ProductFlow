package ocr

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	defaultSchemaVersion = 1
	engineGlyphTemplate  = "glyph_template_liberation_sans"
	engineLiveVision     = "live_vision"
	liveEnvKey           = "PRODUCTFLOW_OCR_LIVE"
)

// Extractor 从成片 PNG 字节提取可见文字（真实像素/视觉识别，禁止旁路假 OCR）。
type Extractor interface {
	Extract(ctx context.Context, pngBytes []byte) (ExtractResult, error)
}

// ExtractResult 是一次提取产物。
type ExtractResult struct {
	Text   string
	Engine string
}

// GlyphExtractor 用嵌入 Liberation Sans 字形模板做高对比成片 OCR。
// 适合受控排版夹具与近似该字体的拉丁/数字文案；不宣称对任意生成式成片全覆盖。
type GlyphExtractor struct {
	FontBytes []byte
	Sizes     []float64
}

var (
	embeddedFontOnce sync.Once
	embeddedFont     []byte
	embeddedFontErr  error
)

// DefaultExtractor 返回 B0 默认提取器；PRODUCTFLOW_OCR_LIVE=1 时优先 live 视觉。
func DefaultExtractor() Extractor {
	if liveEnabled() {
		return LiveVisionExtractor{}
	}
	return MustGlyphExtractor()
}

func liveEnabled() bool {
	v := strings.TrimSpace(os.Getenv(liveEnvKey))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// MustGlyphExtractor 加载嵌入字体；失败时 panic（包初始化路径仅测试/进程启动使用 Default）。
func MustGlyphExtractor() *GlyphExtractor {
	ex, err := NewGlyphExtractor(nil)
	if err != nil {
		panic(err)
	}
	return ex
}

// NewGlyphExtractor 使用给定 TTF 字节；nil 则用嵌入 Liberation Sans。
func NewGlyphExtractor(fontBytes []byte) (*GlyphExtractor, error) {
	if len(fontBytes) == 0 {
		b, err := loadEmbeddedFont()
		if err != nil {
			return nil, err
		}
		fontBytes = b
	}
	sizes := []float64{18, 22, 24, 28, 32, 36, 42, 48}
	return &GlyphExtractor{FontBytes: fontBytes, Sizes: sizes}, nil
}

func loadEmbeddedFont() ([]byte, error) {
	embeddedFontOnce.Do(func() {
		embeddedFont, embeddedFontErr = liberationSansTTF()
	})
	return embeddedFont, embeddedFontErr
}

// Extract 识别 PNG 中的可见拉丁/数字文字。
func (e *GlyphExtractor) Extract(ctx context.Context, pngBytes []byte) (ExtractResult, error) {
	if err := ctx.Err(); err != nil {
		return ExtractResult{}, err
	}
	gray, err := decodeInkMask(pngBytes)
	if err != nil {
		return ExtractResult{}, err
	}
	text := e.recognize(gray)
	return ExtractResult{Text: text, Engine: engineGlyphTemplate}, nil
}

// Contains 判断 needle 是否作为可见子串出现在成片中（多字号模板搜索）。
func (e *GlyphExtractor) Contains(pngBytes []byte, needle string) (bool, error) {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return true, nil
	}
	gray, err := decodeInkMask(pngBytes)
	if err != nil {
		return false, err
	}
	for _, size := range e.Sizes {
		tmpl, err := e.renderInkMask(needle, size)
		if err != nil {
			return false, err
		}
		if templatePresent(gray, tmpl) {
			return true, nil
		}
	}
	return false, nil
}

func decodeInkMask(pngBytes []byte) (*image.Gray, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("ocr: 无效 PNG: %w", err)
	}
	b := img.Bounds()
	gray := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				gray.SetGray(x, y, color.Gray{Y: 255})
				continue
			}
			// ITU-R BT.601 luma；深色笔画像素标为墨迹(0)。
			luma := (299*r + 587*g + 114*bl) / 1000
			if luma < 0x8000 {
				gray.SetGray(x, y, color.Gray{Y: 0})
			} else {
				gray.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return gray, nil
}

func (e *GlyphExtractor) renderInkMask(text string, size float64) (*image.Gray, error) {
	face, err := e.face(size)
	if err != nil {
		return nil, err
	}
	defer face.Close()
	advance := font.MeasureString(face, text).Ceil()
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	height := ascent + descent + 4
	if advance < 1 {
		advance = 1
	}
	if height < 1 {
		height = int(size) + 4
	}
	rgba := image.NewRGBA(image.Rect(0, 0, advance+4, height))
	draw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	d := &font.Drawer{
		Dst:  rgba,
		Src:  image.Black,
		Face: face,
		Dot:  fixed.P(2, ascent+2),
	}
	d.DrawString(text)
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return decodeInkMask(buf.Bytes())
}

func (e *GlyphExtractor) face(size float64) (font.Face, error) {
	ft, err := opentype.Parse(e.FontBytes)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingNone,
	})
}

func templatePresent(hay, needle *image.Gray) bool {
	_, ok := findTemplate(hay, needle)
	return ok
}

type templateLoc struct{ X, Y int }

func findTemplate(hay, needle *image.Gray) (templateLoc, bool) {
	hb := hay.Bounds()
	nb := needle.Bounds()
	nw, nh := nb.Dx(), nb.Dy()
	if nw == 0 || nh == 0 || nw > hb.Dx() || nh > hb.Dy() {
		return templateLoc{}, false
	}
	needleInk := countInk(needle)
	if needleInk == 0 {
		return templateLoc{}, false
	}
	minHit := int(float64(needleInk) * 0.88)
	maxExtra := int(float64(needleInk) * 0.35)
	if minHit < 1 {
		minHit = 1
	}
	// 只在墨迹包围盒内搜索，步长 1（受控夹具字号有限）。
	x0, y0, x1, y1 := inkBounds(hay)
	if x1-x0 < nw || y1-y0 < nh {
		x0, y0, x1, y1 = hb.Min.X, hb.Min.Y, hb.Max.X, hb.Max.Y
	}
	for y := y0; y+nh <= y1; y++ {
		for x := x0; x+nw <= x1; x++ {
			hit, extra := scoreTemplate(hay, needle, x, y)
			if hit >= minHit && extra <= maxExtra {
				return templateLoc{X: x, Y: y}, true
			}
		}
	}
	return templateLoc{}, false
}

func inkBounds(img *image.Gray) (x0, y0, x1, y1 int) {
	b := img.Bounds()
	x0, y0 = b.Max.X, b.Max.Y
	x1, y1 = b.Min.X, b.Min.Y
	found := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.GrayAt(x, y).Y < 128 {
				found = true
				if x < x0 {
					x0 = x
				}
				if y < y0 {
					y0 = y
				}
				if x+1 > x1 {
					x1 = x + 1
				}
				if y+1 > y1 {
					y1 = y + 1
				}
			}
		}
	}
	if !found {
		return b.Min.X, b.Min.Y, b.Max.X, b.Max.Y
	}
	// 扩边，避免贴边漏检
	pad := 4
	x0 -= pad
	y0 -= pad
	x1 += pad
	y1 += pad
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	return x0, y0, x1, y1
}

func bleachTemplate(hay, needle *image.Gray, loc templateLoc) {
	nb := needle.Bounds()
	for y := nb.Min.Y; y < nb.Max.Y; y++ {
		for x := nb.Min.X; x < nb.Max.X; x++ {
			if needle.GrayAt(x, y).Y >= 128 {
				continue
			}
			hx := loc.X + x - nb.Min.X
			hy := loc.Y + y - nb.Min.Y
			hay.SetGray(hx, hy, color.Gray{Y: 255})
		}
	}
}

func countInk(img *image.Gray) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.GrayAt(x, y).Y < 128 {
				n++
			}
		}
	}
	return n
}

func scoreTemplate(hay, needle *image.Gray, ox, oy int) (hit, extra int) {
	nb := needle.Bounds()
	for y := nb.Min.Y; y < nb.Max.Y; y++ {
		for x := nb.Min.X; x < nb.Max.X; x++ {
			nInk := needle.GrayAt(x, y).Y < 128
			hInk := hay.GrayAt(ox+x-nb.Min.X, oy+y-nb.Min.Y).Y < 128
			if nInk && hInk {
				hit++
			} else if !nInk && hInk {
				extra++
			}
		}
	}
	return hit, extra
}

// recognize 按行带做贪心字形匹配，拼出可见拉丁串。
func (e *GlyphExtractor) recognize(gray *image.Gray) string {
	bands := inkBands(gray)
	var parts []string
	charset := ocrCharset
	for _, band := range bands {
		best := ""
		bestScore := 0
		for _, size := range e.Sizes {
			line, score := e.recognizeBand(gray, band, charset, size)
			if score > bestScore && line != "" {
				bestScore = score
				best = line
			}
		}
		if best != "" {
			parts = append(parts, best)
		}
	}
	return strings.Join(parts, "\n")
}

type band struct {
	y0, y1 int
}

func inkBands(gray *image.Gray) []band {
	b := gray.Bounds()
	rowHas := make([]bool, b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if gray.GrayAt(x, y).Y < 128 {
				rowHas[y-b.Min.Y] = true
				break
			}
		}
	}
	var out []band
	i := 0
	for i < len(rowHas) {
		for i < len(rowHas) && !rowHas[i] {
			i++
		}
		if i >= len(rowHas) {
			break
		}
		start := i
		for i < len(rowHas) && rowHas[i] {
			i++
		}
		// 合并细缝
		for i < len(rowHas) {
			gap := 0
			j := i
			for j < len(rowHas) && !rowHas[j] && gap < 3 {
				gap++
				j++
			}
			if j < len(rowHas) && rowHas[j] && gap < 3 {
				i = j
				for i < len(rowHas) && rowHas[i] {
					i++
				}
				continue
			}
			break
		}
		out = append(out, band{y0: b.Min.Y + start, y1: b.Min.Y + i})
	}
	return out
}

const ocrCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ .,%-+/"

func (e *GlyphExtractor) recognizeBand(gray *image.Gray, bd band, charset string, size float64) (string, int) {
	face, err := e.face(size)
	if err != nil {
		return "", 0
	}
	defer face.Close()
	glyphs := map[rune]*image.Gray{}
	advances := map[rune]int{}
	for _, r := range charset {
		if r == ' ' {
			continue
		}
		g, err := e.renderInkMask(string(r), size)
		if err != nil {
			continue
		}
		glyphs[r] = g
		advances[r] = font.MeasureString(face, string(r)).Ceil()
		if advances[r] < 1 {
			advances[r] = g.Bounds().Dx()
		}
	}
	b := gray.Bounds()
	x := b.Min.X
	// 跳到本带首个墨迹列
	for x < b.Max.X && !columnHasInk(gray, x, bd.y0, bd.y1) {
		x++
	}
	var out strings.Builder
	score := 0
	spaceAdvance := font.MeasureString(face, " ").Ceil()
	if spaceAdvance < 4 {
		spaceAdvance = int(size / 3)
	}
	for x < b.Max.X {
		if !columnHasInk(gray, x, bd.y0, bd.y1) {
			// 空白：若已有字则可能是空格
			gapEnd := x
			for gapEnd < b.Max.X && !columnHasInk(gray, gapEnd, bd.y0, bd.y1) {
				gapEnd++
			}
			if out.Len() > 0 && gapEnd < b.Max.X && gapEnd-x >= spaceAdvance/2 {
				out.WriteByte(' ')
			}
			x = gapEnd
			continue
		}
		bestR := rune(0)
		bestHit := 0
		bestExtra := 1 << 30
		bestAdv := 0
		for r, tmpl := range glyphs {
			tw := tmpl.Bounds().Dx()
			th := tmpl.Bounds().Dy()
			if x+tw > b.Max.X {
				continue
			}
			// 垂直对齐：模板贴在带内顶部附近试几个偏移
			for dy := -2; dy <= 4; dy++ {
				oy := bd.y0 + dy
				if oy < b.Min.Y || oy+th > b.Max.Y {
					continue
				}
				hit, extra := scoreTemplate(gray, tmpl, x, oy)
				ink := countInk(tmpl)
				if ink == 0 {
					continue
				}
				if hit*100/ink >= 80 && hit >= bestHit && extra <= bestExtra {
					bestHit = hit
					bestExtra = extra
					bestR = r
					bestAdv = advances[r]
				}
			}
		}
		if bestR == 0 || bestAdv < 1 {
			x++
			continue
		}
		out.WriteRune(bestR)
		score += bestHit
		x += bestAdv
		// 允许字符间距 0–2px
		for skip := 0; skip < 3 && x < b.Max.X && !columnHasInk(gray, x, bd.y0, bd.y1); skip++ {
			x++
		}
	}
	return strings.TrimSpace(out.String()), score
}

func columnHasInk(gray *image.Gray, x, y0, y1 int) bool {
	b := gray.Bounds()
	if x < b.Min.X || x >= b.Max.X {
		return false
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	for y := y0; y < y1; y++ {
		if gray.GrayAt(x, y).Y < 128 {
			return true
		}
	}
	return false
}

func foldOCR(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\t", "")
	return s
}
