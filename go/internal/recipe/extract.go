package recipe

import (
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// ExtractInput 指定从 live 图提取完整配方、分组或选区。
type ExtractInput struct {
	SourceType string // workflow | group | selection
	GroupID    *string
	NodeIDs    []string // 仅 selection；workflow/group 必须为空
}

func recipeKind(sourceType string) string {
	if sourceType == SourceWorkflow {
		return kindWorkflow
	}
	return kindFragment
}

// validateSourceFields 检查提取范围三选一：完整工作流不能带 group/node；分组只能带 group_id；
// 多选必须非空且不重复的 node_ids。
func validateSourceFields(in ExtractInput) error {
	nodeIDs := in.NodeIDs
	if nodeIDs == nil {
		nodeIDs = []string{}
	}
	switch in.SourceType {
	case SourceWorkflow:
		if in.GroupID != nil || len(nodeIDs) > 0 {
			return apperr.Validation("完整工作流来源不能指定 group_id 或 node_ids")
		}
	case SourceGroup:
		if in.GroupID == nil || len(nodeIDs) > 0 {
			return apperr.Validation("分组来源必须且只能指定 group_id")
		}
	case SourceSelection:
		if in.GroupID != nil || len(nodeIDs) == 0 {
			return apperr.Validation("多选来源必须且只能指定非空 node_ids")
		}
		if _, err := uniqueKeys(nodeIDs, "node_ids"); err != nil {
			return apperr.Validation("node_ids 不能重复")
		}
	default:
		return apperr.Validation("请求体无效")
	}
	return nil
}

// extractPayload 从 live 图抽出配方：只保留选中节点及其之间的边，config 经 sanitizeConfig 去掉商品身份和生成结果。
func extractPayload(applied graph.AppliedGraph, in ExtractInput) (Payload, error) {
	if err := validateSourceFields(in); err != nil {
		return Payload{}, err
	}
	selectedIDs, err := selectedNodeIDs(applied, in)
	if err != nil {
		return Payload{}, err
	}
	selected := map[string]struct{}{}
	nodes := []graph.AppliedNode{}
	for _, node := range applied.Nodes {
		if _, ok := selectedIDs[node.ID]; ok {
			selected[node.ID] = struct{}{}
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return Payload{}, apperr.Validation("配方至少需要一个节点")
	}
	edges := []graph.AppliedEdge{}
	for _, edge := range applied.Edges {
		if _, ok := selected[edge.SourceNodeID]; !ok {
			continue
		}
		if _, ok := selected[edge.TargetNodeID]; !ok {
			continue
		}
		edges = append(edges, edge)
	}
	groups := []PayloadGroup{}
	for _, group := range applied.Groups {
		members := []string{}
		for _, node := range nodes {
			if node.GroupID != nil && *node.GroupID == group.ID {
				members = append(members, node.ID)
			}
		}
		if len(members) == 0 {
			continue
		}
		if in.SourceType == SourceSelection {
			partial := false
			for _, node := range applied.Nodes {
				if node.GroupID != nil && *node.GroupID == group.ID {
					if _, ok := selected[node.ID]; !ok {
						partial = true
						break
					}
				}
			}
			if partial {
				continue
			}
		}
		memberKeys := make([]string, 0, len(members))
		for _, id := range members {
			memberKeys = append(memberKeys, recipeKey(id))
		}
		groups = append(groups, PayloadGroup{
			Key:        recipeKey(group.ID),
			Title:      group.Title,
			MemberKeys: memberKeys,
		})
	}
	includedGroups := map[string]struct{}{}
	for _, group := range groups {
		includedGroups[group.Key] = struct{}{}
	}
	outNodes := make([]PayloadNode, 0, len(nodes))
	for _, node := range nodes {
		var groupKey *string
		if node.GroupID != nil {
			key := recipeKey(*node.GroupID)
			if _, ok := includedGroups[key]; ok {
				groupKey = strPtr(key)
			}
		}
		outNodes = append(outNodes, PayloadNode{
			Key:       recipeKey(node.ID),
			NodeType:  node.NodeType,
			Title:     node.Title,
			PositionX: node.PositionX,
			PositionY: node.PositionY,
			GroupKey:  groupKey,
			Config:    reusableConfig(node),
		})
	}
	outEdges := make([]PayloadEdge, 0, len(edges))
	for _, edge := range edges {
		outEdges = append(outEdges, PayloadEdge{
			Key:           recipeKey(edge.ID),
			SourceNodeKey: recipeKey(edge.SourceNodeID),
			TargetNodeKey: recipeKey(edge.TargetNodeID),
			DataType:      edge.DataType,
			Role:          edge.Role,
			Order:         edge.Order,
		})
	}
	payload := Payload{SchemaVersion: schemaVersion, Nodes: outNodes, Edges: outEdges, Groups: groups}
	if err := validatePayload(payload); err != nil {
		return Payload{}, err
	}
	return payload, nil
}

func selectedNodeIDs(applied graph.AppliedGraph, in ExtractInput) (map[string]struct{}, error) {
	known := map[string]struct{}{}
	for _, node := range applied.Nodes {
		known[node.ID] = struct{}{}
	}
	switch in.SourceType {
	case SourceWorkflow:
		out := map[string]struct{}{}
		for _, node := range applied.Nodes {
			out[node.ID] = struct{}{}
		}
		return out, nil
	case SourceGroup:
		if _, err := applied.Group(*in.GroupID); err != nil {
			return nil, err
		}
		out := map[string]struct{}{}
		for _, node := range applied.Nodes {
			if node.GroupID != nil && *node.GroupID == *in.GroupID {
				out[node.ID] = struct{}{}
			}
		}
		if len(out) == 0 {
			return nil, apperr.Validation("分组里没有可保存的节点")
		}
		return out, nil
	default:
		out := map[string]struct{}{}
		for _, id := range in.NodeIDs {
			if _, ok := known[id]; !ok {
				return nil, apperr.Validation("选区包含图上不存在的节点")
			}
			out[id] = struct{}{}
		}
		return out, nil
	}
}

func reusableConfig(node graph.AppliedNode) map[string]any {
	raw, _ := cloneValue(node.Config).(map[string]any)
	if raw == nil {
		raw = map[string]any{}
	}
	cleaned, _ := sanitizeConfig(raw).(map[string]any)
	if cleaned == nil {
		return map[string]any{}
	}
	return cleaned
}

// sanitizeConfig 递归丢掉提示词正文、拓扑字段，并把商品身份键写成 null。配方不能带走源商品或生成结果。
func sanitizeConfig(value any) any {
	switch t := value.(type) {
	case map[string]any:
		cleaned := map[string]any{}
		for key, item := range t {
			if _, ok := strippedPromptKeys[key]; ok {
				continue
			}
			if _, ok := forbiddenPlanKeys[key]; ok {
				continue
			}
			if _, ok := identityConfigKeys[key]; ok {
				cleaned[key] = nil
				continue
			}
			cleaned[key] = sanitizeConfig(item)
		}
		return cleaned
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, sanitizeConfig(item))
		}
		return out
	default:
		return value
	}
}

func recipeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
