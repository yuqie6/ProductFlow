package subjectextract

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"
	"strings"

	xdraw "golang.org/x/image/draw"
)

const (
	composeSchemaVersion = 1
	composeEngineFitSafe = "cutout_bg_shadow_fit_safe"
	composeLineageKind   = "subject_compose"

	defaultCanvas = 800
	defaultInset  = 48
)

// Insets 是安全区边距（画布内侧像素）。
type Insets struct {
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
	Left   int `json:"left"`
}

// BackgroundSpec 可控背景：纯色或垂直双色渐变。
type BackgroundSpec struct {
	Kind   string `json:"kind"`             // solid|gradient_v
	Color  string `json:"color,omitempty"`  // #RRGGBB（solid）
	Top    string `json:"top,omitempty"`    // #RRGGBB（gradient_v）
	Bottom string `json:"bottom,omitempty"` // #RRGGBB（gradient_v）
}

// ComposeSpec 是 B1 最小合成输入（画布、安全区、背景、接触阴影占位）。
type ComposeSpec struct {
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	SafeArea      Insets         `json:"safe_area"`
	Background    BackgroundSpec `json:"background"`
	ContactShadow bool           `json:"contact_shadow"`
}

// ComposePlacement 是主体在画布上的最终矩形。
type ComposePlacement struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// ComposeLineage 供调用方写 ProductImageAsset parent 链；本包不写库。
type ComposeLineage struct {
	Kind                 string `json:"kind"`
	CutoutContentSHA256  string `json:"cutout_content_sha256"`
	ComposeContentSHA256 string `json:"compose_content_sha256,omitempty"`
	SourceContentSHA256  string `json:"source_content_sha256,omitempty"`
}

// ComposeResult 是一次主体合成产物与质检结论。
type ComposeResult struct {
	SchemaVersion   int
	Engine          string
	Pass            bool
	Width           int
	Height          int
	PNG             []byte
	PNGSHA256       string
	CutoutSHA256    string
	Placement       ComposePlacement
	ContactShadow   bool
	BackgroundKind  string
	UnresolvedItems []string
	Detail          string
	Lineage         ComposeLineage
}

// DefaultComposeSpec 棚拍白底 + 安全区 + 接触阴影占位。
func DefaultComposeSpec() ComposeSpec {
	return ComposeSpec{
		Width:  defaultCanvas,
		Height: defaultCanvas,
		SafeArea: Insets{
			Top: defaultInset, Right: defaultInset,
			Bottom: defaultInset, Left: defaultInset,
		},
		Background: BackgroundSpec{
			Kind:  "solid",
			Color: "#F7F7F7",
		},
		ContactShadow: true,
	}
}

// ComposeFromExtract 将通过质检的抠图放入可控背景与安全区；可选接触阴影占位。
// 提取未通过或无 cutout → Pass=false。不宣称像素保真。
func ComposeFromExtract(extract Result, spec ComposeSpec) ComposeResult {
	base := ComposeResult{
		SchemaVersion:  composeSchemaVersion,
		Engine:         composeEngineFitSafe,
		CutoutSHA256:   extract.CutoutSHA256,
		ContactShadow:  spec.ContactShadow,
		BackgroundKind: strings.TrimSpace(spec.Background.Kind),
		Lineage: ComposeLineage{
			Kind:                composeLineageKind,
			CutoutContentSHA256: extract.CutoutSHA256,
			SourceContentSHA256: extract.SourceSHA256,
		},
	}
	if !extract.Pass {
		return composeFail(base, "主体提取未通过，无法合成")
	}
	if len(extract.CutoutPNG) == 0 || extract.CutoutSHA256 == "" {
		return composeFail(base, "缺少可合成的抠图字节")
	}
	if err := validateComposeSpec(spec); err != nil {
		return composeFail(base, err.Error())
	}
	base.Width, base.Height = spec.Width, spec.Height
	if base.BackgroundKind == "" {
		base.BackgroundKind = "solid"
	}

	cutoutImg, err := png.Decode(bytes.NewReader(extract.CutoutPNG))
	if err != nil {
		return composeFail(base, "抠图 PNG 无法解码")
	}
	subject, err := cropOpaqueSubject(cutoutImg, extract.Bounds)
	if err != nil {
		return composeFail(base, err.Error())
	}
	sb := subject.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw < 1 || sh < 1 {
		return composeFail(base, "主体裁切为空")
	}

	safe := safeRect(spec)
	place, ok := fitCentered(sw, sh, safe)
	if !ok {
		return composeFail(base, "安全区无法容纳主体")
	}
	base.Placement = place

	canvas := image.NewRGBA(image.Rect(0, 0, spec.Width, spec.Height))
	if err := fillBackground(canvas, spec.Background); err != nil {
		return composeFail(base, err.Error())
	}
	if spec.ContactShadow {
		drawContactShadow(canvas, place)
	}
	dst := image.Rect(place.X, place.Y, place.X+place.W, place.Y+place.H)
	xdraw.CatmullRom.Scale(canvas, dst, subject, sb, draw.Over, nil)

	var unresolved []string
	if outsideSafe(spec, place) {
		unresolved = append(unresolved, "主体放置超出安全区")
	}
	if !subjectHasOpaquePixels(canvas, place) {
		unresolved = append(unresolved, "合成后安全区内未检测到不透明主体像素")
	}

	pngBytes, err := encodeRGBAPNG(canvas)
	if err != nil {
		return composeFail(base, "合成 PNG 编码失败")
	}
	hash := contentSHA256(pngBytes)
	base.PNG = pngBytes
	base.PNGSHA256 = hash
	base.Lineage.ComposeContentSHA256 = hash

	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		base.Pass = false
		base.UnresolvedItems = unresolved
		base.Detail = "主体合成质检未通过"
		return base
	}
	base.Pass = true
	base.Detail = fmt.Sprintf(
		"%s bg=%s shadow=%v place=%dx%d@%d,%d",
		composeEngineFitSafe, base.BackgroundKind, spec.ContactShadow,
		place.W, place.H, place.X, place.Y,
	)
	return base
}

