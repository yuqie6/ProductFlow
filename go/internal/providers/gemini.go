package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

var geminiAspectRatios = []struct {
	label string
	value float64
}{
	{"1:1", 1},
	{"2:3", 2.0 / 3.0},
	{"3:2", 1.5},
	{"3:4", 0.75},
	{"4:3", 4.0 / 3.0},
	{"4:5", 0.8},
	{"5:4", 1.25},
	{"9:16", 9.0 / 16.0},
	{"16:9", 16.0 / 9.0},
	{"21:9", 21.0 / 9.0},
}

var gemini3ImageModels = map[string]struct{}{
	"gemini-3.1-flash-image-preview": {},
	"gemini-3-pro-image-preview":     {},
}

// GeminiImage 调用 Google generateContent，把参考图作为 inlineData 交给模型。
type GeminiImage struct {
	APIKey     string        // 明文，只给进程内调用
	BaseURL    string        // 非空会拒绝：Gemini 暂不支持自定义 Base URL
	Model      string        // generateContent 模型 id
	APIVersion string        // 默认 v1beta
	OutputMIME string        // 请求的输出 MIME；空则按供应商默认
	Transport  jsonRoundTrip // 可注入 HTTP；测试用
}

func geminiRejectCustomBaseURL(baseURL string) error {
	if strings.TrimSpace(baseURL) != "" {
		return fmt.Errorf("Google Gemini 供应商暂不支持自定义 Base URL")
	}
	return nil
}

// Name 实现 ImageClient，返回 "google-gemini-image"。
func (p GeminiImage) Name() string { return "google-gemini-image" }

func (p GeminiImage) ReconcileResponse(context.Context, string) (string, error) {
	return "unsupported", nil
}

// Capability 实现 ImageClient；Gemini 未声明 masked local edit。
func (p GeminiImage) Capability() EditCapability {
	return UnsupportedEditCapability(p.Name())
}

// Edit 实现 ImageClient，拒绝 masked local edit，不打网。
func (p GeminiImage) Edit(context.Context, EditRequest) (EditResult, error) {
	return EditResult{}, apperr.Validation("图片 provider 未显式声明 masked local edit 能力")
}

// Generate 实现 ImageClient，调用 Google generateContent。
func (p GeminiImage) Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error) {
	fallback := openaiSizeFromSpec
	if !req.chat() {
		fallback = pixelSizeFromSpec
	}
	size := req.resolvedSize(fallback)
	bytesData, mime, model, id, err := p.generateContent(ctx, req.Prompt, size, req.Refs, req.statusMapper())
	if err != nil {
		return GenerateResult{}, err
	}
	out := finishGenerateResult(p.Name(), bytesData, mime, model, id, size, "", len(req.Refs))
	out.Images = [][]byte{bytesData}
	if req.chat() {
		out.PromptVersion = "gemini-poster-image-v1"
	}
	return out, nil
}

// generateContent 调 Google generateContent，参考图当 inlineData。classify 把 HTTP 状态收成 unknown/failed。
func (p GeminiImage) generateContent(ctx context.Context, prompt, size string, refs []ImageRef, classify func(int, []byte) error) ([]byte, string, string, string, error) {
	parts := []map[string]any{{"text": prompt}}
	for _, ref := range refs {
		mime := ref.MIME
		if mime == "" {
			mime = sniffMIME(ref.Bytes)
		}
		parts = append(parts, map[string]any{
			"inlineData": map[string]any{
				"mimeType": mime,
				"data":     base64.StdEncoding.EncodeToString(ref.Bytes),
			},
		})
	}
	aspect, imageSize := geminiImageConfig(size, p.Model)
	imageConfig := map[string]any{"aspectRatio": aspect}
	if imageSize != "" {
		imageConfig["imageSize"] = imageSize
	}
	if strings.TrimSpace(p.OutputMIME) != "" {
		imageConfig["outputMimeType"] = p.OutputMIME
	}
	payload := map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
			"imageConfig":        imageConfig,
		},
	}
	body, _ := json.Marshal(payload)
	status, raw, err := p.post(ctx, p.generateURL(), body)
	if err != nil {
		return nil, "", "", "", err
	}
	if err := classify(status, raw); err != nil {
		return nil, "", "", "", err
	}
	return parseGeminiImage(raw, p.Model)
}

