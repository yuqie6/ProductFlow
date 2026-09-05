package providers

import "testing"

func TestGenerationOptionsMatchAdapterControls(t *testing.T) {
	images := generationOptions(OpenAIImages{})
	if len(images["aspect_ratio"]) != 3 || len(images["quality_intent"]) != 3 {
		t.Fatal(images)
	}
	if len(images["resolution_tier"]) != 0 || len(images["reference_fidelity"]) != 0 || len(images["background_intent"]) != 0 {
		t.Fatal("Images advertised unused controls", images)
	}
	responses := generationOptions(OpenAIResponses{AllowedFields: []string{"background"}})
	if len(responses["background_intent"]) != 3 || len(responses["quality_intent"]) != 0 || len(responses["reference_fidelity"]) != 0 {
		t.Fatal(responses)
	}
	for model := range gemini3ImageModels {
		options := generationOptions(GeminiImage{Model: model})
		if len(options["resolution_tier"]) != 3 || len(options["quality_intent"]) != 0 {
			t.Fatal(options)
		}
	}
	if len(generationOptions(GeminiImage{Model: "gemini-2.5-flash-image"})["resolution_tier"]) != 0 {
		t.Fatal("unconsumed resolution exposed")
	}
	if len(generationOptions(MockImage{})) != 0 {
		t.Fatal("mock has no real generation controls")
	}
}
