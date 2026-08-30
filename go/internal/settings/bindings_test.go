package settings

import "testing"

func TestImageCapabilityForKind(t *testing.T) {
	if imageCapabilityForKind("openai_responses") != "image_responses" {
		t.Fatal(imageCapabilityForKind("openai_responses"))
	}
	if imageCapabilityForKind("openai_images") != "image_images" {
		t.Fatal(imageCapabilityForKind("openai_images"))
	}
	if imageCapabilityForKind("google_gemini_image") != "image_google_gemini" {
		t.Fatal(imageCapabilityForKind("google_gemini_image"))
	}
	if imageCapabilityForKind("mock") != "" {
		t.Fatal("mock")
	}
}
