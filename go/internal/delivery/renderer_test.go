package delivery

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/yuqie6/productflow/internal/media"
)

func TestEncodeImageWritesWebP(t *testing.T) {
	src := solidPNG(t, 48, 32, color.RGBA{R: 200, G: 40, B: 40, A: 255})
	rendered, err := Render(src, Spec{Width: 24, Height: 24, Format: "webp", Fit: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := media.Inspect(rendered.Bytes, "image/webp")
	if err != nil {
		t.Fatal(err)
	}
	if meta.MIMEType != "image/webp" || meta.Width != 24 || meta.Height != 24 {
		t.Fatalf("%+v", meta)
	}
	if bytes.Contains(rendered.Bytes, []byte("VP8L")) {
		t.Fatal("default webp must be lossy, not VP8L")
	}
	limited := 4096
	small, err := Render(src, Spec{Width: 80, Height: 80, Format: "webp", Fit: "cover", MaxByteSize: &limited})
	if err != nil {
		t.Fatal(err)
	}
	if small.Metadata.ByteSize > limited || small.Metadata.MIMEType != "image/webp" {
		t.Fatalf("%+v", small.Metadata)
	}
}

func TestEncodeWebPUsesLossyQualityLadder(t *testing.T) {
	src := noisyPNG(t, 160, 160)
	img, err := decodeSource(src)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{Width: 160, Height: 160, Format: "webp", Fit: "cover"}
	resized := resize(img, spec)
	q95, err := encodeWebP(resized, 95)
	if err != nil {
		t.Fatal(err)
	}
	q1, err := encodeWebP(resized, 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(q95, []byte("VP8L")) {
		t.Fatal("quality 95 webp must be lossy")
	}
	if len(q95) <= len(q1) {
		t.Fatalf("quality 95 size %d should exceed quality 1 size %d", len(q95), len(q1))
	}
	limit := (len(q95) + len(q1)) / 2
	if limit >= len(q95) || limit <= len(q1) {
		t.Fatalf("limit %d not between q1=%d and q95=%d", limit, len(q1), len(q95))
	}
	capped := spec
	capped.MaxByteSize = &limit
	encoded, err := encodeImage(resized, capped)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > limit {
		t.Fatalf("byte size %d exceeds %d", len(encoded), limit)
	}
	tooSmall := 32
	failSpec := spec
	failSpec.MaxByteSize = &tooSmall
	if _, err := encodeImage(resized, failSpec); err == nil {
		t.Fatal("expected max_byte_size failure")
	}
}

func TestEncodeJPEGUses444AndQualityLadder(t *testing.T) {
	src := noisyPNG(t, 160, 160)
	img, err := decodeSource(src)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{Width: 160, Height: 160, Format: "jpeg", Fit: "cover"}
	resized := resize(img, spec)
	q95, err := encodeJPEG(resized, 95)
	if err != nil {
		t.Fatal(err)
	}
	y, cb, cr := jpegComponentSampling(t, q95)
	if y != 0x11 || cb != 0x11 || cr != 0x11 {
		t.Fatalf("sampling Y=%#x Cb=%#x Cr=%#x want 4:4:4", y, cb, cr)
	}
	q1, err := encodeJPEG(resized, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(q95) <= len(q1) {
		t.Fatalf("quality 95 size %d should exceed quality 1 size %d", len(q95), len(q1))
	}
	limit := (len(q95) + len(q1)) / 2
	if limit >= len(q95) || limit <= len(q1) {
		t.Fatalf("limit %d not between q1=%d and q95=%d", limit, len(q1), len(q95))
	}
	capped := spec
	capped.MaxByteSize = &limit
	encoded, err := encodeImage(resized, capped)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > limit {
		t.Fatalf("byte size %d exceeds %d", len(encoded), limit)
	}
}

func jpegComponentSampling(t *testing.T, raw []byte) (y, cb, cr byte) {
	t.Helper()
	if len(raw) < 4 || raw[0] != 0xff || raw[1] != 0xd8 {
		t.Fatal("not jpeg")
	}
	offset := 2
	for offset+4 <= len(raw) {
		if raw[offset] != 0xff {
			t.Fatalf("marker at %d", offset)
		}
		marker := raw[offset+1]
		offset += 2
		if marker == 0xda || marker == 0xd9 {
			t.Fatal("missing SOF0")
		}
		if marker == 0xd0 || marker == 0xd1 || marker == 0xd2 || marker == 0xd3 ||
			marker == 0xd4 || marker == 0xd5 || marker == 0xd6 || marker == 0xd7 ||
			marker == 0x01 {
			continue
		}
		if offset+2 > len(raw) {
			t.Fatal("truncated jpeg")
		}
		length := int(binary.BigEndian.Uint16(raw[offset:]))
		if length < 2 || offset+length > len(raw) {
			t.Fatal("bad segment length")
		}
		payload := raw[offset+2 : offset+length]
		offset += length
		if marker != 0xc0 {
			continue
		}
		if len(payload) < 15 || payload[5] < 3 {
			t.Fatalf("sof payload %d", len(payload))
		}
		return payload[7], payload[10], payload[13]
	}
	t.Fatal("missing SOF0")
	return 0, 0, 0
}

func TestDecodeSourceTransposesJPEGExifOrientation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			if x < 8 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	jpegBytes := jpegWithOrientation(t, img, 6)
	decoded, err := decodeSource(jpegBytes)
	if err != nil {
		t.Fatal(err)
	}
	b := decoded.Bounds()
	if b.Dx() != 8 || b.Dy() != 16 {
		t.Fatalf("size %dx%d", b.Dx(), b.Dy())
	}
	r, g, bl, _ := decoded.At(b.Min.X+1, b.Min.Y+1).RGBA()
	if r>>8 < 180 || bl>>8 > 80 {
		t.Fatalf("top-left after orientation 6 should be red, got %d %d %d", r>>8, g>>8, bl>>8)
	}
}

func TestDecodeSourceTransposesPNGExifOrientation(t *testing.T) {
	decoded, err := decodeSource(pngWithOrientation(t, orientedColorImage(), 6))
	if err != nil {
		t.Fatal(err)
	}
	assertOrientation6(t, decoded)
}

func TestDecodeSourceTransposesWebPExifOrientation(t *testing.T) {
	decoded, err := decodeSource(webpWithOrientation(t, orientedColorImage(), 6))
	if err != nil {
		t.Fatal(err)
	}
	assertOrientation6(t, decoded)
}

func orientedColorImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			if x < 8 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	return img
}

func assertOrientation6(t *testing.T, decoded image.Image) {
	t.Helper()
	b := decoded.Bounds()
	if b.Dx() != 8 || b.Dy() != 16 {
		t.Fatalf("size %dx%d", b.Dx(), b.Dy())
	}
	r, g, bl, _ := decoded.At(b.Min.X+1, b.Min.Y+1).RGBA()
	if r>>8 < 180 || bl>>8 > 80 {
		t.Fatalf("top-left after orientation 6 should be red, got %d %d %d", r>>8, g>>8, bl>>8)
	}
}

func noisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x*37 + y*19) % 251),
				G: uint8((x*11 + y*73) % 241),
				B: uint8((x*91 + y*5) % 239),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegWithOrientation(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if len(raw) < 2 || raw[0] != 0xff || raw[1] != 0xd8 {
		t.Fatal("not jpeg")
	}
	app1 := buildExifAPP1(orientation)
	out := make([]byte, 0, 2+len(app1)+len(raw)-2)
	out = append(out, 0xff, 0xd8)
	out = append(out, app1...)
	out = append(out, raw[2:]...)
	return out
}

