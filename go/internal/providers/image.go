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
	Kind      string        // openai_images；空时 Name() 返回 openai-images
	APIKey    string        // 明文，只给进程内调用
	BaseURL   string        // 空则用 OpenAI 默认
	Model     string        // Images API 模型 id
	Quality   string        // Images API quality；空则按请求 spec
	Style     string        // Images API style
	MaskEdit  bool          // false 时局部编辑 Capability 为 unsupported
	Transport jsonRoundTrip // 可注入 HTTP；测试用
}

// Name 实现 graph.ImageProvider；Kind 为空时返回 "openai-images"。
func (p OpenAIImages) Name() string {
	if p.Kind != "" {
		return providerDisplayName(p.Kind)
	}
	return "openai-images"
}

// GenerateImage 实现 graph.ImageProvider。有参考图走 /v1/images/edits；不可证明的失败经 asGraphUnknown 标 unknown。
func (p OpenAIImages) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	size := openaiSizeFromSpec(req.GenerationSpec)
	prompt := graph.CompileImageModelPrompt(req)
	quality := openaiQualityFromSpec(req.GenerationSpec)
	var bytesData []byte
	var mime, model, id string
	var err error
	if len(req.References) > 0 {
		bytesData, mime, model, id, err = p.edit(ctx, prompt, size, quality, graphRefsToParts(req.References), nil, 1, mapGraphStatus)
	} else {
		bytesData, mime, model, id, err = p.generate(ctx, prompt, size, quality, mapGraphStatus)
	}
	if err != nil {
		return graph.ImageResult{}, asGraphUnknown(err)
	}
	return finishImageResult(p.Name(), bytesData, mime, model, id, size, quality, len(req.References)), nil
}

// Generate 实现 imagesession.ChatProvider。有参考图走 edits；否则走 generations。
// 已证明 4xx 返回 Validation；超时/断流/非图响应走 unknown，调用方不得当失败自动重试。
func (p OpenAIImages) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	n := clampImageN(req.Count)
	call := p
	call.Model, call.Quality = chatImagesOverrides(req.ToolOptions, p.Model, p.Quality)
	parts := chatImageParts(req, true)
	var images [][]byte
	var mime, model, id string
	var err error
	if len(parts) > 0 {
		images, mime, model, id, err = call.editN(ctx, req.Prompt, size, call.Quality, parts, nil, n, mapChatStatus)
	} else {
		images, mime, model, id, err = call.generateN(ctx, req.Prompt, size, call.Quality, n, mapChatStatus)
	}
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	first := []byte(nil)
	if len(images) > 0 {
		first = images[0]
	}
	_ = id
	return imagesession.ChatResult{
		Bytes: first, Images: images, MIME: mime, Model: model,
		ProviderStatus: "completed", OutputJSON: map[string]any{"status": "completed"},
	}, nil
}

// Capability 实现 localedit.Provider；仅 MaskEdit 为 true 时声明 masked local edit。
func (p OpenAIImages) Capability() localedit.Capability {
	if p.MaskEdit {
		return localedit.SupportedCapability(p.Name())
	}
	return localedit.UnsupportedCapability(p.Name())
}

// Edit 实现 localedit.Provider，走 /v1/images/edits；未声明 mask 能力时拒绝且不打网。
// 未声明 mask 或缺少遮罩返回 Validation；超时/断流/非图响应走 unknown，调用方不得当失败自动重试。
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
	size := openaiSizeFromPixels(req.Size)
	if size == "" {
		size = "1024x1024"
	}
	bytesData, mime, model, id, err := p.edit(ctx, req.Instruction, size, p.Quality, parts, req.MaskPNG, 1, mapChatStatus)
	if err != nil {
		return localedit.EditResult{}, err
	}
	return localedit.EditResult{Bytes: bytesData, MIME: mime, Model: model, ResponseID: id, ProviderStatus: "completed"}, nil
}

func (p OpenAIImages) generate(ctx context.Context, prompt, size, quality string, classify func(int, []byte) error) ([]byte, string, string, string, error) {
	images, mime, model, id, err := p.generateN(ctx, prompt, size, quality, 1, classify)
	if err != nil {
		return nil, "", "", "", err
	}
	if len(images) == 0 {
		return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
	}
	return images[0], mime, model, id, nil
}