func composeFail(base ComposeResult, reason string) ComposeResult {
	base.Pass = false
	base.Detail = reason
	base.UnresolvedItems = []string{reason}
	return base
}

func validateComposeSpec(spec ComposeSpec) error {
	if spec.Width <= 0 || spec.Height <= 0 {
		return fmt.Errorf("合成画布宽高须为正")
	}
	if spec.Width > 8192 || spec.Height > 8192 {
		return fmt.Errorf("合成画布边长不得超过 8192")
	}
	sa := spec.SafeArea
	if sa.Top < 0 || sa.Right < 0 || sa.Bottom < 0 || sa.Left < 0 {
		return fmt.Errorf("安全区不得为负")
	}
	if sa.Left+sa.Right >= spec.Width || sa.Top+sa.Bottom >= spec.Height {
		return fmt.Errorf("安全区过大")
	}
	bg := spec.Background
	kind := strings.TrimSpace(bg.Kind)
	if kind == "" {
		kind = "solid"
	}
	switch kind {
	case "solid":
		if _, ok := parseHexColor(bg.Color); !ok {
			return fmt.Errorf("solid 背景须为 #RRGGBB")
		}
	case "gradient_v":
		if _, ok := parseHexColor(bg.Top); !ok {
			return fmt.Errorf("gradient_v top 须为 #RRGGBB")
		}
		if _, ok := parseHexColor(bg.Bottom); !ok {
			return fmt.Errorf("gradient_v bottom 须为 #RRGGBB")
		}
	default:
		return fmt.Errorf("背景 kind 非法: %q", kind)
	}
	return nil
}

func safeRect(spec ComposeSpec) image.Rectangle {
	sa := spec.SafeArea
	return image.Rect(sa.Left, sa.Top, spec.Width-sa.Right, spec.Height-sa.Bottom)
}

func fitCentered(sw, sh int, safe image.Rectangle) (ComposePlacement, bool) {
	aw, ah := safe.Dx(), safe.Dy()
	if aw < 1 || ah < 1 || sw < 1 || sh < 1 {
		return ComposePlacement{}, false
	}
	scale := math.Min(float64(aw)/float64(sw), float64(ah)/float64(sh))
	if scale <= 0 {
		return ComposePlacement{}, false
	}
	w := int(math.Floor(float64(sw) * scale))
	h := int(math.Floor(float64(sh) * scale))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w > aw {
		w = aw
	}
	if h > ah {
		h = ah
	}
	x := safe.Min.X + (aw-w)/2
	y := safe.Min.Y + (ah-h)/2
	return ComposePlacement{X: x, Y: y, W: w, H: h}, true
}

func outsideSafe(spec ComposeSpec, p ComposePlacement) bool {
	sa := spec.SafeArea
	left, top := sa.Left, sa.Top
	right, bottom := spec.Width-sa.Right, spec.Height-sa.Bottom
	return p.X < left || p.Y < top || p.X+p.W > right || p.Y+p.H > bottom
}

