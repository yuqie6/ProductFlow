package graph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// StageNew 在空图上应用 ChangeSet 并写入 workflow_graphs；只 flush 不 commit。
func StageNew(ctx context.Context, tx pgx.Tx, productID, title string, changeSet ChangeSet) (CommandResult, error) {
	if changeSet.BaseGraphRevision != 0 {
		return CommandResult{}, apperr.Conflict("新建图的 base_graph_revision 必须为 0")
	}
	if err := lockProduct(ctx, tx, productID); err != nil {
		return CommandResult{}, err
	}
	exists, err := activeGraphExists(ctx, tx, productID)
	if err != nil {
		return CommandResult{}, err
	}
	if exists {
		return CommandResult{}, apperr.Conflict("商品已有 active schema-v3 工作流")
	}
	applied, err := Apply(EmptyGraph, changeSet)
	if err != nil {
		return CommandResult{}, err
	}
	applied = assignPersistentIDs(EmptyGraph, applied, clockid.New)
	if err := validateBoundAssets(ctx, tx, productID, applied); err != nil {
		return CommandResult{}, err
	}
	if err := validateProductSourceConfigs(ctx, tx, productID, applied); err != nil {
		return CommandResult{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = DefaultGraphTitle
	}
	actor := changeSet.ActorType
	if actor == "" {
		actor = ActorUser
	}
	graphID := clockid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, $3, TRUE, $4, $5, NOW(), NOW())
	`, graphID, productID, title, SchemaVersion, applied.Revision)
	if err != nil {
		return CommandResult{}, err
	}
	if err := insertGraphContents(ctx, tx, graphID, applied); err != nil {
		return CommandResult{}, err
	}
	opsJSON, err := marshalOperations(changeSet.Operations)
	if err != nil {
		return CommandResult{}, err
	}
	inverseJSON, err := marshalOperations(Invert(EmptyGraph, applied))
	if err != nil {
		return CommandResult{}, err
	}
	operationGroupID := clockid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_operation_groups (
			id, graph_id, actor_type, history_kind, summary, base_revision, result_revision,
			operations_json, inverse_operations_json, created_at
		) VALUES ($1, $2, $3, $4, $5, 0, $6, $7, $8, NOW())
	`, operationGroupID, graphID, actor, HistoryEdit, changeSet.Summary, applied.Revision, opsJSON, inverseJSON)
	if err != nil {
		return CommandResult{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE workflow_graphs SET updated_at = NOW() WHERE id = $1`, graphID)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{
		GraphID:          graphID,
		ProductID:        productID,
		Title:            title,
		Active:           true,
		SchemaVersion:    SchemaVersion,
		Revision:         applied.Revision,
		Applied:          applied,
		OperationGroupID: operationGroupID,
	}, nil
}

func lockProduct(ctx context.Context, tx pgx.Tx, productID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM products WHERE id = $1 FOR UPDATE`, productID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("商品不存在")
	}
	return err
}

func activeGraphExists(ctx context.Context, tx pgx.Tx, productID string) (bool, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM workflow_graphs WHERE product_id = $1 AND active = TRUE`, productID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func validateBoundAssets(ctx context.Context, tx pgx.Tx, productID string, graph AppliedGraph) error {
	wanted := map[string]struct{}{}
	for _, node := range graph.Nodes {
		if node.BoundAssetID != nil && *node.BoundAssetID != "" {
			wanted[*node.BoundAssetID] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	ids := sortedKeys(wanted)
	rows, err := tx.Query(ctx, `
		SELECT id FROM product_image_assets
		WHERE product_id = $1 AND id = ANY($2)
	`, productID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		found[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(found) != len(wanted) {
		return apperr.Validation("节点绑定了不属于该商品的图片")
	}
	return nil
}

func validateProductSourceConfigs(ctx context.Context, tx pgx.Tx, graphProductID string, graph AppliedGraph) error {
	for _, node := range graph.Nodes {
		if node.NodeType != NodeProductSource {
			continue
		}
		if err := resolveProductSource(ctx, tx, graphProductID, node.Config); err != nil {
			return err
		}
	}
	return nil
}

func resolveProductSource(ctx context.Context, tx pgx.Tx, graphProductID string, config map[string]any) error {
	payload := config
	if payload == nil {
		payload = map[string]any{}
	}
	_, hasSourceBinding := payload["source_product_id"]
	rawSourceID := payload["source_product_id"]
	if rawSourceID != nil {
		s, ok := rawSourceID.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return apperr.Validation("商品资料节点的 source_product_id 无效")
		}
	}
	var sourceProductID *string
	if s, ok := rawSourceID.(string); ok {
		trimmed := strings.TrimSpace(s)
		sourceProductID = &trimmed
	}
	if !hasSourceBinding {
		sourceProductID = &graphProductID
	}
	if sourceProductID == nil {
		if payload["fact_set_version_id"] != nil {
			return apperr.Validation("未绑定商品的商品资料节点不能绑定 fact_set_version_id")
		}
		return nil
	}
	var currentFactSetID *string
	err := tx.QueryRow(ctx, `SELECT current_fact_set_version_id FROM products WHERE id = $1`, *sourceProductID).Scan(&currentFactSetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Validation("商品资料节点绑定的商品不存在")
	}
	if err != nil {
		return err
	}
	rawFactSetID := payload["fact_set_version_id"]
	if rawFactSetID != nil {
		s, ok := rawFactSetID.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return apperr.Validation("商品资料节点的 fact_set_version_id 无效")
		}
		factSetID := strings.TrimSpace(s)
		var owner string
		err := tx.QueryRow(ctx, `
			SELECT product_id FROM product_fact_set_versions WHERE id = $1 AND product_id = $2
		`, factSetID, *sourceProductID).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Validation("fact_set_version_id 不属于绑定商品")
		}
		return err
	}
	if currentFactSetID == nil || *currentFactSetID == "" {
		return nil
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT product_id FROM product_fact_set_versions WHERE id = $1`, *currentFactSetID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if owner != *sourceProductID {
		return apperr.Validation("商品当前事实版本不属于该商品")
	}
	return nil
}

func insertGraphContents(ctx context.Context, tx pgx.Tx, graphID string, applied AppliedGraph) error {
	for index, group := range applied.Groups {
		_, err := tx.Exec(ctx, `
			INSERT INTO workflow_graph_groups (id, graph_id, title, sort_order, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW())
		`, group.ID, graphID, group.Title, index)
		if err != nil {
			return err
		}
	}
	for _, node := range applied.Nodes {
		configJSON, err := json.Marshal(nonemptyMap(node.Config))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO workflow_graph_nodes (
				id, graph_id, node_type, title, position_x, position_y,
				config_json, bound_image_asset_id, group_id, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		`, node.ID, graphID, node.NodeType, node.Title, node.PositionX, node.PositionY, configJSON, node.BoundAssetID, node.GroupID)
		if err != nil {
			return err
		}
	}
	for _, edge := range applied.Edges {
		_, err := tx.Exec(ctx, `
			INSERT INTO workflow_graph_edges (
				id, graph_id, source_node_id, target_node_id, data_type, role, sort_order, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		`, edge.ID, graphID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order)
		if err != nil {
			return err
		}
	}
	return nil
}
