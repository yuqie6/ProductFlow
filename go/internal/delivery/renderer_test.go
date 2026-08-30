package delivery

import (
	"bytes"
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
	limited := 4096
	small, err := Render(src, Spec{Width: 80, Height: 80, Format: "webp", Fit: "cover", MaxByteSize: &limited})
	if err != nil {
		t.Fatal(err)
	}
	if small.Metadata.ByteSize > limited || small.Metadata.MIMEType != "image/webp" {
		t.Fatalf("%+v", small.Metadata)
	}
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
	tiff := []byte{
		'I', 'I', 0x2a, 0x00,
		0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01,
		0x03, 0x00,
		0x01, 0x00, 0x00, 0x00,
		byte(orientation), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	length := len(payload) + 2
	return append([]byte{0xff, 0xe1, byte(length >> 8), byte(length)}, payload...)
}
