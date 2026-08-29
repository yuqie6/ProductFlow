package graph

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SnapshotGraph 固定当前 revision 的节点、边与源事实，执行不再读 live graph。
func SnapshotGraph(graph AppliedGraph, sources map[string]SourceRecord) map[string]any {
	nodes := make([]map[string]any, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes = append(nodes, map[string]any{
			"id":             node.ID,
			"node_type":      string(node.NodeType),
			"title":          node.Title,
			"position_x":     node.PositionX,
			"position_y":     node.PositionY,
			"config":         cloneMap(node.Config),
			"bound_asset_id": node.BoundAssetID,
			"group_id":       node.GroupID,
		})
	}
	edges := make([]map[string]any, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		edges = append(edges, map[string]any{
			"id":             edge.ID,
			"source_node_id": edge.SourceNodeID,
			"target_node_id": edge.TargetNodeID,
			"data_type":      string(edge.DataType),
			"role":           string(edge.Role),
			"order":          edge.Order,
		})
	}
	groups := make([]map[string]any, 0, len(graph.Groups))
	for _, group := range graph.Groups {
		groups = append(groups, map[string]any{"id": group.ID, "title": group.Title})
	}
	sourceOut := map[string]any{}
	for nodeID, record := range sources {
		sourceOut[nodeID] = sourceSnapshot(record)
	}
	return map[string]any{
		"schema_version": GraphSnapshotSchemaVersion,
		"revision":       graph.Revision,
		"nodes":          nodes,
		"edges":          edges,
		"groups":         groups,
		"sources":        sourceOut,
	}
}

func sourceSnapshot(record SourceRecord) map[string]any {
	var artifactType any
	if record.CurrentArtifactType != nil {
		artifactType = *record.CurrentArtifactType
	}
	return map[string]any{
		"facts":                    factsOrEmpty(record.Facts),
		"product_source":           productSourceDict(record.ProductSource),
		"brief":                    record.Brief,
		"visual_payload":           record.VisualPayload,
		"visual_system_version_id": record.VisualSystemVersionID,
		"bound_asset_id":           record.BoundAssetID,
		"bound_asset_label":        record.BoundAssetLabel,
		"bound_asset_mime_type":    record.BoundAssetMIME,
		"current_artifact_id":      record.CurrentArtifactID,
		"current_artifact_type":    artifactType,
		"current_artifact_payload": record.CurrentArtifactPayload,
		"current_output_asset_id":  record.CurrentOutputAssetID,
		"current_input_digest":     record.CurrentInputDigest,
	}
}

func productSourceDict(snap *productSourceSnapshot) any {
	if snap == nil {
		return nil
	}
	var product any
	if snap.SourceProduct != nil {
		product = map[string]any{
			"id":          snap.SourceProduct.ID,
			"name":        snap.SourceProduct.Name,
			"category":    snap.SourceProduct.Category,
			"price":       snap.SourceProduct.Price,
			"source_note": snap.SourceProduct.SourceNote,
		}
	}
	var factSet any
	if snap.FactSetVersion != nil {
		factSet = map[string]any{
			"id":         snap.FactSetVersion.ID,
			"product_id": snap.FactSetVersion.ProductID,
			"version":    snap.FactSetVersion.Version,
			"facts":      factsOrEmpty(snap.FactSetVersion.Facts),
		}
	}
	return map[string]any{
		"source_product_id":   snap.SourceProductID,
		"fact_set_version_id": snap.FactSetVersionID,
		"source_product":      product,
		"fact_set_version":    factSet,
		"facts":               factsOrEmpty(snap.Facts),
		"legacy_fallback":     false,
	}
}

func snapshotNodeTitle(snapshot map[string]any, nodeID string) *string {
	if nodeID == "" {
		return nil
	}
	for _, raw := range snapshotSlice(snapshot["nodes"]) {
		id, _ := raw["id"].(string)
		if id != nodeID {
			continue
		}
		title, _ := raw["title"].(string)
		if strings.TrimSpace(title) == "" {
			return nil
		}
		return &title
	}
	return nil
}

