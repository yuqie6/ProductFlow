package recipe

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

type Preview struct {
	Mode              string
	RecipeID          string
	RecipeVersion     int
	BaseGraphRevision int
	PreviewDigest     string
	Nodes             []PreviewNode
	Edges             []PreviewEdge
	Groups            []PreviewGroup
	UpdatedNodes      []PreviewUpdatedNode
	RequiredBindings  []string
}

type PreviewNode struct {
	Key       string         `json:"key"`
	NodeType  graph.NodeType `json:"node_type"`
	Title     string         `json:"title"`
	PositionX int            `json:"position_x"`
	PositionY int            `json:"position_y"`
}

type PreviewEdge struct {
	Key           string `json:"key"`
	SourceNodeKey string `json:"source_node_key"`
	TargetNodeKey string `json:"target_node_key"`
	Role          string `json:"role"`
	DataType      string `json:"data_type"`
	Order         int    `json:"order"`
}

type PreviewGroup struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	MemberKeys []string `json:"member_keys"`
}

type PreviewUpdatedNode struct {
	ID                string         `json:"id"`
	NodeType          graph.NodeType `json:"node_type"`
	Title             string         `json:"title"`
	ChangedConfigKeys []string       `json:"changed_config_keys"`
}

type applyPlan struct {
	Mode              string
	RecipeID          string
	RecipeVersion     int
	GraphID           *string
	Existing          graph.AppliedGraph
	ChangeSet         graph.ChangeSet
	BaseGraphRevision int
	PreviewDigest     string
	Nodes             []PreviewNode
	Edges             []PreviewEdge
	Groups            []PreviewGroup
	UpdatedNodes      []PreviewUpdatedNode
	RequiredBindings  []string
}

func (p applyPlan) preview() Preview {
	bindings := p.RequiredBindings
	if bindings == nil {
		bindings = []string{}
	}
	updated := p.UpdatedNodes
	if updated == nil {
		updated = []PreviewUpdatedNode{}
	}
	return Preview{
		Mode:              p.Mode,
		RecipeID:          p.RecipeID,
		RecipeVersion:     p.RecipeVersion,
		BaseGraphRevision: p.BaseGraphRevision,
		PreviewDigest:     p.PreviewDigest,
		Nodes:             p.Nodes,
		Edges:             p.Edges,
		Groups:            p.Groups,
		UpdatedNodes:      updated,
		RequiredBindings:  bindings,
	}
}

type productTarget struct {
	ID               string
	FactSetVersionID *string
}

func wrapMergeErr(err error) error {
	if err == nil {
		return nil
	}
	var e apperr.Error
	if errors.As(err, &e) && e.Status == http.StatusBadRequest {
		return apperr.Conflict("配方无法合并进当前工作流: " + e.Detail)
	}
	return err
}

func applicationSummary(title string) string {
	text := strings.TrimSpace(title)
	if text == "" {
		text = "工作流配方"
	}
	return limitRunes("应用配方："+text, 500)
}

func mergeOffset(applied graph.AppliedGraph) (int, int) {
	if len(applied.Nodes) == 0 {
		return 0, 0
	}
	maxX := applied.Nodes[0].PositionX
	for _, node := range applied.Nodes {
		if node.PositionX > maxX {
			maxX = node.PositionX
		}
	}
	return maxX + 280, 0
}