func cropOpaqueSubject(cutout image.Image, bounds Bounds) (image.Image, error) {
	b := cutout.Bounds()
	crop := b
	if bounds.W > 0 && bounds.H > 0 {
		crop = image.Rect(
			b.Min.X+bounds.X,
			b.Min.Y+bounds.Y,
			b.Min.X+bounds.X+bounds.W,
			b.Min.Y+bounds.Y+bounds.H,
		)
		crop = crop.Intersect(b)
	}
	if crop.Empty() {
		return nil, fmt.Errorf("主体外接矩形无效")
	}
	out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(out, out.Bounds(), cutout, crop.Min, draw.Src)
	return out, nil
}

func fillBackground(dst *image.RGBA, bg BackgroundSpec) error {
	kind := strings.TrimSpace(bg.Kind)
	if kind == "" {
		kind = "solid"
	}
	b := dst.Bounds()
	switch kind {
	case "solid":
		c, ok := parseHexColor(bg.Color)
		if !ok {
			return fmt.Errorf("solid 背景色无效")
		}
		draw.Draw(dst, b, &image.Uniform{C: c}, image.Point{}, draw.Src)
	case "gradient_v":
		top, ok1 := parseHexColor(bg.Top)
		bottom, ok2 := parseHexColor(bg.Bottom)
		if !ok1 || !ok2 {
			return fmt.Errorf("渐变背景色无效")
		}
		h := b.Dy()
		if h < 1 {
			return fmt.Errorf("画布高度无效")
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			t := float64(y-b.Min.Y) / float64(h-1)
			if h == 1 {
				t = 0
			}
			c := lerpRGBA(top, bottom, t)
			for x := b.Min.X; x < b.Max.X; x++ {
				dst.SetRGBA(x, y, c)
			}
		}
	default:
		return fmt.Errorf("背景 kind 非法: %q", kind)
	}
	return nil
}

func drawContactShadow(dst *image.RGBA, place ComposePlacement) {
	// 接触阴影占位：主体底部椭圆半透明暗斑，非物理准确阴影。
	cx := float64(place.X) + float64(place.W)/2
	cy := float64(place.Y+place.H) - float64(place.H)*0.06
	rx := float64(place.W) * 0.38
	ry := float64(place.H) * 0.08
	if rx < 2 {
		rx = 2
	}
	if ry < 1 {
		ry = 1
	}
	minX := int(math.Floor(cx - rx - 1))
	maxX := int(math.Ceil(cx + rx + 1))
	minY := int(math.Floor(cy - ry - 1))
	maxY := int(math.Ceil(cy + ry + 1))
	b := dst.Bounds()
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
				continue
			}
			dx := (float64(x) - cx) / rx
			dy := (float64(y) - cy) / ry
			d := dx*dx + dy*dy
			if d > 1 {
				continue
			}
			alpha := uint8(90 * (1 - d))
			if alpha == 0 {
				continue
			}
			overRGBA(dst, x, y, color.RGBA{0, 0, 0, alpha})
		}
	}
}

func overRGBA(dst *image.RGBA, x, y int, src color.RGBA) {
	i := dst.PixOffset(x, y)
	dr, dg, db, da := dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3]
	a := float64(src.A) / 255
	inv := 1 - a
	dst.Pix[i] = uint8(float64(src.R)*a + float64(dr)*inv)
	dst.Pix[i+1] = uint8(float64(src.G)*a + float64(dg)*inv)
	dst.Pix[i+2] = uint8(float64(src.B)*a + float64(db)*inv)
	outA := src.A + uint8(float64(da)*inv)
	dst.Pix[i+3] = outA
}

func subjectHasOpaquePixels(img *image.RGBA, place ComposePlacement) bool {
	for y := place.Y; y < place.Y+place.H; y++ {
		for x := place.X; x < place.X+place.W; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0xC000 {
				r, g, b, _ := img.At(x, y).RGBA()
				// 排除近白背景与纯黑阴影：要求有色主体。
				if r>>8 < 230 || g>>8 < 230 || b>>8 < 230 {
					if r>>8 > 8 || g>>8 > 8 || b>>8 > 8 {
						return true
					}
				}
			}
		}
	}
	return false
}

func lerpRGBA(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 255,
	}
}

func parseHexColor(raw string) (color.RGBA, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return color.RGBA{}, false
	}
	if s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	var v uint32
	if _, err := fmt.Sscanf(s, "%06x", &v); err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: uint8(v >> 16),
		G: uint8(v >> 8),
		B: uint8(v),
		A: 255,
	}, true
}

func encodeRGBAPNG(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
