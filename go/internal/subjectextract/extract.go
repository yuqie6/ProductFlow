package subjectextract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
)

const (
	schemaVersion = 1
	engineCorner  = "corner_chroma_local"

	// 主体像素占比闸：过小像噪点，过大像整图当主体。
	minCoverage = 0.03
	maxCoverage = 0.92

	cornerSample = 6
	chromaThresh = 42.0 // RGB 欧氏距离；棚拍白底/灰底可分
)

// Result 是一次主体提取产物与质检结论。
type Result struct {
	SchemaVersion   int
	Engine          string
	Pass            bool
	Width           int
	Height          int
	MaskPNG         []byte
	CutoutPNG       []byte
	MaskSHA256      string
	CutoutSHA256    string
	SourceSHA256    string
	Coverage        float64
	Bounds          Bounds
	UnresolvedItems []string
	Detail          string
	Lineage         Lineage
}

// Bounds 是主体外接矩形（像素，含右下开区间宽高）。
type Bounds struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Lineage 供调用方写 ProductImageAsset parent 链；本包不写库。
type Lineage struct {
	Kind                string `json:"kind"`
	SourceContentSHA256 string `json:"source_content_sha256"`
	MaskContentSHA256   string `json:"mask_content_sha256,omitempty"`
	CutoutContentSHA256 string `json:"cutout_content_sha256,omitempty"`
}

// ExtractPNG 对参考图做真实像素分割；禁止旁路假蒙版。解码失败或质检失败 → Pass=false。
func ExtractPNG(pngBytes []byte) Result {
	srcHash := contentSHA256(pngBytes)
	base := Result{
		SchemaVersion: schemaVersion,
		Engine:        engineCorner,
		SourceSHA256:  srcHash,
		Lineage: Lineage{
			Kind:                "subject_extract",
			SourceContentSHA256: srcHash,
		},
	}
	if len(pngBytes) == 0 {
		return fail(base, "参考图为空，无法主体提取")
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return fail(base, "参考图不是可读取的 PNG")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 8 || h < 8 {
		return fail(base, fmt.Sprintf("参考图过小（%dx%d）", w, h))
	}
	base.Width, base.Height = w, h

	bg := estimateBackground(img)
	mask := image.NewGray(image.Rect(0, 0, w, h))
	fgCount := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a < 0x8000 {
				mask.SetGray(x, y, color.Gray{Y: 0})
				continue
			}
			cr, cg, cb := float64(r>>8), float64(g>>8), float64(bl>>8)
			dist := math.Hypot(math.Hypot(cr-bg[0], cg-bg[1]), cb-bg[2])
			if dist >= chromaThresh {
				mask.SetGray(x, y, color.Gray{Y: 255})
				fgCount++
			} else {
				mask.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	if fgCount == 0 {
		return fail(base, "未检测到与背景可分的主体像素")
	}

	keep, bounds, kept := largestComponent(mask)
	if kept == 0 {
		return fail(base, "主体连通域为空")
	}
	coverage := float64(kept) / float64(w*h)
	base.Coverage = coverage
	base.Bounds = bounds

	var unresolved []string
	if coverage < minCoverage {
		unresolved = append(unresolved, fmt.Sprintf("主体覆盖过低（%.3f < %.2f）", coverage, minCoverage))
	}
	if coverage > maxCoverage {
		unresolved = append(unresolved, fmt.Sprintf("主体覆盖过高（%.3f > %.2f），疑似背景未分离", coverage, maxCoverage))
	}
	if bounds.W <= 0 || bounds.H <= 0 {
		unresolved = append(unresolved, "主体外接矩形无效")
	}

	maskPNG, err := encodeGrayPNG(keep)
	if err != nil {
		return fail(base, "蒙版编码失败")
	}
	cutoutPNG, err := encodeCutoutPNG(img, keep)
	if err != nil {
		return fail(base, "抠图编码失败")
	}
	maskHash := contentSHA256(maskPNG)
	cutoutHash := contentSHA256(cutoutPNG)
	base.MaskPNG = maskPNG
	base.CutoutPNG = cutoutPNG
	base.MaskSHA256 = maskHash
	base.CutoutSHA256 = cutoutHash
	base.Lineage.MaskContentSHA256 = maskHash
	base.Lineage.CutoutContentSHA256 = cutoutHash

	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		base.Pass = false
		base.UnresolvedItems = unresolved
		base.Detail = "主体提取质检未通过"
		return base
	}
	base.Pass = true
	base.Detail = fmt.Sprintf("corner_chroma coverage=%.3f bounds=%dx%d@%d,%d", coverage, bounds.W, bounds.H, bounds.X, bounds.Y)
	return base
}

func fail(base Result, reason string) Result {
	base.Pass = false
	base.Detail = reason
	base.UnresolvedItems = []string{reason}
	return base
}

func estimateBackground(img image.Image) [3]float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var rs, gs, bs []float64
	sample := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				if a < 0x8000 {
					continue
				}
				rs = append(rs, float64(r>>8))
				gs = append(gs, float64(g>>8))
				bs = append(bs, float64(bl>>8))
			}
		}
	}
	cs := cornerSample
	if cs > w/2 {
		cs = w / 2
	}
	if cs > h/2 {
		cs = h / 2
	}
	if cs < 1 {
		cs = 1
	}
	sample(0, 0, cs, cs)
	sample(w-cs, 0, w, cs)
	sample(0, h-cs, cs, h)
	sample(w-cs, h-cs, w, h)
	return [3]float64{median(rs), median(gs), median(bs)}
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 255
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	return cp[len(cp)/2]
}

func largestComponent(mask *image.Gray) (*image.Gray, Bounds, int) {
	b := mask.Bounds()
	w, h := b.Dx(), b.Dy()
	visited := make([]bool, w*h)
	bestLabel := make([]bool, w*h)
	bestCount := 0
	bestBounds := Bounds{}

	idx := func(x, y int) int { return y*w + x }
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := idx(x, y)
			if visited[i] || mask.GrayAt(x, y).Y < 128 {
				continue
			}
			stack := [][2]int{{x, y}}
			visited[i] = true
			comp := make([]bool, w*h)
			count := 0
			minX, minY, maxX, maxY := x, y, x, y
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				px, py := p[0], p[1]
				comp[idx(px, py)] = true
				count++
				if px < minX {
					minX = px
				}
				if py < minY {
					minY = py
				}
				if px > maxX {
					maxX = px
				}
				if py > maxY {
					maxY = py
				}
				for _, d := range dirs {
					nx, ny := px+d[0], py+d[1]
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					ni := idx(nx, ny)
					if visited[ni] || mask.GrayAt(nx, ny).Y < 128 {
						continue
					}
					visited[ni] = true
					stack = append(stack, [2]int{nx, ny})
				}
			}
			if count > bestCount {
				bestCount = count
				bestLabel = comp
				bestBounds = Bounds{X: minX, Y: minY, W: maxX - minX + 1, H: maxY - minY + 1}
			}
		}
	}

	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if bestLabel[idx(x, y)] {
				out.SetGray(x, y, color.Gray{Y: 255})
			} else {
				out.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	return out, bestBounds, bestCount
}

func encodeGrayPNG(img *image.Gray) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeCutoutPNG(src image.Image, mask *image.Gray) ([]byte, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if mask.GrayAt(x, y).Y < 128 {
				out.SetRGBA(x, y, color.RGBA{0, 0, 0, 0})
				continue
			}
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(bl >> 8),
				A: uint8(a >> 8),
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func contentSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
