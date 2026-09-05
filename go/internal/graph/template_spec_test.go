package graph

import "testing"

func TestGenerationSpecForShotUsesTypeAspectWithoutText(t *testing.T) {
	hero, err := generationSpecForShot(DirectCreateImageType{Key: "hero"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hero["text_policy"] != nil || hero["aspect_ratio"] != "3:4" {
		t.Fatalf("hero %+v", hero)
	}
	selling, err := generationSpecForShot(DirectCreateImageType{Key: "selling_point"}, map[string]any{
		"aspect_ratio": "1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selling["text_policy"] != nil || selling["aspect_ratio"] != "1:1" {
		t.Fatalf("selling_point keeps per-shot ratio: %+v", selling)
	}
	scene, err := generationSpecForShot(DirectCreateImageType{Key: "scene", AspectRatio: "16:9"}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if scene["text_policy"] != nil || scene["aspect_ratio"] != "16:9" {
		t.Fatalf("ratio from type field: %+v", scene)
	}
}

func TestFillDefaultNodeConfigUsesImageTypeDefaults(t *testing.T) {
	hero := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{"image_type_key": "hero"})
	spec, _ := hero["generation_spec"].(map[string]any)
	if spec["text_policy"] != nil || spec["aspect_ratio"] != "3:4" {
		t.Fatalf("hero fill %+v", spec)
	}
	selling := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{"image_type_key": "selling_point"})
	spec, _ = selling["generation_spec"].(map[string]any)
	if spec["text_policy"] != nil || spec["aspect_ratio"] != "3:4" {
		t.Fatalf("selling_point fill %+v", spec)
	}
	kept := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{
		"image_type_key":  "selling_point",
		"generation_spec": map[string]any{"aspect_ratio": "1:1"},
	})
	spec, _ = kept["generation_spec"].(map[string]any)
	if spec["text_policy"] != nil || spec["aspect_ratio"] != "1:1" {
		t.Fatalf("explicit spec must not be overwritten: %+v", spec)
	}
}

func TestPromptDefaultsOwnTextSettings(t *testing.T) {
	for _, key := range []string{"hero", "selling_point"} {
		config := FillDefaultNodeConfig(NodeImagePrompt, map[string]any{"image_type_key": key})
		if !documentValueEqual(config["text_settings"], defaultTextSettings(key)) {
			t.Fatalf("settings %+v", config)
		}
	}
}
