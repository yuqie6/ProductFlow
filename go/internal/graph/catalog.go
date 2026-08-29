package graph

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

var forbiddenConfigKeys = map[string]struct{}{
	"prompt_plan_key":  {},
	"image_plan_key":   {},
	"prompt_plan_keys": {},
	"image_plan_keys":  {},
}

var imageAssetRoles = map[string]struct{}{
	"product_identity": {},
	"environment":      {},
	"style":            {},
	"evidence":         {},
}

var visualOverlayKeys = map[string]struct{}{
	"style":        {},
	"colors":       {},
	"prohibitions": {},
}

type inputContract struct {
	DataType      EdgeDataType
	Role          EdgeRole
	MaxCount      *int
	RequiredToRun bool
}

type configField struct {
	key       string
	valueKind string
	control   string
	choices   []string
	minValue  *int
	maxValue  *int
	maxLength *int
	fields    []configField
}

func i(v int) *int { return &v }

func hid(key, kind string) configField {
	return configField{key: key, valueKind: kind, control: "hidden"}
}

func fld(key, kind, control string, opts ...func(*configField)) configField {
	item := configField{key: key, valueKind: kind, control: control}
	if control == "" {
		switch kind {
		case "boolean":
			item.control = "checkbox"
		case "number":
			item.control = "number"
		case "string_list":
			item.control = "string_list"
		case "object_or_null":
			item.control = "optional_object"
		case "object":
			item.control = "group"
		default:
			item.control = "text"
		}
	}
	for _, opt := range opts {
		opt(&item)
	}
	return item
}

func withChoices(choices ...string) func(*configField) {
	return func(f *configField) { f.choices = choices }
}

func withMinMax(minV, maxV int) func(*configField) {
	return func(f *configField) {
		f.minValue = i(minV)
		f.maxValue = i(maxV)
	}
}

func withMaxLen(n int) func(*configField) {
	return func(f *configField) { f.maxLength = i(n) }
}

func withFields(fields ...configField) func(*configField) {
	return func(f *configField) { f.fields = fields }
}

var outputType = map[NodeType]EdgeDataType{
	NodeProductSource:    DataProductFacts,
	NodeImageAsset:       DataImageAsset,
	NodeCreativeBrief:    DataCreativeBrief,
	NodeVisualSystem:     DataVisualSystem,
	NodePromptGeneration: DataPrompt,
	NodeImageGeneration:  DataImageAsset,
}

func one(n int) *int { return i(n) }

var acceptance = map[[2]string]inputContract{
	{string(DataProductFacts), string(NodeCreativeBrief)}:     {DataProductFacts, RoleFacts, one(1), false},
	{string(DataImageAsset), string(NodeCreativeBrief)}:       {DataImageAsset, RoleReference, nil, false},
	{string(DataProductFacts), string(NodeVisualSystem)}:      {DataProductFacts, RoleFacts, one(1), false},
	{string(DataImageAsset), string(NodeVisualSystem)}:        {DataImageAsset, RoleReference, nil, false},
	{string(DataProductFacts), string(NodePromptGeneration)}:  {DataProductFacts, RoleFacts, nil, false},
	{string(DataImageAsset), string(NodePromptGeneration)}:    {DataImageAsset, RoleReference, nil, false},
	{string(DataCreativeBrief), string(NodePromptGeneration)}: {DataCreativeBrief, RoleBrief, nil, false},
	{string(DataVisualSystem), string(NodePromptGeneration)}:  {DataVisualSystem, RoleVisualGuidance, one(1), false},
	{string(DataImageAsset), string(NodeImageGeneration)}:     {DataImageAsset, RoleReference, nil, false},
	{string(DataVisualSystem), string(NodeImageGeneration)}:   {DataVisualSystem, RoleVisualGuidance, one(1), false},
	{string(DataPrompt), string(NodeImageGeneration)}:         {DataPrompt, RolePrompt, one(1), true},
}

