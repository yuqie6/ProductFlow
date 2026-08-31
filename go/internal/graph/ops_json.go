package graph

import "encoding/json"

func marshalOperations(ops []Operation) ([]byte, error) {
	raws := make([]json.RawMessage, 0, len(ops))
	for _, op := range ops {
		b, err := marshalOperation(op)
		if err != nil {
			return nil, err
		}
		raws = append(raws, b)
	}
	if raws == nil {
		raws = []json.RawMessage{}
	}
	return json.Marshal(raws)
}

// marshalOperation 把内存 op 编成提案/账本 JSON。未知类型报错。DocumentOrigin 只在内部 op 出现。
func marshalOperation(op Operation) ([]byte, error) {
	switch t := op.(type) {
	case CreateNodeOp:
		return json.Marshal(struct {
			Op             string         `json:"op"`
			ClientRef      string         `json:"client_ref"`
			NodeType       NodeType       `json:"node_type"`
			Title          string         `json:"title"`
			PositionX      int            `json:"position_x"`
			PositionY      int            `json:"position_y"`
			Config         map[string]any `json:"config"`
			BoundAssetID   *string        `json:"bound_asset_id"`
			GroupRef       *string        `json:"group_ref"`
			DocumentOrigin *string        `json:"document_origin,omitempty"`
		}{
			Op:             "create_node",
			ClientRef:      t.ClientRef,
			NodeType:       t.NodeType,
			Title:          t.Title,
			PositionX:      t.PositionX,
			PositionY:      t.PositionY,
			Config:         nonemptyMap(t.Config),
			BoundAssetID:   t.BoundAssetID,
			GroupRef:       t.GroupRef,
			DocumentOrigin: t.DocumentOrigin,
		})
	case UpdateNodeConfigOp:
		return json.Marshal(struct {
			Op             string         `json:"op"`
			NodeRef        string         `json:"node_ref"`
			Config         map[string]any `json:"config"`
			BoundAssetID   *string        `json:"bound_asset_id"`
			DocumentOrigin *string        `json:"document_origin,omitempty"`
		}{
			Op:             "update_node_config",
			NodeRef:        t.NodeRef,
			Config:         nonemptyMap(t.Config),
			BoundAssetID:   t.BoundAssetID,
			DocumentOrigin: t.DocumentOrigin,
		})
	case RenameNodeOp:
		return json.Marshal(struct {
			Op      string `json:"op"`
			NodeRef string `json:"node_ref"`
			Title   string `json:"title"`
		}{Op: "rename_node", NodeRef: t.NodeRef, Title: t.Title})
	case DeleteNodeOp:
		return json.Marshal(struct {
			Op      string `json:"op"`
			NodeRef string `json:"node_ref"`
		}{Op: "delete_node", NodeRef: t.NodeRef})
	case ConnectNodesOp:
		return json.Marshal(struct {
			Op        string `json:"op"`
			ClientRef string `json:"client_ref"`
			SourceRef string `json:"source_ref"`
			TargetRef string `json:"target_ref"`
			Order     int    `json:"order"`
		}{
			Op:        "connect_nodes",
			ClientRef: t.ClientRef,
			SourceRef: t.SourceRef,
			TargetRef: t.TargetRef,
			Order:     t.Order,
		})
	case DisconnectEdgeOp:
		return json.Marshal(struct {
			Op      string `json:"op"`
			EdgeRef string `json:"edge_ref"`
		}{Op: "disconnect_edge", EdgeRef: t.EdgeRef})
	case MoveNodesOp:
		nodes := make([][]any, 0, len(t.Nodes))
		for _, move := range t.Nodes {
			nodes = append(nodes, []any{move.Ref, move.X, move.Y})
		}
		return json.Marshal(struct {
			Op    string  `json:"op"`
			Nodes [][]any `json:"nodes"`
		}{Op: "move_nodes", Nodes: nodes})
	case CreateGroupOp:
		members := t.MemberRefs
		if members == nil {
			members = []string{}
		}
		return json.Marshal(struct {
			Op         string   `json:"op"`
			ClientRef  string   `json:"client_ref"`
			Title      string   `json:"title"`
			MemberRefs []string `json:"member_refs"`
		}{Op: "create_group", ClientRef: t.ClientRef, Title: t.Title, MemberRefs: members})
	case MoveNodesToGroupOp:
		return json.Marshal(struct {
			Op       string   `json:"op"`
			GroupRef *string  `json:"group_ref"`
			NodeRefs []string `json:"node_refs"`
		}{Op: "move_nodes_to_group", GroupRef: t.GroupRef, NodeRefs: t.NodeRefs})
	case RenameGroupOp:
		return json.Marshal(struct {
			Op       string `json:"op"`
			GroupRef string `json:"group_ref"`
			Title    string `json:"title"`
		}{Op: "rename_group", GroupRef: t.GroupRef, Title: t.Title})
	case DissolveGroupOp:
		return json.Marshal(struct {
			Op       string `json:"op"`
			GroupRef string `json:"group_ref"`
		}{Op: "dissolve_group", GroupRef: t.GroupRef})
	case ReorderEdgesOp:
		refs := t.EdgeRefs
		if refs == nil {
			refs = []string{}
		}
		return json.Marshal(struct {
			Op       string   `json:"op"`
			NodeRef  string   `json:"node_ref"`
			Role     EdgeRole `json:"role"`
			EdgeRefs []string `json:"edge_refs"`
		}{Op: "reorder_edges", NodeRef: t.NodeRef, Role: t.Role, EdgeRefs: refs})
	default:
		return json.Marshal(map[string]any{"op": "unknown"})
	}
}

func nonemptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// MarshalChangeSet 把内存 ChangeSet 编成提案/账本用的 JSON。
func MarshalChangeSet(cs ChangeSet) ([]byte, error) {
	ops, err := marshalOperations(cs.Operations)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		BaseGraphRevision int             `json:"base_graph_revision"`
		Summary           string          `json:"summary"`
		ActorType         ActorType       `json:"actor_type"`
		Operations        json.RawMessage `json:"operations"`
	}{
		BaseGraphRevision: cs.BaseGraphRevision,
		Summary:           cs.Summary,
		ActorType:         cs.ActorType,
		Operations:        ops,
	})
}
