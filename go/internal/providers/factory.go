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

func (l LivePrompt) Name() string {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return "unconfigured"
	}
	return p.Name()
}

func (l LivePrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GenerateCreativeBrief(ctx, req)
}

func (l LivePrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GenerateVisualOverlay(ctx, req)
}

func (l LivePrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GeneratePrompt(ctx, req)
}

// LiveImage 每次调用按当前 image 绑定解析生图 / 连续生图 / 局部编辑。
type LiveImage struct{ Store *settings.Store }

func (l LiveImage) resolve(ctx context.Context) (imageAdapterSet, error) {
	return imageAdapter(ctx, l.Store)
}

func (l LiveImage) Name() string {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return "unconfigured"
	}
	return p.Name()
}

func (l LiveImage) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.ImageResult{}, err
	}
	return p.GenerateImage(ctx, req)
}

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

func (l LiveImage) Capability() localedit.Capability {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return localedit.UnsupportedCapability("unconfigured")
	}
	return p.Capability()
}

func (l LiveImage) Edit(ctx context.Context, req localedit.EditRequest) (localedit.EditResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return localedit.EditResult{}, err
	}
	return p.Edit(ctx, req)
}

// Prompt 按当前 prompt 绑定构造图运行提示词供应商。
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
		tool := settings.ImageToolRuntime{}
		if store != nil {
			resolved, err := store.ImageToolRuntime(ctx)
			if err != nil {
				return nil, err
			}
			tool = resolved
		}
		return OpenAIResponses{
			OpenAIImages: OpenAIImages{
				Kind: "openai_responses", APIKey: binding.APIKey, BaseURL: binding.BaseURL,
				Model: binding.Model,
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
