package ocr

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// RenderTextPNG 用嵌入 Liberation Sans 把 text 画进白底 PNG（测试夹具 / 本地验光）。
func RenderTextPNG(text string, width, height int, fontSize float64) ([]byte, error) {
	if width < 8 {
		width = 320
	}
	if height < 8 {
		height = 120
	}
	if fontSize < 8 {
		fontSize = 28
	}
	fontBytes, err := loadEmbeddedFont()
	if err != nil {
		return nil, err
	}
	ft, err := opentype.Parse(fontBytes)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		return nil, err
	}
	defer face.Close()

	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	ascent := face.Metrics().Ascent.Ceil()
	d := &font.Drawer{
		Dst:  rgba,
		Src:  image.Black,
		Face: face,
		Dot:  fixed.P(24, 24+ascent),
	}
	d.DrawString(text)
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
