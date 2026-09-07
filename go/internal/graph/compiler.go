package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

var promptStrippedKeys = map[string]struct{}{
	"images": {}, "fact_keys": {}, "evidence_asset_ids": {}, "text_trace": {},
	"prompt_plan_key": {}, "image_plan_key": {}, "prompt_plan_keys": {}, "image_plan_keys": {},
}

var promptEmptyKeys = map[string]struct{}{
	"schema_version": {}, "visual_variant_key": {},
}

func requestConfigForDigest(nodeType NodeType, config map[string]any) map[string]any {
	allowed := catalogDigestKeys(nodeType)
	out := map[string]any{}
	for key, value := range config {
		if _, ok := allowed[key]; !ok {
			continue
		}
		out[key] = cloneValue(value)
	}
	return out
}

// SourceRecord 是 compiler 从入边解析出的运行输入。断开边即从 digest 消失；不扫描图其余节点。
type SourceRecord struct {
	Facts                  []map[string]any       // 来自入边 product_source 的 payload；空列表是 [] 不是 nil
	ProductSource          *productSourceSnapshot // 仅 product_source 节点
	Brief                  map[string]any         // 入边 creative_brief 的可见文稿
	VisualPayload          map[string]any         // 入边 visual_system 的 overlay 或版本 payload
	VisualSystemVersionID  *string
	BoundAssetID           *string
	BoundAssetLabel        *string // 绑定图展示名；未绑定时用节点标题
	BoundAssetMIME         *string // 绑定图 MIME；未绑定或读失败为 nil
	CurrentArtifactID      *string
	CurrentArtifactType    *string        // 当前产物类型，如 image；无产物为 nil
	CurrentArtifactPayload map[string]any // 无产物时为空 map
	CurrentOutputAssetID   *string
	// CurrentInputDigest 是当前产物编译时的 input digest；nil 表示尚无产物，cook 必须生成。
	CurrentInputDigest *string
	PromptDocument     map[string]any // 入边 image_prompt 的 prompt 文档
}

type compiledReference struct {
	EdgeID       string
	SourceNodeID string
	AssetID      string
	Label        string
	MIMEType     *string
	Order        int
	Role         string
}

