package providers

import "context"

// GenerationOptions describes only controls consumed by the current workflow adapter.
func (l LiveImage) GenerationOptions(ctx context.Context) (map[string][]string, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return generationOptions(p), nil
}

func generationOptions(p ImageClient) map[string][]string {
	out := map[string][]string{}
	openai := func() {
		out["aspect_ratio"] = []string{"1:1", "2:3", "3:2"}
		out["quality_intent"] = []string{"draft", "standard", "high"}
	}
	switch provider := p.(type) {
	case OpenAIResponses:
		openai()
		allowed := provider.AllowedFields
		if len(allowed) == 0 {
			allowed = defaultImageToolAllowedFields
		}
		delete(out, "quality_intent")
		for _, field := range allowed {
			switch field {
			case "quality":
				out["quality_intent"] = []string{"draft", "standard", "high"}
			case "input_fidelity":
				out["reference_fidelity"] = []string{"low", "high"}
			case "background":
				out["background_intent"] = []string{"auto", "opaque", "transparent"}
			}
		}
	case OpenAIImages:
		openai()
	case GeminiImage:
		for _, ratio := range geminiAspectRatios {
			out["aspect_ratio"] = append(out["aspect_ratio"], ratio.label)
		}
		if _, ok := gemini3ImageModels[provider.Model]; ok {
			out["resolution_tier"] = []string{"standard", "high", "ultra"}
		}
	}
	return out
}
