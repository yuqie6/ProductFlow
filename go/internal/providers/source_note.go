package providers

func MockSourceNotePayload() map[string]any {
	return map[string]any{
		"visible": "厚壁玻璃密封瓶，球盖锁扣。瓶身通透能看见内容，厚壁更耐磕。",
		"fields": []any{
			map[string]any{"label": "材质", "value": "玻璃"},
			map[string]any{"label": "容量", "value": ""},
			map[string]any{"label": "价格", "value": ""},
		},
	}
}