// incomingSorted 只收集指向该节点的边。compiler 运行输入不含未连入的 facts / 参考图 / 文稿。
func incomingSorted(graph AppliedGraph, nodeID string) []AppliedEdge {
	var out []AppliedEdge
	for _, edge := range graph.Edges {
		if edge.TargetNodeID == nodeID {
			out = append(out, edge)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// incomingFactSetVersions 把选中的 fact-set 版本身份写进下游签名。
// 两个版本即使当前 facts 内容相同，版本 id 不同也会让 digest 变化，避免错复用旧产物。
// 只扫入边 RoleFacts；断开边即从签名消失。改 digest 算法时必须连同 skipUnchanged 一起看。
func incomingFactSetVersions(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) []map[string]any {
	entries := make([]map[string]any, 0)
	for _, edge := range incomingSorted(graph, nodeID) {
		if edge.Role != RoleFacts {
			continue
		}
		entry := map[string]any{
			"edge_id":             edge.ID,
			"source_node_id":      edge.SourceNodeID,
			"fact_set_version_id": nil,
		}
		if record := sources[edge.SourceNodeID]; record.ProductSource != nil && record.ProductSource.FactSetVersionID != nil {
			entry["fact_set_version_id"] = *record.ProductSource.FactSetVersionID
		}
		entries = append(entries, entry)
	}
	return entries
}

// compileInputDigest 哈希目标节点入边与 affects_digest 的 config。image digest 省略 delivery_spec。
func compileInputDigest(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (string, error) {
	node, err := graph.Node(nodeID)
	if err != nil {
		return "", err
	}
	switch node.NodeType {
	case NodeImagePrompt:
		runtime, err := compilePromptRuntime(graph, nodeID, sources)
		if err != nil {
			return "", err
		}
		return runtime, nil
	case NodeImageGeneration:
		return compileImageRuntime(graph, nodeID, sources)
	case NodeCreativeBrief, NodeVisualSystem:
		return compileContextRuntime(graph, nodeID, sources)
	default:
		return "", apperr.Validation("不支持的 Graph 操作")
	}
}

// compilePromptRuntime 编 image_prompt 的 input digest：只扫入边 facts/brief/reference/visual。
// 缺运行所需边返回 Validation。digest 含 fact_set_versions，同内容不同版本也会变。
// 改字段集合必须同步 skipUnchanged 与 compiled_context，否则会错复用或永远 stale。
func compilePromptRuntime(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (string, error) {
	node, err := graph.Node(nodeID)
	if err != nil {
		return "", err
	}
	if node.NodeType != NodeImagePrompt {
		return "", apperr.Validation("只有提示词生成节点可以编译为 PromptRuntimeInput")
	}
	if err := rejectIncompleteRequiredEdges(graph, node); err != nil {
		return "", err
	}
	edges := incomingSorted(graph, nodeID)
	var facts []map[string]any
	var briefs []map[string]any
	var references []compiledReference
	var visualPayload any
	var visualVersionID any
	var visualOverlay any
	for _, edge := range edges {
		source, err := graph.Node(edge.SourceNodeID)
		if err != nil {
			return "", err
		}
		record := sources[source.ID]
		switch edge.Role {
		case RoleFacts:
			facts = append(facts, mergeRuntimeFacts(record.Facts, record.ProductSource)...)
		case RoleBrief:
			if record.Brief != nil {
				briefs = append(briefs, cloneMap(record.Brief))
			}
		case RoleReference:
			ref, err := compileReference(graph, edge, sources)
			if err != nil {
				return "", err
			}
			references = append(references, ref)
		case RoleVisualGuidance:
			payload, versionID, overlay, err := compileVisual(source, record)
			if err != nil {
				return "", err
			}
			visualPayload, visualVersionID, visualOverlay = payload, versionID, overlay
		}
	}
	incomingIDs := make([]string, 0, len(edges))
	for _, edge := range edges {
		incomingIDs = append(incomingIDs, edge.ID)
	}
	return inputDigest(map[string]any{
		"node_id":                  nodeID,
		"config":                   requestConfigForDigest(node.NodeType, node.Config),
		"facts":                    factsOrEmpty(facts),
		"fact_set_versions":        incomingFactSetVersions(graph, nodeID, sources),
		"briefs":                   briefsOrEmpty(briefs),
		"references":               referenceDigestInputs(references),
		"visual_system":            visualPayload,
		"visual_system_version_id": visualVersionID,
		"visual_overlay":           visualOverlay,
		"incoming_edge_ids":        incomingIDs,
	}), nil
}

// compileContextRuntime 编 creative_brief / visual_system 的 digest。只消费 facts 与 reference 入边。
// 类型不对或缺必填边返回 Validation。不要把 live 文档本身编进 digest——adopt 后会自我 stale。
func compileContextRuntime(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (string, error) {
	node, err := graph.Node(nodeID)
	if err != nil {
		return "", err
	}
	if node.NodeType != NodeCreativeBrief && node.NodeType != NodeVisualSystem {
		return "", apperr.Validation("只有视觉规范或创作要求节点可以编译为 ContextRuntimeInput")
	}
	if err := rejectIncompleteRequiredEdges(graph, node); err != nil {
		return "", err
	}
	edges := incomingSorted(graph, nodeID)
	var facts []map[string]any
	var references []compiledReference
	for _, edge := range edges {
		record := sources[edge.SourceNodeID]
		switch edge.Role {
		case RoleFacts:
			facts = append(facts, mergeRuntimeFacts(record.Facts, record.ProductSource)...)
		case RoleReference:
			ref, err := compileReference(graph, edge, sources)
			if err != nil {
				return "", err
			}
			references = append(references, ref)
		}
	}
	incomingIDs := make([]string, 0, len(edges))
	for _, edge := range edges {
		incomingIDs = append(incomingIDs, edge.ID)
	}
	return inputDigest(map[string]any{
		"node_id":           nodeID,
		"node_type":         node.NodeType,
		"config":            requestConfigForDigest(node.NodeType, node.Config),
		"facts":             factsOrEmpty(facts),
		"fact_set_versions": incomingFactSetVersions(graph, nodeID, sources),
		"references":        referenceDigestInputs(references),
		"incoming_edge_ids": incomingIDs,
	}), nil
}

// compileImageRuntime 编 image_generation 的 digest。必须有 prompt 入边，文稿空则 Validation。
// digest 用 strip 后的 prompt_document + 参考资产 id + visual version/overlay + 规范化 config。
// 产物本身不进 digest。改字段须同步 skipUnchanged，否则会错跳过或永远重新生成。
func compileImageRuntime(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (string, error) {
	node, err := graph.Node(nodeID)
	if err != nil {
		return "", err
	}
	if node.NodeType != NodeImageGeneration {
		return "", apperr.Validation("只有图片生成节点可以编译为 ImageRuntimeInput")
	}
	if err := rejectIncompleteRequiredEdges(graph, node); err != nil {
		return "", err
	}
	edges := incomingSorted(graph, nodeID)
	var promptEdge *AppliedEdge
	for i := range edges {
		if edges[i].Role == RolePrompt {
			promptEdge = &edges[i]
			break
		}
	}
	if promptEdge == nil {
		return "", apperr.Validation("图片生成节点缺少 prompt 边，不能运行")
	}
	promptPayload, effectiveSpec, _, err := resolveImageDocument(graph, nodeID, sources)
	if err != nil {
		return "", err
	}
	var references []compiledReference
	var visualVersionID any
	var visualOverlay any
	for _, edge := range edges {
		switch edge.Role {
		case RoleReference:
			ref, err := compileReference(graph, edge, sources)
			if err != nil {
				return "", err
			}
			references = append(references, ref)
		case RoleVisualGuidance:
			source, err := graph.Node(edge.SourceNodeID)
			if err != nil {
				return "", err
			}
			payload, versionID, overlay, err := compileVisual(source, sources[source.ID])
			if err != nil {
				return "", err
			}
			visualVersionID = versionID
			published := CatalogVisualOverlay(asMapOrNil(payload))
			if overlayMap, ok := overlay.(map[string]any); ok && len(overlayMap) > 0 {
				visualOverlay = overlayMap
			} else if len(published) > 0 {
				visualOverlay = published
			}
		}
	}
	normalized, err := NormalizeNodeConfig(node.NodeType, node.Config)
	if err != nil {
		return "", err
	}
	incomingIDs := make([]string, 0, len(edges))
	for _, edge := range edges {
		incomingIDs = append(incomingIDs, edge.ID)
	}
	configInput := requestConfigForDigest(node.NodeType, normalized)
	delete(configInput, "prompt_overrides")
	delete(configInput, "text_override")
	delete(configInput, "generation_spec")
	return inputDigest(map[string]any{
		"node_id":                   nodeID,
		"config":                    configInput,
		"prompt_document":           stripV3Prompt(promptPayload),
		"effective_generation_spec": effectiveSpec,
		"references":                referenceDigestInputs(references),
		"visual_system_version_id":  visualVersionID,
		"visual_overlay":            visualOverlay,
		"incoming_edge_ids":         incomingIDs,
	}), nil
}

func rejectIncompleteRequiredEdges(graph AppliedGraph, node AppliedNode) error {
	msg := nodeConfigError(RuleNode{node.ID, node.NodeType, node.Config, node.BoundAssetID})
	if msg != "" {
		if strings.HasPrefix(msg, "图片生成节点") || strings.HasPrefix(msg, "generation_spec") || strings.HasPrefix(msg, "delivery_spec") {
			return apperr.Validation(msg)
		}
		return apperr.Validation("节点配置无效: " + msg)
	}
	incoming := incomingSorted(graph, node.ID)
	ruleIncoming := make([]RuleEdge, 0, len(incoming))
	for _, edge := range incoming {
		ruleIncoming = append(ruleIncoming, RuleEdge{edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
	}
	if len(missingRequiredInputs(RuleNode{node.ID, node.NodeType, node.Config, node.BoundAssetID}, ruleIncoming)) > 0 {
		return apperr.Validation("节点缺少运行所需的输入边")
	}
	return nil
}

// compileReference 把 reference 入边收成资产 id。image_asset 用绑定；image_generation 用当前输出资产。
// 未绑定或上游尚无输出返回 Validation。绑定不是 reference 边——断边才会从 digest 消失。
func compileReference(graph AppliedGraph, edge AppliedEdge, sources map[string]SourceRecord) (compiledReference, error) {
	source, err := graph.Node(edge.SourceNodeID)
	if err != nil {
		return compiledReference{}, err
	}
	record := sources[source.ID]
	var assetID *string
	label := source.Title
	mimeType := record.BoundAssetMIME
	switch source.NodeType {
	case NodeImageAsset:
		assetID = record.BoundAssetID
		if assetID == nil {
			assetID = source.BoundAssetID
		}
		if record.BoundAssetLabel != nil && *record.BoundAssetLabel != "" {
			label = *record.BoundAssetLabel
		}
	case NodeImageGeneration:
		assetID = record.CurrentOutputAssetID
		if assetID == nil {
			return compiledReference{}, apperr.Validation("上游图片生成节点尚无当前输出，不能作为参考")
		}
	}
	if assetID == nil || *assetID == "" {
		return compiledReference{}, apperr.Validation("参考输入缺少已绑定的图片资产")
	}
	role := "reference"
	if raw, ok := source.Config["role"].(string); ok && strings.TrimSpace(raw) != "" {
		role = strings.TrimSpace(raw)
	}
	if note := strings.TrimSpace(asString(source.Config["label"])); note != "" {
		label = note
	}
	return compiledReference{
		EdgeID: edge.ID, SourceNodeID: source.ID, AssetID: *assetID,
		Label: label, MIMEType: mimeType, Order: edge.Order, Role: role,
	}, nil
}

// Reference metadata changes provider meaning even when the media bytes stay the same.
func referenceDigestInputs(references []compiledReference) []map[string]any {
	inputs := make([]map[string]any, 0, len(references))
	for _, ref := range references {
		inputs = append(inputs, map[string]any{"asset_id": ref.AssetID, "role": ref.Role, "label": ref.Label})
	}
	return inputs
}

// compileVisual 合并 visual_system 的 version payload 与节点 visual_overlay。
// 源类型必须是 visual_system，否则 Validation。返回 payload、version_id、overlay；空 overlay 为 nil。
func compileVisual(source AppliedNode, record SourceRecord) (any, any, any, error) {
	if source.NodeType != NodeVisualSystem {
		return nil, nil, nil, apperr.Validation("visual_guidance 边的源必须是视觉规范节点")
	}
	rawVersion := any(nil)
	if record.VisualSystemVersionID != nil {
		rawVersion = *record.VisualSystemVersionID
	} else {
		rawVersion = source.Config["visual_system_version_id"]
	}
	var versionID any
	if s, ok := rawVersion.(string); ok && strings.TrimSpace(s) != "" {
		versionID = strings.TrimSpace(s)
	}
	overlay := visualOverlayFromConfig(source.Config)
	if record.VisualPayload == nil {
		if overlay == nil {
			return nil, versionID, nil, nil
		}
		cloned := cloneMap(overlay)
		return cloned, versionID, cloned, nil
	}
	payload := cloneMap(record.VisualPayload)
	for key, value := range overlay {
		payload[key] = cloneValue(value)
	}
	var overlayOut any
	if overlay != nil {
		overlayOut = cloneMap(overlay)
	}
	return payload, versionID, overlayOut, nil
}

func visualOverlayFromConfig(config map[string]any) map[string]any {
	payload := config
	if payload == nil {
		payload = map[string]any{}
	}
	if overlay, ok := asMap(payload["visual_overlay"]); ok && len(overlay) > 0 {
		return CatalogVisualOverlay(overlay)
	}
	return nil
}

// incomingPromptDocument 取第一条 RolePrompt 入边的已发布文稿与 artifact id。
// 缺边或文稿空返回 Validation。只扫入边，不扫图其余部分。
func incomingPromptDocument(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (map[string]any, string, error) {
	for _, edge := range incomingSorted(graph, nodeID) {
		if edge.Role != RolePrompt {
			continue
		}
		source, err := graph.Node(edge.SourceNodeID)
		if err != nil {
			return nil, "", err
		}
		record := sources[source.ID]
		payload := publishedPromptDocument(source, record)
		if !hasPromptPayload(stripV3Prompt(payload)) {
			return nil, "", apperr.Validation("上游提示词文稿为空")
		}
		artifactID := ""
		if record.CurrentArtifactID != nil {
			artifactID = *record.CurrentArtifactID
		}
		return payload, artifactID, nil
	}
	return nil, "", apperr.Validation("图片生成节点缺少 prompt 边，不能运行")
}

// collectPromptInputs 按入边组装 cook 用的 facts/briefs/visual/references。
// 只给 AssemblePromptRequest 用，不写库。参考边编译失败会整份返回 error。
func collectPromptInputs(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (facts []map[string]any, briefs []map[string]any, visual map[string]any, refs []compiledReference, err error) {
	for _, edge := range incomingSorted(graph, nodeID) {
		record := sources[edge.SourceNodeID]
		switch edge.Role {
		case RoleFacts:
			facts = append(facts, mergeRuntimeFacts(record.Facts, record.ProductSource)...)
		case RoleBrief:
			if record.Brief != nil {
				briefs = append(briefs, cloneMap(record.Brief))
			}
		case RoleReference:
			ref, refErr := compileReference(graph, edge, sources)
			if refErr != nil {
				return nil, nil, nil, nil, refErr
			}
			refs = append(refs, ref)
		case RoleVisualGuidance:
			source, nodeErr := graph.Node(edge.SourceNodeID)
			if nodeErr != nil {
				return nil, nil, nil, nil, nodeErr
			}
			payload, _, overlay, visErr := compileVisual(source, record)
			if visErr != nil {
				return nil, nil, nil, nil, visErr
			}
			// Python _compile_visual 把 version draft 与 overlay merge 后作为 visual_system；
			// overlay-wins 会丢掉 visual_system_version_id 对应的 draft。
			if payloadMap, ok := payload.(map[string]any); ok && len(payloadMap) > 0 {
				visual = cloneMap(payloadMap)
			} else if overlayMap, ok := overlay.(map[string]any); ok {
				visual = cloneMap(overlayMap)
			}
		}
	}
	return facts, briefs, visual, refs, nil
}

// compiledContextTrace 对齐 Python _prompt/_context/_image_context_trace：只统计 incoming 边，merge 进已有 compiled_context。
func compiledContextTrace(graph AppliedGraph, node AppliedNode, sources map[string]SourceRecord, digest string) map[string]any {
	incoming := incomingSorted(graph, node.ID)
	incomingIDs := make([]string, 0, len(incoming))
	var facts []map[string]any
	var briefs []map[string]any
	refIDs := []string{}
	for _, edge := range incoming {
		incomingIDs = append(incomingIDs, edge.ID)
		record := sources[edge.SourceNodeID]
		switch edge.Role {
		case RoleFacts:
			facts = append(facts, mergeRuntimeFacts(record.Facts, record.ProductSource)...)
		case RoleBrief:
			if record.Brief != nil {
				briefs = append(briefs, cloneMap(record.Brief))
			}
		case RoleReference:
			ref, refErr := compileReference(graph, edge, sources)
			if refErr == nil && ref.AssetID != "" {
				refIDs = append(refIDs, ref.AssetID)
			}
		}
	}
	trace := map[string]any{
		"node_title":          node.Title,
		"incoming_edge_ids":   incomingIDs,
		"input_digest":        digest,
		"reference_asset_ids": refIDs,
	}
	switch node.NodeType {
	case NodeImagePrompt:
		trace["fact_count"] = len(facts)
		trace["brief_count"] = len(briefs)
		if version := incomingVisualVersionID(graph, node.ID, sources); version != nil {
			trace["visual_system_version_id"] = version
		}
	case NodeCreativeBrief, NodeVisualSystem:
		trace["node_type"] = string(node.NodeType)
		trace["fact_count"] = len(facts)
	case NodeImageGeneration:
		var promptEdgeID, promptArtifactID string
		for _, edge := range incoming {
			if edge.Role != RolePrompt {
				continue
			}
			promptEdgeID = edge.ID
			if record := sources[edge.SourceNodeID]; record.CurrentArtifactID != nil {
				promptArtifactID = *record.CurrentArtifactID
			}
			break
		}
		if promptArtifactID != "" {
			trace["prompt_artifact_id"] = promptArtifactID
		}
		if promptEdgeID != "" {
			trace["prompt_edge_id"] = promptEdgeID
		}
		if version := incomingVisualVersionID(graph, node.ID, sources); version != nil {
			trace["visual_system_version_id"] = version
		}
	}
	trace["input_trace"] = graphRuntimeInputTrace(graph, node.ID, sources)
	return trace
}

// graphRuntimeInputTrace 对齐 Python graph_runtime_input_trace：按 incoming 边写入 artifact/asset/version 身份。
func graphRuntimeInputTrace(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) []map[string]any {
	var incoming []AppliedEdge
	for _, edge := range graph.Edges {
		if edge.TargetNodeID == nodeID {
			incoming = append(incoming, edge)
		}
	}
	sort.Slice(incoming, func(i, j int) bool {
		if incoming[i].Order != incoming[j].Order {
			return incoming[i].Order < incoming[j].Order
		}
		return incoming[i].ID < incoming[j].ID
	})
	entries := make([]map[string]any, 0, len(incoming))
	for _, edge := range incoming {
		source, err := graph.Node(edge.SourceNodeID)
		if err != nil {
			entries = append(entries, map[string]any{
				"edge_id":        edge.ID,
				"source_node_id": edge.SourceNodeID,
				"source_title":   nil,
				"role":           string(edge.Role),
				"order":          edge.Order,
				"artifact_id":    nil,
				"artifact_type":  nil,
				"asset_id":       nil,
				"version_id":     nil,
			})
			continue
		}
		record := sources[source.ID]
		artifactID := record.CurrentArtifactID
		artifactType := record.CurrentArtifactType
		if artifactID != nil && *artifactID != "" && (artifactType == nil || *artifactType == "") {
			artifactType = inferredArtifactType(source.NodeType)
		}
		var assetID *string
		switch source.NodeType {
		case NodeImageAsset:
			if record.BoundAssetID != nil && *record.BoundAssetID != "" {
				assetID = record.BoundAssetID
			} else {
				assetID = source.BoundAssetID
			}
		case NodeImageGeneration:
			assetID = record.CurrentOutputAssetID
		}
		var versionID *string
		if source.NodeType == NodeProductSource && record.ProductSource != nil {
			versionID = record.ProductSource.FactSetVersionID
		} else if source.NodeType == NodeVisualSystem {
			versionID = record.VisualSystemVersionID
		}
		var sourceTitle any
		if strings.TrimSpace(source.Title) != "" {
			sourceTitle = source.Title
		}
		entries = append(entries, map[string]any{
			"edge_id":        edge.ID,
			"source_node_id": source.ID,
			"source_title":   sourceTitle,
			"role":           string(edge.Role),
			"order":          edge.Order,
			"artifact_id":    emptyToNilPtr(artifactID),
			"artifact_type":  emptyToNilPtr(artifactType),
			"asset_id":       emptyToNilPtr(assetID),
			"version_id":     emptyToNilPtr(versionID),
		})
	}
	return entries
}

func inferredArtifactType(nodeType NodeType) *string {
	switch nodeType {
	case NodeCreativeBrief:
		return strPtr("creative_brief")
	case NodeVisualSystem:
		return strPtr("visual_system")
	case NodeImagePrompt:
		return strPtr("prompt")
	case NodeImageGeneration:
		return strPtr("image")
	default:
		return nil
	}
}

func emptyToNilPtr(v *string) any {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return *v
}

func incomingVisualVersionID(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) any {
	for _, edge := range incomingSorted(graph, nodeID) {
		if edge.Role != RoleVisualGuidance {
			continue
		}
		source, err := graph.Node(edge.SourceNodeID)
		if err != nil {
			return nil
		}
		_, versionID, _, visErr := compileVisual(source, sources[source.ID])
		if visErr != nil {
			return nil
		}
		if versionID != nil {
			return versionID
		}
	}
	return nil
}

func stripV3Prompt(payload map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range payload {
		if _, skip := promptStrippedKeys[key]; skip {
			continue
		}
		out[key] = value
	}
	return out
}

func hasPromptPayload(payload map[string]any) bool {
	for key, value := range payload {
		if _, emptyKey := promptEmptyKeys[key]; emptyKey {
			continue
		}
		if isEmptyPromptValue(value) {
			continue
		}
		return true
	}
	return false
}

func isEmptyPromptValue(value any) bool {
	if value == nil {
		return true
	}
	switch t := value.(type) {
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return false
	}
}

func inputDigest(payload map[string]any) string {
	sum := sha256.Sum256([]byte(pythonDumps(payload)))
	return hex.EncodeToString(sum[:])
}

// pythonDumps 按 Python json.dumps(sort_keys=True, separators=(', ', ': ')) 的字节序编 digest。
// 改分隔符或 key 排序会让所有存量 input_digest 失效，旧产物会全部被当成 stale。整数浮点必须写成不带小数。
func pythonDumps(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		b, _ := json.Marshal(t)
		return string(b)
	case json.Number:
		return t.String()
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		b, _ := json.Marshal(t)
		return string(b)
	case NodeType:
		return pythonDumps(string(t))
	case []string:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, pythonDumps(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []map[string]any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, pythonDumps(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, pythonDumps(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(t))
		for key := range t {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, pythonDumps(key)+": "+pythonDumps(t[key]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		b, err := json.Marshal(t)
		if err != nil {
			s, _ := json.Marshal(fmt.Sprint(t))
			return string(s)
		}
		return string(b)
	}
}

func factsOrEmpty(items []map[string]any) any {
	if items == nil {
		return []map[string]any{}
	}
	return items
}

func briefsOrEmpty(items []map[string]any) any {
	if items == nil {
		return []map[string]any{}
	}
	return items
}
