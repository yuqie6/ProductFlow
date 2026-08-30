package graph

import (
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

const (
	RunScopeGraph     = "graph"
	RunScopeNode      = "node"
	RunScopeToNode    = "to_node"
	RunScopeSelection = "selection"

	RunStatusQueued    = "queued"
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
	NodeRunSkipped   = "skipped"
	NodeRunCancelled = "cancelled"

	PlannedGenerate = "generate"
	PlannedReuse    = "reuse"
	PlannedFrozen   = "frozen"
	PlannedBlocked  = "blocked"

	GraphCancelledReason       = "已取消"
	ProviderUnknownDetail      = "工作流供应商请求结果未知，系统未自动重试。请检查供应商记录后重新发起工作流。"
	GraphSnapshotSchemaVersion = 1
)

// SelectRunNodeIDs GRAPH 入队所有具备必需输入的处理节点，由执行器把无需重算的节点落成 skipped。
// NODE 只入队目标。TO_NODE 入队目标及 fill 仍会干活的祖先。SELECTION 入队显式节点集合。
func SelectRunNodeIDs(graph AppliedGraph, scope, targetNodeID string, sources map[string]SourceRecord) ([]string, error) {
	return SelectRunNodeIDsWithMode(graph, scope, targetNodeID, nil, sources, false, RegenerateFill)
}

func SelectRunNodeIDsWithMode(graph AppliedGraph, scope, targetNodeID string, nodeIDs []string, sources map[string]SourceRecord, force bool, mode string) ([]string, error) {
	mode = validRegenerateMode(mode)
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
			if !hasRequiredEdges(graph, nodeID) {
				continue
			}
			selected = append(selected, nodeID)
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
		selected = append(selected, targetNodeID)
		if scope == RunScopeToNode {
			for _, nodeID := range processingAncestors(graph, targetNodeID) {
				if !hasRequiredEdges(graph, nodeID) {
					continue
				}
				node, err := graph.Node(nodeID)
				if err != nil {
					continue
				}
				if nodeShouldCook(graph, node, sources, false, RegenerateFill) {
					selected = append(selected, nodeID)
				}
			}
		}
	case RunScopeSelection:
		if len(nodeIDs) == 0 {
			return nil, apperr.Validation("选区运行必须指定 node_ids")
		}
		seen := map[string]struct{}{}
		for _, nodeID := range nodeIDs {
			nodeID = strings.TrimSpace(nodeID)
			if nodeID == "" {
				continue
			}
			if _, ok := seen[nodeID]; ok {
				continue
			}
			seen[nodeID] = struct{}{}
			target, err := graph.Node(nodeID)
			if err != nil {
				return nil, err
			}
			if !IsProcessingNode(target.NodeType) {
				return nil, apperr.Validation("只能运行视觉规范、创作要求、提示词生成或图片生成节点")
			}
			if err := rejectIncompleteRequiredEdges(graph, target); err != nil {
				return nil, apperr.Validation("目标节点不可运行: " + err.Error())
			}
			selected = append(selected, nodeID)
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

func forceTargetSet(scope, targetNodeID string, nodeIDs []string, force bool) map[string]bool {
	out := map[string]bool{}
	if !force {
		return out
	}
	switch scope {
	case RunScopeNode, RunScopeToNode:
		if targetNodeID != "" {
			out[targetNodeID] = true
		}
	case RunScopeSelection:
		for _, id := range nodeIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				out[id] = true
			}
		}
	}
	return out
}

type RunPreviewNode struct {
	NodeID string `json:"node_id"`
	Title  string `json:"title"`
	Action string `json:"planned_action"`
	Reason string `json:"reason"`
}

func PlanRun(graph AppliedGraph, scope, targetNodeID string, nodeIDs []string, sources map[string]SourceRecord, force bool, mode string) ([]RunPreviewNode, error) {
	mode = validRegenerateMode(mode)
	forceTargets := forceTargetSet(scope, targetNodeID, nodeIDs, force)
	inScope, err := previewScopeSet(graph, scope, targetNodeID, nodeIDs)
	if err != nil {
		return nil, err
	}
	out := make([]RunPreviewNode, 0)
	for _, node := range graph.Nodes {
		if !IsProcessingNode(node.NodeType) {
			continue
		}
		if _, ok := inScope[node.ID]; !ok && len(inScope) > 0 {
			continue
		}
		action, reason := plannedActionFor(graph, node, sources, forceTargets[node.ID], mode)
		out = append(out, RunPreviewNode{NodeID: node.ID, Title: node.Title, Action: action, Reason: reason})
	}
	return out, nil
}

func previewScopeSet(graph AppliedGraph, scope, targetNodeID string, nodeIDs []string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	switch scope {
	case RunScopeGraph, "":
		for _, node := range graph.Nodes {
			if IsProcessingNode(node.NodeType) {
				out[node.ID] = struct{}{}
			}
		}
	case RunScopeNode:
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
		out[targetNodeID] = struct{}{}
	case RunScopeToNode:
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
		out[targetNodeID] = struct{}{}
		for _, id := range processingAncestors(graph, targetNodeID) {
			out[id] = struct{}{}
		}
	case RunScopeSelection:
		if len(nodeIDs) == 0 {
			return nil, apperr.Validation("选区运行必须指定 node_ids")
		}
		for _, id := range nodeIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			node, err := graph.Node(id)
			if err != nil {
				return nil, err
			}
			if !IsProcessingNode(node.NodeType) {
				return nil, apperr.Validation("只能运行视觉规范、创作要求、提示词生成或图片生成节点")
			}
			out[id] = struct{}{}
		}
		if len(out) == 0 {
			return nil, apperr.Validation("选区运行必须指定 node_ids")
		}
	default:
		return nil, apperr.Validation("请求体无效")
	}
	return out, nil
}

