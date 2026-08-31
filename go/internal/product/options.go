package product

import "github.com/yuqie6/productflow/prompts"

// workspaceOptionsJSON 给出创建页图种目录与数量上限，权威来自 prompts.ImageTypes 与 intake 常量。
func workspaceOptionsJSON() map[string]any {
	types := []map[string]any{}
	for _, item := range prompts.ImageTypes() {
		types = append(types, map[string]any{
			"key": item.Key, "title": item.Title, "description": item.Description, "order": item.Order,
		})
	}
	return map[string]any{
		"schema_version": 1,
		"image_types":    types,
		"limits": map[string]any{
			"min_image_types":          1,
			"default_images_per_type":  2,
			"min_images_per_type":      1,
			"max_images_per_type":      6,
			"max_total_images":         30,
			"min_reference_images":     1,
			"max_reference_images":     6,
			"allowed_image_mime_types": []string{"image/png", "image/jpeg", "image/webp"},
		},
	}
}
