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

var imageAssetRoleOrder = []string{"product_identity", "environment", "style", "evidence"}

var defaultGenerationSpec = map[string]any{
	"aspect_ratio":       "1:1",
	"resolution_tier":    "high",
	"quality_intent":     "high",
	"reference_fidelity": "high",
	"background_intent":  "auto",
	"text_policy":        "none",
	"text_language":      nil,
}

var defaultDeliverySpec = map[string]any{
	"width":            1200,
	"height":           1200,
	"format":           "png",
	"max_byte_size":    nil,
	"fit":              "contain",
	"background_color": nil,
	"crop_anchor":      nil,
}

var catalogNodeOrder = []NodeType{
	NodeProductSource,
	NodeImageAsset,
	NodeCreativeBrief,
	NodeVisualSystem,
	NodePromptGeneration,
	NodeImageGeneration,
}

var catalogAcceptanceOrder = [][2]string{
	{string(DataProductFacts), string(NodeCreativeBrief)},
	{string(DataImageAsset), string(NodeCreativeBrief)},
	{string(DataProductFacts), string(NodeVisualSystem)},
	{string(DataImageAsset), string(NodeVisualSystem)},
	{string(DataProductFacts), string(NodePromptGeneration)},
	{string(DataImageAsset), string(NodePromptGeneration)},
	{string(DataCreativeBrief), string(NodePromptGeneration)},
	{string(DataVisualSystem), string(NodePromptGeneration)},
	{string(DataImageAsset), string(NodeImageGeneration)},
	{string(DataVisualSystem), string(NodeImageGeneration)},
	{string(DataPrompt), string(NodeImageGeneration)},
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

type visibleWhen struct {
	Field  string
	Op     string
	Values []string
}

type configField struct {
	key            string
	valueKind      string
	control        string
	required       bool
	labelKey       string
	hintKey        string
	toggleLabelKey string
	choices        []string
	minValue       *int
	maxValue       *int
	maxLength      *int
	defaultValue   any
	panel          string
	visibleWhen    *visibleWhen
	fields         []configField
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

func withLabel(key string) func(*configField) {
	return func(f *configField) { f.labelKey = key }
}

func withHint(key string) func(*configField) {
	return func(f *configField) { f.hintKey = key }
}

func withToggle(key string) func(*configField) {
	return func(f *configField) { f.toggleLabelKey = key }
}

func withDefault(value any) func(*configField) {
	return func(f *configField) { f.defaultValue = value }
}

func withPanel(panel string) func(*configField) {
	return func(f *configField) { f.panel = panel }
}

func withVisible(field string, values ...string) func(*configField) {
	return func(f *configField) {
		f.visibleWhen = &visibleWhen{Field: field, Op: "in", Values: values}
	}
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
		fld("style", "string_list", "", withLabel("graph.inspector.visualStyle")),
		fld("colors", "object_list", "visual_background", withLabel("graph.inspector.visualBackground"), withMaxLen(32), withFields(
			fld("role", "string", "", withMaxLen(80)),
			fld("value", "string", "", withMaxLen(7)),
			fld("label", "string", "", withMaxLen(255)),
		)),
		fld("prohibitions", "string_list", "", withLabel("workflowConfirmation.creativeBoundary")),
	}
}

func promptFields() []configField {
	return []configField{
		hid("schema_version", "number"),
		fld("design_goal", "string", "textarea", withLabel("workflowConfirmation.designGoal")),
		fld("shared_rules", "string_list", "", withLabel("workflowConfirmation.sharedRules")),
		fld("creative_boundary", "string_list", "", withLabel("workflowConfirmation.creativeBoundary")),
		fld("product_fidelity", "object", "group", withLabel("workflowConfirmation.productFidelity"), withFields(
			fld("complex_structure", "boolean", "", withLabel("agentWorkbench.nodeEditor.complexStructure")),
			fld("product_present", "boolean", "", withLabel("agentWorkbench.nodeEditor.productPresent"), withDefault(true)),
			fld("picture_in_picture", "string", "select", withLabel("agentWorkbench.nodeEditor.pictureInPicture"), withChoices("none", "allowed", "required"), withDefault("none")),
			fld("requirements", "string_list", "", withLabel("agentWorkbench.nodeEditor.requirements")),
		)),
		fld("composition", "object", "group", withLabel("workflowConfirmation.composition"), withFields(
			fld("viewpoint", "string", "", withLabel("agentWorkbench.nodeEditor.viewpoint")),
			fld("product_share_percent", "number", "", withLabel("agentWorkbench.nodeEditor.productShare"), withMinMax(1, 100), withDefault(70)),
			fld("layout", "string", "textarea", withLabel("agentWorkbench.nodeEditor.layout")),
			fld("copy_regions", "string_list", "", withLabel("agentWorkbench.nodeEditor.copyRegions")),
		)),
		fld("content", "object", "group", withLabel("workflowConfirmation.content"), withFields(
			fld("focus", "string_list", "", withLabel("agentWorkbench.nodeEditor.focus")),
			fld("selling_points", "string_list", "", withLabel("agentWorkbench.nodeEditor.sellingPoints")),
			fld("background", "string", "textarea", withLabel("agentWorkbench.nodeEditor.background")),
			fld("decorations", "string_list", "", withLabel("agentWorkbench.nodeEditor.decorations")),
		)),
		fld("text", "object", "group", withLabel("workflowConfirmation.textContent"), withFields(
			fld("headline", "string_or_null", "", withLabel("agentWorkbench.nodeEditor.headline")),
			fld("subtitle", "string_or_null", "", withLabel("agentWorkbench.nodeEditor.subtitle")),
			fld("body", "string_or_null", "textarea", withLabel("agentWorkbench.nodeEditor.body")),
		)),
		fld("atmosphere", "object", "group", withLabel("workflowConfirmation.atmosphere"), withFields(
			fld("keywords", "string_list", "", withLabel("agentWorkbench.nodeEditor.keywords")),
			fld("lighting", "string", "textarea", withLabel("agentWorkbench.nodeEditor.lighting")),
		)),
		hid("visual_variant_key", "string_or_null"),
	}
}

func generationSpecFields() []configField {
	return []configField{
		fld("aspect_ratio", "string", "aspect_ratio", withLabel("agentWorkbench.nodeEditor.aspectRatio"), withPanel("basic"), withDefault("1:1")),
		fld("resolution_tier", "string", "select", withLabel("agentWorkbench.nodeEditor.resolution"), withChoices("standard", "high", "ultra"), withPanel("basic"), withDefault("high")),
		fld("quality_intent", "string", "select", withLabel("agentWorkbench.nodeEditor.quality"), withChoices("draft", "standard", "high"), withPanel("basic"), withDefault("high")),
		fld("reference_fidelity", "string", "select", withLabel("agentWorkbench.nodeEditor.referenceFidelity"), withChoices("low", "medium", "high"), withPanel("advanced"), withDefault("high")),
		fld("background_intent", "string", "select", withLabel("agentWorkbench.nodeEditor.backgroundIntent"), withChoices("auto", "opaque", "transparent"), withPanel("advanced"), withDefault("auto")),
		fld("text_policy", "string", "select", withLabel("agentWorkbench.nodeEditor.textPolicy"), withChoices("none", "allow", "required"), withPanel("advanced"), withDefault("none")),
		fld("text_language", "string_or_null", "", withLabel("agentWorkbench.nodeEditor.textLanguage"), withMaxLen(80), withPanel("advanced"), withVisible("text_policy", "allow", "required")),
	}
}

func deliverySpecFields() []configField {
	return []configField{
		fld("width", "number", "", withLabel("agentWorkbench.nodeEditor.width"), withMinMax(1, 16384), withDefault(1200)),
		fld("height", "number", "", withLabel("agentWorkbench.nodeEditor.height"), withMinMax(1, 16384), withDefault(1200)),
		fld("format", "string", "select", withLabel("agentWorkbench.nodeEditor.format"), withChoices("png", "jpeg", "webp"), withDefault("png")),
		hid("max_byte_size", "number_or_null"),
		fld("fit", "string", "select", withLabel("agentWorkbench.nodeEditor.fit"), withChoices("contain", "cover"), withDefault("contain")),
		fld("background_color", "string_or_null", "", withLabel("agentWorkbench.nodeEditor.backgroundColor"), withMaxLen(7), withVisible("fit", "contain")),
		fld("crop_anchor", "string_or_null", "select", withLabel("agentWorkbench.nodeEditor.cropAnchor"), withChoices("center", "top", "bottom", "left", "right"), withVisible("fit", "cover")),
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
		return []configField{
			fld("role", "string_or_null", "select", withLabel("graph.inspector.assetRole"), withChoices(imageAssetRoleOrder...), withMaxLen(120)),
			fld("label", "string_or_null", "", withLabel("graph.inspector.assetLabel"), withMaxLen(255)),
		}, true
	case NodeCreativeBrief:
		return []configField{
			hid("title", "string"),
			fld("goal", "string", "textarea", withLabel("workflowConfirmation.designGoal")),
			fld("design_goals", "string_list", "", withLabel("graph.inspector.designGoals")),
			fld("required_copy", "string_list", "", withLabel("graph.inspector.requiredCopy")),
			fld("prohibitions", "string_list", "", withLabel("workflowConfirmation.creativeBoundary")),
		}, true
	case NodeVisualSystem:
		return []configField{
			hid("visual_system_version_id", "string_or_null"),
			fld("visual_overlay", "object_or_null", "group", withHint("graph.inspector.visualVersionHint"), withFields(visualOverlayFields()...)),
			hid("visual_overrides", "object"),
		}, true
	case NodePromptGeneration:
		return []configField{
			hid("image_type_key", "string"),
			fld("prompt", "object", "group", withLabel("graph.inspector.promptSection"), withFields(promptFields()...)),
		}, true
	case NodeImageGeneration:
		return []configField{
			hid("image_type_key", "string"),
			fld("variation_instruction", "string_or_null", "textarea", withLabel("workflowConfirmation.variation"), withMaxLen(4000)),
			fld("generation_spec", "object", "group", withLabel("agentWorkbench.nodeEditor.generationSettings"), withDefault(cloneMap(defaultGenerationSpec)), withFields(generationSpecFields()...)),
			fld("delivery_spec", "object_or_null", "optional_object", withLabel("workflowConfirmation.deliverySpec"), withToggle("agentWorkbench.nodeEditor.deliveryEnabled"), withPanel("advanced"), withDefault(cloneMap(defaultDeliverySpec)), withFields(deliverySpecFields()...)),
			fld("visual_overlay", "object_or_null", "group", withFields(visualOverlayFields()...)),
			hid("visual_overrides", "object"),
		}, true
	default:
		return nil, false
	}
}

func IsProcessingNode(nodeType NodeType) bool {
	switch nodeType {
	case NodeCreativeBrief, NodeVisualSystem, NodePromptGeneration, NodeImageGeneration:
		return true
	default:
		return false
	}
}

func catalogAccepts(nodeType NodeType) []inputContract {
	var out []inputContract
	for _, key := range catalogAcceptanceOrder {
		if key[1] != string(nodeType) {
			continue
		}
		if c, ok := acceptance[key]; ok {
			out = append(out, c)
		}
	}
	return out
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

// RunInputContract 是配方应用后检查「运行所需输入边」用的端口。
type RunInputContract struct {
	DataType EdgeDataType
	Role     EdgeRole
}

// RequiredRunContracts 返回该节点类型跑图时必须存在的入边。
func RequiredRunContracts(nodeType NodeType) []RunInputContract {
	var out []RunInputContract
	for _, c := range runRequiredInputs(nodeType) {
		out = append(out, RunInputContract{DataType: c.DataType, Role: c.Role})
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
	if len(overlay) == 0 {
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
