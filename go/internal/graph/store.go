package graph

import (
	"context"
	"encoding/json"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

// Identity 是 live 图对外可见的身份；持久化行 graphRow 不导出。
type Identity struct {
	ID            string
	ProductID     string
	Title         string
	Active        bool
	SchemaVersion int
	Revision      int
}

type graphRow struct {
	Identity
}

type operationGroupRow struct {
	ID                    string
	HistoryKind           HistoryKind
	Summary               string
	InverseOperationsJSON []byte
	ResultRevision        int
}

func loadGraph(ctx context.Context, tx *gorm.DB, productID, graphID string) (graphRow, error) {
	var row graphRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, product_id, title, active, schema_version, revision
		FROM workflow_graphs
		WHERE id = $1 AND product_id = $2
	`, graphID, productID).Scan(&row.ID, &row.ProductID, &row.Title, &row.Active, &row.SchemaVersion, &row.Revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return graphRow{}, apperr.NotFound("商品工作流不存在")
	}
	return row, err
}

func loadGraphForUpdate(ctx context.Context, tx *gorm.DB, productID, graphID string) (graphRow, error) {
	var row graphRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, product_id, title, active, schema_version, revision
		FROM workflow_graphs
		WHERE id = $1 AND product_id = $2
		FOR UPDATE
	`, graphID, productID).Scan(&row.ID, &row.ProductID, &row.Title, &row.Active, &row.SchemaVersion, &row.Revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return graphRow{}, apperr.NotFound("商品工作流不存在")
	}
	return row, err
}