// generateN 调 /v1/images/generations 一次出 n 张。classify 把 HTTP 状态收成 unknown/failed；4xx 不重试。
func (p OpenAIImages) generateN(ctx context.Context, prompt, size, quality string, n int, classify func(int, []byte) error) ([][]byte, string, string, string, error) {
	if quality == "" {
		quality = p.Quality
	}
	n = clampImageN(n)
	req := map[string]any{
		"model": p.Model, "prompt": prompt, "size": size, "n": n, "response_format": "b64_json",
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
	return parseImageResponses(raw, p.Model)
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

// parseImageResponses 从 Images API JSON 抽出 b64 图。没有 data 或解码失败返回 unknown。
func parseImageResponses(raw []byte, fallbackModel string) ([][]byte, string, string, string, error) {
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
	if len(parsed.Data) == 0 {
		return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
	}
	images := make([][]byte, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.B64JSON == "" {
			continue
		}
		decoded, err := decodeB64(item.B64JSON)
		if err != nil {
			return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
		}
		images = append(images, decoded)
	}
	if len(images) == 0 {
		return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
	}
	model := parsed.Model
	if model == "" {
		model = fallbackModel
	}
	return images, sniffMIME(images[0]), model, parsed.ID, nil
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

func clampImageN(n int) int {
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return n
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
	if ratio <= 0.8 {
		return "1024x1536"
	}
	return "1024x1024"
}

// OpenAIResponses 只用 /v1/responses 出图；4xx 不回退 Images edits/generations。
type OpenAIResponses struct {
	OpenAIImages
	Background    bool           // responses_background_enabled
	ToolRuntime   map[string]any // 设置页默认 tool 选项
	AllowedFields []string       // image tool 允许字段白名单
}

// Name 实现 graph.ImageProvider，返回 "openai-responses"。
func (p OpenAIResponses) Name() string { return "openai-responses" }

// ReconcileResponse 查询 /v1/responses/{id}。空 id 返回 "unsupported"；4xx/5xx 或证据不足返回 "unknown"。
// 无法证明时返回 unknown 且不带 error，调用方不得当失败自动重试。
func (p OpenAIResponses) ReconcileResponse(ctx context.Context, responseID string) (string, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return "unsupported", nil
	}
	status, raw, err := p.call(ctx, http.MethodGet, endpoint(p.BaseURL, "/v1/responses/"+responseID), nil)
	if err != nil {
		return "unknown", nil
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		return "unknown", nil
	}
	if status >= 400 {
		return "unknown", nil
	}
	parsed, err := decodeProviderObject(raw)
	if err != nil {
		return "unknown", nil
	}
	if responsesTerminalFailure(parsed) {
		return "failed", nil
	}
	if _, _, _, _, ok := extractResponsesImage(parsed, p.Model); ok {
		return "applied", nil
	}
	return "unknown", nil
}

var (
	responsesPollInterval = 2 * time.Second
	imageToolOptionalKeys = []string{
		"model", "quality", "output_format", "output_compression",
		"background", "moderation", "action", "input_fidelity", "partial_images",
	}
)

const imageToolInputMaskKey = "input_image_mask"

// GenerateImage 实现 graph.ImageProvider，只用 /v1/responses；不可证明的失败标 unknown。
func (p OpenAIResponses) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	size := openaiSizeFromSpec(req.GenerationSpec)
	prompt := graph.CompileImageModelPrompt(req)
	opts := WorkflowImageToolOptions(req, p.ToolRuntime, p.AllowedFields)
	bytesData, mime, model, id, err := p.generateResponses(ctx, prompt, size, opts, req.References, nil, mapGraphStatus)
	if err != nil {
		return graph.ImageResult{}, asGraphUnknown(err)
	}
	quality := openaiQualityFromSpec(req.GenerationSpec)
	return finishImageResult(p.Name(), bytesData, mime, model, id, size, quality, len(req.References)), nil
}

// Generate 实现 imagesession.ChatProvider，只用 /v1/responses。
// 已证明 4xx 返回 Validation；超时/断流/非图响应走 unknown，调用方不得当失败自动重试。
func (p OpenAIResponses) Generate(ctx context.Context, req imagesession.ChatRequest) (imagesession.ChatResult, error) {
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	refs := chatGraphRefs(req, true)
	opts := filterImageToolOptions(mergeToolOptions(p.ToolRuntime, req.ToolOptions), p.AllowedFields)
	bytesData, mime, model, id, err := p.generateResponses(ctx, req.Prompt, size, opts, refs, req.PreviousResponseID, mapChatStatus)
	if err != nil {
		return imagesession.ChatResult{}, err
	}
	return imagesession.ChatResult{
		Bytes: bytesData, MIME: mime, Model: model, ResponseID: id,
		ProviderStatus: "completed", OutputJSON: map[string]any{"status": "completed"},
	}, nil
}

// Edit 实现 localedit.Provider，走 Responses image tool；未声明 mask 能力时拒绝且不打网。
// 未声明 mask、缺遮罩或原图返回 Validation；超时/断流/非图响应走 unknown，调用方不得当失败自动重试。
func (p OpenAIResponses) Edit(ctx context.Context, req localedit.EditRequest) (localedit.EditResult, error) {
	if !p.MaskEdit {
		return localedit.EditResult{}, apperr.Validation("图片 provider 未显式声明 masked local edit 能力")
	}
	if len(req.MaskPNG) == 0 {
		return localedit.EditResult{}, apperr.Validation("局部编辑缺少遮罩")
	}
	if len(req.SourceBytes) == 0 {
		return localedit.EditResult{}, apperr.Validation("局部编辑缺少原图")
	}
	size := openaiSizeFromPixels(req.Size)
	if size == "" {
		size = "1024x1024"
	}
	refs := []graph.ReferenceImage{{Bytes: req.SourceBytes, MIME: req.SourceMIME, Filename: "source.png"}}
	for i, value := range req.ReferenceBytes {
		refs = append(refs, graph.ReferenceImage{
			Bytes: value, MIME: sniffMIME(value), Filename: fmt.Sprintf("reference-%d.png", i+1),
		})
	}
	opts := filterImageToolOptions(p.ToolRuntime, p.AllowedFields)
	required := map[string]any{
		"action":              "edit",
		imageToolInputMaskKey: map[string]any{"image_url": dataURL("image/png", req.MaskPNG)},
	}
	bytesData, mime, model, id, err := p.generateResponsesRequired(
		ctx, req.Instruction, size, opts, refs, nil, mapChatStatus, required,
	)
	if err != nil {
		return localedit.EditResult{}, err
	}
	return localedit.EditResult{
		Bytes: bytesData, MIME: mime, Model: model, ResponseID: id, ProviderStatus: "completed",
	}, nil
}

func imageGenerationTool(size string, opts map[string]any) map[string]any {
	tool := map[string]any{"type": "image_generation", "size": size}
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
	if mask, ok := opts[imageToolInputMaskKey]; ok && mask != nil {
		tool[imageToolInputMaskKey] = mask
	}
	return tool
}

// createResponsesRequired 发 /v1/responses，强制 image_generation tool。previousID 用于连续对话，没有则开新响应。
func (p OpenAIResponses) createResponsesRequired(ctx context.Context, input any, size string, toolOptions map[string]any, previousID *string, requiredToolOptions map[string]any) (int, []byte, error) {
	tool := imageGenerationTool(size, mergeToolOptions(toolOptions, requiredToolOptions))
	payload := map[string]any{
		"model": p.Model,
		"input": input,
		"tools": []map[string]any{cloneJSONMap(tool)},
	}
	if previousID != nil && strings.TrimSpace(*previousID) != "" {
		payload["previous_response_id"] = strings.TrimSpace(*previousID)
	}
	if p.Background {
		payload["background"] = true
	}
	url := endpoint(p.BaseURL, "/v1/responses")
	var status int
	var raw []byte
	for range 8 {
		body, _ := json.Marshal(payload)
		var err error
		status, raw, err = p.call(ctx, http.MethodPost, url, body)
		if err != nil {
			return 0, nil, err
		}
		if status < 400 || status >= 500 || status == http.StatusTooManyRequests {
			return status, raw, nil
		}
		if background, _ := payload["background"].(bool); background && isBackgroundUnsupported(status, raw) {
			delete(payload, "background")
			continue
		}
		if tools, ok := payload["tools"].([]map[string]any); ok && len(tools) > 0 && hasRemovableImageToolFields(tools[0], requiredToolOptions) {
			payload["tools"] = []map[string]any{imageGenerationTool(size, requiredToolOptions)}
			continue
		}
		return status, raw, nil
	}
	return status, raw, nil
}

func hasRemovableImageToolFields(tool map[string]any, required map[string]any) bool {
	for _, key := range imageToolOptionalKeys {
		if _, protected := required[key]; protected {
			continue
		}
		if _, ok := tool[key]; ok {
			return true
		}
	}
	return false
}

func isBackgroundUnsupported(status int, body []byte) bool {
	if status < 400 || status >= 500 {
		return false
	}
	message := strings.ToLower(string(body))
	if !strings.Contains(message, "background") {
		return false
	}
	for _, marker := range []string{"unknown", "unsupported", "unexpected", "extra", "unrecognized", "not support", "not_supported"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (p OpenAIResponses) generateResponses(ctx context.Context, prompt, size string, toolOptions map[string]any, refs []graph.ReferenceImage, previousID *string, classify func(int, []byte) error) ([]byte, string, string, string, error) {
	return p.generateResponsesRequired(ctx, prompt, size, toolOptions, refs, previousID, classify, nil)
}

// generateResponsesRequired 调 Responses 出图。4xx 不回退 Images API；证据不足标 unknown。
func (p OpenAIResponses) generateResponsesRequired(ctx context.Context, prompt, size string, toolOptions map[string]any, refs []graph.ReferenceImage, previousID *string, classify func(int, []byte) error, requiredToolOptions map[string]any) ([]byte, string, string, string, error) {
	input := responsesInput(prompt, refs)
	status, raw, err := p.createResponsesRequired(ctx, input, size, toolOptions, previousID, requiredToolOptions)
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
			return nil, "", "", "", responsesNoImageError(parsed)
		}
		if !responsesShouldRetrieve(parsed) {
			return nil, "", "", "", responsesNoImageError(parsed)
		}
		id := responsesID(parsed)
		if id == "" {
			return nil, "", "", "", responsesNoImageError(parsed)
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
		return mapTransport(ctx.Err())
	case <-timer.C:
		return nil
	}
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

// extractResponsesImage 从 Responses JSON 找第一张图。找不到返回 ok=false，调用方再看是否有文本输出。
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

// responsesHasTextOutput 报告响应是否只有文字没有图。有字无图时调用方应标 failed 而不是 unknown。
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
