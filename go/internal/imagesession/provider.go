package imagesession

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

type ChatRequest struct {
	Prompt      string
	Size        string
	ToolOptions map[string]any
}

type ChatResult struct {
	Bytes          []byte
	MIME           string
	Model          string
	ResponseID     string
	ProviderStatus string
	OutputJSON     map[string]any
}

type ChatProvider interface {
	Name() string
	Generate(ctx context.Context, req ChatRequest) (ChatResult, error)
}

type MockChatProvider struct {
	ProviderName string
	PNG          []byte
	Err          error
}

func (m MockChatProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

func (m MockChatProvider) Generate(ctx context.Context, req ChatRequest) (ChatResult, error) {
	if m.Err != nil {
		return ChatResult{}, m.Err
	}
	pngBytes := m.PNG
	if len(pngBytes) == 0 {
		w, h := parseSize(req.Size)
		pngBytes = grayPNG(w, h)
	}
	return ChatResult{
		Bytes: pngBytes, MIME: "image/png", Model: "mock-image",
		ProviderStatus: "completed",
		OutputJSON:     map[string]any{"status": "completed"},
	}, nil
}

func grayPNG(w, h int) []byte {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{238, 240, 242, 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
