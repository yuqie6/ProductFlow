package delivery

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"

	// chai2010/webp 需要 CGO（镜像见 go/Dockerfile 的 CGO_ENABLED=1），才能按 Pillow 的 quality 阶做有损 WebP。
	chaiwebp "github.com/chai2010/webp"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Rendered 是本地渲染后的字节与 media 核验元数据，内部类型。
// 不是商品图身份；落盘由 Executor 写成新的 ProductImageAsset。
type Rendered struct {
	Bytes    []byte         // 按 Spec 编码后的交付图，不是源图
	Metadata media.Verified // 对 Bytes 再核验的 MIME/尺寸/哈希
}

// Render 按 DeliverySpec 缩放编码已有原图，不调用图像模型。
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
	img = exifTranspose(source, img)
	b := img.Bounds()
	if int64(b.Dx())*int64(b.Dy()) > int64(maxTotalPixels)*4 {
		return nil, apperr.Validation("交付派生原图像素规模过大")
	}
	return img, nil
}

// resize 按 fit 缩放。contain 居中铺到画布；jpeg 无背景时填白，避免透明通道进 JPEG。
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

// fitCover 放大后按 crop_anchor 裁切。anchor 为 nil 时从中心裁。
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

// encodeImage 按 format 编码。有 MaxByteSize 时 jpeg/webp 沿 quality 阶下降；仍超限返回 400，不改尺寸。
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
		// Pillow WEBP：无上限时 quality=95；有 max_byte_size 再按同一阶 95→1 降，而不是无损 VP8L 的 encoder effort。
		if spec.MaxByteSize == nil {
			return encodeWebP(img, 95)
		}
		for _, q := range qualityLadder {
			encoded, err := encodeWebP(img, q)
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
	if err := encodeJPEG444(&buf, img, quality); err != nil {
		return nil, apperr.Validation("当前运行环境不支持 JPEG 交付编码")
	}
	return buf.Bytes(), nil
}

func encodeWebP(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	// Exact 对齐 Pillow exact=True：透明像素下保留 RGB。有损路径仍走 quality 阶。
	if err := chaiwebp.Encode(&buf, img, &chaiwebp.Options{Quality: float32(quality), Exact: true}); err != nil {
		return nil, apperr.Validation("当前运行环境不支持 WEBP 交付编码")
	}
	return buf.Bytes(), nil
}

// exifTranspose 对齐 Pillow ImageOps.exif_transpose：读 JPEG APP1 / PNG eXIf / WebP EXIF。
func exifTranspose(source []byte, img image.Image) image.Image {
	orientation, ok := sourceExifOrientation(source)
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

func sourceExifOrientation(source []byte) (int, bool) {
	if orientation, ok := jpegExifOrientation(source); ok {
		return orientation, true
	}
	if orientation, ok := pngExifOrientation(source); ok {
		return orientation, true
	}
	return webpExifOrientation(source)
}

// jpegExifOrientation 扫 APP1 段取 TIFF Orientation。不是 JPEG 或没有 EXIF 返回 (0, false)。
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

// pngExifOrientation 读 eXIf chunk。签名不对或没有该 chunk 返回 (0, false)。
func pngExifOrientation(source []byte) (int, bool) {
	sig := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if !bytes.HasPrefix(source, sig) {
		return 0, false
	}
	offset := 8
	for offset+12 <= len(source) {
		length := int(binary.BigEndian.Uint32(source[offset:]))
		typ := string(source[offset+4 : offset+8])
		offset += 8
		if length < 0 || offset+length+4 > len(source) {
			return 0, false
		}
		data := source[offset : offset+length]
		if typ == "eXIf" {
			return tiffOrientation(data)
		}
		if typ == "IEND" {
			return 0, false
		}
		offset += length + 4
	}
	return 0, false
}

// webpExifOrientation 读 RIFF EXIF chunk。奇数 chunk 后跳过 pad 字节。
func webpExifOrientation(source []byte) (int, bool) {
	if len(source) < 12 || string(source[:4]) != "RIFF" || string(source[8:12]) != "WEBP" {
		return 0, false
	}
	offset := 12
	for offset+8 <= len(source) {
		fourcc := string(source[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(source[offset+4 : offset+8]))
		offset += 8
		if size < 0 || offset+size > len(source) {
			return 0, false
		}
		data := source[offset : offset+size]
		if fourcc == "EXIF" {
			return tiffOrientation(data)
		}
		offset += size
		if size%2 == 1 {
			offset++
		}
	}
	return 0, false
}

// tiffOrientation 解析 EXIF TIFF IFD 的 Orientation(0x0112)。字节序非法或找不到标签返回 (0, false)。
func tiffOrientation(data []byte) (int, bool) {
	const prefix = "Exif\x00\x00"
	tiff := data
	if bytes.HasPrefix(data, []byte(prefix)) {
		tiff = data[len(prefix):]
	}
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
