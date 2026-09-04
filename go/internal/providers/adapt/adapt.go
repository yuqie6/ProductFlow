package adapt

import (
	"context"
	"errors"
	"fmt"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/providers"
	"github.com/yuqie6/productflow/internal/settings"
)

// GraphImage 把中立 ImageClient 接到图运行 ImageProvider。listing prompt 在 graph 侧 compile。
func GraphImage(client providers.ImageClient) graph.ImageProvider {
	return graphImage{client: client}
}

type graphImage struct {
	client providers.ImageClient
}

func (a graphImage) Name() string { return a.client.Name() }

func (a graphImage) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	out, err := a.client.Generate(ctx, providers.GenerateRequest{
		Prompt:         graph.CompileImageModelPrompt(req),
		Count:          1,
		Refs:           graphRefs(req.References),
		ImageTypeKey:   req.ImageTypeKey,
		GenerationSpec: req.GenerationSpec,
		Mode:           providers.ModeWorkflow,
	})
	if err != nil {
		return graph.ImageResult{}, mapGraphErr(err)
	}
	return graph.ImageResult{
		Bytes: out.Bytes, MIME: out.MIME, Model: out.Model, ResponseID: out.ResponseID,
		ProviderStatus: out.ProviderStatus, Width: out.Width, Height: out.Height,
		EffectiveParameters: out.EffectiveParameters,
	}, nil
}

func mapGraphErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, providers.ErrUnknown) ||
		errors.Is(err, providers.ErrTimeout) ||
		errors.Is(err, providers.ErrConnection) ||
		errors.Is(err, providers.ErrProvider5xx) {
		return graph.ErrProviderUnknown()
	}
	return err
}

func graphRefs(refs []graph.ReferenceImage) []providers.ImageRef {
	out := make([]providers.ImageRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, providers.ImageRef{Bytes: ref.Bytes, MIME: ref.MIME, Filename: ref.Filename})
	}
	return out
}

// Chat 把中立 ImageClient 接到连续生图 ChatProvider。模板渲染留在 imagesession。
func Chat(client providers.ImageClient, store *settings.Store) imagesession.ChatProvider {
	return chatImage{client: client, store: store}
}

type chatImage struct {
	client providers.ImageClient
	store  *settings.Store
}

func (a chatImage) Name() string { return a.client.Name() }

func (a chatImage) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	prompt := req.Prompt
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	if a.store != nil {
		tmpl, err := a.store.ImageChatPromptTemplate(ctx)
		if err != nil {
			return imagesession.ChatResult{}, err
		}
		prompt = imagesession.RenderChatPrompt(tmpl, req.Prompt, size, req.HistoryBlock)
	}
	out, err := a.client.Generate(ctx, providers.GenerateRequest{
		Prompt:             prompt,
		Size:               size,
		Count:              req.Count,
		Refs:               chatRefs(req),
		ToolOptions:        req.ToolOptions,
		PreviousResponseID: req.PreviousResponseID,
		Mode:               providers.ModeChat,
	})
	if err != nil {
		return imagesession.ChatResult{}, mapChatErr(err)
	}
	images := out.Images
	if len(images) == 0 && len(out.Bytes) > 0 {
		images = [][]byte{out.Bytes}
	}
	return imagesession.ChatResult{
		Bytes: out.Bytes, Images: images, MIME: out.MIME, Model: out.Model,
		PromptVersion: out.PromptVersion, ResponseID: out.ResponseID,
		ProviderStatus: out.ProviderStatus, OutputJSON: map[string]any{"status": "completed"},
	}, nil
}

func chatRefs(req imagesession.ChatRequest) []providers.ImageRef {
	var out []providers.ImageRef
	if len(req.BaseBytes) > 0 {
		out = append(out, providers.ImageRef{Bytes: req.BaseBytes, Filename: "base.png"})
	}
	for i, data := range req.ReferenceBytes {
		out = append(out, providers.ImageRef{Bytes: data, Filename: fmt.Sprintf("reference-%d.png", i+1)})
	}
	return out
}

func mapChatErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, providers.ErrTimeout):
		return imagesession.ErrTimeout
	case errors.Is(err, providers.ErrConnection):
		return imagesession.ErrConnection
	case errors.Is(err, providers.ErrProvider5xx):
		return imagesession.ErrProvider5xx
	case errors.Is(err, providers.ErrRateLimit):
		return imagesession.ErrRateLimit
	case errors.Is(err, providers.ErrTextOutput):
		return imagesession.ErrTextOutput
	case errors.Is(err, providers.ErrMissingOutput):
		return imagesession.ErrMissingOutput
	case errors.Is(err, providers.ErrUnknown):
		return imagesession.ErrUnknown()
	default:
		return err
	}
}

// Reconciler 把中立 ImageClient 接到连续生图对账缝。
func Reconciler(client providers.ImageClient) imagesessionReconciler {
	return imagesessionReconciler{client: client}
}

type imagesessionReconciler struct {
	client providers.ImageClient
}

func (r imagesessionReconciler) ReconcileResponse(ctx context.Context, responseID string) (string, error) {
	return r.client.ReconcileResponse(ctx, responseID)
}

// LocalEdit 把中立 ImageClient 接到局部编辑 Provider。
func LocalEdit(client providers.ImageClient) localedit.Provider {
	return localEdit{client: client}
}

type localEdit struct {
	client providers.ImageClient
}

func (a localEdit) Capability() localedit.Capability {
	cap := a.client.Capability()
	return localedit.Capability{
		ProviderName: cap.ProviderName, Supported: cap.Supported, Mode: cap.Mode,
		Operations: cap.Operations, RequiresMask: cap.RequiresMask,
		MaxReferenceImages: cap.MaxReferenceImages, Reason: cap.Reason,
	}
}

func (a localEdit) Edit(ctx context.Context, req localedit.EditRequest) (localedit.EditResult, error) {
	out, err := a.client.Edit(ctx, providers.EditRequest{
		Instruction: req.Instruction, Size: req.Size,
		SourceBytes: req.SourceBytes, SourceMIME: req.SourceMIME, MaskPNG: req.MaskPNG,
		ReferenceBytes: req.ReferenceBytes, Operation: req.Operation,
	})
	if err != nil {
		return localedit.EditResult{}, err
	}
	return localedit.EditResult{
		Bytes: out.Bytes, MIME: out.MIME, Model: out.Model,
		ResponseID: out.ResponseID, ProviderStatus: out.ProviderStatus,
	}, nil
}
