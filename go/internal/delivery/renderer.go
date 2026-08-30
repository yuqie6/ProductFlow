package delivery

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/HugoSmits86/nativewebp"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
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
	img, format, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, apperr.Validation("交付派生原图不是可解码图片")
	}
	if format == "jpeg" {
		img = exifTransposeJPEG(source, img)
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
		// nativewebp 只提供无损 VP8L，没有 Pillow 的 quality 阶；用 encoder effort 去贴 max_byte_size。
		if spec.MaxByteSize == nil {
			return encodeWebP(img, nativewebp.BestCompression)
		}
		for _, level := range []nativewebp.CompressionLevel{nativewebp.BestCompression, nativewebp.DefaultCompression, nativewebp.BestSpeed} {
			encoded, err := encodeWebP(img, level)
			if err != nil {
				return nil, err
			}
			if len(encoded) <= *spec.MaxByteSize {
				return encoded, nil
			}
		}
		return nil, apperr.Validation("交付图无法在保持尺寸和格式的前提下满足最大字节限制")
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

func encodeWebP(img image.Image, level nativewebp.CompressionLevel) ([]byte, error) {
	var buf bytes.Buffer
	if err := nativewebp.Encode(&buf, img, &nativewebp.Options{CompressionLevel: level}); err != nil {
		return nil, apperr.Validation("当前运行环境不支持 WEBP 交付编码")
	}
	return buf.Bytes(), nil
}

// exifTransposeJPEG 对齐 Pillow ImageOps.exif_transpose：只读 JPEG EXIF Orientation 并变换像素。
func exifTransposeJPEG(source []byte, img image.Image) image.Image {
	orientation, ok := jpegExifOrientation(source)
	if !ok {
		return img
	}
	switch orientation {
	case 2:
		return flipHorizontal(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipVertical(img)
	case 5:
		return transposeImage(img)
	case 6:
		return rotate90CW(img)
	case 7:
		return transverseImage(img)
	case 8:
		return rotate90CCW(img)
	default:
		return img
	}
}

func jpegExifOrientation(source []byte) (int, bool) {
	if len(source) < 4 || source[0] != 0xff || source[1] != 0xd8 {
		return 0, false
	}
	offset := 2
	for offset+4 <= len(source) {
		if source[offset] != 0xff {
			return 0, false
		}
		marker := source[offset+1]
		offset += 2
		if marker == 0xda || marker == 0xd9 {
			return 0, false
		}
		if marker == 0xd0 || marker == 0xd1 || marker == 0xd2 || marker == 0xd3 ||
			marker == 0xd4 || marker == 0xd5 || marker == 0xd6 || marker == 0xd7 ||
			marker == 0x01 {
			continue
		}
		if offset+2 > len(source) {
			return 0, false
		}
		length := int(binary.BigEndian.Uint16(source[offset:]))
		if length < 2 || offset+length > len(source) {
			return 0, false
		}
		payload := source[offset+2 : offset+length]
		offset += length
		if marker != 0xe1 {
			continue
		}
		if orientation, ok := tiffOrientation(payload); ok {
			return orientation, true
		}
	}
	return 0, false
}

func tiffOrientation(app1 []byte) (int, bool) {
	const prefix = "Exif\x00\x00"
	if !bytes.HasPrefix(app1, []byte(prefix)) {
		return 0, false
	}
	tiff := app1[len(prefix):]
	if len(tiff) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(tiff, []byte("II")):
		order = binary.LittleEndian
	case bytes.HasPrefix(tiff, []byte("MM")):
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0, false
	}
	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 0 || ifd+2 > len(tiff) {
		return 0, false
	}
	count := int(order.Uint16(tiff[ifd : ifd+2]))
	entryStart := ifd + 2
	for i := 0; i < count; i++ {
		start := entryStart + i*12
		if start+12 > len(tiff) {
			return 0, false
		}
		tag := order.Uint16(tiff[start : start+2])
		if tag != 0x0112 {
			continue
		}
		typ := order.Uint16(tiff[start+2 : start+4])
		n := order.Uint32(tiff[start+4 : start+8])
		if n != 1 {
			return 0, false
		}
		value := tiff[start+8 : start+12]
		switch typ {
		case 3:
			return int(order.Uint16(value[:2])), true
		case 4:
			return int(order.Uint32(value)), true
		default:
			return 0, false
		}
	}
	return 0, false
}

func rotate90CW(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func rotate90CCW(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func flipHorizontal(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(w-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func flipVertical(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func transposeImage(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func transverseImage(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(h-1-y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
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
