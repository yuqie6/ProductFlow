package imagesession

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

var (
	ErrMissingOutput = errors.New("图片供应商没有返回图片结果，请稍后重试")
	ErrTextOutput    = errors.New("图片供应商已完成请求，但返回的是文字回复，没有返回图片结果")
	ErrRateLimit     = errors.New("图片供应商限流或配额不足，请稍后重试或降低并发后再试")
	ErrTimeout       = errors.New("图片供应商请求超时，请稍后重试")
	ErrConnection    = errors.New("图片供应商连接中断，请检查网络或代理后重试")
	ErrProvider5xx   = errors.New("图片供应商服务异常，请稍后重试")
)

func IsConfirmedProviderFailure(err error) bool {
	return errors.Is(err, ErrTextOutput) || errors.Is(err, ErrRateLimit)
}

func IsUncertainProviderFailure(err error) bool {
	return errors.Is(err, ErrTimeout) || errors.Is(err, ErrConnection) || errors.Is(err, ErrProvider5xx)
}

func IsRetryableProviderFailure(err error) bool {
	return errors.Is(err, ErrRateLimit) || errors.Is(err, ErrTimeout) || errors.Is(err, ErrConnection) || errors.Is(err, ErrProvider5xx)
}

type ChatRequest struct {
	Prompt             string
	Size               string
	Count              int
	ToolOptions        map[string]any
	BaseBytes          []byte
	ReferenceBytes     [][]byte
	HistoryBlock       string
	PreviousResponseID *string
}

type ChatResult struct {
	Bytes          []byte
	Images         [][]byte
	MIME           string
	Model          string
	PromptVersion  string
	ResponseID     string
	ProviderStatus string
	OutputJSON     map[string]any
}

type ChatProvider interface {
	Name() string
	Generate(ctx context.Context, req ChatRequest) (ChatResult, error)
}

type MockChatProvider struct {
	ProviderName  string
	PNG           []byte
	Err           error
	Model         string
	PromptVersion string
	ResponseID    string
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
	model := m.Model
	if model == "" {
		model = "mock-image"
	}
	promptVersion := m.PromptVersion
	if promptVersion == "" {
		promptVersion = "mock-image-v1"
	}
	n := req.Count
	if n < 1 {
		n = 1
	}
	images := make([][]byte, n)
	for i := range images {
		images[i] = pngBytes
	}
	return ChatResult{
		Bytes: pngBytes, Images: images, MIME: "image/png", Model: model, PromptVersion: promptVersion,
		ResponseID: m.ResponseID, ProviderStatus: "completed",
		OutputJSON: map[string]any{"status": "completed"},
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