func snapshotInputTrace(snapshot map[string]any, nodeID string) []map[string]any {
	if nodeID == "" {
		return []map[string]any{}
	}
	titles := map[string]any{}
	for _, raw := range snapshotSlice(snapshot["nodes"]) {
		id, _ := raw["id"].(string)
		titles[id] = raw["title"]
	}
	var entries []map[string]any
	for _, edge := range snapshotSlice(snapshot["edges"]) {
		target, _ := edge["target_node_id"].(string)
		if target != nodeID {
			continue
		}
		sourceID, _ := edge["source_node_id"].(string)
		var sourceTitle any
		if title, ok := titles[sourceID].(string); ok && strings.TrimSpace(title) != "" {
			sourceTitle = title
		}
		order := 0
		switch typed := edge["order"].(type) {
		case int:
			order = typed
		case float64:
			order = int(typed)
		}
		var sourcePtr any
		if sourceID != "" {
			sourcePtr = sourceID
		}
		entries = append(entries, map[string]any{
			"edge_id":        fmt.Sprint(edge["id"]),
			"source_node_id": sourcePtr,
			"source_title":   sourceTitle,
			"role":           fmt.Sprint(edge["role"]),
			"order":          order,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		oi, _ := entries[i]["order"].(int)
		oj, _ := entries[j]["order"].(int)
		if oi != oj {
			return oi < oj
		}
		return fmt.Sprint(entries[i]["edge_id"]) < fmt.Sprint(entries[j]["edge_id"])
	})
	if entries == nil {
		return []map[string]any{}
	}
	return entries
}

func snapshotSlice(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func appliedGraphFromSnapshot(payload map[string]any) (AppliedGraph, error) {
	revision := 0
	switch typed := payload["revision"].(type) {
	case int:
		revision = typed
	case float64:
		revision = int(typed)
	}
	out := AppliedGraph{Revision: revision, Nodes: []AppliedNode{}, Edges: []AppliedEdge{}, Groups: []AppliedGroup{}}
	for _, raw := range snapshotSlice(payload["nodes"]) {
		node := AppliedNode{
			ID:        strField(raw["id"]),
			NodeType:  NodeType(strField(raw["node_type"])),
			Title:     strField(raw["title"]),
			PositionX: intField(raw["position_x"]),
			PositionY: intField(raw["position_y"]),
		}
		if cfg, ok := raw["config"].(map[string]any); ok {
			node.Config = cloneMap(cfg)
		} else {
			node.Config = map[string]any{}
		}
		node.BoundAssetID = strPtrField(raw["bound_asset_id"])
		node.GroupID = strPtrField(raw["group_id"])
		out.Nodes = append(out.Nodes, node)
	}
	for _, raw := range snapshotSlice(payload["edges"]) {
		out.Edges = append(out.Edges, AppliedEdge{
			ID:           strField(raw["id"]),
			SourceNodeID: strField(raw["source_node_id"]),
			TargetNodeID: strField(raw["target_node_id"]),
			DataType:     EdgeDataType(strField(raw["data_type"])),
			Role:         EdgeRole(strField(raw["role"])),
			Order:        intField(raw["order"]),
		})
	}
	for _, raw := range snapshotSlice(payload["groups"]) {
		out.Groups = append(out.Groups, AppliedGroup{ID: strField(raw["id"]), Title: strField(raw["title"])})
	}
	return out, nil
}

func sourcesFromSnapshot(payload map[string]any) map[string]SourceRecord {
	out := map[string]SourceRecord{}
	rawSources, _ := payload["sources"].(map[string]any)
	for nodeID, raw := range rawSources {
		rec, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		record := SourceRecord{Facts: []map[string]any{}}
		if facts, ok := rec["facts"].([]any); ok {
			for _, item := range facts {
				if m, ok := item.(map[string]any); ok {
					record.Facts = append(record.Facts, m)
				}
			}
		}
		if brief, ok := rec["brief"].(map[string]any); ok {
			record.Brief = cloneMap(brief)
		}
		if visual, ok := rec["visual_payload"].(map[string]any); ok {
			record.VisualPayload = cloneMap(visual)
		}
		record.VisualSystemVersionID = strPtrField(rec["visual_system_version_id"])
		record.BoundAssetID = strPtrField(rec["bound_asset_id"])
		record.BoundAssetLabel = strPtrField(rec["bound_asset_label"])
		record.BoundAssetMIME = strPtrField(rec["bound_asset_mime_type"])
		record.CurrentArtifactID = strPtrField(rec["current_artifact_id"])
		record.CurrentArtifactType = strPtrField(rec["current_artifact_type"])
		if payloadMap, ok := rec["current_artifact_payload"].(map[string]any); ok {
			record.CurrentArtifactPayload = cloneMap(payloadMap)
		}
		record.CurrentOutputAssetID = strPtrField(rec["current_output_asset_id"])
		record.CurrentInputDigest = strPtrField(rec["current_input_digest"])
		if src, ok := rec["product_source"].(map[string]any); ok {
			snap := productSourceFromDict(src)
			record.ProductSource = &snap
			if len(record.Facts) == 0 {
				record.Facts = snap.Facts
			}
		}
		out[nodeID] = record
	}
	return out
}

func productSourceFromDict(src map[string]any) productSourceSnapshot {
	snap := productSourceSnapshot{Facts: []map[string]any{}}
	snap.SourceProductID = strPtrField(src["source_product_id"])
	snap.FactSetVersionID = strPtrField(src["fact_set_version_id"])
	if product, ok := src["source_product"].(map[string]any); ok {
		summary := productSummary{
			ID:   strField(product["id"]),
			Name: strField(product["name"]),
		}
		summary.Category = strPtrField(product["category"])
		summary.Price = strPtrField(product["price"])
		summary.SourceNote = strPtrField(product["source_note"])
		snap.SourceProduct = &summary
	}
	if factSet, ok := src["fact_set_version"].(map[string]any); ok {
		set := factSetSnapshot{
			ID:        strField(factSet["id"]),
			ProductID: strField(factSet["product_id"]),
			Version:   intField(factSet["version"]),
			Facts:     []map[string]any{},
		}
		if facts, ok := factSet["facts"].([]any); ok {
			for _, item := range facts {
				if m, ok := item.(map[string]any); ok {
					set.Facts = append(set.Facts, m)
				}
			}
		}
		snap.FactSetVersion = &set
	}
	if facts, ok := src["facts"].([]any); ok {
		for _, item := range facts {
			if m, ok := item.(map[string]any); ok {
				snap.Facts = append(snap.Facts, m)
			}
		}
	}
	return snap
}

func strField(v any) string {
	s, _ := v.(string)
	return s
}

func strPtrField(v any) *string {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func intField(v any) int {
	switch typed := v.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

func marshalJSON(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}