func visualOverlayFields() []configField {
	return []configField{
		fld("style", "string_list", ""),
		fld("colors", "object_list", "visual_background", withMaxLen(32), withFields(
			fld("role", "string", "", withMaxLen(80)),
			fld("value", "string", "", withMaxLen(7)),
			fld("label", "string", "", withMaxLen(255)),
		)),
		fld("prohibitions", "string_list", ""),
	}
}

func promptFields() []configField {
	return []configField{
		hid("schema_version", "number"),
		fld("design_goal", "string", "textarea"),
		fld("shared_rules", "string_list", ""),
		fld("creative_boundary", "string_list", ""),
		fld("product_fidelity", "object", "group", withFields(
			fld("complex_structure", "boolean", ""),
			fld("product_present", "boolean", ""),
			fld("picture_in_picture", "string", "select", withChoices("none", "allowed", "required")),
			fld("requirements", "string_list", ""),
		)),
		fld("composition", "object", "group", withFields(
			fld("viewpoint", "string", ""),
			fld("product_share_percent", "number", "", withMinMax(1, 100)),
			fld("layout", "string", "textarea"),
			fld("copy_regions", "string_list", ""),
		)),
		fld("content", "object", "group", withFields(
			fld("focus", "string_list", ""),
			fld("selling_points", "string_list", ""),
			fld("background", "string", "textarea"),
			fld("decorations", "string_list", ""),
		)),
		fld("text", "object", "group", withFields(
			fld("headline", "string_or_null", ""),
			fld("subtitle", "string_or_null", ""),
			fld("body", "string_or_null", "textarea"),
		)),
		fld("atmosphere", "object", "group", withFields(
			fld("keywords", "string_list", ""),
			fld("lighting", "string", "textarea"),
		)),
		hid("visual_variant_key", "string_or_null"),
	}
}

func generationSpecFields() []configField {
	return []configField{
		fld("aspect_ratio", "string", "aspect_ratio"),
		fld("resolution_tier", "string", "select", withChoices("standard", "high", "ultra")),
		fld("quality_intent", "string", "select", withChoices("draft", "standard", "high")),
		fld("reference_fidelity", "string", "select", withChoices("low", "medium", "high")),
		fld("background_intent", "string", "select", withChoices("auto", "opaque", "transparent")),
		fld("text_policy", "string", "select", withChoices("none", "allow", "required")),
		fld("text_language", "string_or_null", "", withMaxLen(80)),
	}
}

func deliverySpecFields() []configField {
	return []configField{
		fld("width", "number", "", withMinMax(1, 16384)),
		fld("height", "number", "", withMinMax(1, 16384)),
		fld("format", "string", "select", withChoices("png", "jpeg", "webp")),
		hid("max_byte_size", "number_or_null"),
		fld("fit", "string", "select", withChoices("contain", "cover")),
		fld("background_color", "string_or_null", "", withMaxLen(7)),
		fld("crop_anchor", "string_or_null", "select", withChoices("center", "top", "bottom", "left", "right")),
	}
}

func nodeConfigFields(nodeType NodeType) ([]configField, bool) {
	switch nodeType {
	case NodeProductSource:
		return []configField{
			hid("source_product_id", "string_or_null"),
			hid("fact_set_version_id", "string_or_null"),
		}, true
	case NodeImageAsset:
		choices := make([]string, 0, len(imageAssetRoles))
		for role := range imageAssetRoles {
			choices = append(choices, role)
		}
		sort.Strings(choices)
		return []configField{
			fld("role", "string_or_null", "select", withChoices(choices...), withMaxLen(120)),
			fld("label", "string_or_null", "", withMaxLen(255)),
		}, true
	case NodeCreativeBrief:
		return []configField{
			hid("title", "string"),
			fld("goal", "string", "textarea"),
			fld("design_goals", "string_list", ""),
			fld("required_copy", "string_list", ""),
			fld("prohibitions", "string_list", ""),
		}, true
	case NodeVisualSystem:
		return []configField{
			hid("visual_system_version_id", "string_or_null"),
			fld("visual_overlay", "object_or_null", "group", withFields(visualOverlayFields()...)),
			hid("visual_overrides", "object"),
		}, true
	case NodePromptGeneration:
		return []configField{
			hid("image_type_key", "string"),
			fld("prompt", "object", "group", withFields(promptFields()...)),
		}, true
	case NodeImageGeneration:
		return []configField{
			hid("image_type_key", "string"),
			fld("variation_instruction", "string_or_null", "textarea", withMaxLen(4000)),
			fld("generation_spec", "object", "group", withFields(generationSpecFields()...)),
			fld("delivery_spec", "object_or_null", "optional_object", withFields(deliverySpecFields()...)),
			fld("visual_overlay", "object_or_null", "group", withFields(visualOverlayFields()...)),
			hid("visual_overrides", "object"),
		}, true
	default:
		return nil, false
	}
}