func buildChangeSet(
	payload Payload,
	baseRevision int,
	summary string,
	targetProductID string,
	factSetVersionID *string,
	offsetX, offsetY int,
) (graph.ChangeSet, error) {
	token := strings.ReplaceAll(clockid.New(), "-", "")[:10]
	nodeRefs := map[string]string{}
	for index, node := range payload.Nodes {
		nodeRefs[node.Key] = fmt.Sprintf("rn%s%03d", token, index)
	}
	groupRefs := map[string]string{}
	for index, group := range payload.Groups {
		groupRefs[group.Key] = fmt.Sprintf("rg%s%03d", token, index)
	}
	operations := make([]graph.Operation, 0, len(payload.Groups)+len(payload.Nodes)+len(payload.Edges))
	for _, group := range payload.Groups {
		operations = append(operations, graph.CreateGroupOp{
			ClientRef:  groupRefs[group.Key],
			Title:      group.Title,
			MemberRefs: []string{},
		})
	}
	for _, node := range payload.Nodes {
		config, _ := cloneValue(node.Config).(map[string]any)
		if config == nil {
			config = map[string]any{}
		}
		if node.NodeType == graph.NodeProductSource && targetProductID != "" {
			config["source_product_id"] = targetProductID
			config["fact_set_version_id"] = nil
			if factSetVersionID != nil {
				config["fact_set_version_id"] = *factSetVersionID
			}
		}
		var groupRef *string
		if node.GroupKey != nil {
			if ref, ok := groupRefs[*node.GroupKey]; ok {
				groupRef = strPtr(ref)
			}
		}
		operations = append(operations, graph.CreateNodeOp{
			ClientRef:    nodeRefs[node.Key],
			NodeType:     node.NodeType,
			Title:        node.Title,
			PositionX:    node.PositionX + offsetX,
			PositionY:    node.PositionY + offsetY,
			Config:       config,
			BoundAssetID: nil,
			GroupRef:     groupRef,
		})
	}
	for index, edge := range payload.Edges {
		operations = append(operations, graph.ConnectNodesOp{
			ClientRef: fmt.Sprintf("re%s%03d", token, index),
			SourceRef: nodeRefs[edge.SourceNodeKey],
			TargetRef: nodeRefs[edge.TargetNodeKey],
			Order:     edge.Order,
		})
	}
	if len(operations) == 0 {
		return graph.ChangeSet{}, apperr.Validation("配方没有可写入的节点或连线")
	}
	return graph.ChangeSet{
		BaseGraphRevision: baseRevision,
		Summary:           limitRunes(summary, 500),
		ActorType:         graph.ActorRecipe,
		Operations:        operations,
	}, nil
}

func attachSharedInputs(existing graph.AppliedGraph, changeSet graph.ChangeSet) (graph.ChangeSet, map[string][]string, error) {
	promptRefs := []string{}
	imageRefs := []string{}
	for _, op := range changeSet.Operations {
		create, ok := op.(graph.CreateNodeOp)
		if !ok {
			continue
		}
		switch create.NodeType {
		case graph.NodePromptGeneration:
			promptRefs = append(promptRefs, create.ClientRef)
		case graph.NodeImageGeneration:
			imageRefs = append(imageRefs, create.ClientRef)
		}
	}
	connected := map[string][]string{
		"product_source":   {},
		"creative_brief":   {},
		"visual_system":    {},
		"product_identity": {},
	}
	if len(promptRefs) == 0 && len(imageRefs) == 0 {
		return changeSet, connected, nil
	}
	token := strings.ReplaceAll(clockid.New(), "-", "")[:10]
	index := 0
	extra := []graph.Operation{}
	connect := func(sourceID, targetRef string, order int) {
		extra = append(extra, graph.ConnectNodesOp{
			ClientRef: fmt.Sprintf("rs%s%03d", token, index),
			SourceRef: sourceID,
			TargetRef: targetRef,
			Order:     order,
		})
		index++
	}
	productSource := firstNode(existing, graph.NodeProductSource)
	brief := firstNode(existing, graph.NodeCreativeBrief)
	visual := firstNode(existing, graph.NodeVisualSystem)
	identities := referenceAssetNodes(existing)
	sort.Slice(identities, func(i, j int) bool {
		a, b := identities[i], identities[j]
		if a.PositionY != b.PositionY {
			return a.PositionY < b.PositionY
		}
		if a.PositionX != b.PositionX {
			return a.PositionX < b.PositionX
		}
		return a.ID < b.ID
	})
	for _, promptRef := range promptRefs {
		if productSource != nil {
			connect(productSource.ID, promptRef, 0)
			connected["product_source"] = []string{productSource.ID}
		}
		if brief != nil {
			connect(brief.ID, promptRef, 0)
			connected["creative_brief"] = []string{brief.ID}
		}
		if visual != nil {
			connect(visual.ID, promptRef, 0)
			connected["visual_system"] = []string{visual.ID}
		}
		for order, identity := range identities {
			connect(identity.ID, promptRef, order)
		}
	}
	for _, imageRef := range imageRefs {
		if visual != nil {
			connect(visual.ID, imageRef, 0)
			connected["visual_system"] = []string{visual.ID}
		}
		for order, identity := range identities {
			connect(identity.ID, imageRef, order)
		}
	}
	if len(identities) > 0 {
		ids := []string{}
		for _, node := range identities {
			if configString(node.Config, "role") == "product_identity" {
				ids = append(ids, node.ID)
			}
		}
		if len(ids) == 0 {
			for _, node := range identities {
				ids = append(ids, node.ID)
			}
		}
		connected["product_identity"] = ids
	}
	if len(extra) == 0 {
		return changeSet, connected, nil
	}
	changeSet.Operations = append(changeSet.Operations, extra...)
	return changeSet, connected, nil
}

