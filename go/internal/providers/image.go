package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// OpenAIImages 调用 /v1/images/generations；局部编辑走 /v1/images/edits。
type OpenAIImages struct {
	Kind      string
	APIKey    string
	BaseURL   string
	Model     string
	Quality   string
	Style     string
	MaskEdit  bool
	Transport jsonRoundTrip
}

func (p OpenAIImages) Name() string {
	if p.Kind != "" {
		return p.Kind
	}
	return "openai_images"
}

func (p OpenAIImages) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	size := openaiSizeFromSpec(req.GenerationSpec)
	prompt := compilePrompt(req)
	bytesData, mime, model, id, err := p.generate(ctx, prompt, size, mapGraphStatus)
	if err != nil {
		return graph.ImageResult{}, err
	}
	return graph.ImageResult{Bytes: bytesData, MIME: mime, Model: model, ResponseID: id, ProviderStatus: "completed"}, nil
}

func (p OpenAIImages) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	bytesData, mime, model, id, err := p.generate(ctx, req.Prompt, size, mapChatStatus)
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	return imagesession.ChatResult{
		Bytes: bytesData, MIME: mime, Model: model, ResponseID: id,
		ProviderStatus: "completed", OutputJSON: map[string]any{"status": "completed"},
	}, nil
}

func (p OpenAIImages) Capability() localedit.Capability {
	if p.MaskEdit {
		return localedit.SupportedCapability(p.Name())
	}
	return localedit.UnsupportedCapability(p.Name())
}

func (p OpenAIImages) Edit(ctx context.Context, req localedit.EditRequest) (localedit.EditResult, error) {
	if !p.MaskEdit {
		return localedit.EditResult{}, apperr.Validation("图片 provider 未显式声明 masked local edit 能力")
	}
	if len(req.MaskPNG) == 0 {
		return localedit.EditResult{}, apperr.Validation("局部编辑缺少遮罩")
	}
	size := "1024x1024"
	payload := map[string]any{
		"model": p.Model, "prompt": req.Instruction, "size": size, "n": 1, "response_format": "b64_json",
	}
	body, _ := json.Marshal(payload)
	status, raw, err := p.post(ctx, endpoint(p.BaseURL, "/v1/images/edits"), body)
	if err != nil {
		return localedit.EditResult{}, err
	}
	if err := mapChatStatus(status, raw); err != nil {
		return localedit.EditResult{}, err
	}
	bytesData, mime, model, id, err := parseImageResponse(raw, p.Model)
	if err != nil {
		return localedit.EditResult{}, err
	}
	return localedit.EditResult{Bytes: bytesData, MIME: mime, Model: model, ResponseID: id, ProviderStatus: "completed"}, nil
}

func (p OpenAIImages) generate(ctx context.Context, prompt, size string, classify func(int, []byte) error) ([]byte, string, string, string, error) {
	req := map[string]any{
		"model": p.Model, "prompt": prompt, "size": size, "n": 1, "response_format": "b64_json",
	}
	if p.Quality != "" {
		req["quality"] = p.Quality
	}
	if p.Style != "" {
		req["style"] = p.Style
	}
	body, _ := json.Marshal(req)
	status, raw, err := p.post(ctx, endpoint(p.BaseURL, "/v1/images/generations"), body)
	if err != nil {
		return nil, "", "", "", err
	}
	if status >= 400 && (p.Quality != "" || p.Style != "") {
		delete(req, "quality")
		delete(req, "style")
		body, _ = json.Marshal(req)
		status, raw, err = p.post(ctx, endpoint(p.BaseURL, "/v1/images/generations"), body)
		if err != nil {
			return nil, "", "", "", err
		}
	}
	if err := classify(status, raw); err != nil {
		return nil, "", "", "", err
	}
	return parseImageResponse(raw, p.Model)
}

func (p OpenAIImages) post(ctx context.Context, url string, body []byte) (int, []byte, error) {
	if p.Transport != nil {
		return p.Transport(ctx, "POST", url, p.APIKey, body)
	}
	return doJSON(ctx, newHTTPClient(), "POST", url, p.APIKey, bytes.NewReader(body), "application/json")
}