func GraphNodeOutputType(nodeType NodeType) (EdgeDataType, error) {
	out, ok := outputType[nodeType]
	if !ok {
		return "", apperr.Validation("不支持的 Graph 操作")
	}
	return out, nil
}

func GraphInputContract(sourceType, targetType NodeType) (inputContract, bool) {
	out, err := GraphNodeOutputType(sourceType)
	if err != nil {
		return inputContract{}, false
	}
	c, ok := acceptance[[2]string{string(out), string(targetType)}]
	return c, ok
}

func RequireGraphConnection(sourceType, targetType NodeType) (inputContract, error) {
	c, ok := GraphInputContract(sourceType, targetType)
	if !ok {
		return inputContract{}, apperr.Validation("节点类型不兼容，不能创建连线")
	}
	return c, nil
}

func acceptedInputs(nodeType NodeType) []inputContract {
	var out []inputContract
	for key, c := range acceptance {
		if key[1] == string(nodeType) {
			out = append(out, c)
		}
	}
	return out
}

func runRequiredInputs(nodeType NodeType) []inputContract {
	var out []inputContract
	for _, c := range acceptedInputs(nodeType) {
		if c.RequiredToRun {
			out = append(out, c)
		}
	}
	return out
}

func rejectForbiddenKeys(config map[string]any) error {
	var illegal []string
	for key := range config {
		if _, ok := forbiddenConfigKeys[key]; ok {
			illegal = append(illegal, key)
		}
	}
	if len(illegal) == 0 {
		return nil
	}
	sort.Strings(illegal)
	return apperr.Validation("节点配置不能包含拓扑字段: " + strings.Join(illegal, ", "))
}

