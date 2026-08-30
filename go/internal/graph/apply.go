package graph

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func (g AppliedGraph) Node(nodeID string) (AppliedNode, error) {
	for _, node := range g.Nodes {
		if node.ID == nodeID {
			return node, nil
		}
	}
	return AppliedNode{}, apperr.Validation("图节点不存在")
}

func (g AppliedGraph) Group(groupID string) (AppliedGroup, error) {
	for _, group := range g.Groups {
		if group.ID == groupID {
			return group, nil
		}
	}
	return AppliedGroup{}, apperr.Validation("图分组不存在")
}

func (g AppliedGraph) Incoming(nodeID string) []AppliedEdge {
	var out []AppliedEdge
	for _, edge := range g.Edges {
		if edge.TargetNodeID == nodeID {
			out = append(out, edge)
		}
	}
	return out
}

func (g AppliedGraph) ConfigStatus(nodeID string) (ConfigStatus, error) {
	node, err := g.Node(nodeID)
	if err != nil {
		return "", err
	}
	incoming := g.Incoming(nodeID)
	ruleIncoming := make([]RuleEdge, 0, len(incoming))
	for _, edge := range incoming {
		ruleIncoming = append(ruleIncoming, RuleEdge{edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
	}
	return NodeConfigStatus(RuleNode{node.ID, node.NodeType, node.Config, node.BoundAssetID}, ruleIncoming), nil
}

// Apply 把 ops 打到当前图上并跑 catalog/规则校验；不碰数据库。
func Apply(graph AppliedGraph, changeSet ChangeSet) (AppliedGraph, error) {
	if err := validateChangeSet(changeSet); err != nil {
		return AppliedGraph{}, err
	}
	if changeSet.BaseGraphRevision != graph.Revision {
		return AppliedGraph{}, apperr.Conflict("图 revision 已变化，请刷新后重试")
	}
	nodes := map[string]AppliedNode{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = cloneAppliedNode(node)
	}
	edges := map[string]AppliedEdge{}
	for _, edge := range graph.Edges {
		edges[edge.ID] = edge
	}
	groups := map[string]AppliedGroup{}
	for _, group := range graph.Groups {
		groups[group.ID] = group
	}
	aliases := map[string]string{}
	for id := range nodes {
		aliases[id] = id
	}
	for id := range groups {
		aliases[id] = id
	}
	for id := range edges {
		aliases[id] = id
	}
	resolve := func(ref string) (string, error) {
		ref = strings.TrimSpace(ref)
		if id, ok := aliases[ref]; ok {
			return id, nil
		}
		return "", apperr.Validation("ChangeSet 引用了不存在的图对象")
	}

	for _, operation := range changeSet.Operations {
		switch op := operation.(type) {
		case CreateGroupOp:
			clientRef := strings.TrimSpace(op.ClientRef)
			if _, exists := aliases[clientRef]; exists {
				return AppliedGraph{}, apperr.Validation("ChangeSet client_ref 与已有对象冲突")
			}
			groups[clientRef] = AppliedGroup{ID: clientRef, Title: strings.TrimSpace(op.Title)}
			aliases[clientRef] = clientRef
			for _, memberRef := range op.MemberRefs {
				memberID, err := resolve(memberRef)
				if err != nil {
					return AppliedGraph{}, err
				}
				node, ok := nodes[memberID]
				if !ok {
					return AppliedGraph{}, apperr.Validation("ChangeSet 引用了不存在的图对象")
				}
				node.GroupID = stringPtr(clientRef)
				nodes[memberID] = node
			}
		case CreateNodeOp:
			clientRef := strings.TrimSpace(op.ClientRef)
			if _, exists := aliases[clientRef]; exists {
				return AppliedGraph{}, apperr.Validation("ChangeSet client_ref 与已有对象冲突")
			}
			origin := stampDocumentOriginOnCreate(op.NodeType, op.DocumentOrigin)
			filled := FillDefaultNodeConfig(op.NodeType, cloneMap(op.Config))
			config, err := NormalizeNodeConfig(op.NodeType, filled)
			if err != nil {
				return AppliedGraph{}, err
			}
			var groupID *string
			if op.GroupRef != nil && strings.TrimSpace(*op.GroupRef) != "" {
				resolved, err := resolve(*op.GroupRef)
				if err != nil {
					return AppliedGraph{}, err
				}
				groupID = stringPtr(resolved)
			}
			nodes[clientRef] = AppliedNode{
				ID:             clientRef,
				NodeType:       op.NodeType,
				Title:          strings.TrimSpace(op.Title),
				PositionX:      op.PositionX,
				PositionY:      op.PositionY,
				Config:         config,
				BoundAssetID:   cloneStringPtr(op.BoundAssetID),
				GroupID:        groupID,
				DocumentOrigin: origin,
			}
			aliases[clientRef] = clientRef
		case ConnectNodesOp:
			sourceID, err := resolve(op.SourceRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			targetID, err := resolve(op.TargetRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			source, ok := nodes[sourceID]
			if !ok {
				return AppliedGraph{}, apperr.Validation("ChangeSet 引用了不存在的图对象")
			}
			target, ok := nodes[targetID]
			if !ok {
				return AppliedGraph{}, apperr.Validation("ChangeSet 引用了不存在的图对象")
			}
			var existing []RuleEdge
			var existingApplied []AppliedEdge
			for _, edge := range edges {
				if edge.TargetNodeID == target.ID {
					existingApplied = append(existingApplied, edge)
					existing = append(existing, RuleEdge{edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
				}
			}
			contract, err := validateGraphEdge(
				RuleNode{source.ID, source.NodeType, source.Config, source.BoundAssetID},
				RuleNode{target.ID, target.NodeType, target.Config, target.BoundAssetID},
				existing,
			)
			if err != nil {
				return AppliedGraph{}, err
			}
			for _, edge := range existingApplied {
				if edge.SourceNodeID == source.ID && edge.TargetNodeID == target.ID && edge.Role == contract.Role {
					return AppliedGraph{}, apperr.Validation("相同输入连线已存在")
				}
			}
			clientRef := strings.TrimSpace(op.ClientRef)
			if _, exists := aliases[clientRef]; exists {
				return AppliedGraph{}, apperr.Validation("ChangeSet client_ref 与已有对象冲突")
			}
			dataType, err := GraphNodeOutputType(source.NodeType)
			if err != nil {
				return AppliedGraph{}, err
			}
			edges[clientRef] = AppliedEdge{
				ID:           clientRef,
				SourceNodeID: source.ID,
				TargetNodeID: target.ID,
				DataType:     dataType,
				Role:         contract.Role,
				Order:        op.Order,
			}
			aliases[clientRef] = clientRef
		case DisconnectEdgeOp:
			edgeID, err := resolve(op.EdgeRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			delete(edges, edgeID)
			delete(aliases, edgeID)
		case ReorderEdgesOp:
			nodeID, err := resolve(op.NodeRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			if _, ok := nodes[nodeID]; !ok {
				return AppliedGraph{}, apperr.Validation("图节点不存在")
			}
			role := op.Role
			seen := map[string]struct{}{}
			for index, ref := range op.EdgeRefs {
				edgeID, err := resolve(ref)
				if err != nil {
					return AppliedGraph{}, err
				}
				edge, ok := edges[edgeID]
				if !ok {
					return AppliedGraph{}, apperr.Validation("工作流连线不存在")
				}
				if edge.TargetNodeID != nodeID || edge.Role != role {
					return AppliedGraph{}, apperr.Validation("只能重排指向该节点同一角色的入边")
				}
				if _, dup := seen[edgeID]; dup {
					return AppliedGraph{}, apperr.Validation("ChangeSet 内部 client_ref 不能重复")
				}
				seen[edgeID] = struct{}{}
				edge.Order = index
				edges[edgeID] = edge
			}
			var remaining []string
			for edgeID, edge := range edges {
				if edge.TargetNodeID != nodeID || edge.Role != role {
					continue
				}
				if _, ok := seen[edgeID]; ok {
					continue
				}
				remaining = append(remaining, edgeID)
			}
			sort.Strings(remaining)
			for index, edgeID := range remaining {
				edge := edges[edgeID]
				edge.Order = len(op.EdgeRefs) + index
				edges[edgeID] = edge
			}
		case DeleteNodeOp:
			nodeID, err := resolve(op.NodeRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			var incident []string
			for edgeID, edge := range edges {
				if edge.SourceNodeID == nodeID || edge.TargetNodeID == nodeID {
					incident = append(incident, edgeID)
				}
			}
			for _, edgeID := range incident {
				delete(edges, edgeID)
				delete(aliases, edgeID)
			}
			delete(nodes, nodeID)
			delete(aliases, nodeID)
		case RenameNodeOp:
			nodeID, err := resolve(op.NodeRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			node, ok := nodes[nodeID]
			if !ok {
				return AppliedGraph{}, apperr.Validation("图节点不存在")
			}
			node.Title = strings.TrimSpace(op.Title)
			nodes[nodeID] = node
		case UpdateNodeConfigOp:
			nodeID, err := resolve(op.NodeRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			node, ok := nodes[nodeID]
			if !ok {
				return AppliedGraph{}, apperr.Validation("图节点不存在")
			}
			config, err := NormalizeNodeConfig(node.NodeType, op.Config)
			if err != nil {
				return AppliedGraph{}, err
			}
			node.DocumentOrigin = nextDocumentOrigin(node.NodeType, node, config, op.DocumentOrigin)
			bound := node.BoundAssetID
			if op.BoundAssetIDSet {
				bound = cloneStringPtr(op.BoundAssetID)
			}
			node.Config = config
			node.BoundAssetID = bound
			nodes[nodeID] = node
		case MoveNodesOp:
			for _, move := range op.Nodes {
				nodeID, err := resolve(move.Ref)
				if err != nil {
					return AppliedGraph{}, err
				}
				node, ok := nodes[nodeID]
				if !ok {
					return AppliedGraph{}, apperr.Validation("图节点不存在")
				}
				node.PositionX = move.X
				node.PositionY = move.Y
				nodes[nodeID] = node
			}
		case MoveNodesToGroupOp:
			var groupID *string
			if op.GroupRef != nil {
				resolved, err := resolve(*op.GroupRef)
				if err != nil {
					return AppliedGraph{}, err
				}
				groupID = stringPtr(resolved)
			}
			for _, nodeRef := range op.NodeRefs {
				nodeID, err := resolve(nodeRef)
				if err != nil {
					return AppliedGraph{}, err
				}
				node, ok := nodes[nodeID]
				if !ok {
					return AppliedGraph{}, apperr.Validation("图节点不存在")
				}
				node.GroupID = cloneStringPtr(groupID)
				nodes[nodeID] = node
			}
		case RenameGroupOp:
			groupID, err := resolve(op.GroupRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			if _, ok := groups[groupID]; !ok {
				return AppliedGraph{}, apperr.Validation("图分组不存在")
			}
			groups[groupID] = AppliedGroup{ID: groupID, Title: strings.TrimSpace(op.Title)}
		case DissolveGroupOp:
			groupID, err := resolve(op.GroupRef)
			if err != nil {
				return AppliedGraph{}, err
			}
			delete(groups, groupID)
			delete(aliases, groupID)
			for nodeID, node := range nodes {
				if node.GroupID != nil && *node.GroupID == groupID {
					node.GroupID = nil
					nodes[nodeID] = node
				}
			}
		default:
			return AppliedGraph{}, apperr.Validation("不支持的 Graph 操作")
		}
	}

	imageGenerationCount := 0
	for _, node := range nodes {
		if node.NodeType == NodeImageGeneration {
			imageGenerationCount++
		}
		if node.BoundAssetID != nil && *node.BoundAssetID != "" && node.NodeType != NodeImageAsset {
			return AppliedGraph{}, apperr.Validation("只有图片素材节点可以绑定商品图片")
		}
	}
	if imageGenerationCount > maxTotalImages {
		return AppliedGraph{}, apperr.Validation(fmt.Sprintf("图片生成总数不能超过 %d", maxTotalImages))
	}

	ruleNodes := make([]RuleNode, 0, len(nodes))
	for _, node := range nodes {
		ruleNodes = append(ruleNodes, RuleNode{node.ID, node.NodeType, node.Config, node.BoundAssetID})
	}
	ruleEdges := make([]RuleEdge, 0, len(edges))
	for _, edge := range edges {
		ruleEdges = append(ruleEdges, RuleEdge{edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
	}
	if _, err := TopologicalGraphNodeIDs(ruleNodes, ruleEdges); err != nil {
		return AppliedGraph{}, err
	}

	outNodes := make([]AppliedNode, 0, len(nodes))
	for _, id := range sortedKeys(nodes) {
		outNodes = append(outNodes, nodes[id])
	}
	outEdges := make([]AppliedEdge, 0, len(edges))
	for _, id := range sortedKeys(edges) {
		outEdges = append(outEdges, edges[id])
	}
	outGroups := make([]AppliedGroup, 0, len(groups))
	for _, id := range sortedKeys(groups) {
		outGroups = append(outGroups, groups[id])
	}
	return AppliedGraph{
		Revision: graph.Revision + 1,
		Nodes:    outNodes,
		Edges:    outEdges,
		Groups:   outGroups,
	}, nil
}

func Invert(before, after AppliedGraph) []Operation {
	operations := make([]Operation, 0)
	beforeNodeIDs := idSet(before.Nodes, func(n AppliedNode) string { return n.ID })
	afterNodeIDs := idSet(after.Nodes, func(n AppliedNode) string { return n.ID })
	beforeEdgeIDs := idSet(before.Edges, func(e AppliedEdge) string { return e.ID })
	afterEdgeIDs := idSet(after.Edges, func(e AppliedEdge) string { return e.ID })
	beforeGroupIDs := idSet(before.Groups, func(g AppliedGroup) string { return g.ID })
	afterGroupIDs := idSet(after.Groups, func(g AppliedGroup) string { return g.ID })

	for _, edge := range after.Edges {
		if !beforeEdgeIDs[edge.ID] {
			operations = append(operations, DisconnectEdgeOp{EdgeRef: edge.ID})
		}
	}
	for _, node := range after.Nodes {
		if !beforeNodeIDs[node.ID] {
			operations = append(operations, DeleteNodeOp{NodeRef: node.ID})
		}
	}
	for _, group := range after.Groups {
		if !beforeGroupIDs[group.ID] {
			operations = append(operations, DissolveGroupOp{GroupRef: group.ID})
		}
	}

	beforeGroupByID := map[string]AppliedGroup{}
	for _, group := range before.Groups {
		beforeGroupByID[group.ID] = group
	}
	afterGroupByID := map[string]AppliedGroup{}
	for _, group := range after.Groups {
		afterGroupByID[group.ID] = group
	}
	for _, group := range before.Groups {
		if !afterGroupIDs[group.ID] {
			operations = append(operations, CreateGroupOp{ClientRef: group.ID, Title: group.Title, MemberRefs: []string{}})
		} else if afterGroupByID[group.ID].Title != group.Title {
			operations = append(operations, RenameGroupOp{GroupRef: group.ID, Title: group.Title})
		}
	}

	membershipMoves := map[string][]string{}
	nilMembership := []string{}
	var positionMoves []NodeMove
	beforeNodeByID := map[string]AppliedNode{}
	for _, node := range before.Nodes {
		beforeNodeByID[node.ID] = node
	}
	for _, node := range after.Nodes {
		if !beforeNodeIDs[node.ID] {
			continue
		}
		previous := beforeNodeByID[node.ID]
		if !samePtr(node.GroupID, previous.GroupID) {
			if previous.GroupID == nil {
				nilMembership = append(nilMembership, node.ID)
			} else {
				membershipMoves[*previous.GroupID] = append(membershipMoves[*previous.GroupID], node.ID)
			}
		}
		if node.PositionX != previous.PositionX || node.PositionY != previous.PositionY {
			positionMoves = append(positionMoves, NodeMove{Ref: node.ID, X: previous.PositionX, Y: previous.PositionY})
		}
		if node.Title != previous.Title {
			operations = append(operations, RenameNodeOp{NodeRef: node.ID, Title: previous.Title})
		}
		if !mapsEqual(node.Config, previous.Config) || !samePtr(node.BoundAssetID, previous.BoundAssetID) || node.DocumentOrigin != previous.DocumentOrigin {
			operations = append(operations, UpdateNodeConfigOp{
				NodeRef:         node.ID,
				Config:          originConfigForInvert(previous),
				BoundAssetID:    cloneStringPtr(previous.BoundAssetID),
				BoundAssetIDSet: true,
				DocumentOrigin:  documentOriginPtr(previous),
			})
		}
	}
	if len(nilMembership) > 0 {
		operations = append(operations, MoveNodesToGroupOp{GroupRef: nil, NodeRefs: nilMembership})
	}
	for _, groupID := range sortedKeys(membershipMoves) {
		gid := groupID
		operations = append(operations, MoveNodesToGroupOp{GroupRef: &gid, NodeRefs: membershipMoves[groupID]})
	}
	if len(positionMoves) > 0 {
		operations = append(operations, MoveNodesOp{Nodes: positionMoves})
	}

	for _, node := range before.Nodes {
		if afterNodeIDs[node.ID] {
			continue
		}
		operations = append(operations, CreateNodeOp{
			ClientRef:      node.ID,
			NodeType:       node.NodeType,
			Title:          node.Title,
			PositionX:      node.PositionX,
			PositionY:      node.PositionY,
			Config:         originConfigForInvert(node),
			BoundAssetID:   cloneStringPtr(node.BoundAssetID),
			GroupRef:       cloneStringPtr(node.GroupID),
			DocumentOrigin: documentOriginPtr(node),
		})
	}
	for _, edge := range before.Edges {
		if afterEdgeIDs[edge.ID] {
			continue
		}
		operations = append(operations, ConnectNodesOp{
			ClientRef: edge.ID,
			SourceRef: edge.SourceNodeID,
			TargetRef: edge.TargetNodeID,
			Order:     edge.Order,
		})
	}
	beforeEdgeOrder := groupedEdgeOrder(before.Edges)
	afterEdgeOrder := groupedEdgeOrder(after.Edges)
	for _, key := range sortedEdgeOrderKeys(beforeEdgeOrder) {
		beforeIDs := beforeEdgeOrder[key]
		if sameStringSlice(beforeIDs, afterEdgeOrder[key]) {
			continue
		}
		operations = append(operations, ReorderEdgesOp{
			NodeRef:  key.TargetNodeID,
			Role:     key.Role,
			EdgeRefs: append([]string(nil), beforeIDs...),
		})
	}
	return operations
}

type edgeOrderKey struct {
	TargetNodeID string
	Role         EdgeRole
}

func groupedEdgeOrder(edges []AppliedEdge) map[edgeOrderKey][]string {
	grouped := map[edgeOrderKey][]AppliedEdge{}
	for _, edge := range edges {
		key := edgeOrderKey{TargetNodeID: edge.TargetNodeID, Role: edge.Role}
		grouped[key] = append(grouped[key], edge)
	}
	out := make(map[edgeOrderKey][]string, len(grouped))
	for key, items := range grouped {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Order != items[j].Order {
				return items[i].Order < items[j].Order
			}
			return items[i].ID < items[j].ID
		})
		ids := make([]string, 0, len(items))
		for _, edge := range items {
			ids = append(ids, edge.ID)
		}
		out[key] = ids
	}
	return out
}

func sortedEdgeOrderKeys(groups map[edgeOrderKey][]string) []edgeOrderKey {
	keys := make([]edgeOrderKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].TargetNodeID != keys[j].TargetNodeID {
			return keys[i].TargetNodeID < keys[j].TargetNodeID
		}
		return keys[i].Role < keys[j].Role
	})
	return keys
}

func assignPersistentIDs(before, after AppliedGraph, newID func() string) AppliedGraph {
	idMap := map[string]string{}
	for _, node := range before.Nodes {
		idMap[node.ID] = node.ID
	}
	for _, edge := range before.Edges {
		idMap[edge.ID] = edge.ID
	}
	for _, group := range before.Groups {
		idMap[group.ID] = group.ID
	}
	for _, group := range after.Groups {
		if _, ok := idMap[group.ID]; !ok {
			idMap[group.ID] = newID()
		}
	}
	for _, node := range after.Nodes {
		if _, ok := idMap[node.ID]; !ok {
			idMap[node.ID] = newID()
		}
	}
	for _, edge := range after.Edges {
		if _, ok := idMap[edge.ID]; !ok {
			idMap[edge.ID] = newID()
		}
	}
	nodes := make([]AppliedNode, 0, len(after.Nodes))
	for _, node := range after.Nodes {
		mapped := cloneAppliedNode(node)
		mapped.ID = idMap[node.ID]
		if node.GroupID != nil {
			mapped.GroupID = stringPtr(idMap[*node.GroupID])
		}
		nodes = append(nodes, mapped)
	}
	edges := make([]AppliedEdge, 0, len(after.Edges))
	for _, edge := range after.Edges {
		edges = append(edges, AppliedEdge{
			ID:           idMap[edge.ID],
			SourceNodeID: idMap[edge.SourceNodeID],
			TargetNodeID: idMap[edge.TargetNodeID],
			DataType:     edge.DataType,
			Role:         edge.Role,
			Order:        edge.Order,
		})
	}
	groups := make([]AppliedGroup, 0, len(after.Groups))
	for _, group := range after.Groups {
		groups = append(groups, AppliedGroup{ID: idMap[group.ID], Title: group.Title})
	}
	return AppliedGraph{Revision: after.Revision, Nodes: nodes, Edges: edges, Groups: groups}
}

func cloneAppliedNode(node AppliedNode) AppliedNode {
	node.Config = cloneMap(node.Config)
	node.BoundAssetID = cloneStringPtr(node.BoundAssetID)
	node.GroupID = cloneStringPtr(node.GroupID)
	return node
}

func idSet[T any](items []T, id func(T) string) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		out[id(item)] = true
	}
	return out
}

func samePtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func mapsEqual(a, b map[string]any) bool {
	return reflect.DeepEqual(a, b)
}
