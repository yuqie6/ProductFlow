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
	"images": {}, "fact_keys": {}, "evidence_asset_ids": {},
	"prompt_plan_key": {}, "image_plan_key": {}, "prompt_plan_keys": {}, "image_plan_keys": {},
}

var promptEmptyKeys = map[string]struct{}{
	"schema_version": {}, "visual_variant_key": {},
}

var digestSelfOutputKeys = map[NodeType]map[string]struct{}{
	NodeVisualSystem:     {"visual_overlay": {}},
	NodeCreativeBrief:    {"goal": {}, "design_goals": {}, "required_copy": {}, "prohibitions": {}},
	NodePromptGeneration: {"prompt": {}},
}

var digestIgnoredKeys = map[NodeType]map[string]struct{}{
	NodeImageGeneration: {"delivery_spec": {}},
}

type SourceRecord struct {
	Facts                  []map[string]any
	ProductSource          *productSourceSnapshot
	Brief                  map[string]any
	VisualPayload          map[string]any
	VisualSystemVersionID  *string
	BoundAssetID           *string
	BoundAssetLabel        *string
	BoundAssetMIME         *string
	CurrentArtifactID      *string
	CurrentArtifactType    *string
	CurrentArtifactPayload map[string]any
	CurrentOutputAssetID   *string
	CurrentInputDigest     *string
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

func compileInputDigest(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (string, error) {
	node, err := graph.Node(nodeID)
	if err != nil {
		return "", err
	}
	switch node.NodeType {
	case NodePromptGeneration:
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

func compilePromptRuntime(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (string, error) {
	node, err := graph.Node(nodeID)
	if err != nil {
		return "", err
	}
	if node.NodeType != NodePromptGeneration {
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
	refIDs := make([]string, 0, len(references))
	for _, ref := range references {
		refIDs = append(refIDs, ref.AssetID)
	}
	return inputDigest(map[string]any{
		"node_id":                  nodeID,
		"config":                   requestConfigForDigest(node.NodeType, node.Config),
		"facts":                    factsOrEmpty(facts),
		"briefs":                   briefsOrEmpty(briefs),
		"references":               refIDs,
		"visual_system":            visualPayload,
		"visual_system_version_id": visualVersionID,
		"visual_overlay":           visualOverlay,
		"incoming_edge_ids":        incomingIDs,
	}), nil
}

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
	refIDs := make([]string, 0, len(references))
	for _, ref := range references {
		refIDs = append(refIDs, ref.AssetID)
	}
	return inputDigest(map[string]any{
		"node_id":           nodeID,
		"node_type":         node.NodeType,
		"config":            requestConfigForDigest(node.NodeType, node.Config),
		"facts":             factsOrEmpty(facts),
		"references":        refIDs,
		"incoming_edge_ids": incomingIDs,
	}), nil
}

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
	promptSource, err := graph.Node(promptEdge.SourceNodeID)
	if err != nil {
		return "", err
	}
	_, promptArtifactID, err := promptArtifact(promptSource.ID, sources)
	if err != nil {
		return "", err
	}
	var references []compiledReference
	var visualVersionID any
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
			_, versionID, _, err := compileVisual(source, sources[source.ID])
			if err != nil {
				return "", err
			}
			visualVersionID = versionID
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
	refIDs := make([]string, 0, len(references))
	for _, ref := range references {
		refIDs = append(refIDs, ref.AssetID)
	}
	return inputDigest(map[string]any{
		"node_id":                  nodeID,
		"config":                   requestConfigForDigest(node.NodeType, normalized),
		"prompt_artifact_id":       promptArtifactID,
		"references":               refIDs,
		"visual_system_version_id": visualVersionID,
		"incoming_edge_ids":        incomingIDs,
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
	return compiledReference{
		EdgeID: edge.ID, SourceNodeID: source.ID, AssetID: *assetID,
		Label: label, MIMEType: mimeType, Order: edge.Order, Role: role,
	}, nil
}

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
	if overrides, ok := payload["visual_overrides"].([]any); ok {
		return CatalogVisualOverlay(mergeVisualOverrideItems(overrides))
	}
	return nil
}

func mergeVisualOverrideItems(items []any) map[string]any {
	overlay := map[string]any{}
	for _, item := range items {
		entry, ok := asMap(item)
		if !ok {
			continue
		}
		overrides, ok := entry["overrides"].([]any)
		if !ok {
			continue
		}
		for _, fieldOverride := range overrides {
			fo, ok := asMap(fieldOverride)
			if !ok {
				continue
			}
			fieldName, _ := fo["field"].(string)
			if fieldName != "" {
				overlay[fieldName] = fo["value"]
			}
		}
	}
	return overlay
}

func promptArtifact(nodeID string, sources map[string]SourceRecord) (map[string]any, string, error) {
	record := sources[nodeID]
	if record.CurrentArtifactPayload == nil || record.CurrentArtifactID == nil {
		return nil, "", apperr.Validation("上游提示词尚未生成")
	}
	if !hasPromptPayload(stripV3Prompt(record.CurrentArtifactPayload)) {
		return nil, "", apperr.Validation("上游提示词尚未生成")
	}
	return cloneMap(record.CurrentArtifactPayload), *record.CurrentArtifactID, nil
}

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
			if overlayMap, ok := overlay.(map[string]any); ok {
				visual = cloneMap(overlayMap)
			} else if payloadMap, ok := payload.(map[string]any); ok {
				visual = cloneMap(payloadMap)
			}
		}
	}
	return facts, briefs, visual, refs, nil
}

func incomingPromptPayload(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (map[string]any, error) {
	for _, edge := range incomingSorted(graph, nodeID) {
		if edge.Role != RolePrompt {
			continue
		}
		payload, _, err := promptArtifact(edge.SourceNodeID, sources)
		return payload, err
	}
	return nil, apperr.Validation("图片生成节点缺少 prompt 边，不能运行")
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

func requestConfigForDigest(nodeType NodeType, config map[string]any) map[string]any {
	excluded := map[string]struct{}{}
	for key := range digestSelfOutputKeys[nodeType] {
		excluded[key] = struct{}{}
	}
	for key := range digestIgnoredKeys[nodeType] {
		excluded[key] = struct{}{}
	}
	out := map[string]any{}
	for key, value := range config {
		if _, skip := excluded[key]; skip {
			continue
		}
		out[key] = cloneValue(value)
	}
	return out
}

func inputDigest(payload map[string]any) string {
	sum := sha256.Sum256([]byte(pythonDumps(payload)))
	return hex.EncodeToString(sum[:])
}

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