func NormalizeNodeConfig(nodeType NodeType, config map[string]any) (map[string]any, error) {
	payload := cloneMap(config)
	if payload == nil {
		payload = map[string]any{}
	}
	if err := rejectForbiddenKeys(payload); err != nil {
		return nil, err
	}
	fields, ok := nodeConfigFields(nodeType)
	if !ok {
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
	if err := validateConfigFields(fields, payload, ""); err != nil {
		return nil, err
	}
	if nodeType == NodeImageGeneration {
		if raw, exists := payload["generation_spec"]; exists {
			normalized, err := normalizeGenerationSpec(raw)
			if err != nil {
				return nil, err
			}
			payload["generation_spec"] = normalized
		}
		if raw, exists := payload["delivery_spec"]; exists && raw != nil {
			normalized, err := normalizeDeliverySpec(raw)
			if err != nil {
				return nil, err
			}
			payload["delivery_spec"] = normalized
		}
	}
	return payload, nil
}

func CatalogVisualOverlay(overlay map[string]any) map[string]any {
	if overlay == nil || len(overlay) == 0 {
		return nil
	}
	filtered := map[string]any{}
	for key, value := range overlay {
		if _, ok := visualOverlayKeys[key]; ok {
			filtered[key] = cloneValue(value)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func validateConfigFields(fields []configField, payload map[string]any, path string) error {
	known := map[string]struct{}{}
	for _, item := range fields {
		known[item.key] = struct{}{}
	}
	var unknown []string
	for key := range payload {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		scope := path
		if scope == "" {
			scope = "节点配置"
		}
		return apperr.Validation(fmt.Sprintf("%s包含未登记字段: %s", scope, strings.Join(unknown, ", ")))
	}
	for _, item := range fields {
		value, exists := payload[item.key]
		if !exists {
			continue
		}
		fieldPath := item.key
		if path != "" {
			fieldPath = path + "." + item.key
		}
		if err := validateConfigField(item, value, fieldPath); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigField(item configField, value any, path string) error {
	if item.control == "hidden" && (item.valueKind == "object" || item.valueKind == "object_or_null" || item.valueKind == "object_list") {
		return nil
	}
	if !matchesValueKind(item.valueKind, value) {
		return apperr.Validation(fmt.Sprintf("节点配置字段 %s 不符合 value_kind=%s", path, item.valueKind))
	}
	if value == nil {
		return nil
	}
	if len(item.choices) > 0 {
		if s, ok := value.(string); ok {
			found := false
			for _, choice := range item.choices {
				if s == choice {
					found = true
					break
				}
			}
			if !found {
				return apperr.Validation(fmt.Sprintf("节点配置字段 %s 不在 choices 范围内", path))
			}
		}
	}
	if item.minValue != nil || item.maxValue != nil {
		n, ok := asFiniteNumber(value)
		if !ok {
			return apperr.Validation(fmt.Sprintf("节点配置字段 %s 不是可比较的 number", path))
		}
		if item.minValue != nil && n < float64(*item.minValue) {
			return apperr.Validation(fmt.Sprintf("节点配置字段 %s 不能小于 %d", path, *item.minValue))
		}
		if item.maxValue != nil && n > float64(*item.maxValue) {
			return apperr.Validation(fmt.Sprintf("节点配置字段 %s 不能大于 %d", path, *item.maxValue))
		}
	}
	if item.maxLength != nil {
		if n, ok := valueLen(value); ok && n > *item.maxLength {
			return apperr.Validation(fmt.Sprintf("节点配置字段 %s 不能超过 max_length=%d", path, *item.maxLength))
		}
	}
	if (item.valueKind == "object" || item.valueKind == "object_or_null") && len(item.fields) > 0 {
		if child, ok := asMap(value); ok {
			return validateConfigFields(item.fields, child, path)
		}
	} else if item.valueKind == "object_list" && len(item.fields) > 0 {
		list, ok := asObjectList(value)
		if !ok {
			return nil
		}
		for index, entry := range list {
			if err := validateConfigFields(item.fields, entry, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func matchesValueKind(kind string, value any) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "string_or_null":
		if value == nil {
			return true
		}
		_, ok := value.(string)
		return ok
	case "string_list":
		return isStringList(value)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := asFiniteNumber(value)
		return ok
	case "number_or_null":
		if value == nil {
			return true
		}
		_, ok := asFiniteNumber(value)
		return ok
	case "object":
		_, ok := asMap(value)
		return ok
	case "object_or_null":
		if value == nil {
			return true
		}
		_, ok := asMap(value)
		return ok
	case "object_list":
		_, ok := asObjectList(value)
		return ok
	default:
		return false
	}
}

func asFiniteNumber(value any) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		f := float64(n)
		return f, !math.IsNaN(f) && !math.IsInf(f, 0)
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	default:
		return 0, false
	}
}

func valueLen(value any) (int, bool) {
	switch t := value.(type) {
	case string:
		return len(t), true
	case []any:
		return len(t), true
	case []string:
		return len(t), true
	case []map[string]any:
		return len(t), true
	default:
		return 0, false
	}
}

func isStringList(value any) bool {
	switch t := value.(type) {
	case []string:
		return true
	case []any:
		for _, item := range t {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func asObjectList(value any) ([]map[string]any, bool) {
	switch t := value.(type) {
	case []map[string]any:
		return t, true
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			m, ok := asMap(item)
			if !ok {
				return nil, false
			}
			out = append(out, m)
		}
		return out, true
	default:
		return nil, false
	}
}