func (p GeminiImage) generateURL() string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	version := strings.Trim(strings.TrimSpace(p.APIVersion), "/")
	if version == "" {
		version = "v1beta"
	}
	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = "gemini-2.5-flash-image"
	}
	return fmt.Sprintf("%s/%s/models/%s:generateContent?key=%s", base, version, url.PathEscape(model), url.QueryEscape(p.APIKey))
}

// post 发 JSON POST。网络/超时走 mapTransport，不要把 transport error 当 4xx。
func (p GeminiImage) post(ctx context.Context, target string, body []byte) (int, []byte, error) {
	if p.Transport != nil {
		return p.Transport(ctx, http.MethodPost, target, p.APIKey, body)
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, ErrUnknown
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, mapTransport(err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProviderJSONBytes+1))
	if readErr != nil {
		return resp.StatusCode, raw, ErrUnknown
	}
	if int64(len(raw)) > maxProviderJSONBytes {
		return resp.StatusCode, raw[:maxProviderJSONBytes], ErrUnknown
	}
	return resp.StatusCode, raw, nil
}

// geminiImageConfig 把 WxH 收成 Gemini aspectRatio 与 imageSize。对不上的尺寸回落到 1:1。
func geminiImageConfig(size, model string) (aspect, imageSize string) {
	width, height := 1024, 1024
	if parts := strings.SplitN(size, "x", 2); len(parts) == 2 {
		fmt.Sscanf(parts[0], "%d", &width)
		fmt.Sscanf(parts[1], "%d", &height)
	}
	if width <= 0 || height <= 0 {
		width, height = 1024, 1024
	}
	requested := float64(width) / float64(height)
	aspect = "1:1"
	best := 1e9
	for _, item := range geminiAspectRatios {
		delta := item.value - requested
		if delta < 0 {
			delta = -delta
		}
		if delta < best {
			best = delta
			aspect = item.label
		}
	}
	if _, ok := gemini3ImageModels[model]; ok {
		largest := width
		if height > largest {
			largest = height
		}
		switch {
		case largest <= 1024:
			imageSize = "1K"
		case largest <= 2048:
			imageSize = "2K"
		default:
			imageSize = "4K"
		}
	}
	return aspect, imageSize
}

// parseGeminiImage 从 generateContent 响应抽出第一张 inline 图。没有图或 JSON 坏掉返回 unknown，不要当 failed。
func parseGeminiImage(raw []byte, fallbackModel string) ([]byte, string, string, string, error) {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", "", "", ErrUnknown
	}
	responseID, _ := parsed["responseId"].(string)
	if responseID == "" {
		responseID, _ = parsed["response_id"].(string)
	}
	model, _ := parsed["modelVersion"].(string)
	if model == "" {
		model = fallbackModel
	}
	candidates, _ := parsed["candidates"].([]any)
	for _, candidate := range candidates {
		cand, _ := candidate.(map[string]any)
		content, _ := cand["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			inline, _ := part["inlineData"].(map[string]any)
			if inline == nil {
				inline, _ = part["inline_data"].(map[string]any)
			}
			if inline == nil {
				continue
			}
			b64, _ := inline["data"].(string)
			if b64 == "" {
				continue
			}
			decoded, err := decodeB64(b64)
			if err != nil {
				return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
			}
			mime, _ := inline["mimeType"].(string)
			if mime == "" {
				mime, _ = inline["mime_type"].(string)
			}
			if mime == "" {
				mime = sniffMIME(decoded)
			}
			return decoded, mime, model, responseID, nil
		}
	}
	return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
}
