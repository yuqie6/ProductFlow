package graph

import (
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

const (
	// RunScopeGraph 入队所有具备必需输入的处理节点。全图运行不能携带 force。
	RunScopeGraph = "graph"
	// RunScopeNode 只入队显式目标节点。
	RunScopeNode = "node"
	// RunScopeToNode 入队目标及仍需 cook 的处理祖先。
	RunScopeToNode = "to_node"
	// RunScopeSelection 入队显式 node_ids；镜头「跑这一组」走此范围。
	RunScopeSelection = "selection"

	// RunStatusQueued 表示已提交、等待当前 running 结束。
	RunStatusQueued = "queued"
	// RunStatusRunning 表示该图当前唯一在执行的 WorkflowGraphRun。
	RunStatusRunning = "running"
	// RunStatusSucceeded 表示范围内节点均 succeeded 或 skipped。
	RunStatusSucceeded = "succeeded"
	// RunStatusFailed 表示有失败节点且没有 unknown；可 RetryRun。
	RunStatusFailed = "failed"
	// RunStatusCancelled 表示用户取消；剩余 queued/running 节点写成 cancelled。
	RunStatusCancelled = "cancelled"
	// RunStatusUnknown 表示无法证明的 provider 结果；IsRetryable=false，不自动当失败重试。
	RunStatusUnknown = "unknown"

	// NodeRunQueued 等待上游就绪或容量。
	NodeRunQueued = "queued"
	// NodeRunRunning 已被 worker claim。
	NodeRunRunning = "running"
	// NodeRunSucceeded 已写入产物。
	NodeRunSucceeded = "succeeded"
	// NodeRunFailed 已证明失败；下游处理节点标 failed，无依赖路径的兄弟继续跑。
	NodeRunFailed = "failed"
	// NodeRunUnknown 无法证明的 provider 结果；与 failed 一样挡住下游，但不自动重试。
	NodeRunUnknown = "unknown"
	// NodeRunSkipped 输入 digest 未变或文稿冻结；对下游视为就绪。
	NodeRunSkipped = "skipped"
	// NodeRunCancelled 随 run 取消写入。
	NodeRunCancelled = "cancelled"

	// PlannedGenerate 预览：将调用 provider。
	PlannedGenerate = "generate"
	// PlannedReuse 预览：digest 未变，执行时 skipped。
	PlannedReuse = "reuse"
	// PlannedFrozen 预览：authored/generated 文稿非 force 不 cook。
	PlannedFrozen = "frozen"
	// PlannedBlocked 预览：缺少运行所需入边或类型不可跑。
	PlannedBlocked = "blocked"

	// GraphCancelledReason 写入 cancelled run 与节点的 failure_reason。
	GraphCancelledReason = "已取消"
	// ProviderUnknownDetail 是 unknown 终态给用户看的说明，系统未自动重试。
	ProviderUnknownDetail = "工作流供应商请求结果未知，系统未自动重试。请检查供应商记录后重新发起工作流。"
	// GraphSnapshotSchemaVersion 是 run snapshot JSON 的版本号。
	GraphSnapshotSchemaVersion = 1
)

// SelectRunNodeIDs GRAPH 入队所有具备必需输入的处理节点，由执行器把无需重算的节点落成 skipped。
// NODE 只入队目标。TO_NODE 入队目标及仍需生成的祖先。SELECTION 入队显式节点集合。
// 范围或目标非法时返回 Validation。
func SelectRunNodeIDs(graph AppliedGraph, scope, targetNodeID string, sources map[string]SourceRecord) ([]string, error) {
	return SelectRunNodeIDsWithMode(graph, scope, targetNodeID, nil, sources, false, DocumentActionComplete)
}

// SelectRunNodeIDsWithMode 按范围选出处理节点。force 只对 node|to_node|selection 的显式目标生效；graph 范围忽略 force。
// 缺目标、非处理节点、缺必连边或无可运行节点返回 Validation。
func SelectRunNodeIDsWithMode(graph AppliedGraph, scope, targetNodeID string, nodeIDs []string, sources map[string]SourceRecord, force bool, mode string) ([]string, error) {
	mode = validDocumentAction(mode)
	inScope, err := previewScopeSet(graph, scope, targetNodeID, nodeIDs)
	if err != nil {
		return nil, err
	}
	if scope == RunScopeNode || scope == RunScopeSelection {
		for nodeID := range inScope {
			for _, ancestorID := range processingAncestors(graph, nodeID) {
				inScope[ancestorID] = struct{}{}
			}
		}
	}
	var processingIDs []string
	for _, node := range graph.Nodes {
		if IsProcessingNode(node.NodeType) {
			// Frozen ancestors still supply inputs, so validate them without enqueueing them.
			if _, selected := inScope[node.ID]; selected {
				if msg := nodeConfigError(RuleNode{node.ID, node.NodeType, node.Config, node.BoundAssetID}); msg != "" {
					return nil, apperr.Validation("节点「" + node.Title + "」配置无效: " + msg)
				}
			}
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
				if nodeShouldCook(graph, node, sources, false, DocumentActionComplete) {
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

// forceTargetSet 只把 node|to_node 的 NodeID 或 selection 的 node_ids 标为 force；graph 范围得到空集。
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

// RunPreviewNode 是 PreviewRun / PlanRun 里一个处理节点的计划动作，给预览 HTTP 用。
// Action 为 generate（将调供应商）、reuse（digest 未变，执行 skipped）、frozen（文稿非 force 不 cook）、blocked（缺入边）。
// 不是 workflow_graph_node_runs 行。改 Action 枚举必须同步 Web 预览文案与 Planned* 常量。
type RunPreviewNode struct {
	NodeID string `json:"node_id"`
	Title  string `json:"title"`
	// Action 为 generate|reuse|frozen|blocked，JSON 键是 planned_action。
	Action string `json:"planned_action"`
	Reason string `json:"reason"` // 给预览文案，如「输入签名未变」
}

// PlanRun 为范围内每个处理节点计算 planned_action，不入队。force 仅作用于显式目标。
// 范围或目标非法时返回 Validation。
func PlanRun(graph AppliedGraph, scope, targetNodeID string, nodeIDs []string, sources map[string]SourceRecord, force bool, mode string) ([]RunPreviewNode, error) {
	mode = validDocumentAction(mode)
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

// plannedActionFor 给 PreviewRun 算 generate/reuse/frozen/blocked，不写库。
// 内容节点非 force 且非 seed 为 frozen；图片看 digest。缺必连边为 blocked。force 只对显式目标为 true。
func plannedActionFor(graph AppliedGraph, node AppliedNode, sources map[string]SourceRecord, forceTarget bool, mode string) (string, string) {
	if err := rejectIncompleteRequiredEdges(graph, node); err != nil {
		return PlannedBlocked, err.Error()
	}
	for _, ancestorID := range processingAncestors(graph, node.ID) {
		ancestor, err := graph.Node(ancestorID)
		if err != nil {
			return PlannedBlocked, err.Error()
		}
		if msg := nodeConfigError(RuleNode{ancestor.ID, ancestor.NodeType, ancestor.Config, ancestor.BoundAssetID}); msg != "" {
			return PlannedBlocked, "节点「" + ancestor.Title + "」配置无效: " + msg
		}
	}
	if isContentNodeType(node.NodeType) {
		if contentNodeShouldGenerate(node, forceTarget, mode) {
			return PlannedGenerate, "种子文稿将生成"
		}
		origin := DocumentOrigin(node)
		if origin == OriginAuthored {
			return PlannedFrozen, "人工文稿保持不变"
		}
		return PlannedFrozen, "正式文稿保持不变"
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

// nodeShouldCook 决定 TO_NODE 是否拉入祖先。Fill cook 只对 seed 文稿为 true；force 目标除外。图片看 digest。
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

// processingAncestors 沿入边收集处理祖先，供 to_node 选点。非处理源不入列。只扫入边，不保证拓扑序。
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

// topoOrder 只在选中集合内按入边排序，供入队 sort_order。环或不在集合的边被忽略。
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
