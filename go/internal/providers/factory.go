// Package providers 构造 prompt 与 image 供应商；不可证明的结果保持 unknown。
//
// 职责：按 settings 里当前绑定选出 OpenAI / Gemini / mock，并翻译 HTTP 错误。
// 调用时机：graph cook、创建页 source_note、连续生图、局部编辑。每次 Live* 调用都重新 Resolve，不要缓存过期 Key。
// 副作用：只打外网；业务行由调用方写 provider_effects。本包不 Stage 队列。
// 错误：已证明的 4xx/内容拒绝可以 failed；超时、断流、非图响应走 unknown，调用方不得当失败自动重试。
// 禁区：不要在这里写 workflow_* 表；mock 实现不能打网。
// Image 工厂只返回 ImageClient，不得 import graph / imagesession / localedit。
package providers

import (
	"context"
	"fmt"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/settings"
)

// LiveImage 每次调用按当前 image 绑定解析生图 / 连续生图 / 局部编辑。
type LiveImage struct {
	// Store 每次调用重新 Resolve，不要缓存过期 Key；nil 时回落 mock，不报错。
	Store *settings.Store
}

func (l LiveImage) resolve(ctx context.Context) (ImageClient, error) {
	return Image(ctx, l.Store)
}

// Name 实现 ImageClient。每次从 settings 解析当前 image 绑定；无法解析时返回 "unconfigured"。
func (l LiveImage) Name() string {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return "unconfigured"
	}
	return p.Name()
}

// Generate 按当前 settings 绑定调用底层供应商。绑定无法 Resolve 时返回 error。
func (l LiveImage) Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return GenerateResult{}, err
	}
	return p.Generate(ctx, req)
}

// Capability 按当前 image 绑定声明 masked local edit 能力。
func (l LiveImage) Capability() EditCapability {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return UnsupportedEditCapability("unconfigured")
	}
	return p.Capability()
}

// Edit 按当前 settings 绑定调用底层供应商。绑定无法 Resolve 时返回 error。
func (l LiveImage) Edit(ctx context.Context, req EditRequest) (EditResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return EditResult{}, err
	}
	return p.Edit(ctx, req)
}

// ReconcileResponse 查询供应商原请求状态；底层不支持时返回 "unsupported"。
func (l LiveImage) ReconcileResponse(ctx context.Context, responseID string) (string, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return "unsupported", err
	}
	return p.ReconcileResponse(ctx, responseID)
}

// Image 按当前 image 绑定构造生图/编辑客户端。store 为 nil 或绑定是 mock 时返回不打网的 MockImage。
func Image(ctx context.Context, store *settings.Store) (ImageClient, error) {
	if store == nil {
		return MockImage{}, nil
	}
	binding, err := store.ResolveImage(ctx)
	if err != nil {
		return nil, err
	}
	switch binding.Kind {
	case "", "mock":
		return MockImage{}, nil
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

type unsupportedImage struct{ kind string }

func (u unsupportedImage) Name() string { return u.kind }

func (u unsupportedImage) Generate(context.Context, GenerateRequest) (GenerateResult, error) {
	return GenerateResult{}, fmt.Errorf("暂不支持的图片 provider: %s", u.kind)
}

func (u unsupportedImage) Capability() EditCapability {
	return UnsupportedEditCapability(u.kind)
}

func (u unsupportedImage) Edit(context.Context, EditRequest) (EditResult, error) {
	return EditResult{}, apperr.Validation("图片 provider 未显式声明 masked local edit 能力")
}

func (u unsupportedImage) ReconcileResponse(context.Context, string) (string, error) {
	return "unsupported", nil
}