func firstNode(applied graph.AppliedGraph, nodeType graph.NodeType) *graph.AppliedNode {
	for i := range applied.Nodes {
		if applied.Nodes[i].NodeType == nodeType {
			node := applied.Nodes[i]
			return &node
		}
	}
	return nil
}

func referenceAssetNodes(applied graph.AppliedGraph) []graph.AppliedNode {
	out := []graph.AppliedNode{}
	for _, node := range applied.Nodes {
		if node.NodeType != graph.NodeImageAsset {
			continue
		}
		if node.BoundAssetID == nil || *node.BoundAssetID == "" {
			continue
		}
		if configString(node.Config, "role") == "evidence" {
			continue
		}
		out = append(out, node)
	}
	return out
}

func productIdentityNodes(applied graph.AppliedGraph) []graph.AppliedNode {
	out := []graph.AppliedNode{}
	for _, node := range referenceAssetNodes(applied) {
		if configString(node.Config, "role") == "product_identity" {
			out = append(out, node)
		}
	}
	return out
}

func configString(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	raw, ok := config[key]
	if !ok || raw == nil {
		return ""
	}
	s, _ := raw.(string)
	return s
}

func additionSemantic(
	payload Payload,
	targetProductID string,
	factSetVersionID *string,
	offsetX, offsetY int,
) (map[string]any, error) {
	nodes := make([]any, 0, len(payload.Nodes))
	for _, node := range payload.Nodes {
		config, _ := cloneValue(node.Config).(map[string]any)
		if config == nil {
			config = map[string]any{}
		}
		if node.NodeType == graph.NodeProductSource {
			config["source_product_id"] = targetProductID
			config["fact_set_version_id"] = nil
			if factSetVersionID != nil {
				config["fact_set_version_id"] = *factSetVersionID
			}
		}
		normalized, err := graph.NormalizeNodeConfig(node.NodeType, config)
		if err != nil {
			return nil, err
		}
		var groupKey any
		if node.GroupKey != nil {
			groupKey = *node.GroupKey
		}
		nodes = append(nodes, map[string]any{
			"key":        node.Key,
			"node_type":  string(node.NodeType),
			"title":      node.Title,
			"position_x": node.PositionX + offsetX,
			"position_y": node.PositionY + offsetY,
			"group_key":  groupKey,
			"config":     normalized,
		})
	}
	edges := make([]any, 0, len(payload.Edges))
	for _, edge := range payload.Edges {
		edges = append(edges, map[string]any{
			"key":             edge.Key,
			"source_node_key": edge.SourceNodeKey,
			"target_node_key": edge.TargetNodeKey,
			"data_type":       string(edge.DataType),
			"role":            string(edge.Role),
			"order":           edge.Order,
		})
	}
	groups := make([]any, 0, len(payload.Groups))
	for _, group := range payload.Groups {
		members := group.MemberKeys
		if members == nil {
			members = []string{}
		}
		groups = append(groups, map[string]any{
			"key":         group.Key,
			"title":       group.Title,
			"member_keys": members,
		})
	}
	return map[string]any{
		"nodes":  nodes,
		"edges":  edges,
		"groups": groups,
	}, nil
}