func buildExifAPP1(orientation int) []byte {
	payload := append([]byte("Exif\x00\x00"), orientationTIFF(orientation)...)
	length := len(payload) + 2
	return append([]byte{0xff, 0xe1, byte(length >> 8), byte(length)}, payload...)
}

func orientationTIFF(orientation int) []byte {
	return []byte{
		'I', 'I', 0x2a, 0x00,
		0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01,
		0x03, 0x00,
		0x01, 0x00, 0x00, 0x00,
		byte(orientation), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
}

func pngWithOrientation(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	sig := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if !bytes.HasPrefix(raw, sig) || len(raw) < 33 {
		t.Fatal("not png")
	}
	chunk := pngChunk("eXIf", orientationTIFF(orientation))
	out := make([]byte, 0, len(raw)+len(chunk))
	out = append(out, raw[:33]...)
	out = append(out, chunk...)
	out = append(out, raw[33:]...)
	return out
}

func pngChunk(typ string, data []byte) []byte {
	out := make([]byte, 0, 12+len(data))
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	out = append(out, typ...)
	out = append(out, data...)
	crc := crc32.ChecksumIEEE(out[4:])
	return binary.BigEndian.AppendUint32(out, crc)
}

func webpWithOrientation(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	raw, err := encodeWebP(img, 95)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WEBP" {
		t.Fatal("not webp")
	}
	payload := append([]byte("Exif\x00\x00"), orientationTIFF(orientation)...)
	chunk := make([]byte, 0, 8+len(payload)+1)
	chunk = append(chunk, "EXIF"...)
	chunk = binary.LittleEndian.AppendUint32(chunk, uint32(len(payload)))
	chunk = append(chunk, payload...)
	if len(payload)%2 == 1 {
		chunk = append(chunk, 0)
	}
	out := append(append([]byte{}, raw...), chunk...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}
