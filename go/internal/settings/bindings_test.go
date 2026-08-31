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

func TestImageMaskEditForKind(t *testing.T) {
	caps := []string{"image_responses", "image_mask_edit"}
	if !imageMaskEditForKind("openai_responses", caps) {
		t.Fatal("responses binding dropped image_mask_edit")
	}
	if !imageMaskEditForKind("openai_images", caps) {
		t.Fatal("images binding dropped image_mask_edit")
	}
	if imageMaskEditForKind("google_gemini_image", caps) {
		t.Fatal("gemini must not inherit the OpenAI masked edit contract")
	}
	if imageMaskEditForKind("openai_responses", []string{"image_responses"}) {
		t.Fatal("masked edit must remain opt-in")
	}
}
