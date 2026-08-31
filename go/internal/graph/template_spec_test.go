package graph

import "testing"

func TestGenerationSpecForShotUsesFamilyPolicyAndTypeAspect(t *testing.T) {
	hero, err := generationSpecForShot(DirectCreateImageType{Key: "hero"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hero["text_policy"] != "none" || hero["text_language"] != nil || hero["aspect_ratio"] != "3:4" {
		t.Fatalf("hero %+v", hero)
	}
	selling, err := generationSpecForShot(DirectCreateImageType{Key: "selling_point"}, map[string]any{
		"text_policy": "none", "aspect_ratio": "1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selling["text_policy"] != "required" || selling["text_language"] != "zh-CN" || selling["aspect_ratio"] != "1:1" {
		t.Fatalf("selling_point keeps per-shot ratio and forces copy: %+v", selling)
	}
	scene, err := generationSpecForShot(DirectCreateImageType{Key: "scene", AspectRatio: "16:9"}, map[string]any{
		"text_policy": "required", "text_language": "en-US",
	})
	if err != nil {
		t.Fatal(err)
	}
	if scene["text_policy"] != "required" || scene["text_language"] != "en-US" || scene["aspect_ratio"] != "16:9" {
		t.Fatalf("photography may inherit shared copy; ratio from type field: %+v", scene)
	}
}

func TestFillDefaultNodeConfigUsesImageTypeDefaults(t *testing.T) {
	hero := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{"image_type_key": "hero"})
	spec, _ := hero["generation_spec"].(map[string]any)
	if spec["text_policy"] != "none" || spec["aspect_ratio"] != "3:4" {
		t.Fatalf("hero fill %+v", spec)
	}
	selling := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{"image_type_key": "selling_point"})
	spec, _ = selling["generation_spec"].(map[string]any)
	if spec["text_policy"] != "required" || spec["text_language"] != "zh-CN" || spec["aspect_ratio"] != "3:4" {
		t.Fatalf("selling_point fill %+v", spec)
	}
	kept := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{
		"image_type_key":  "selling_point",
		"generation_spec": map[string]any{"aspect_ratio": "1:1", "text_policy": "none"},
	})
	spec, _ = kept["generation_spec"].(map[string]any)
	if spec["text_policy"] != "none" || spec["aspect_ratio"] != "1:1" {
		t.Fatalf("explicit spec must not be overwritten: %+v", spec)
	}
}