func loadActiveGraph(ctx context.Context, tx *gorm.DB, productID string) (graphRow, error) {
	var row graphRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, product_id, title, active, schema_version, revision
		FROM workflow_graphs
		WHERE product_id = $1 AND active = TRUE
	`, productID).Scan(&row.ID, &row.ProductID, &row.Title, &row.Active, &row.SchemaVersion, &row.Revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return graphRow{}, apperr.NotFound("商品工作流不存在")
	}
	return row, err
}

func loadActiveGraphForUpdate(ctx context.Context, tx *gorm.DB, productID string) (*graphRow, error) {
	var row graphRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, product_id, title, active, schema_version, revision
		FROM workflow_graphs
		WHERE product_id = $1 AND active = TRUE
		FOR UPDATE
	`, productID).Scan(&row.ID, &row.ProductID, &row.Title, &row.Active, &row.SchemaVersion, &row.Revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func loadAppliedGraph(ctx context.Context, tx *gorm.DB, row graphRow) (AppliedGraph, error) {
	groupRows, err := pfdb.Query(ctx, tx, `
		SELECT id, title FROM workflow_graph_groups
		WHERE graph_id = $1
		ORDER BY sort_order, id
	`, row.ID)
	if err != nil {
		return AppliedGraph{}, err
	}
	defer groupRows.Close()
	groups := []AppliedGroup{}
	for groupRows.Next() {
		var group AppliedGroup
		if err := groupRows.Scan(&group.ID, &group.Title); err != nil {
			return AppliedGraph{}, err
		}
		groups = append(groups, group)
	}
	if err := groupRows.Err(); err != nil {
		return AppliedGraph{}, err
	}

	nodeRows, err := pfdb.Query(ctx, tx, `
		SELECT id, node_type, title, position_x, position_y, config_json, bound_image_asset_id, group_id
		FROM workflow_graph_nodes
		WHERE graph_id = $1
		ORDER BY id
	`, row.ID)
	if err != nil {
		return AppliedGraph{}, err
	}
	defer nodeRows.Close()
	nodes := []AppliedNode{}
	for nodeRows.Next() {
		var node AppliedNode
		var configJSON []byte
		if err := nodeRows.Scan(
			&node.ID, &node.NodeType, &node.Title, &node.PositionX, &node.PositionY,
			&configJSON, &node.BoundAssetID, &node.GroupID,
		); err != nil {
			return AppliedGraph{}, err
		}
		config := map[string]any{}
		if len(configJSON) > 0 {
			if err := json.Unmarshal(configJSON, &config); err != nil {
				return AppliedGraph{}, err
			}
		}
		node.Config = config
		nodes = append(nodes, node)
	}
	if err := nodeRows.Err(); err != nil {
		return AppliedGraph{}, err
	}

	edgeRows, err := pfdb.Query(ctx, tx, `
		SELECT id, source_node_id, target_node_id, data_type, role, sort_order
		FROM workflow_graph_edges
		WHERE graph_id = $1
		ORDER BY id
	`, row.ID)
	if err != nil {
		return AppliedGraph{}, err
	}
	defer edgeRows.Close()
	edges := []AppliedEdge{}
	for edgeRows.Next() {
		var edge AppliedEdge
		if err := edgeRows.Scan(&edge.ID, &edge.SourceNodeID, &edge.TargetNodeID, &edge.DataType, &edge.Role, &edge.Order); err != nil {
			return AppliedGraph{}, err
		}
		edges = append(edges, edge)
	}
	if err := edgeRows.Err(); err != nil {
		return AppliedGraph{}, err
	}
	return AppliedGraph{Revision: row.Revision, Nodes: nodes, Edges: edges, Groups: groups}, nil
}

func lastOperationGroup(ctx context.Context, tx *gorm.DB, graph graphRow) (*operationGroupRow, error) {
	var row operationGroupRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, history_kind, summary, inverse_operations_json, result_revision
		FROM workflow_operation_groups
		WHERE graph_id = $1 AND result_revision = $2
	`, graph.ID, graph.Revision).Scan(&row.ID, &row.HistoryKind, &row.Summary, &row.InverseOperationsJSON, &row.ResultRevision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func replaceGraphContents(ctx context.Context, tx *gorm.DB, graphID string, applied AppliedGraph) error {
	// 先删不再存在的边，再清 current_artifact_id 后删节点，避免历史 artifact 外键卡住 live 图。
	nextGroupIDs := idsOf(applied.Groups, func(g AppliedGroup) string { return g.ID })
	nextNodeIDs := idsOf(applied.Nodes, func(n AppliedNode) string { return n.ID })
	nextEdgeIDs := idsOf(applied.Edges, func(e AppliedEdge) string { return e.ID })

	if _, err := pfdb.Exec(ctx, tx, `
		DELETE FROM workflow_graph_edges
		WHERE graph_id = $1 AND NOT (id = ANY($2::text[]))
	`, graphID, nextEdgeIDs); err != nil {
		return err
	}
	if _, err := pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_nodes
		SET current_artifact_id = NULL, updated_at = NOW()
		WHERE graph_id = $1 AND NOT (id = ANY($2::text[]))
	`, graphID, nextNodeIDs); err != nil {
		return err
	}
	if _, err := pfdb.Exec(ctx, tx, `
		DELETE FROM workflow_graph_nodes
		WHERE graph_id = $1 AND NOT (id = ANY($2::text[]))
	`, graphID, nextNodeIDs); err != nil {
		return err
	}
	if _, err := pfdb.Exec(ctx, tx, `
		DELETE FROM workflow_graph_groups
		WHERE graph_id = $1 AND NOT (id = ANY($2::text[]))
	`, graphID, nextGroupIDs); err != nil {
		return err
	}

	for index, group := range applied.Groups {
		if _, err := pfdb.Exec(ctx, tx, `
			INSERT INTO workflow_graph_groups (id, graph_id, title, sort_order, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, sort_order = EXCLUDED.sort_order, updated_at = NOW()
		`, group.ID, graphID, group.Title, index); err != nil {
			return err
		}
	}
	for _, node := range applied.Nodes {
		configJSON, err := json.Marshal(nonemptyMap(node.Config))
		if err != nil {
			return err
		}
		if _, err := pfdb.Exec(ctx, tx, `
			INSERT INTO workflow_graph_nodes (
				id, graph_id, node_type, title, position_x, position_y,
				config_json, bound_image_asset_id, group_id, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET
				node_type = EXCLUDED.node_type,
				title = EXCLUDED.title,
				position_x = EXCLUDED.position_x,
				position_y = EXCLUDED.position_y,
				config_json = EXCLUDED.config_json,
				bound_image_asset_id = EXCLUDED.bound_image_asset_id,
				group_id = EXCLUDED.group_id,
				updated_at = NOW()
		`, node.ID, graphID, node.NodeType, node.Title, node.PositionX, node.PositionY, configJSON, node.BoundAssetID, node.GroupID); err != nil {
			return err
		}
	}
	for _, edge := range applied.Edges {
		if _, err := pfdb.Exec(ctx, tx, `
			INSERT INTO workflow_graph_edges (
				id, graph_id, source_node_id, target_node_id, data_type, role, sort_order, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			ON CONFLICT (id) DO UPDATE SET
				source_node_id = EXCLUDED.source_node_id,
				target_node_id = EXCLUDED.target_node_id,
				data_type = EXCLUDED.data_type,
				role = EXCLUDED.role,
				sort_order = EXCLUDED.sort_order
		`, edge.ID, graphID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order); err != nil {
			return err
		}
	}
	return nil
}

func idsOf[T any](items []T, id func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, id(item))
	}
	if out == nil {
		return []string{}
	}
	return out
}
