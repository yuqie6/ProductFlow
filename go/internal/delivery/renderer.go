package delivery

import (
	"bytes"
	"fmt"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
)

type Rendered struct {
	Bytes    []byte
	Metadata media.Verified
}

func Render(source []byte, spec Spec) (Rendered, error) {
	if len(source) == 0 {
		return Rendered{}, apperr.Validation("交付派生原图内容为空")
	}
	img, err := decodeSource(source)
	if err != nil {
		return Rendered{}, err
	}
	resized := resize(img, spec)
	encoded, err := encodeImage(resized, spec)
	if err != nil {
		return Rendered{}, err
	}
	expectedMIME := formatMIME[spec.Format]
	meta, err := media.Inspect(encoded, expectedMIME)
	if err != nil {
		return Rendered{}, apperr.Validation("交付图编码后的实际尺寸不符合 DeliverySpec")
	}
	if meta.Width != spec.Width || meta.Height != spec.Height {
		return Rendered{}, apperr.Validation("交付图编码后的实际尺寸不符合 DeliverySpec")
	}
	if spec.MaxByteSize != nil && meta.ByteSize > *spec.MaxByteSize {
		return Rendered{}, apperr.Validation("交付图无法在保持尺寸和格式的前提下满足最大字节限制")
	}
	return Rendered{Bytes: encoded, Metadata: meta}, nil
}

func decodeSource(source []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, apperr.Validation("交付派生原图不是可解码图片")
	}
	b := img.Bounds()
	if int64(b.Dx())*int64(b.Dy()) > int64(maxTotalPixels)*4 {
		return nil, apperr.Validation("交付派生原图像素规模过大")
	}
	return img, nil
}

func resize(src image.Image, spec Spec) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	tw, th := spec.Width, spec.Height
	if spec.Fit == "cover" {
		return fitCover(src, tw, th, spec.CropAnchor)
	}
	scale := minFloat(float64(tw)/float64(sw), float64(th)/float64(sh))
	nw := maxInt(1, int(float64(sw)*scale))
	nh := maxInt(1, int(float64(sh)*scale))
	contained := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(contained, contained.Bounds(), src, sb, xdraw.Over, nil)

	var canvas *image.RGBA
	if spec.BackgroundColor != nil {
		r, g, b, ok := parseRGB(*spec.BackgroundColor)
		if !ok {
			r, g, b = 255, 255, 255
		}
		canvas = image.NewRGBA(image.Rect(0, 0, tw, th))
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{r, g, b, 255}}, image.Point{}, draw.Src)
	} else if spec.Format == "jpeg" {
		canvas = image.NewRGBA(image.Rect(0, 0, tw, th))
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	} else {
		canvas = image.NewRGBA(image.Rect(0, 0, tw, th))
	}
	offX := (tw - nw) / 2
	offY := (th - nh) / 2
	draw.Draw(canvas, image.Rect(offX, offY, offX+nw, offY+nh), contained, image.Point{}, draw.Over)
	return canvas
}

func fitCover(src image.Image, tw, th int, anchor *string) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	scale := maxFloat(float64(tw)/float64(sw), float64(th)/float64(sh))
	nw := maxInt(1, int(float64(sw)*scale+0.5))
	nh := maxInt(1, int(float64(sh)*scale+0.5))
	scaled := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, xdraw.Over, nil)
	cx, cy := 0.5, 0.5
	if anchor != nil {
		switch *anchor {
		case "top":
			cy = 0
		case "bottom":
			cy = 1
		case "left":
			cx = 0
		case "right":
			cx = 1
		}
	}
	extraW := nw - tw
	extraH := nh - th
	ox := int(float64(extraW) * cx)
	oy := int(float64(extraH) * cy)
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}
	if ox+tw > nw {
		ox = nw - tw
	}
	if oy+th > nh {
		oy = nh - th
	}
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(out, out.Bounds(), scaled, image.Pt(ox, oy), draw.Src)
	return out
}

func encodeImage(img image.Image, spec Spec) ([]byte, error) {
	switch spec.Format {
	case "png":
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, apperr.Validation("当前运行环境不支持 PNG 交付编码")
		}
		if spec.MaxByteSize != nil && buf.Len() > *spec.MaxByteSize {
			return nil, apperr.Validation("PNG 交付图无法在不量化颜色的前提下满足最大字节限制")
		}
		return buf.Bytes(), nil
	case "jpeg":
		prepared := flattenJPEG(img)
		if spec.MaxByteSize == nil {
			return encodeJPEG(prepared, 95)
		}
		for _, q := range qualityLadder {
			encoded, err := encodeJPEG(prepared, q)
			if err != nil {
				return nil, err
			}
			if len(encoded) <= *spec.MaxByteSize {
				return encoded, nil
			}
		}
		return nil, apperr.Validation("交付图无法在保持尺寸和格式的前提下满足最大字节限制")
	case "webp":
		return nil, apperr.Validation("当前运行环境不支持 WEBP 交付编码")
	default:
		return nil, apperr.Validation(fmt.Sprintf("当前运行环境不支持 %s 交付编码", spec.Format))
	}
}

func flattenJPEG(img image.Image) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(out, b, img, b.Min, draw.Over)
	return out
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, apperr.Validation("当前运行环境不支持 JPEG 交付编码")
	}
	return buf.Bytes(), nil
}

func parseRGB(s string) (uint8, uint8, uint8, bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	var r, g, b int
	if _, err := fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b); err != nil {
		return 0, 0, 0, false
	}
	return uint8(r), uint8(g), uint8(b), true
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
