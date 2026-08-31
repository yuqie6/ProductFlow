// Package providers 构造 prompt 与 image 供应商；不可证明的结果保持 unknown。
//
// 职责：按 settings 里当前绑定选出 OpenAI / Gemini / mock，并翻译 HTTP 错误。
// 调用时机：graph cook、创建页 source_note、连续生图、局部编辑。每次 Live* 调用都重新 Resolve，不要缓存过期 Key。
// 副作用：只打外网；业务行由调用方写 provider_effects。本包不 Stage 队列。
// 错误：已证明的 4xx/内容拒绝可以 failed；超时、断流、非图响应走 unknown，调用方不得当失败自动重试。
// 禁区：不要在这里写 workflow_* 表；mock 实现不能打网。
package providers

import (
	"context"
	"fmt"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/settings"
)

// LivePrompt 每次调用按当前 PostgreSQL 绑定解析提示词供应商。
type LivePrompt struct{ Store *settings.Store }

func (l LivePrompt) resolve(ctx context.Context) (graph.PromptProvider, error) {
	return Prompt(ctx, l.Store)
}

// Name 实现 graph.PromptProvider。每次从 settings 解析当前绑定；无法解析时返回 "unconfigured"。
func (l LivePrompt) Name() string {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return "unconfigured"
	}
	return p.Name()
}

// GenerateCreativeBrief 实现 graph.PromptProvider，按当前 settings 绑定调用底层供应商。
func (l LivePrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GenerateCreativeBrief(ctx, req)
}

// GenerateVisualOverlay 实现 graph.PromptProvider，按当前 settings 绑定调用底层供应商。
func (l LivePrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GenerateVisualOverlay(ctx, req)
}

// GeneratePrompt 实现 graph.PromptProvider，按当前 settings 绑定调用底层供应商。
func (l LivePrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GeneratePrompt(ctx, req)
}

// GenerateSourceNote 实现 graph.PromptProvider。底层没有该方法时回退 MockSourceNotePayload，不打网。
func (l LivePrompt) GenerateSourceNote(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	if g, ok := p.(interface {
		GenerateSourceNote(context.Context, graph.PromptRequest) (graph.PromptResult, error)
	}); ok {
		return g.GenerateSourceNote(ctx, req)
	}
	return graph.PromptResult{Payload: MockSourceNotePayload(), Model: "mock-source-note"}, nil
}

// LiveImage 每次调用按当前 image 绑定解析生图 / 连续生图 / 局部编辑。
type LiveImage struct{ Store *settings.Store }

func (l LiveImage) resolve(ctx context.Context) (imageAdapterSet, error) {
	return imageAdapter(ctx, l.Store)
}

// Name 实现 graph.ImageProvider。每次从 settings 解析当前 image 绑定；无法解析时返回 "unconfigured"。
func (l LiveImage) Name() string {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return "unconfigured"
	}
	return p.Name()
}

// GenerateImage 实现 graph.ImageProvider，按当前 settings 绑定调用底层供应商。
func (l LiveImage) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.ImageResult{}, err
	}
	return p.GenerateImage(ctx, req)
}

// Generate 实现 imagesession.ChatProvider。先读 settings 的 chat prompt 模板再调底层；无法证明的结果保持 unknown。
func (l LiveImage) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	if l.Store != nil {
		tmpl, err := l.Store.ImageChatPromptTemplate(ctx)
		if err != nil {
			return imagesession.ChatResult{}, err
		}
		size := req.Size
		if size == "" {
			size = "1024x1024"
		}
		req.Prompt = imagesession.RenderChatPrompt(tmpl, req.Prompt, size, req.HistoryBlock)
	}
	return p.Generate(ctx, req)
}

// Capability 实现 localedit.Provider，按当前 image 绑定声明能力。
func (l LiveImage) Capability() localedit.Capability {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return localedit.UnsupportedCapability("unconfigured")
	}
	return p.Capability()
}

// Edit 实现 localedit.Provider，按当前 settings 绑定调用底层供应商。
func (l LiveImage) Edit(ctx context.Context, req localedit.EditRequest) (localedit.EditResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return localedit.EditResult{}, err
	}
	return p.Edit(ctx, req)
}