func previewAdditions(after, existing graph.AppliedGraph) ([]PreviewNode, []PreviewEdge, []PreviewGroup) {
	existingNodes := map[string]struct{}{}
	for _, node := range existing.Nodes {
		existingNodes[node.ID] = struct{}{}
	}
	existingEdges := map[string]struct{}{}
	for _, edge := range existing.Edges {
		existingEdges[edge.ID] = struct{}{}
	}
	existingGroups := map[string]struct{}{}
	for _, group := range existing.Groups {
		existingGroups[group.ID] = struct{}{}
	}
	nodes := []PreviewNode{}
	addedNodeIDs := map[string]struct{}{}
	for _, node := range after.Nodes {
		if _, ok := existingNodes[node.ID]; ok {
			continue
		}
		addedNodeIDs[node.ID] = struct{}{}
		nodes = append(nodes, PreviewNode{
			Key:       node.ID,
			NodeType:  node.NodeType,
			Title:     node.Title,
			PositionX: node.PositionX,
			PositionY: node.PositionY,
		})
	}
	edges := []PreviewEdge{}
	for _, edge := range after.Edges {
		if _, ok := existingEdges[edge.ID]; ok {
			continue
		}
		edges = append(edges, PreviewEdge{
			Key:           edge.ID,
			SourceNodeKey: edge.SourceNodeID,
			TargetNodeKey: edge.TargetNodeID,
			Role:          string(edge.Role),
			DataType:      string(edge.DataType),
			Order:         edge.Order,
		})
	}
	groups := []PreviewGroup{}
	for _, group := range after.Groups {
		if _, ok := existingGroups[group.ID]; ok {
			continue
		}
		members := []string{}
		for _, node := range after.Nodes {
			if _, ok := addedNodeIDs[node.ID]; !ok {
				continue
			}
			if node.GroupID != nil && *node.GroupID == group.ID {
				members = append(members, node.ID)
			}
		}
		groups = append(groups, PreviewGroup{Key: group.ID, Title: group.Title, MemberKeys: members})
	}
	return nodes, edges, groups
}

func assertRequiredBindings(applied graph.AppliedGraph, required []string, affected map[string]struct{}) error {
	if len(required) == 0 {
		return nil
	}
	want := map[string]struct{}{}
	for _, item := range required {
		want[item] = struct{}{}
	}
	imageNodes := []graph.AppliedNode{}
	for _, node := range applied.Nodes {
		if _, ok := affected[node.ID]; !ok {
			continue
		}
		if node.NodeType == graph.NodeImageGeneration {
			imageNodes = append(imageNodes, node)
		}
	}
	if len(imageNodes) == 0 {
		return nil
	}
	identityIDs := map[string]struct{}{}
	for _, node := range productIdentityNodes(applied) {
		identityIDs[node.ID] = struct{}{}
	}
	if _, ok := want["product_identity"]; ok && len(identityIDs) == 0 {
		return apperr.Conflict("配方需要商品身份参考图")
	}
	for _, node := range imageNodes {
		incoming := applied.Incoming(node.ID)
		for _, contract := range graph.RequiredRunContracts(node.NodeType) {
			found := false
			for _, edge := range incoming {
				if edge.DataType == contract.DataType && edge.Role == contract.Role {
					found = true
					break
				}
			}
			if !found {
				return apperr.Conflict("配方应用后镜头仍缺少运行所需的输入边")
			}
		}
		if _, ok := want["product_identity"]; ok {
			hasIdentity := false
			for _, edge := range incoming {
				if _, ok := identityIDs[edge.SourceNodeID]; ok {
					hasIdentity = true
					break
				}
			}
			if !hasIdentity {
				return apperr.Conflict("配方需要商品身份参考图")
			}
		}
	}
	return nil
}

