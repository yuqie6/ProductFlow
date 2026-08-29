package product

func workspaceOptionsJSON() map[string]any {
	types := []map[string]any{}
	catalog := []struct {
		key, title, description string
		order                   int
	}{
		{"hero", "首屏海报图", "搜索列表首图，商品够大能认", 0},
		{"selling_point", "核心卖点图", "详情卖点图，层次清楚，不要空棚贴字", 1},
		{"scene", "场景展示图", "使用场景里拍，商品是主角", 2},
		{"detail", "细节展示图", "材质和工艺特写", 3},
		{"sku", "SKU 展示图", "白底规格对照，方便选款", 4},
		{"dimensions", "尺寸图", "尺寸线清晰的信息图", 5},
		{"specifications", "规格参数图", "参数对照信息图", 6},
		{"after_sales", "售后保障图", "质保退换说明图", 7},
		{"brand_story", "品牌故事图", "品牌故事海报", 8},
		{"precautions", "注意事项图", "使用保养说明图", 9},
		{"certification", "资质认证图", "只用已提供的资质", 10},
		{"faq", "常见问题图", "问答说明图", 11},
		{"factory", "工厂实力图", "只用已提供的工厂画面", 12},
		{"packaging", "包装展示图", "包装全貌", 13},
		{"shipping", "发货物流图", "发货物流说明图", 14},
	}
	for _, item := range catalog {
		types = append(types, map[string]any{
			"key": item.key, "title": item.title, "description": item.description, "order": item.order,
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
