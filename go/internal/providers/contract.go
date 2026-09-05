package providers

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strconv"
	"strings"
)

// Mode 区分工作流生图与连续生图的失败分类。空值按 ModeWorkflow。
type Mode string

const (
	// ModeWorkflow 是画布 image_generation：429/5xx/超时走 unknown。
	ModeWorkflow Mode = "workflow"
	// ModeChat 是连续生图：429 走限流，5xx 走供应商异常，已证明 4xx 走 Validation。
	ModeChat Mode = "chat"
)

// ImageRef 是发给生图/编辑供应商的像素，不是商品图身份。
type ImageRef struct {
	Bytes    []byte
	MIME     string
	Filename string
}

// GenerateRequest 是供应商中立的生图入参。Prompt 必须是调用方已经编好的自然语言。
// 工作流把 listing compile 结果放进 Prompt；连续生图把模板渲染结果放进 Prompt。
type GenerateRequest struct {
	Prompt             string
	Size               string
	Count              int
	Refs               []ImageRef
	ToolOptions        map[string]any
	PreviousResponseID *string
	ImageTypeKey       string
	GenerationSpec     map[string]any
	Mode               Mode
}

func (r GenerateRequest) chat() bool { return r.Mode == ModeChat }

func (r GenerateRequest) statusMapper() func(int, []byte) error {
	if r.chat() {
		return mapChatStatus
	}
	return mapWorkflowStatus
}

func (r GenerateRequest) resolvedSize(fallback func(map[string]any) string) string {
	if size := strings.TrimSpace(r.Size); size != "" {
		return size
	}
	if fallback != nil {
		if size := strings.TrimSpace(fallback(r.GenerationSpec)); size != "" {
			return size
		}
	}
	return "1024x1024"
}

// GenerateResult 是供应商中立的生图结果。无法证明成功时应返回 error 而不是空 Bytes。
type GenerateResult struct {
	Bytes               []byte
	Images              [][]byte
	MIME                string
	Model               string
	ResponseID          string
	ProviderStatus      string
	Width               int
	Height              int
	EffectiveParameters map[string]any
	PromptVersion       string
}

// EditRequest 是供应商中立的局部编辑入参。MaskPNG 必须已对齐源图像素。
type EditRequest struct {
	Instruction    string
	Size           string
	SourceBytes    []byte
	SourceMIME     string
	MaskPNG        []byte
	ReferenceBytes [][]byte
	Operation      string
}

// EditResult 是供应商中立的局部编辑出图，不是商品图身份。
type EditResult struct {
	Bytes          []byte
	MIME           string
	Model          string
	ResponseID     string
	ProviderStatus string
}

// EditCapability 声明当前绑定是否支持 masked local edit。
type EditCapability struct {
	ProviderName       string
	Supported          bool
	Mode               string
	Operations         []string
	RequiresMask       bool
	MaxReferenceImages int
	Reason             string
}

const (
	editModeMasked    = "masked_edit"
	editMaxReferences = 6
)

// SupportedEditCapability 声明支持 remove / replace_text / inpaint，且必须提供 mask。
func SupportedEditCapability(name string) EditCapability {
	return EditCapability{
		ProviderName: name, Supported: true, Mode: editModeMasked,
		Operations:   []string{"remove", "replace_text", "inpaint"},
		RequiresMask: true, MaxReferenceImages: editMaxReferences,
	}
}

// UnsupportedEditCapability 声明未支持 masked local edit。
func UnsupportedEditCapability(name string) EditCapability {
	return EditCapability{
		ProviderName: name, Supported: false, MaxReferenceImages: 0,
		Reason: "图片 provider 未显式声明 masked local edit 能力",
	}
}

// ImageClient 是生图与局部编辑的供应商缝。工厂只构造本接口，不认识 graph / imagesession / localedit。
type ImageClient interface {
	Name() string
	Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
	Edit(ctx context.Context, req EditRequest) (EditResult, error)
	Capability() EditCapability
	ReconcileResponse(ctx context.Context, responseID string) (string, error)
}

// MockImage 不打网。ModeChat 出灰色 PNG；工作流 mock 在未指定 Size 时出 64×64 灰图，保证局部编辑选区能同时有编辑区和保护区。
type MockImage struct {
	ProviderName string
	PNG          []byte
	Err          error
	Cap          EditCapability
}

func (m MockImage) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

func (m MockImage) Capability() EditCapability {
	if m.Cap.ProviderName != "" {
		return m.Cap
	}
	return UnsupportedEditCapability(m.Name())
}

func (m MockImage) ReconcileResponse(context.Context, string) (string, error) {
	return "unsupported", nil
}

func (m MockImage) Generate(_ context.Context, req GenerateRequest) (GenerateResult, error) {
	if m.Err != nil {
		return GenerateResult{}, m.Err
	}
	pngBytes := m.PNG
	width, height := 0, 0
	if len(pngBytes) == 0 {
		width, height = parsePixelSize(req.Size)
		if !req.chat() && strings.TrimSpace(req.Size) == "" {
			width, height = 64, 64
		}
		pngBytes = grayPNG(width, height)
	}
	n := req.Count
	if n < 1 {
		n = 1
	}
	images := make([][]byte, n)
	for i := range images {
		images[i] = pngBytes
	}
	return GenerateResult{
		Bytes: pngBytes, Images: images, MIME: "image/png", Model: "mock-image",
		ProviderStatus: "completed", PromptVersion: "mock-image-v1",
		Width: width, Height: height,
	}, nil
}

func (m MockImage) Edit(_ context.Context, req EditRequest) (EditResult, error) {
	if m.Err != nil {
		return EditResult{}, m.Err
	}
	png := m.PNG
	if len(png) == 0 {
		png = req.SourceBytes
	}
	return EditResult{Bytes: png, MIME: "image/png", Model: "mock-local-edit", ProviderStatus: "completed"}, nil
}

func parsePixelSize(size string) (int, int) {
	w, h := 1024, 1024
	parts := strings.SplitN(size, "x", 2)
	if len(parts) != 2 {
		return w, h
	}
	if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && n > 0 {
		w = n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && n > 0 {
		h = n
	}
	return w, h
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