func previewDigest(
	productID, recipeID string,
	recipeVersion int,
	payloadHash string,
	graphID *string,
	baseRevision int,
	mode string,
	required []string,
	semantic map[string]any,
) (string, error) {
	bindings := required
	if bindings == nil {
		bindings = []string{}
	}
	var graphIDValue any
	if graphID != nil {
		graphIDValue = *graphID
	}
	return jsonHash(map[string]any{
		"product_id":          productID,
		"recipe_id":           recipeID,
		"recipe_version":      recipeVersion,
		"payload_hash":        payloadHash,
		"graph_id":            graphIDValue,
		"base_graph_revision": baseRevision,
		"mode":                mode,
		"required_bindings":   bindings,
		"semantic":            semantic,
	})
}

func planPayload(
	target productTarget,
	live *graph.GraphRow,
	existing graph.AppliedGraph,
	recipeID, kind string,
	recipeVersion int,
	summary string,
	payload Payload,
	payloadHashValue string,
	required []string,
	expectedGraphRevision *int,
) (applyPlan, error) {
	mode := ModeCreate
	offsetX, offsetY := 0, 0
	var graphID *string
	if live == nil {
		if kind == kindFragment {
			return applyPlan{}, apperr.Conflict("片段配方需要已有 schema-v3 工作流")
		}
		existing = graph.EmptyGraph
	} else {
		if kind != kindFragment {
			return applyPlan{}, apperr.Conflict("完整配方不能合并进已有工作流")
		}
		mode = ModeMerge
		offsetX, offsetY = mergeOffset(existing)
		id := live.ID
		graphID = &id
	}
	if expectedGraphRevision != nil && existing.Revision != *expectedGraphRevision {
		return applyPlan{}, apperr.Conflict("工作流已变化，请重新预览后重试")
	}
	changeSet, err := buildChangeSet(payload, existing.Revision, summary, target.ID, target.FactSetVersionID, offsetX, offsetY)
	if err != nil {
		return applyPlan{}, wrapMergeErr(err)
	}
	connected := map[string][]string{
		"product_source":   {},
		"creative_brief":   {},
		"visual_system":    {},
		"product_identity": {},
	}
	if live != nil {
		changeSet, connected, err = attachSharedInputs(existing, changeSet)
		if err != nil {
			return applyPlan{}, wrapMergeErr(err)
		}
	}
	semanticPayload, err := additionSemantic(payload, target.ID, target.FactSetVersionID, offsetX, offsetY)
	if err != nil {
		return applyPlan{}, wrapMergeErr(err)
	}
	semantic := map[string]any{
		"operation":        "add",
		"offset":           []any{offsetX, offsetY},
		"payload":          semanticPayload,
		"connected_inputs": connected,
	}
	after, err := graph.Apply(existing, changeSet)
	if err != nil {
		return applyPlan{}, wrapMergeErr(err)
	}
	addedIDs := map[string]struct{}{}
	beforeIDs := map[string]struct{}{}
	for _, node := range existing.Nodes {
		beforeIDs[node.ID] = struct{}{}
	}
	for _, node := range after.Nodes {
		if _, ok := beforeIDs[node.ID]; !ok {
			addedIDs[node.ID] = struct{}{}
		}
	}
	if err := assertRequiredBindings(after, required, addedIDs); err != nil {
		return applyPlan{}, err
	}
	nodes, edges, groups := previewAdditions(after, existing)
	digest, err := previewDigest(
		target.ID,
		recipeID,
		recipeVersion,
		payloadHashValue,
		graphID,
		existing.Revision,
		mode,
		required,
		semantic,
	)
	if err != nil {
		return applyPlan{}, err
	}
	if required == nil {
		required = []string{}
	}
	return applyPlan{
		Mode:              mode,
		RecipeID:          recipeID,
		RecipeVersion:     recipeVersion,
		GraphID:           graphID,
		Existing:          existing,
		ChangeSet:         changeSet,
		BaseGraphRevision: existing.Revision,
		PreviewDigest:     digest,
		Nodes:             nodes,
		Edges:             edges,
		Groups:            groups,
		UpdatedNodes:      []PreviewUpdatedNode{},
		RequiredBindings:  required,
	}, nil
}
