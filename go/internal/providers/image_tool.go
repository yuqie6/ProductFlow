package providers

import (
	"strconv"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
)

var imageToolFieldKeys = []string{
	"model", "quality", "output_format", "output_compression",
	"background", "moderation", "action", "input_fidelity", "partial_images",
}

var defaultImageToolAllowedFields = []string{
	"model", "quality", "output_format", "output_compression",
	"moderation", "action", "input_fidelity", "partial_images",
}

func providerDisplayName(kind string) string {
	switch strings.TrimSpace(kind) {
	case "openai_images", "openai-images":
		return "openai-images"
	case "openai_responses", "openai-responses":
		return "openai-responses"
	case "google_gemini_image", "google-gemini-image":
		return "google-gemini-image"
	default:
		return kind
	}
}

// WorkflowImageToolOptions 对齐 Python _workflow_responses_tool_options：卖点图强制 low fidelity。
func WorkflowImageToolOptions(req graph.ImageRequest, runtime map[string]any, allowed []string) map[string]any {
	spec := req.GenerationSpec
	if spec == nil {
		spec = map[string]any{}
	}
	request := map[string]any{
		"action":        "generate",
		"output_format": "png",
	}
	if quality := openaiQualityFromSpec(spec); quality != "" {
		request["quality"] = quality
	}
	if background, _ := spec["background_intent"].(string); background != "" && background != "auto" {
		request["background"] = background
	}
	if len(req.References) > 0 {
		family := imageTypeFamilyOf(req.ImageTypeKey)
		if family == "infographic" {
			request["input_fidelity"] = "low"
		} else if fidelity, _ := spec["reference_fidelity"].(string); fidelity != "" {
			if fidelity == "medium" {
				request["input_fidelity"] = "high"
			} else {
				request["input_fidelity"] = fidelity
			}
		}
	}
	return filterImageToolOptions(mergeToolOptions(runtime, request), allowed)
}

// filterImageToolOptions 只保留 allowed 字段。空 allowed 用默认集；结果供 Responses image tool。
func filterImageToolOptions(opts map[string]any, allowed []string) map[string]any {
	if len(opts) == 0 {
		return nil
	}
	if len(allowed) == 0 {
		allowed = defaultImageToolAllowedFields
	}
	allow := map[string]struct{}{}
	for _, key := range allowed {
		allow[key] = struct{}{}
	}
	out := map[string]any{}
	for _, key := range imageToolFieldKeys {
		if _, ok := allow[key]; !ok {
			continue
		}
		value, exists := opts[key]
		if !exists || value == nil {
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeToolOptions(runtime, request map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range runtime {
		out[key] = value
	}
	for key, value := range request {
		out[key] = value
	}
	return out
}

func imageTypeFamilyOf(key string) string {
	switch key {
	case "certification", "factory":
		return "evidence"
	case "selling_point", "dimensions", "specifications", "after_sales",
		"precautions", "faq", "shipping", "brand_story":
		return "infographic"
	default:
		return "photography"
	}
}

// pixelSizeFromSpec 从 generation_spec 读 WxH。缺宽高回落 1024x1024。
func pixelSizeFromSpec(spec map[string]any) string {
	longest := 2048
	switch strings.TrimSpace(asToolString(spec["resolution_tier"])) {
	case "standard":
		longest = 1024
	case "ultra":
		longest = 4096
	case "high", "":
		longest = 2048
	}
	ratio := 1.0
	if raw := asToolString(spec["aspect_ratio"]); strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		w, _ := strconv.ParseFloat(parts[0], 64)
		h, _ := strconv.ParseFloat(parts[1], 64)
		if w > 0 && h > 0 {
			ratio = w / h
		}
	}
	if ratio >= 1 {
		return strconv.Itoa(longest) + "x" + strconv.Itoa(maxInt(1, int(float64(longest)/ratio+0.5)))
	}
	return strconv.Itoa(maxInt(1, int(float64(longest)*ratio+0.5))) + "x" + strconv.Itoa(longest)
}

// openaiSizeFromPixels 把像素尺寸收到 Images API 认识的档位（如 1024x1024）。对不上回落 1024x1024。
func openaiSizeFromPixels(size string) string {
	width, height := 1024, 1024
	if parts := strings.SplitN(size, "x", 2); len(parts) == 2 {
		fmtAtoi := func(s string) int {
			n, _ := strconv.Atoi(strings.TrimSpace(s))
			return n
		}
		width, height = fmtAtoi(parts[0]), fmtAtoi(parts[1])
	}
	if width <= 0 || height <= 0 {
		return "1024x1024"
	}
	ratio := float64(width) / float64(height)
	if ratio > 1.25 {
		return "1536x1024"
	}
	if ratio < 0.8 {
		return "1024x1536"
	}
	return "1024x1024"
}

func asToolString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
