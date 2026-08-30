package providers

// 与 Python Pydantic GeneratedCreativeBrief / GeneratedVisualOverlay / ListingPromptPayload 对齐的 json_schema。
// Responses parse 依赖 strict schema，不能改成 json_object。
var briefJSONSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"goal", "design_goals", "required_copy", "prohibitions"},
	"properties": map[string]any{
		"goal":          nonEmptyTextSchema(),
		"design_goals":  stringArraySchema(),
		"required_copy": stringArraySchema(),
		"prohibitions":  stringArraySchema(),
	},
}

var overlayJSONSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"style", "colors", "prohibitions"},
	"properties": map[string]any{
		"style": map[string]any{"type": "array", "minItems": 1, "items": nonEmptyTextSchema()},
		"colors": map[string]any{
			"type":     "array",
			"minItems": 1,
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
		"prohibitions": stringArraySchema(),
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
		"shared_rules":      map[string]any{"type": "array", "minItems": 1, "items": nonEmptyTextSchema()},
		"design_goal":       nonEmptyTextSchema(),
		"product_fidelity":  fidelitySchema,
		"creative_boundary": stringArraySchema(),
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
		"requirements":       map[string]any{"type": "array", "minItems": 1, "items": nonEmptyTextSchema()},
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
		"copy_regions":          stringArraySchema(),
	},
}

var contentSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"focus", "selling_points", "background", "decorations"},
	"properties": map[string]any{
		"focus":          map[string]any{"type": "array", "minItems": 1, "items": nonEmptyTextSchema()},
		"selling_points": stringArraySchema(),
		"background":     nonEmptyTextSchema(),
		"decorations":    stringArraySchema(),
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
		"keywords": map[string]any{"type": "array", "minItems": 1, "items": nonEmptyTextSchema()},
		"lighting": nonEmptyTextSchema(),
	},
}

func nonEmptyTextSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 4000}
}

func stringArraySchema() map[string]any {
	return map[string]any{"type": "array", "items": nonEmptyTextSchema()}
}

func nullableStringSchema() map[string]any {
	return map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}}
}