// ReconcileResponse 查询供应商原请求状态；底层不支持时返回 "unsupported"，不可证明时返回 "unknown"。
func (l LiveImage) ReconcileResponse(ctx context.Context, responseID string) (string, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return "unsupported", err
	}
	r, ok := p.(interface {
		ReconcileResponse(context.Context, string) (string, error)
	})
	if !ok {
		return "unsupported", nil
	}
	return r.ReconcileResponse(ctx, responseID)
}

// Prompt 按当前 prompt 用途绑定构造图运行提示词供应商。
// 调用时机：跑图 cook prompt 节点。store 为 nil 或 Kind=mock 返回 MockPromptProvider（不打网）。
// 非 openai 的真实绑定返回 Unavailable。不要用本函数构造 image 或 agent 供应商。
func Prompt(ctx context.Context, store *settings.Store) (graph.PromptProvider, error) {
	if store == nil {
		return graph.MockPromptProvider{}, nil
	}
	binding, err := store.ResolvePrompt(ctx)
	if err != nil {
		return nil, err
	}
	if binding.Kind == "" || binding.Kind == "mock" {
		return graph.MockPromptProvider{}, nil
	}
	if binding.Kind != "openai" {
		return nil, apperr.Unavailable(fmt.Sprintf("暂不支持的 prompt provider: %s", binding.Kind))
	}
	return OpenAIPrompt{APIKey: binding.APIKey, BaseURL: binding.BaseURL, Model: binding.Model}, nil
}

// imageAdapter 按当前 image 绑定构造具体适配器。store 为 nil 或绑定是 mock 时返回不打网的 mockBundle。
func imageAdapter(ctx context.Context, store *settings.Store) (imageAdapterSet, error) {
	if store == nil {
		return mockBundle{}, nil
	}
	binding, err := store.ResolveImage(ctx)
	if err != nil {
		return nil, err
	}
	switch binding.Kind {
	case "", "mock":
		return mockBundle{}, nil
	case "openai_images":
		return OpenAIImages{
			Kind: "openai_images", APIKey: binding.APIKey, BaseURL: binding.BaseURL,
			Model: binding.Model, Quality: binding.ImagesQuality, Style: binding.ImagesStyle,
			MaskEdit: binding.MaskEdit,
		}, nil
	case "openai_responses":
		tool, err := store.ImageToolRuntime(ctx)
		if err != nil {
			return nil, err
		}
		return OpenAIResponses{
			OpenAIImages: OpenAIImages{
				Kind: "openai_responses", APIKey: binding.APIKey, BaseURL: binding.BaseURL,
				Model: binding.Model, MaskEdit: binding.MaskEdit,
			},
			Background:    binding.ResponsesBackground,
			ToolRuntime:   tool.Options,
			AllowedFields: tool.Allowed,
		}, nil
	case "google_gemini_image":
		if err := geminiRejectCustomBaseURL(binding.BaseURL); err != nil {
			return nil, err
		}
		version := binding.GeminiAPIVersion
		if version == "" {
			version = "v1beta"
		}
		return GeminiImage{
			APIKey: binding.APIKey, Model: binding.Model,
			APIVersion: version, OutputMIME: binding.GeminiOutputMIME,
		}, nil
	default:
		return unsupportedImage{kind: binding.Kind}, nil
	}
}

type imageAdapterSet interface {
	graph.ImageProvider
	imagesession.ChatProvider
	localedit.Provider
}

type mockBundle struct {
	graph.MockImageProvider
	imagesession.MockChatProvider
	localedit.MockProvider
}

func (mockBundle) Name() string { return "mock" }

type unsupportedImage struct{ kind string }

func (u unsupportedImage) Name() string { return u.kind }

func (u unsupportedImage) GenerateImage(context.Context, graph.ImageRequest) (graph.ImageResult, error) {
	return graph.ImageResult{}, fmt.Errorf("暂不支持的图片 provider: %s", u.kind)
}

func (u unsupportedImage) Generate(context.Context, imagesession.ChatRequest) (imagesession.ChatResult, error) {
	return imagesession.ChatResult{}, fmt.Errorf("暂不支持的图片 provider: %s", u.kind)
}

func (u unsupportedImage) Capability() localedit.Capability {
	return localedit.UnsupportedCapability(u.kind)
}

func (u unsupportedImage) Edit(context.Context, localedit.EditRequest) (localedit.EditResult, error) {
	return localedit.EditResult{}, apperr.Validation("图片 provider 未显式声明 masked local edit 能力")
}
