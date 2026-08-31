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
	// ErrMissingOutput 表示供应商完成但没有返回图片。
	ErrMissingOutput = errors.New("图片供应商没有返回图片结果，请稍后重试")
	// ErrTextOutput 表示供应商返回文字而非图片，视为已证明失败。
	ErrTextOutput = errors.New("图片供应商已完成请求，但返回的是文字回复，没有返回图片结果")
	// ErrRateLimit 表示供应商限流或配额不足，视为已证明失败且可重试。
	ErrRateLimit = errors.New("图片供应商限流或配额不足，请稍后重试或降低并发后再试")
	// ErrTimeout 表示请求超时，结果不可证明。
	ErrTimeout = errors.New("图片供应商请求超时，请稍后重试")
	// ErrConnection 表示连接中断，结果不可证明。
	ErrConnection = errors.New("图片供应商连接中断，请检查网络或代理后重试")
	// ErrProvider5xx 表示供应商 5xx，结果不可证明。
	ErrProvider5xx = errors.New("图片供应商服务异常，请稍后重试")
)

// IsConfirmedProviderFailure 判断错误是否已证明失败，可标 failed 而不标 unknown。
func IsConfirmedProviderFailure(err error) bool {
	return errors.Is(err, ErrTextOutput) || errors.Is(err, ErrRateLimit)
}

// IsUncertainProviderFailure 判断错误是否不可证明，应标 unknown。
func IsUncertainProviderFailure(err error) bool {
	return errors.Is(err, ErrTimeout) || errors.Is(err, ErrConnection) || errors.Is(err, ErrProvider5xx)
}

// IsRetryableProviderFailure 判断错误是否允许用户稍后重试。
func IsRetryableProviderFailure(err error) bool {
	return errors.Is(err, ErrRateLimit) || errors.Is(err, ErrTimeout) || errors.Is(err, ErrConnection) || errors.Is(err, ErrProvider5xx)
}

// ChatRequest 是连续生图供应商的内部调用参数，不是 HTTP JSON。
// BaseBytes 是续画底图；ReferenceBytes 是参考图。Count<1 时 mock 按 1 张处理。
type ChatRequest struct {
	Prompt             string         // 用户提示词
	Size               string         // 规范化后的宽x高
	Count              int            // 本批候选张数；mock 在 <1 时按 1 张
	ToolOptions        map[string]any // 已过滤的 image tool 字段
	BaseBytes          []byte         // 续画底图；空表示文生图
	ReferenceBytes     [][]byte       // 参考图 bytes，不是节点绑定
	HistoryBlock       string         // 拼进模板的历史块
	PreviousResponseID *string
}

// ChatResult 是连续生图供应商返回的内部结果，不是 HTTP 投影。
// Images 是多候选；Bytes 通常等于第一张。不可证明失败应返回 error 而不是空 Images。
type ChatResult struct {
	Bytes          []byte   // 通常等于 Images[0]
	Images         [][]byte // 多候选；不可证明失败应返回 error 而不是空切片
	MIME           string
	Model          string // 供应商实际模型名
	PromptVersion  string // 连续生图模板版本
	ResponseID     string
	ProviderStatus string         // 常见 completed；空表示供应商未给
	OutputJSON     map[string]any // 原始输出旁路，供 notes/对账
}

// ChatProvider 生成连续生图候选。worker Executor 调用，HTTP 不要直接打网。
// 不可证明的失败返回 timeout/connection/5xx 类 error，由 Execute 标 unknown。
type ChatProvider interface {
	// Name 返回供应商标识。
	Name() string
	// Generate 调用图像模型生成候选；不可证明的失败应标 unknown。
	Generate(ctx context.Context, req ChatRequest) (ChatResult, error)
}

// MockChatProvider 实现 ChatProvider，不打网；默认返回灰色 PNG，或返回注入的 Err。
type MockChatProvider struct {
	ProviderName  string // 空时 Name() 返回 "mock"
	PNG           []byte // 空时生成灰色 PNG
	Err           error  // 非 nil 时 Generate 直接返回，不打网
	Model         string // 注入到 ChatResult.Model；空则 mock 默认
	PromptVersion string // 注入到 ChatResult.PromptVersion
	ResponseID    string
}

// Name 实现 ChatProvider；空 ProviderName 时返回 "mock"。
func (m MockChatProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

// Generate 实现 ChatProvider，不打网。Err 非 nil 时原样返回；否则出灰色 PNG。
// 调用时机：测试与未绑定真实供应商。不要把它当 Live 适配器的回退路径写进生产路由。
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
