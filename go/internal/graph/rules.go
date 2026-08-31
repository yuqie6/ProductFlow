package graph

import (
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// RuleNode 给拓扑排序与 NodeConfigStatus 用的精简节点，不含坐标、分组或文稿来源。
// BoundAssetID 为 nil 或空串时 image_asset 判 incomplete。不要和 AppliedNode 或 NodeView 搞混；不落库。
type RuleNode struct {
	ID           string
	NodeType     NodeType       // schema-v3 闭集
	Config       map[string]any // 只看 Catalog 必填项，不看产物 digest
	BoundAssetID *string
}

// RuleEdge 给环检测与入边计数用的精简边。Order 参与同一 role 多边次序，环检测只用 Source/Target。
// 不要和 AppliedEdge / EdgeView 搞混；不落库。有环时 TopologicalGraphNodeIDs 返回 Validation。
type RuleEdge struct {
	ID           string
	SourceNodeID string
	TargetNodeID string
	DataType     EdgeDataType // 由 Catalog accepts 决定
	Role         EdgeRole     // 等于 Catalog 端口 / React Flow handle id
	Order        int          // 同一 role 多边次序；环检测只用 Source/Target
}

// TopologicalGraphNodeIDs 返回稳定拓扑序。有环返回 Validation。
func TopologicalGraphNodeIDs(nodes []RuleNode, edges []RuleEdge) ([]string, error) {
	nodesByID := map[string]RuleNode{}
	incomingCount := map[string]int{}
	outgoing := map[string][]string{}
	for _, node := range nodes {
		nodesByID[node.ID] = node
		incomingCount[node.ID] = 0
		outgoing[node.ID] = nil
	}
	for _, edge := range edges {
		if _, ok := nodesByID[edge.SourceNodeID]; !ok {
			return nil, apperr.Validation("工作流连线引用了不存在的节点")
		}
		if _, ok := nodesByID[edge.TargetNodeID]; !ok {
			return nil, apperr.Validation("工作流连线引用了不存在的节点")
		}
		outgoing[edge.SourceNodeID] = append(outgoing[edge.SourceNodeID], edge.TargetNodeID)
		incomingCount[edge.TargetNodeID]++
	}
	var queue []string
	for _, nodeID := range sortedKeys(incomingCount) {
		if incomingCount[nodeID] == 0 {
			queue = append(queue, nodeID)
		}
	}
	ordered := make([]string, 0, len(nodesByID))
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		ordered = append(ordered, nodeID)
		for _, targetID := range outgoing[nodeID] {
			incomingCount[targetID]--
			if incomingCount[targetID] == 0 {
				queue = append(queue, targetID)
			}
		}
	}
	if len(ordered) != len(nodesByID) {
		return nil, apperr.Validation("工作流不能包含循环依赖")
	}
	return ordered, nil
}

func validateGraphEdge(source, target RuleNode, existing []RuleEdge) (inputContract, error) {
	if source.ID == target.ID {
		return inputContract{}, apperr.Validation("节点不能连接自身")
	}
	contract, err := RequireGraphConnection(source.NodeType, target.NodeType)
	if err != nil {
		return inputContract{}, err
	}
	sameRole := 0
	for _, edge := range existing {
		if edge.DataType == contract.DataType && edge.Role == contract.Role {
			sameRole++
		}
	}
	if contract.MaxCount != nil && sameRole >= *contract.MaxCount {
		return inputContract{}, apperr.Validation("目标节点该类输入已达到上限")
	}
	return contract, nil
}

// NodeConfigStatus 只看必填 config 与运行所需入边，不看产物 digest。
func NodeConfigStatus(node RuleNode, incoming []RuleEdge) ConfigStatus {
	if msg := nodeConfigError(node); msg != "" {
		return ConfigIncomplete
	}
	if node.NodeType == NodeProductSource {
		config := node.Config
		if config == nil {
			config = map[string]any{}
		}
		if _, has := config["source_product_id"]; has {
			sourceID, ok := config["source_product_id"].(string)
			if !ok || strings.TrimSpace(sourceID) == "" {
				return ConfigIncomplete
			}
			if factSetID, exists := config["fact_set_version_id"]; exists && factSetID != nil {
				s, ok := factSetID.(string)
				if !ok || strings.TrimSpace(s) == "" {
					return ConfigIncomplete
				}
			}
		}
	}
	if node.NodeType == NodeImageAsset && (node.BoundAssetID == nil || *node.BoundAssetID == "") {
		return ConfigIncomplete
	}
	if node.NodeType == NodeVisualSystem && !hasVisualSystemConfig(node.Config) {
		return ConfigIncomplete
	}
	if requiredConfigIncomplete(node.NodeType, node.Config) {
		return ConfigIncomplete
	}
	if len(missingRequiredInputs(node, incoming)) > 0 {
		return ConfigIncomplete
	}
	return ConfigReady
}

func nodeConfigError(node RuleNode) string {
	normalized, err := NormalizeNodeConfig(node.NodeType, node.Config)
	if err != nil {
		return err.Error()
	}
	if node.NodeType == NodeImageGeneration {
		if _, ok := normalized["generation_spec"]; !ok {
			return "图片生成节点缺少有效 GenerationSpec"
		}
	}
	return ""
}

func missingRequiredInputs(node RuleNode, incoming []RuleEdge) []inputContract {
	var missing []inputContract
	for _, contract := range runRequiredInputs(node.NodeType) {
		found := false
		for _, edge := range incoming {
			if edge.DataType == contract.DataType && edge.Role == contract.Role {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, contract)
		}
	}
	return missing
}

func hasVisualSystemConfig(config map[string]any) bool {
	payload := config
	if payload == nil {
		payload = map[string]any{}
	}
	if versionID, ok := payload["visual_system_version_id"].(string); ok && strings.TrimSpace(versionID) != "" {
		return true
	}
	if overlay, ok := asMap(payload["visual_overlay"]); ok && len(overlay) > 0 {
		return true
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
