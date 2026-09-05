package graph

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// forbiddenConfigKeys 是 ChangeSet config 不得引入的退役 plan key 与拓扑字段。未登记 key 由 validateConfigFields 拒绝。
var forbiddenConfigKeys = map[string]struct{}{
	"prompt_plan_key":  {},
	"image_plan_key":   {},
	"prompt_plan_keys": {},
	"image_plan_keys":  {},
	"document_origin":  {},
	"visual_overrides": {},
}

var imageAssetRoleOrder = []string{"product_identity", "environment", "style", "evidence"}

var defaultGenerationSpec = map[string]any{
	"aspect_ratio":       "1:1",
	"resolution_tier":    "high",
	"quality_intent":     "high",
	"reference_fidelity": "high",
	"background_intent":  "auto",
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
	NodeImagePrompt,
	NodeImageGeneration,
}

var catalogAcceptanceOrder = [][2]string{
	{string(DataProductFacts), string(NodeCreativeBrief)},
	{string(DataImageAsset), string(NodeCreativeBrief)},
	{string(DataProductFacts), string(NodeVisualSystem)},
	{string(DataImageAsset), string(NodeVisualSystem)},
	{string(DataProductFacts), string(NodeImagePrompt)},
	{string(DataImageAsset), string(NodeImagePrompt)},
	{string(DataCreativeBrief), string(NodeImagePrompt)},
	{string(DataVisualSystem), string(NodeImagePrompt)},
	{string(DataImageAsset), string(NodeImageGeneration)},
	{string(DataVisualSystem), string(NodeImageGeneration)},
	{string(DataPrompt), string(NodeImageGeneration)},
}

