package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// OpenAIImages 调用 /v1/images/generations；有参考图或局部编辑走 /v1/images/edits。
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
	prompt := graph.CompileImageModelPrompt(req)
	quality := openaiQualityFromSpec(req.GenerationSpec)
	var bytesData []byte
	var mime, model, id string
	var err error
	if len(req.References) > 0 {
		bytesData, mime, model, id, err = p.edit(ctx, prompt, size, quality, graphRefsToParts(req.References), nil, mapGraphStatus)
	} else {
		bytesData, mime, model, id, err = p.generate(ctx, prompt, size, quality, mapGraphStatus)
	}
	if err != nil {
		return graph.ImageResult{}, err
	}
	return finishImageResult(p.Name(), bytesData, mime, model, id, size, quality, len(req.References)), nil
}

func (p OpenAIImages) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	parts := chatImageParts(req, true)
	var bytesData []byte
	var mime, model string
	var err error
	if len(parts) > 0 {
		bytesData, mime, model, _, err = p.edit(ctx, req.Prompt, size, p.Quality, parts, nil, mapChatStatus)
	} else {
		bytesData, mime, model, _, err = p.generate(ctx, req.Prompt, size, p.Quality, mapChatStatus)
	}
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	return imagesession.ChatResult{
		Bytes: bytesData, MIME: mime, Model: model,
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
	parts := []imagePart{{Bytes: req.SourceBytes, MIME: req.SourceMIME, Filename: "source.png"}}
	for i, ref := range req.ReferenceBytes {
		parts = append(parts, imagePart{Bytes: ref, MIME: sniffMIME(ref), Filename: fmt.Sprintf("reference-%d.png", i+1)})
	}
	bytesData, mime, model, id, err := p.edit(ctx, req.Instruction, "1024x1024", p.Quality, parts, req.MaskPNG, mapChatStatus)
	if err != nil {
		return localedit.EditResult{}, err
	}
	return localedit.EditResult{Bytes: bytesData, MIME: mime, Model: model, ResponseID: id, ProviderStatus: "completed"}, nil
}

