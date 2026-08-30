package graph

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

var changeSetKnownKeys = map[string]struct{}{
	"base_graph_revision": {},
	"summary":             {},
	"actor_type":          {},
	"operations":          {},
}

// ParseChangeSet 解 HTTP / 提案里的 WorkflowChangeSet；多余字段按 extra=forbid 拒绝。
func ParseChangeSet(raw []byte) (ChangeSet, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return ChangeSet{}, apperr.Validation("请求体无效")
	}
	if err := rejectUnknownKeys(payload, changeSetKnownKeys); err != nil {
		return ChangeSet{}, err
	}
	baseRaw, ok := payload["base_graph_revision"]
	if !ok {
		return ChangeSet{}, apperr.Validation("不支持的 Graph 操作")
	}
	base, ok := asInt(baseRaw)
	if !ok || base < 0 {
		return ChangeSet{}, apperr.Validation("不支持的 Graph 操作")
	}
	summary, _ := payload["summary"].(string)
	summary = strings.TrimSpace(summary)
	if summary == "" || len([]rune(summary)) > maxSummaryLen {
		return ChangeSet{}, apperr.Validation("不支持的 Graph 操作")
	}
	actor := ActorUser
	if rawActor, exists := payload["actor_type"]; exists && rawActor != nil {
		s, ok := rawActor.(string)
		if !ok {
			return ChangeSet{}, apperr.Validation("不支持的 Graph 操作")
		}
		switch ActorType(s) {
		case ActorUser, ActorAgent, ActorRecipe:
			actor = ActorType(s)
		default:
			return ChangeSet{}, apperr.Validation("不支持的 Graph 操作")
		}
	}
	opsRaw, ok := payload["operations"]
	if !ok {
		return ChangeSet{}, apperr.Validation("不支持的 Graph 操作")
	}
	opsBytes, err := json.Marshal(opsRaw)
	if err != nil {
		return ChangeSet{}, apperr.Validation("不支持的 Graph 操作")
	}
	operations, err := UnmarshalOperations(opsBytes)
	if err != nil {
		return ChangeSet{}, err
	}
	cs := ChangeSet{
		BaseGraphRevision: base,
		Summary:           summary,
		ActorType:         actor,
		Operations:        operations,
	}
	if err := validateChangeSet(cs); err != nil {
		return ChangeSet{}, err
	}
	return cs, nil
}