var visualOverlayKeys = map[string]struct{}{
	"style":  {},
	"colors": {},
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
	affectsDigest  bool
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

func hid(key, kind string, opts ...func(*configField)) configField {
	item := configField{key: key, valueKind: kind, control: "hidden", affectsDigest: false}
	for _, opt := range opts {
		opt(&item)
	}
	return item
}

// fld 声明 Catalog 可编辑字段。control 空则按 kind 选默认控件；hidden 默认不影响 digest。
// 改 key/kind 必须同步 Web/Agent CatalogJSON 与 NormalizeNodeConfig，否则旧图或提案会 Validation。
func fld(key, kind, control string, opts ...func(*configField)) configField {
	item := configField{key: key, valueKind: kind, control: control, affectsDigest: control != "hidden"}
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
		item.affectsDigest = true
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

func withDigest() func(*configField) {
	return func(f *configField) { f.affectsDigest = true }
}

func withNoDigest() func(*configField) {
	return func(f *configField) { f.affectsDigest = false }
}

func withRequired() func(*configField) {
	return func(f *configField) { f.required = true }
}

var outputType = map[NodeType]EdgeDataType{
	NodeProductSource:   DataProductFacts,
	NodeImageAsset:      DataImageAsset,
	NodeCreativeBrief:   DataCreativeBrief,
	NodeVisualSystem:    DataVisualSystem,
	NodeImagePrompt:     DataPrompt,
	NodeImageGeneration: DataImageAsset,
}

func one(n int) *int { return i(n) }

var acceptance = map[[2]string]inputContract{
	{string(DataProductFacts), string(NodeCreativeBrief)}:   {DataProductFacts, RoleFacts, one(1), false},
	{string(DataImageAsset), string(NodeCreativeBrief)}:     {DataImageAsset, RoleReference, nil, false},
	{string(DataProductFacts), string(NodeVisualSystem)}:    {DataProductFacts, RoleFacts, one(1), false},
	{string(DataImageAsset), string(NodeVisualSystem)}:      {DataImageAsset, RoleReference, nil, false},
	{string(DataProductFacts), string(NodeImagePrompt)}:     {DataProductFacts, RoleFacts, nil, false},
	{string(DataImageAsset), string(NodeImagePrompt)}:       {DataImageAsset, RoleReference, nil, false},
	{string(DataCreativeBrief), string(NodeImagePrompt)}:    {DataCreativeBrief, RoleBrief, one(1), false},
	{string(DataVisualSystem), string(NodeImagePrompt)}:     {DataVisualSystem, RoleVisualGuidance, one(1), false},
	{string(DataImageAsset), string(NodeImageGeneration)}:   {DataImageAsset, RoleReference, nil, false},
	{string(DataVisualSystem), string(NodeImageGeneration)}: {DataVisualSystem, RoleVisualGuidance, one(1), false},
	{string(DataPrompt), string(NodeImageGeneration)}:       {DataPrompt, RolePrompt, one(1), true},
}

func visualOverlayFields() []configField {
	return []configField{
		fld("style", "string_list", "", withLabel("graph.inspector.visualStyle")),
		fld("colors", "object_list", "visual_background", withLabel("graph.inspector.visualBackground"), withMaxLen(32), withFields(
			fld("role", "string", "", withMaxLen(80)),
			fld("value", "string", "", withMaxLen(7)),
			fld("label", "string", "", withMaxLen(255)),
		)),
	}
}

// promptFields 是 image_prompt.config.prompt 的 Catalog 字段表。改 key 必须同步 Web/Agent 与 normalize。
func promptFields() []configField {
	return []configField{
		hid("schema_version", "number"),
		fld("design_goal", "string", "textarea", withLabel("workflowConfirmation.designGoal")),
		fld("shared_rules", "string_list", "", withLabel("workflowConfirmation.sharedRules")),
		fld("creative_boundary", "string_list", "", withLabel("workflowConfirmation.creativeBoundary")),
		fld("product_fidelity", "object", "group", withLabel("workflowConfirmation.productFidelity"), withFields(
			hid("complex_structure", "boolean"),
			hid("product_present", "boolean", withDefault(true)),
			hid("picture_in_picture", "string", withChoices("none", "allowed", "required"), withDefault("none")),
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
	}
}

func textSettingsFields() []configField {
	return []configField{
		fld("policy", "string", "select", withChoices("none", "required"), withLabel("agentWorkbench.nodeEditor.textPolicy")),
		fld("language", "string_or_null", "", withLabel("agentWorkbench.nodeEditor.textLanguage"), withMaxLen(80), withVisible("policy", "required")),
	}
}

func imagePromptOverrideFields() []configField {
	var fields []configField
	for _, field := range promptFields() {
		if field.key == "composition" || field.key == "content" || field.key == "atmosphere" {
			if field.key == "composition" {
				var children []configField
				for _, child := range field.fields {
					if child.key != "copy_regions" {
						child.defaultValue = nil
						children = append(children, child)
					}
				}
				field.fields = children
			}
			fields = append(fields, field)
		}
	}
	return fields
}

func imageTextOverrideFields() []configField {
	fields := textSettingsFields()
	for _, field := range promptFields() {
		if field.key == "text" {
			fields = append(fields, field.fields...)
		}
	}
	return append(fields, fld("copy_regions", "string_list", "", withLabel("agentWorkbench.nodeEditor.copyRegions")))
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

// nodeConfigFields 返回该类型可编辑字段。未知类型 ok=false。hidden 字段仍参与校验；withNoDigest 的不进 input digest。
func nodeConfigFields(nodeType NodeType) ([]configField, bool) {
	switch nodeType {
	case NodeProductSource:
		return []configField{
			hid("source_product_id", "string_or_null"),
			hid("fact_set_version_id", "string_or_null", withDigest()),
		}, true
	case NodeImageAsset:
		return []configField{
			fld("role", "string_or_null", "select", withLabel("graph.inspector.assetRole"), withChoices(imageAssetRoleOrder...), withMaxLen(120)),
			fld("label", "string_or_null", "textarea", withLabel("graph.inspector.assetLabel"), withMaxLen(255)),
		}, true
	case NodeCreativeBrief:
		return []configField{
			hid("title", "string"),
			fld("goal", "string", "textarea", withLabel("nodeDetail.briefGoal")),
			fld("key_messages", "string_list", "", withLabel("graph.inspector.designGoals")),
			fld("required_elements", "string_list", "", withLabel("graph.inspector.requiredCopy")),
			hid("fact_gaps", "string_list", withDigest()),
			fld("prohibitions", "string_list", "", withLabel("nodeDetail.prohibitions")),
		}, true
	case NodeVisualSystem:
		return []configField{
			hid("visual_system_version_id", "string_or_null"),
			fld("visual_overlay", "object_or_null", "group", withNoDigest(), withFields(visualOverlayFields()...)),
		}, true
	case NodeImagePrompt:
		return []configField{
			hid("image_type_key", "string", withDigest()),
			fld("text_settings", "object", "group", withLabel("workflowConfirmation.textContent"), withFields(textSettingsFields()...)),
			fld("prompt", "object", "group", withLabel("graph.inspector.promptSection"), withNoDigest(), withFields(promptFields()...)),
		}, true
	case NodeImageGeneration:
		return []configField{
			hid("image_type_key", "string", withDigest()),
			fld("variation_instruction", "string_or_null", "textarea", withLabel("nodeDetail.supplement"), withMaxLen(4000)),
			fld("prompt_overrides", "object_or_null", "group", withFields(imagePromptOverrideFields()...)),
			fld("text_override", "object_or_null", "group", withFields(imageTextOverrideFields()...)),
			fld("generation_spec", "object", "group", withLabel("agentWorkbench.nodeEditor.generationSettings"), withRequired(), withDefault(cloneMap(defaultGenerationSpec)), withFields(generationSpecFields()...)),
			fld("delivery_spec", "object_or_null", "optional_object", withLabel("workflowConfirmation.deliverySpec"), withToggle("agentWorkbench.nodeEditor.deliveryEnabled"), withPanel("advanced"), withNoDigest(), withDefault(cloneMap(defaultDeliverySpec)), withFields(deliverySpecFields()...)),
			fld("visual_overlay", "object_or_null", "group", withFields(visualOverlayFields()...)),
		}, true
	default:
		return nil, false
	}
}

// FillDefaultNodeConfig 填入 Catalog 声明的缺省值；已有键不覆盖。未知节点类型原样返回。
func FillDefaultNodeConfig(nodeType NodeType, config map[string]any) map[string]any {
	out := cloneMap(config)
	if out == nil {
		out = map[string]any{}
	}
	fields, ok := nodeConfigFields(nodeType)
	if !ok {
		return out
	}
	_, hadGenerationSpec := out["generation_spec"]
	applyConfigDefaults(fields, out)
	if nodeType == NodeImagePrompt {
		if _, exists := out["text_settings"]; !exists {
			out["text_settings"] = defaultTextSettings(asString(out["image_type_key"]))
		}
	}
	if nodeType == NodeImageGeneration && !hadGenerationSpec {
		applyImageTypeGenerationDefaults(out)
	}
	return out
}

// applyImageTypeGenerationDefaults 仅在用户没带 generation_spec 时按图种填默认比例等。已有 spec 不覆盖。
func applyImageTypeGenerationDefaults(config map[string]any) {
	key, _ := config["image_type_key"].(string)
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	spec, _ := config["generation_spec"].(map[string]any)
	if spec == nil {
		return
	}
	if aspect, _ := spec["aspect_ratio"].(string); aspect == "" || aspect == "1:1" {
		if def := defaultAspectRatioForImageType(key); def != "1:1" {
			spec["aspect_ratio"] = def
		}
	}
}

func applyConfigDefaults(fields []configField, payload map[string]any) {
	for _, field := range fields {
		if field.defaultValue == nil {
			continue
		}
		if _, exists := payload[field.key]; exists {
			continue
		}
		payload[field.key] = cloneValue(field.defaultValue)
	}
}

func catalogDigestKeys(nodeType NodeType) map[string]struct{} {
	fields, ok := nodeConfigFields(nodeType)
	if !ok {
		return nil
	}
	out := map[string]struct{}{}
	collectDigestKeys(fields, out)
	return out
}

func collectDigestKeys(fields []configField, out map[string]struct{}) {
	for _, field := range fields {
		if field.affectsDigest {
			out[field.key] = struct{}{}
		}
	}
}

func requiredConfigIncomplete(nodeType NodeType, config map[string]any) bool {
	fields, ok := nodeConfigFields(nodeType)
	if !ok {
		return false
	}
	return requiredFieldsMissing(fields, config)
}

// requiredFieldsMissing 递归检查 Catalog required 字段是否空。用于 ConfigIncomplete，不是 HTTP 校验入口。
func requiredFieldsMissing(fields []configField, payload map[string]any) bool {
	if payload == nil {
		payload = map[string]any{}
	}
	for _, field := range fields {
		if field.required && documentFieldEmpty(payload[field.key]) {
			return true
		}
		if len(field.fields) == 0 {
			continue
		}
		child, ok := asMap(payload[field.key])
		if !ok {
			if field.required {
				return true
			}
			continue
		}
		if requiredFieldsMissing(field.fields, child) {
			return true
		}
	}
	return false
}

// IsProcessingNode 为 true 的类型才会进入 GraphRun：creative_brief、visual_system、image_prompt、image_generation。
func IsProcessingNode(nodeType NodeType) bool {
	switch nodeType {
	case NodeCreativeBrief, NodeVisualSystem, NodeImagePrompt, NodeImageGeneration:
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

// GraphNodeOutputType 返回该节点类型的唯一输出 DataType。不支持的类型返回 Validation。
func GraphNodeOutputType(nodeType NodeType) (EdgeDataType, error) {
	out, ok := outputType[nodeType]
	if !ok {
		return "", apperr.Validation("不支持的 Graph 操作")
	}
	return out, nil
}

// GraphInputContract 查 Catalog accepts：源输出类型能否连到目标。处理节点每个 accepts role 一个命名输入端口。
func GraphInputContract(sourceType, targetType NodeType) (inputContract, bool) {
	out, err := GraphNodeOutputType(sourceType)
	if err != nil {
		return inputContract{}, false
	}
	c, ok := acceptance[[2]string{string(out), string(targetType)}]
	return c, ok
}

// RequireGraphConnection 在类型不兼容时返回 Validation，供 ConnectNodes 使用。
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

// RunInputContract 给配方应用后的内部检查看：某节点类型跑图时必须存在的入边端口（data_type + role）。
// 不是 HTTP 体，也不是 CatalogJSON 整份 accepts（accepts 还含 max_count / required_to_run）。
// 改字段必须同步 Catalog acceptance 与 recipe 缺边 Conflict。不要当成 AppliedEdge。
type RunInputContract struct {
	// DataType 是 Catalog accepts 的入边数据类型。
	DataType EdgeDataType
	// Role 是入边端口，等于 React Flow handle id。
	Role EdgeRole
}

// RequiredRunContracts 在配方预览/确认把 ChangeSet 打到内存图之后调用，列出该 NodeType 跑图仍必须存在的入边。
// 当前 Catalog 只有 image_generation 的 prompt 口 RequiredToRun=true；参考图等可选边不会出现在此列表。
// 无副作用。改 RequiredToRun 会同时改 GET /api/v3/node-catalog 的 required_to_run 与 NodeConfigStatus 的 incomplete。
// 不要用它判断「能不能连边」——连边走 RequireGraphConnection。
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

// NormalizeNodeConfig 拒绝未登记 key 与退役 plan key，并规范化 generation_spec / delivery_spec。
// 未知节点类型、未登记 key 或 spec 非法返回 Validation。
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
	if nodeType == NodeImagePrompt {
		if err := validateTextSettings(payload["text_settings"]); err != nil {
			return nil, err
		}
	}
	if nodeType == NodeImageGeneration {
		if err := validateTextSettings(payload["text_override"]); err != nil {
			return nil, err
		}
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

// CatalogVisualOverlay 只保留 style / colors；空则 nil。
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

// validateConfigFields 拒绝未登记 key，再逐字段校验 kind。未知 key 返回 Validation，不要静默丢掉。
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

// validateConfigField 校验单个 Catalog 字段的 kind、choices、min/max。hidden object 跳过结构检查。
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

// matchesValueKind 按 Catalog valueKind 认 JSON 类型。未知 kind 返回 false，等于拒绝该字段。
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

// asFiniteNumber 只认有限数字。NaN/Inf 与非数字返回 false，避免 digest 或校验放进非法 number。
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
	case json.Number:
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func valueLen(value any) (int, bool) {
	switch t := value.(type) {
	case string:
		return utf8.RuneCountInString(t), true
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