func plannedActionFor(graph AppliedGraph, node AppliedNode, sources map[string]SourceRecord, forceTarget bool, mode string) (string, string) {
	if !hasRequiredEdges(graph, node.ID) {
		return PlannedBlocked, "缺少必连输入"
	}
	if isContentNodeType(node.NodeType) {
		if contentNodeShouldGenerate(node, forceTarget, mode) {
			return PlannedGenerate, "种子文稿将生成"
		}
		origin := DocumentOrigin(node)
		if origin == OriginAuthored {
			return PlannedFrozen, "已手写，fill 不覆盖"
		}
		return PlannedFrozen, "已生成，fill 不覆盖"
	}
	if node.NodeType == NodeImageGeneration {
		if forceTarget {
			return PlannedGenerate, "强制重新生成"
		}
		if !imageNeedsCook(graph, node.ID, sources) {
			return PlannedReuse, "输入签名未变"
		}
		return PlannedGenerate, "输入已变化或尚无产物"
	}
	return PlannedBlocked, "不能运行该节点类型"
}

func nodeShouldCook(graph AppliedGraph, node AppliedNode, sources map[string]SourceRecord, forceTarget bool, mode string) bool {
	if isContentNodeType(node.NodeType) {
		return contentNodeShouldGenerate(node, forceTarget, mode)
	}
	if node.NodeType == NodeImageGeneration {
		if forceTarget {
			return true
		}
		return imageNeedsCook(graph, node.ID, sources)
	}
	return false
}

func imageNeedsCook(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) bool {
	if sources == nil {
		return true
	}
	record := sources[nodeID]
	digest, err := compileInputDigest(graph, nodeID, sources)
	if record.CurrentArtifactID != nil && record.CurrentInputDigest != nil && *record.CurrentArtifactID != "" && *record.CurrentInputDigest != "" {
		if err != nil {
			return true
		}
		return digest != *record.CurrentInputDigest
	}
	return true
}

func hasRequiredEdges(graph AppliedGraph, nodeID string) bool {
	node, err := graph.Node(nodeID)
	if err != nil {
		return false
	}
	return rejectIncompleteRequiredEdges(graph, node) == nil
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
