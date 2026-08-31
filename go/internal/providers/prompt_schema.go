package providers

// 与 Python Pydantic GeneratedCreativeBrief / GeneratedVisualOverlay / ListingPromptPayload 对齐的 json_schema。
// Responses parse 依赖 strict schema，不能改成 json_object。
var briefJSONSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"goal", "design_goals", "required_copy", "prohibitions", "fact_gaps"},
	"properties": map[string]any{
		"goal":          nonEmptyTextSchema(),
		"design_goals":  boundedTextArraySchema(1, 4, 320),
		"required_copy": boundedTextArraySchema(0, 6, 120),
		"prohibitions":  boundedStringArraySchema(0, 0),
		"fact_gaps":     boundedTextArraySchema(0, 6, 160),
	},
}

var overlayJSONSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"style", "colors", "prohibitions"},
	"properties": map[string]any{
		"style": boundedTextArraySchema(3, 5, 160),
		"colors": map[string]any{
			"type":     "array",
			"minItems": 1,
			"maxItems": 5,
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"role", "value", "label"},
				"properties": map[string]any{
					"role":  map[string]any{"type": "string", "minLength": 1, "maxLength": 80},
					"value": map[string]any{"type": "string", "pattern": "^#[0-9A-Fa-f]{6}$"},
					"label": map[string]any{"type": "string", "maxLength": 255},
				},
			},
		},
		"prohibitions": boundedStringArraySchema(0, 0),
	},
}

var listingPromptJSONSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required": []any{
		"schema_version", "shared_rules", "design_goal", "product_fidelity",
		"creative_boundary", "composition", "content", "text", "atmosphere", "visual_variant_key",
	},
	"properties": map[string]any{
		"schema_version":    map[string]any{"type": "integer", "const": 1},
		"shared_rules":      boundedStringArraySchema(1, 1),
		"design_goal":       nonEmptyTextSchema(),
		"product_fidelity":  fidelitySchema,
		"creative_boundary": boundedStringArraySchema(0, 1),
		"composition":       compositionSchema,
		"content":           contentSchema,
		"text":              textSchema,
		"atmosphere":        atmosphereSchema,
		"visual_variant_key": map[string]any{
			"anyOf": []any{
				map[string]any{"type": "string", "minLength": 1, "maxLength": 80, "pattern": `^[a-z0-9][a-z0-9_-]*$`},
				map[string]any{"type": "null"},
			},
		},
	},
}

var fidelitySchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"complex_structure", "product_present", "picture_in_picture", "requirements"},
	"properties": map[string]any{
		"complex_structure":  map[string]any{"type": "boolean"},
		"product_present":    map[string]any{"type": "boolean"},
		"picture_in_picture": map[string]any{"type": "string", "enum": []any{"none", "allowed", "required"}},
		"requirements":       boundedStringArraySchema(1, 2),
	},
}

var compositionSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"viewpoint", "product_share_percent", "layout", "copy_regions"},
	"properties": map[string]any{
		"viewpoint":             nonEmptyTextSchema(),
		"product_share_percent": map[string]any{"type": "number", "exclusiveMinimum": 0, "maximum": 100},
		"layout":                nonEmptyTextSchema(),
		"copy_regions":          boundedTextArraySchema(0, 4, 240),
	},
}

var contentSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"focus", "selling_points", "background", "decorations"},
	"properties": map[string]any{
		"focus":          boundedTextArraySchema(1, 3, 240),
		"selling_points": boundedTextArraySchema(0, 5, 240),
		"background":     nonEmptyTextSchema(),
		"decorations":    boundedTextArraySchema(0, 4, 160),
	},
}

var textSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"headline", "subtitle", "body"},
	"properties": map[string]any{
		"headline": nullableStringSchema(),
		"subtitle": nullableStringSchema(),
		"body":     nullableStringSchema(),
	},
}

var atmosphereSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"keywords", "lighting"},
	"properties": map[string]any{
		"keywords": boundedTextArraySchema(2, 5, 120),
		"lighting": nonEmptyTextSchema(),
	},
}

// sourceNoteJSONSchema 是创建页看图起草商品说明的 strict Responses schema。
// fields 按商品动态出项；value 允许空串，留给商家补。不能改成 json_object。
var sourceNoteJSONSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"visible", "fields"},
	"properties": map[string]any{
		"visible": emptyableTextSchema(2000),
		"fields": map[string]any{
			"type":     "array",
			"maxItems": 12,
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"label", "value"},
				"properties": map[string]any{
					"label": map[string]any{"type": "string", "minLength": 1, "maxLength": 16},
					"value": emptyableTextSchema(160),
				},
			},
		},
	},
}

func nonEmptyTextSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 4000}
}

func emptyableTextSchema(maxLength int) map[string]any {
	return map[string]any{"type": "string", "maxLength": maxLength}
}

func boundedStringArraySchema(minItems, maxItems int) map[string]any {
	return boundedTextArraySchema(minItems, maxItems, 240)
}

func boundedTextArraySchema(minItems, maxItems, maxLength int) map[string]any {
	return map[string]any{
		"type": "array", "minItems": minItems, "maxItems": maxItems,
		"items": map[string]any{"type": "string", "minLength": 1, "maxLength": maxLength},
	}
}

func nullableStringSchema() map[string]any {
	return map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}}
}