func (p OpenAIImages) generate(ctx context.Context, prompt, size, quality string, classify func(int, []byte) error) ([]byte, string, string, string, error) {
	if quality == "" {
		quality = p.Quality
	}
	req := map[string]any{
		"model": p.Model, "prompt": prompt, "size": size, "n": 1, "response_format": "b64_json",
	}
	if quality != "" {
		req["quality"] = quality
	}
	if p.Style != "" {
		req["style"] = p.Style
	}
	body, _ := json.Marshal(req)
	status, raw, err := p.post(ctx, endpoint(p.BaseURL, "/v1/images/generations"), body)
	if err != nil {
		return nil, "", "", "", err
	}
	if status >= 400 && (quality != "" || p.Style != "") {
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
	return p.call(ctx, http.MethodPost, url, body)
}

func (p OpenAIImages) call(ctx context.Context, method, url string, body []byte) (int, []byte, error) {
	return p.callTyped(ctx, method, url, "application/json", body)
}

func (p OpenAIImages) callTyped(ctx context.Context, method, url, contentType string, body []byte) (int, []byte, error) {
	if p.Transport != nil {
		return p.Transport(ctx, method, url, p.APIKey, body)
	}
	if contentType == "" {
		contentType = "application/json"
	}
	return doJSON(ctx, newHTTPClient(), method, url, p.APIKey, bytes.NewReader(body), contentType)
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

func openaiQualityFromSpec(spec map[string]any) string {
	intent, _ := spec["quality_intent"].(string)
	switch intent {
	case "draft":
		return "low"
	case "standard":
		return "medium"
	case "high":
		return "high"
	default:
		return ""
	}
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

// OpenAIResponses 只用 /v1/responses 出图；4xx 不回退 Images edits/generations。
type OpenAIResponses struct {
	OpenAIImages
}

func (p OpenAIResponses) Name() string { return "openai_responses" }

var (
	responsesPollInterval = 2 * time.Second
	imageToolOptionalKeys = []string{
		"model", "quality", "output_format", "output_compression",
		"background", "moderation", "action", "input_fidelity", "partial_images",
	}
)

func (p OpenAIResponses) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	size := openaiSizeFromSpec(req.GenerationSpec)
	prompt := graph.CompileImageModelPrompt(req)
	opts := generationSpecToolOptions(req.GenerationSpec)
	bytesData, mime, model, id, err := p.generateResponses(ctx, prompt, size, opts, req.References, nil, mapGraphStatus)
	if err != nil {
		return graph.ImageResult{}, err
	}
	quality := openaiQualityFromSpec(req.GenerationSpec)
	return finishImageResult(p.Name(), bytesData, mime, model, id, size, quality, len(req.References)), nil
}

func (p OpenAIResponses) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	refs := chatGraphRefs(req, true)
	bytesData, mime, model, id, err := p.generateResponses(ctx, req.Prompt, size, req.ToolOptions, refs, req.PreviousResponseID, mapChatStatus)
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	return imagesession.ChatResult{
		Bytes: bytesData, MIME: mime, Model: model, ResponseID: id,
		ProviderStatus: "completed", OutputJSON: map[string]any{"status": "completed"},
	}, nil
}

func imageGenerationTool(size string, opts map[string]any) map[string]any {
	tool := map[string]any{
		"type":   "image_generation",
		"action": "generate",
		"size":   size,
	}
	for _, key := range imageToolOptionalKeys {
		value, ok := opts[key]
		if !ok || value == nil {
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		tool[key] = value
	}
	return tool
}

func (p OpenAIResponses) createResponses(ctx context.Context, input any, size string, toolOptions map[string]any, previousID *string) (int, []byte, error) {
	tool := imageGenerationTool(size, toolOptions)
	base := map[string]any{
		"model": p.Model,
		"input": input,
		"tools": []map[string]any{tool},
	}
	if previousID != nil && strings.TrimSpace(*previousID) != "" {
		base["previous_response_id"] = strings.TrimSpace(*previousID)
	}
	withChoice := cloneJSONMap(base)
	withChoice["tool_choice"] = map[string]any{"type": "image_generation"}
	payloads := []map[string]any{withChoice, base}
	var status int
	var raw []byte
	var err error
	url := endpoint(p.BaseURL, "/v1/responses")
	for i, payload := range payloads {
		body, _ := json.Marshal(payload)
		status, raw, err = p.call(ctx, http.MethodPost, url, body)
		if err != nil {
			return 0, nil, err
		}
		if status < 400 || status >= 500 || status == http.StatusTooManyRequests || i == len(payloads)-1 {
			return status, raw, nil
		}
	}
	return status, raw, nil
}

func (p OpenAIResponses) generateResponses(ctx context.Context, prompt, size string, toolOptions map[string]any, refs []graph.ReferenceImage, previousID *string, classify func(int, []byte) error) ([]byte, string, string, string, error) {
	input := responsesInput(prompt, refs)
	status, raw, err := p.createResponses(ctx, input, size, toolOptions, previousID)
	if err != nil {
		return nil, "", "", "", err
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		return nil, "", "", "", classify(status, raw)
	}
	if status >= 400 {
		return nil, "", "", "", classify(status, raw)
	}
	retrieved := false
	for {
		if err := classify(status, raw); err != nil {
			return nil, "", "", "", err
		}
		parsed, err := decodeProviderObject(raw)
		if err != nil {
			return nil, "", "", "", err
		}
		if bytesData, mime, model, id, ok := extractResponsesImage(parsed, p.Model); ok {
			return bytesData, mime, model, id, nil
		}
		if responsesTerminalFailure(parsed) {
			return nil, "", "", "", imagesession.ErrMissingOutput
		}
		if !responsesShouldRetrieve(parsed) {
			return nil, "", "", "", responsesNoImageError(parsed)
		}
		id := responsesID(parsed)
		if id == "" {
			return nil, "", "", "", imagesession.ErrMissingOutput
		}
		if retrieved && !responsesNeedsPoll(parsed) {
			return nil, "", "", "", responsesNoImageError(parsed)
		}
		if responsesNeedsPoll(parsed) {
			if err := sleepPoll(ctx); err != nil {
				return nil, "", "", "", err
			}
		}
		status, raw, err = p.call(ctx, http.MethodGet, endpoint(p.BaseURL, "/v1/responses/"+id), nil)
		if err != nil {
			return nil, "", "", "", err
		}
		if status >= 500 || status == http.StatusTooManyRequests {
			return nil, "", "", "", classify(status, raw)
		}
		if status >= 400 {
			if responsesNeedsPoll(parsed) {
				return nil, "", "", "", graph.ErrProviderUnknown()
			}
			return nil, "", "", "", responsesNoImageError(parsed)
		}
		retrieved = true
	}
}

func sleepPoll(ctx context.Context) error {
	timer := time.NewTimer(responsesPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return graph.ErrProviderUnknown()
	case <-timer.C:
		return nil
	}
}

func parseResponsesImage(raw []byte, fallbackModel string) ([]byte, string, string, string, error) {
	parsed, err := decodeProviderObject(raw)
	if err != nil {
		return nil, "", "", "", err
	}
	if bytesData, mime, model, id, ok := extractResponsesImage(parsed, fallbackModel); ok {
		return bytesData, mime, model, id, nil
	}
	return nil, "", "", "", responsesNoImageError(parsed)
}

func decodeProviderObject(raw []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	var parsed map[string]any
	if err := json.Unmarshal(trimmed, &parsed); err == nil && parsed != nil {
		return unwrapResponse(parsed), nil
	}
	if payload := lastSSEJSON(trimmed); len(payload) > 0 {
		if err := json.Unmarshal(payload, &parsed); err == nil && parsed != nil {
			return unwrapResponse(parsed), nil
		}
	}
	return nil, graph.ErrProviderUnknown()
}

func unwrapResponse(parsed map[string]any) map[string]any {
	inner, ok := parsed["response"].(map[string]any)
	if ok {
		if _, hasOutput := inner["output"]; hasOutput {
			return inner
		}
		if _, hasID := inner["id"]; hasID {
			return inner
		}
	}
	return parsed
}

func lastSSEJSON(raw []byte) []byte {
	if !bytes.Contains(raw, []byte("data:")) {
		return nil
	}
	var last []byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		last = payload
	}
	return last
}

func extractResponsesImage(parsed map[string]any, fallbackModel string) ([]byte, string, string, string, bool) {
	id := responsesID(parsed)
	model, _ := parsed["model"].(string)
	if model == "" {
		model = fallbackModel
	}
	for _, obj := range responsesOutput(parsed) {
		if obj["type"] != "image_generation_call" {
			continue
		}
		result := imageCallResult(obj)
		if result == "" {
			continue
		}
		decoded, err := decodeB64(result)
		if err != nil {
			continue
		}
		return decoded, sniffMIME(decoded), model, id, true
	}
	return nil, "", "", "", false
}

func responsesID(parsed map[string]any) string {
	id, _ := parsed["id"].(string)
	return id
}

func responsesStatus(parsed map[string]any) string {
	status, _ := parsed["status"].(string)
	return strings.ToLower(status)
}

func responsesOutput(parsed map[string]any) []map[string]any {
	switch typed := parsed["output"].(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{typed}
	default:
		return nil
	}
}

func imageCallResult(obj map[string]any) string {
	if s, ok := obj["result"].(string); ok {
		return s
	}
	nested, ok := obj["result"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"b64_json", "b64", "image_base64"} {
		if s, ok := nested[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func responsesNoImageError(parsed map[string]any) error {
	if responsesHasTextOutput(parsed) {
		return imagesession.ErrTextOutput
	}
	return imagesession.ErrMissingOutput
}

func responsesHasTextOutput(parsed map[string]any) bool {
	for _, obj := range responsesOutput(parsed) {
		if obj["type"] != "message" {
			continue
		}
		switch typed := obj["content"].(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return true
			}
		case []any:
			for _, item := range typed {
				content, ok := item.(map[string]any)
				if !ok {
					continue
				}
				kind, _ := content["type"].(string)
				text, _ := content["text"].(string)
				if kind == "output_text" && strings.TrimSpace(text) != "" {
					return true
				}
			}
		}
	}
	return false
}

func responsesShouldRetrieve(parsed map[string]any) bool {
	if responsesNeedsPoll(parsed) {
		return true
	}
	for _, obj := range responsesOutput(parsed) {
		if obj["type"] == "image_generation_call" && imageCallResult(obj) == "" {
			return true
		}
	}
	return false
}

func responsesNeedsPoll(parsed map[string]any) bool {
	status := responsesStatus(parsed)
	if status == "queued" || status == "in_progress" {
		return true
	}
	for _, obj := range responsesOutput(parsed) {
		if obj["type"] != "image_generation_call" {
			continue
		}
		callStatus, _ := obj["status"].(string)
		callStatus = strings.ToLower(callStatus)
		if imageCallResult(obj) == "" && (callStatus == "in_progress" || callStatus == "generating" || callStatus == "") {
			return true
		}
	}
	return false
}

func responsesTerminalFailure(parsed map[string]any) bool {
	switch responsesStatus(parsed) {
	case "failed", "cancelled", "canceled", "incomplete", "expired":
		return true
	}
	for _, obj := range responsesOutput(parsed) {
		if obj["type"] != "image_generation_call" {
			continue
		}
		callStatus, _ := obj["status"].(string)
		if strings.ToLower(callStatus) == "failed" {
			return true
		}
	}
	return false
}
