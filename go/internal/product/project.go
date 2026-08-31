package product

import (
	"github.com/yuqie6/productflow/internal/graph"
)

// projectGraph 给直连创建响应嵌一份图投影；未连出的 image_asset 标 unused，绑定仍不等于 reference 边。
func projectGraph(cmd graph.CommandResult, product Product, facts []map[string]any) map[string]any {
	outgoingIDs := map[string]struct{}{}
	for _, edge := range cmd.Applied.Edges {
		outgoingIDs[edge.SourceNodeID] = struct{}{}
	}
	memberIDs := map[string][]string{}
	for _, group := range cmd.Applied.Groups {
		memberIDs[group.ID] = []string{}
	}
	nodes := make([]map[string]any, 0, len(cmd.Applied.Nodes))
	for _, node := range cmd.Applied.Nodes {
		if node.GroupID != nil {
			memberIDs[*node.GroupID] = append(memberIDs[*node.GroupID], node.ID)
		}
		status, err := cmd.Applied.ConfigStatus(node.ID)
		if err != nil {
			status = graph.ConfigIncomplete
		}
		unused := false
		if node.NodeType == graph.NodeImageAsset {
			_, connected := outgoingIDs[node.ID]
			unused = !connected
		}
		var sourceProduct any
		var factSet any
		if node.NodeType == graph.NodeProductSource {
			sourceProduct = map[string]any{
				"id":          product.ID,
				"name":        product.Name,
				"category":    product.Category,
				"price":       product.Price,
				"source_note": product.SourceNote,
			}
			if product.FactSetVersionID != nil {
				factSet = map[string]any{
					"id":         *product.FactSetVersionID,
					"product_id": product.ID,
					"version":    1,
					"facts":      facts,
				}
			}
		}
		incoming := []map[string]any{}
		outgoing := []map[string]any{}
		for _, edge := range cmd.Applied.Edges {
			if edge.TargetNodeID == node.ID {
				incoming = append(incoming, map[string]any{
					"id": edge.ID, "node_id": edge.SourceNodeID, "data_type": edge.DataType, "role": edge.Role, "order": edge.Order,
				})
			}
			if edge.SourceNodeID == node.ID {
				outgoing = append(outgoing, map[string]any{
					"id": edge.ID, "node_id": edge.TargetNodeID, "data_type": edge.DataType, "role": edge.Role, "order": edge.Order,
				})
			}
		}
		preview := any(nil)
		if node.BoundAssetID != nil {
			preview = *node.BoundAssetID
		}
		nodes = append(nodes, map[string]any{
			"id":                       node.ID,
			"node_type":                node.NodeType,
			"title":                    node.Title,
			"position_x":               node.PositionX,
			"position_y":               node.PositionY,
			"config":                   node.Config,
			"source_product":           sourceProduct,
			"product_fact_set":         factSet,
			"bound_asset_id":           node.BoundAssetID,
			"group_id":                 node.GroupID,
			"preview_asset_id":         preview,
			"config_status":            status,
			"unused":                   unused,
			"current_artifact_id":      nil,
			"current_artifact_type":    nil,
			"current_artifact_payload": nil,
			"incoming":                 incoming,
			"outgoing":                 outgoing,
		})
	}
	edges := make([]map[string]any, 0, len(cmd.Applied.Edges))
	for _, edge := range cmd.Applied.Edges {
		edges = append(edges, map[string]any{
			"id":             edge.ID,
			"source_node_id": edge.SourceNodeID,
			"target_node_id": edge.TargetNodeID,
			"data_type":      edge.DataType,
			"role":           edge.Role,
			"order":          edge.Order,
		})
	}
	groups := make([]map[string]any, 0, len(cmd.Applied.Groups))
	for _, group := range cmd.Applied.Groups {
		groups = append(groups, map[string]any{
			"id":         group.ID,
			"title":      group.Title,
			"member_ids": memberIDs[group.ID],
		})
	}
	return map[string]any{
		"id":                       cmd.GraphID,
		"product_id":               cmd.ProductID,
		"title":                    cmd.Title,
		"schema_version":           cmd.SchemaVersion,
		"revision":                 cmd.Revision,
		"source_draft_revision_id": nil,
		"last_operation_group_id":  cmd.OperationGroupID,
		"can_undo":                 true,
		"can_redo":                 false,
		"nodes":                    nodes,
		"edges":                    edges,
		"groups":                   groups,
		"pending_proposal":         nil,
	}
}