func parseImageResponse(raw []byte, fallbackModel string) ([]byte, string, string, string, error) {
	var parsed struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Data  []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", "", "", graph.ErrProviderUnknown()
	}
	if len(parsed.Data) == 0 || parsed.Data[0].B64JSON == "" {
		return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
	}
	decoded, err := decodeB64(parsed.Data[0].B64JSON)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
	}
	model := parsed.Model
	if model == "" {
		model = fallbackModel
	}
	return decoded, sniffMIME(decoded), model, parsed.ID, nil
}

func sniffMIME(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "image/png"
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8 {
		return "image/jpeg"
	}
	if len(data) >= 12 && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return "image/png"
}

func compilePrompt(req graph.ImageRequest) string {
	if len(req.Prompt) > 0 {
		raw, err := json.Marshal(req.Prompt)
		if err == nil {
			return string(raw)
		}
	}
	if req.NodeTitle != "" {
		return req.NodeTitle
	}
	return "commercial product listing image"
}

func openaiSizeFromSpec(spec map[string]any) string {
	ratio := 1.0
	if raw, ok := spec["aspect_ratio"].(string); ok && strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		w, _ := strconv.ParseFloat(parts[0], 64)
		h, _ := strconv.ParseFloat(parts[1], 64)
		if w > 0 && h > 0 {
			ratio = w / h
		}
	}
	if ratio > 1.25 {
		return "1536x1024"
	}
	if ratio < 0.8 {
		return "1024x1536"
	}
	return "1024x1024"
}

// OpenAIResponses 用 Responses API 出图；解析失败时回退到 Images generations。
type OpenAIResponses struct {
	OpenAIImages
}

func (p OpenAIResponses) Name() string { return "openai_responses" }

func (p OpenAIResponses) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	size := openaiSizeFromSpec(req.GenerationSpec)
	prompt := compilePrompt(req)
	body, _ := json.Marshal(map[string]any{
		"model": p.Model,
		"input": prompt,
		"tools": []map[string]any{{
			"type": "image_generation",
			"size": size,
		}},
	})
	status, raw, err := p.post(ctx, endpoint(p.BaseURL, "/v1/responses"), body)
	if err != nil {
		return graph.ImageResult{}, err
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		return graph.ImageResult{}, mapGraphStatus(status, raw)
	}
	if status >= 400 {
		return p.OpenAIImages.GenerateImage(ctx, req)
	}
	bytesData, mime, model, id, err := parseResponsesImage(raw, p.Model)
	if err != nil {
		return graph.ImageResult{}, err
	}
	return graph.ImageResult{Bytes: bytesData, MIME: mime, Model: model, ResponseID: id, ProviderStatus: "completed"}, nil
}

func (p OpenAIResponses) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	body, _ := json.Marshal(map[string]any{
		"model": p.Model,
		"input": req.Prompt,
		"tools": []map[string]any{{
			"type": "image_generation",
			"size": req.Size,
		}},
	})
	status, raw, err := p.post(ctx, endpoint(p.BaseURL, "/v1/responses"), body)
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		return imagesession.ChatResult{}, mapChatStatus(status, raw)
	}
	if status >= 400 {
		return p.OpenAIImages.Generate(ctx, req)
	}
	bytesData, mime, model, id, err := parseResponsesImage(raw, p.Model)
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	return imagesession.ChatResult{
		Bytes: bytesData, MIME: mime, Model: model, ResponseID: id,
		ProviderStatus: "completed", OutputJSON: map[string]any{"status": "completed"},
	}, nil
}

func parseResponsesImage(raw []byte, fallbackModel string) ([]byte, string, string, string, error) {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", "", "", graph.ErrProviderUnknown()
	}
	id, _ := parsed["id"].(string)
	model, _ := parsed["model"].(string)
	if model == "" {
		model = fallbackModel
	}
	output, _ := parsed["output"].([]any)
	for _, item := range output {
		obj, _ := item.(map[string]any)
		if obj["type"] != "image_generation_call" {
			continue
		}
		result, _ := obj["result"].(string)
		if result == "" {
			continue
		}
		decoded, err := decodeB64(result)
		if err != nil {
			continue
		}
		return decoded, sniffMIME(decoded), model, id, nil
	}
	return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
}