func UnmarshalOperations(raw []byte) ([]Operation, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var items []json.RawMessage
	if err := dec.Decode(&items); err != nil {
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
	if len(items) < 1 {
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
	out := make([]Operation, 0, len(items))
	for _, item := range items {
		op, err := unmarshalOperation(item)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

func unmarshalOperation(raw json.RawMessage) (Operation, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
	op, _ := payload["op"].(string)
	switch op {
	case "create_node":
		if err := requireKeys(payload, "client_ref", "node_type", "title"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{
			"op": {}, "client_ref": {}, "node_type": {}, "title": {},
			"position_x": {}, "position_y": {}, "config": {}, "bound_asset_id": {}, "group_ref": {},
		}); err != nil {
			return nil, err
		}
		nodeType, ok := payload["node_type"].(string)
		if !ok {
			return nil, apperr.Validation("不支持的 Graph 操作")
		}
		title, err := graphTitle(payload["title"])
		if err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["client_ref"])
		if err != nil {
			return nil, err
		}
		x, err := optionalInt(payload, "position_x")
		if err != nil {
			return nil, err
		}
		y, err := optionalInt(payload, "position_y")
		if err != nil {
			return nil, err
		}
		config := map[string]any{}
		if rawCfg, exists := payload["config"]; exists && rawCfg != nil {
			cfg, ok := rawCfg.(map[string]any)
			if !ok {
				return nil, apperr.Validation("不支持的 Graph 操作")
			}
			config = cfg
		}
		bound, err := optionalRef(payload, "bound_asset_id")
		if err != nil {
			return nil, err
		}
		group, err := optionalRef(payload, "group_ref")
		if err != nil {
			return nil, err
		}
		return CreateNodeOp{
			ClientRef:    ref,
			NodeType:     NodeType(nodeType),
			Title:        title,
			PositionX:    x,
			PositionY:    y,
			Config:       config,
			BoundAssetID: bound,
			GroupRef:     group,
		}, nil
	case "update_node_config":
		if err := requireKeys(payload, "node_ref", "config"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{
			"op": {}, "node_ref": {}, "config": {}, "bound_asset_id": {},
		}); err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["node_ref"])
		if err != nil {
			return nil, err
		}
		cfg, ok := payload["config"].(map[string]any)
		if !ok {
			return nil, apperr.Validation("不支持的 Graph 操作")
		}
		_, boundSet := payload["bound_asset_id"]
		bound, err := optionalRef(payload, "bound_asset_id")
		if err != nil {
			return nil, err
		}
		return UpdateNodeConfigOp{
			NodeRef:         ref,
			Config:          cfg,
			BoundAssetID:    bound,
			BoundAssetIDSet: boundSet,
		}, nil
	case "rename_node":
		if err := requireKeys(payload, "node_ref", "title"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{"op": {}, "node_ref": {}, "title": {}}); err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["node_ref"])
		if err != nil {
			return nil, err
		}
		title, err := graphTitle(payload["title"])
		if err != nil {
			return nil, err
		}
		return RenameNodeOp{NodeRef: ref, Title: title}, nil
	case "delete_node":
		if err := requireKeys(payload, "node_ref"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{"op": {}, "node_ref": {}}); err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["node_ref"])
		if err != nil {
			return nil, err
		}
		return DeleteNodeOp{NodeRef: ref}, nil
	case "connect_nodes":
		if err := requireKeys(payload, "client_ref", "source_ref", "target_ref"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{
			"op": {}, "client_ref": {}, "source_ref": {}, "target_ref": {}, "order": {},
		}); err != nil {
			return nil, err
		}
		clientRef, err := graphRef(payload["client_ref"])
		if err != nil {
			return nil, err
		}
		source, err := graphRef(payload["source_ref"])
		if err != nil {
			return nil, err
		}
		target, err := graphRef(payload["target_ref"])
		if err != nil {
			return nil, err
		}
		order, err := optionalInt(payload, "order")
		if err != nil {
			return nil, err
		}
		if order < 0 {
			return nil, apperr.Validation("不支持的 Graph 操作")
		}
		return ConnectNodesOp{ClientRef: clientRef, SourceRef: source, TargetRef: target, Order: order}, nil
	case "disconnect_edge":
		if err := requireKeys(payload, "edge_ref"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{"op": {}, "edge_ref": {}}); err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["edge_ref"])
		if err != nil {
			return nil, err
		}
		return DisconnectEdgeOp{EdgeRef: ref}, nil
	case "move_nodes":
		if err := requireKeys(payload, "nodes"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{"op": {}, "nodes": {}}); err != nil {
			return nil, err
		}
		rawMoves, ok := payload["nodes"].([]any)
		if !ok || len(rawMoves) < 1 {
			return nil, apperr.Validation("不支持的 Graph 操作")
		}
		moves := make([]NodeMove, 0, len(rawMoves))
		for _, item := range rawMoves {
			tuple, ok := item.([]any)
			if !ok || len(tuple) != 3 {
				return nil, apperr.Validation("不支持的 Graph 操作")
			}
			ref, err := graphRef(tuple[0])
			if err != nil {
				return nil, err
			}
			x, okX := asInt(tuple[1])
			y, okY := asInt(tuple[2])
			if !okX || !okY {
				return nil, apperr.Validation("不支持的 Graph 操作")
			}
			moves = append(moves, NodeMove{Ref: ref, X: x, Y: y})
		}
		return MoveNodesOp{Nodes: moves}, nil
	case "create_group":
		if err := requireKeys(payload, "client_ref", "title"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{
			"op": {}, "client_ref": {}, "title": {}, "member_refs": {},
		}); err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["client_ref"])
		if err != nil {
			return nil, err
		}
		title, err := graphTitle(payload["title"])
		if err != nil {
			return nil, err
		}
		members, err := stringRefs(payload["member_refs"])
		if err != nil {
			return nil, err
		}
		return CreateGroupOp{ClientRef: ref, Title: title, MemberRefs: members}, nil
	case "move_nodes_to_group":
		if err := requireKeys(payload, "node_refs"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{
			"op": {}, "group_ref": {}, "node_refs": {},
		}); err != nil {
			return nil, err
		}
		if _, exists := payload["group_ref"]; !exists {
			return nil, apperr.Validation("不支持的 Graph 操作")
		}
		refs, err := stringRefs(payload["node_refs"])
		if err != nil || len(refs) < 1 {
			return nil, apperr.Validation("不支持的 Graph 操作")
		}
		group, err := optionalRef(payload, "group_ref")
		if err != nil {
			return nil, err
		}
		return MoveNodesToGroupOp{GroupRef: group, NodeRefs: refs}, nil
	case "rename_group":
		if err := requireKeys(payload, "group_ref", "title"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{"op": {}, "group_ref": {}, "title": {}}); err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["group_ref"])
		if err != nil {
			return nil, err
		}
		title, err := graphTitle(payload["title"])
		if err != nil {
			return nil, err
		}
		return RenameGroupOp{GroupRef: ref, Title: title}, nil
	case "dissolve_group":
		if err := requireKeys(payload, "group_ref"); err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(payload, map[string]struct{}{"op": {}, "group_ref": {}}); err != nil {
			return nil, err
		}
		ref, err := graphRef(payload["group_ref"])
		if err != nil {
			return nil, err
		}
		return DissolveGroupOp{GroupRef: ref}, nil
	default:
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
}

func rejectUnknownKeys(payload map[string]any, known map[string]struct{}) error {
	for key := range payload {
		if _, ok := known[key]; !ok {
			return apperr.Validation("请求体无效")
		}
	}
	return nil
}

func requireKeys(payload map[string]any, keys ...string) error {
	for _, key := range keys {
		if _, ok := payload[key]; !ok {
			return apperr.Validation("不支持的 Graph 操作")
		}
	}
	return nil
}

func graphRef(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", apperr.Validation("不支持的 Graph 操作")
	}
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > maxRefLen {
		return "", apperr.Validation("不支持的 Graph 操作")
	}
	return s, nil
}

func graphTitle(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", apperr.Validation("不支持的 Graph 操作")
	}
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > maxTitleLen {
		return "", apperr.Validation("不支持的 Graph 操作")
	}
	return s, nil
}

func optionalRef(payload map[string]any, key string) (*string, error) {
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil, nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if len([]rune(s)) > maxRefLen {
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
	return &s, nil
}

func optionalInt(payload map[string]any, key string) (int, error) {
	raw, exists := payload[key]
	if !exists {
		return 0, nil
	}
	n, ok := asInt(raw)
	if !ok {
		return 0, apperr.Validation("不支持的 Graph 操作")
	}
	return n, nil
}

func stringRefs(v any) ([]string, error) {
	if v == nil {
		return []string{}, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, apperr.Validation("不支持的 Graph 操作")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		ref, err := graphRef(item)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}
