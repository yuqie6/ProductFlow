package graph

import (
	"sort"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

const (
	RunScopeGraph  = "graph"
	RunScopeNode   = "node"
	RunScopeToNode = "to_node"

	RunStatusRunning   = "running"
	RunStatusSucceeded = "succeeded"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"
	RunStatusUnknown   = "unknown"

	NodeRunQueued    = "queued"
	NodeRunRunning   = "running"
	NodeRunSucceeded = "succeeded"
	NodeRunFailed    = "failed"
	NodeRunUnknown   = "unknown"

	GraphCancelledReason       = "已取消"
	ProviderUnknownDetail      = "工作流供应商请求结果未知，系统未自动重试。请检查供应商记录后重新发起工作流。"
	GraphSnapshotSchemaVersion = 1
)

// SelectRunNodeIDs GRAPH 入队可运行处理节点。NODE 入队目标及需要更新的祖先，不顺带下游 image。
func SelectRunNodeIDs(graph AppliedGraph, scope, targetNodeID string, sources map[string]SourceRecord) ([]string, error) {
	var processingIDs []string
	for _, node := range graph.Nodes {
		if IsProcessingNode(node.NodeType) {
			processingIDs = append(processingIDs, node.ID)
		}
	}
	var selected []string
	switch scope {
	case RunScopeGraph:
		for _, nodeID := range processingIDs {
			if hasRequiredEdges(graph, nodeID) {
				selected = append(selected, nodeID)
			}
		}
	case RunScopeNode, RunScopeToNode:
		if targetNodeID == "" {
			return nil, apperr.Validation("节点运行范围必须指定目标节点")
		}
		target, err := graph.Node(targetNodeID)
		if err != nil {
			return nil, err
		}
		if !IsProcessingNode(target.NodeType) {
			return nil, apperr.Validation("只能运行视觉规范、创作要求、提示词生成或图片生成节点")
		}
		if err := rejectIncompleteRequiredEdges(graph, target); err != nil {
			return nil, apperr.Validation("目标节点不可运行: " + err.Error())
		}
		ancestors := processingAncestors(graph, targetNodeID)
		if scope == RunScopeToNode {
			for _, nodeID := range ancestors {
				if hasRequiredEdges(graph, nodeID) {
					selected = append(selected, nodeID)
				}
			}
			selected = append(selected, targetNodeID)
		} else {
			required := requiredProcessingProducers(graph, targetNodeID)
			selected = append(selected, targetNodeID)
			for _, nodeID := range ancestors {
				if !hasRequiredEdges(graph, nodeID) {
					continue
				}
				_, isRequired := required[nodeID]
				if processingNodeNeedsUpdate(graph, nodeID, sources, isRequired) {
					selected = append(selected, nodeID)
				}
			}
		}
	default:
		return nil, apperr.Validation("请求体无效")
	}
	ordered := topoOrder(graph, selected)
	if len(ordered) == 0 {
		return nil, apperr.Validation("没有可运行的处理节点")
	}
	return ordered, nil
}

func hasRequiredEdges(graph AppliedGraph, nodeID string) bool {
	node, err := graph.Node(nodeID)
	if err != nil {
		return false
	}
	return rejectIncompleteRequiredEdges(graph, node) == nil
}

func requiredProcessingProducers(graph AppliedGraph, targetNodeID string) map[string]struct{} {
	required := map[string]struct{}{}
	stack := []string{targetNodeID}
	seen := map[string]struct{}{}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		node, err := graph.Node(current)
		if err != nil {
			continue
		}
		incoming := incomingSorted(graph, current)
		for _, contract := range runRequiredInputs(node.NodeType) {
			for _, edge := range incoming {
				if edge.DataType != contract.DataType || edge.Role != contract.Role {
					continue
				}
				source, err := graph.Node(edge.SourceNodeID)
				if err != nil {
					continue
				}
				if !IsProcessingNode(source.NodeType) {
					continue
				}
				required[source.ID] = struct{}{}
				stack = append(stack, source.ID)
			}
		}
	}
	return required
}

func processingNodeNeedsUpdate(graph AppliedGraph, nodeID string, sources map[string]SourceRecord, requiredProducer bool) bool {
	if sources == nil {
		return requiredProducer
	}
	record := sources[nodeID]
	digest, err := compileInputDigest(graph, nodeID, sources)
	if record.CurrentArtifactID != nil && record.CurrentInputDigest != nil && *record.CurrentArtifactID != "" && *record.CurrentInputDigest != "" {
		if err != nil {
			return requiredProducer
		}
		return digest != *record.CurrentInputDigest
	}
	return requiredProducer
}

func processingAncestors(graph AppliedGraph, nodeID string) []string {
	seen := map[string]struct{}{}
	var ordered []string
	var stack []string
	for _, edge := range graph.Incoming(nodeID) {
		stack = append(stack, edge.SourceNodeID)
	}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		node, err := graph.Node(current)
		if err != nil {
			continue
		}
		if IsProcessingNode(node.NodeType) {
			ordered = append(ordered, current)
		}
		for _, edge := range graph.Incoming(current) {
			stack = append(stack, edge.SourceNodeID)
		}
	}
	return ordered
}

func topoOrder(graph AppliedGraph, selected []string) []string {
	selectedSet := map[string]struct{}{}
	incomingCount := map[string]int{}
	outgoing := map[string][]string{}
	for _, id := range selected {
		selectedSet[id] = struct{}{}
		incomingCount[id] = 0
		outgoing[id] = nil
	}
	for _, edge := range graph.Edges {
		if _, ok := selectedSet[edge.SourceNodeID]; !ok {
			continue
		}
		if _, ok := selectedSet[edge.TargetNodeID]; !ok {
			continue
		}
		outgoing[edge.SourceNodeID] = append(outgoing[edge.SourceNodeID], edge.TargetNodeID)
		incomingCount[edge.TargetNodeID]++
	}
	var ready []string
	for id, count := range incomingCount {
		if count == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	var ordered []string
	for len(ready) > 0 {
		nodeID := ready[0]
		ready = ready[1:]
		ordered = append(ordered, nodeID)
		next := append([]string{}, outgoing[nodeID]...)
		sort.Strings(next)
		for _, target := range next {
			incomingCount[target]--
			if incomingCount[target] == 0 {
				ready = append(ready, target)
				sort.Strings(ready)
			}
		}
	}
	return ordered
}
