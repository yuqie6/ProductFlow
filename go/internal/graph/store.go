package graph

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func graphRowFromSchema(rec schema.WorkflowGraphs) graphRow {
	return graphRow{Identity: Identity{
		ID:            rec.ID,
		ProductID:     rec.ProductID,
		Title:         rec.Title,
		Active:        rec.Active,
		SchemaVersion: rec.SchemaVersion,
		Revision:      rec.Revision,
	}}
}

func jsonPtrBytes(s *string) []byte {
	if s == nil {
		return nil
	}
	return []byte(*s)
}

func loadGraph(ctx context.Context, tx *gorm.DB, productID, graphID string) (graphRow, error) {
	var rec schema.WorkflowGraphs
	err := tx.WithContext(ctx).Where("id = ? AND product_id = ?", graphID, productID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRow{}, apperr.NotFound("商品工作流不存在")
	}
	if err != nil {
		return graphRow{}, err
	}
	return graphRowFromSchema(rec), nil
}

func loadGraphForUpdate(ctx context.Context, tx *gorm.DB, productID, graphID string) (graphRow, error) {
	var rec schema.WorkflowGraphs
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ? AND product_id = ?", graphID, productID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRow{}, apperr.NotFound("商品工作流不存在")
	}
	if err != nil {
		return graphRow{}, err
	}
	return graphRowFromSchema(rec), nil
}

func loadActiveGraph(ctx context.Context, tx *gorm.DB, productID string) (graphRow, error) {
	var rec schema.WorkflowGraphs
	err := tx.WithContext(ctx).Where("product_id = ? AND active = ?", productID, true).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRow{}, apperr.NotFound("商品工作流不存在")
	}
	if err != nil {
		return graphRow{}, err
	}
	return graphRowFromSchema(rec), nil
}

func loadActiveGraphForUpdate(ctx context.Context, tx *gorm.DB, productID string) (*graphRow, error) {
	var rec schema.WorkflowGraphs
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("product_id = ? AND active = ?", productID, true).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row := graphRowFromSchema(rec)
	return &row, nil
}

func loadAppliedGraph(ctx context.Context, tx *gorm.DB, row graphRow) (AppliedGraph, error) {
	var groupRecs []schema.WorkflowGraphGroups
	if err := tx.WithContext(ctx).Where("graph_id = ?", row.ID).Order("sort_order, id").Find(&groupRecs).Error; err != nil {
		return AppliedGraph{}, err
	}
	groups := make([]AppliedGroup, 0, len(groupRecs))
	for _, rec := range groupRecs {
		groups = append(groups, AppliedGroup{ID: rec.ID, Title: rec.Title})
	}

	var nodeRecs []schema.WorkflowGraphNodes
	if err := tx.WithContext(ctx).Where("graph_id = ?", row.ID).Order("id").Find(&nodeRecs).Error; err != nil {
		return AppliedGraph{}, err
	}
	nodes := make([]AppliedNode, 0, len(nodeRecs))
	for _, rec := range nodeRecs {
		config := map[string]any{}
		if rec.ConfigJSON != "" {
			if err := json.Unmarshal([]byte(rec.ConfigJSON), &config); err != nil {
				return AppliedGraph{}, err
			}
		}
		nodes = append(nodes, AppliedNode{
			ID:             rec.ID,
			NodeType:       NodeType(rec.NodeType),
			Title:          rec.Title,
			PositionX:      rec.PositionX,
			PositionY:      rec.PositionY,
			Config:         config,
			BoundAssetID:   rec.BoundImageAssetID,
			GroupID:        rec.GroupID,
			DocumentOrigin: loadDocumentOrigin(NodeType(rec.NodeType), rec.DocumentOrigin),
		})
	}

	var edgeRecs []schema.WorkflowGraphEdges
	if err := tx.WithContext(ctx).Where("graph_id = ?", row.ID).Order("id").Find(&edgeRecs).Error; err != nil {
		return AppliedGraph{}, err
	}
	edges := make([]AppliedEdge, 0, len(edgeRecs))
	for _, rec := range edgeRecs {
		edges = append(edges, AppliedEdge{
			ID:           rec.ID,
			SourceNodeID: rec.SourceNodeID,
			TargetNodeID: rec.TargetNodeID,
			DataType:     EdgeDataType(rec.DataType),
			Role:         EdgeRole(rec.Role),
			Order:        rec.SortOrder,
		})
	}
	return AppliedGraph{Revision: row.Revision, Nodes: nodes, Edges: edges, Groups: groups}, nil
}

func lastOperationGroup(ctx context.Context, tx *gorm.DB, graph graphRow) (*operationGroupRow, error) {
	var rec schema.WorkflowOperationGroups
	err := tx.WithContext(ctx).Where("graph_id = ? AND result_revision = ?", graph.ID, graph.Revision).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &operationGroupRow{
		ID:                    rec.ID,
		HistoryKind:           HistoryKind(rec.HistoryKind),
		Summary:               rec.Summary,
		InverseOperationsJSON: []byte(rec.InverseOperationsJSON),
		ResultRevision:        rec.ResultRevision,
	}, nil
}

func replaceGraphContents(ctx context.Context, tx *gorm.DB, graphID string, applied AppliedGraph) error {
	// 先删不再存在的边，再清 current_artifact_id 后删节点，避免历史 artifact 外键卡住 live 图。
	nextGroupIDs := idsOf(applied.Groups, func(g AppliedGroup) string { return g.ID })
	nextNodeIDs := idsOf(applied.Nodes, func(n AppliedNode) string { return n.ID })
	nextEdgeIDs := idsOf(applied.Edges, func(e AppliedEdge) string { return e.ID })
	now := time.Now().UTC()

	if err := deleteGraphIDExcept(ctx, tx, &schema.WorkflowGraphEdges{}, graphID, nextEdgeIDs); err != nil {
		return err
	}
	nullQ := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).Where("graph_id = ?", graphID)
	if len(nextNodeIDs) > 0 {
		nullQ = nullQ.Where("id NOT IN ?", nextNodeIDs)
	}
	if err := nullQ.Updates(map[string]any{
		"current_artifact_id": nil,
		"updated_at":          now,
	}).Error; err != nil {
		return err
	}
	if err := deleteGraphIDExcept(ctx, tx, &schema.WorkflowGraphNodes{}, graphID, nextNodeIDs); err != nil {
		return err
	}
	if err := deleteGraphIDExcept(ctx, tx, &schema.WorkflowGraphGroups{}, graphID, nextGroupIDs); err != nil {
		return err
	}

	for index, group := range applied.Groups {
		now := time.Now().UTC()
		err := tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"title": group.Title, "sort_order": index, "updated_at": now,
			}),
		}).Create(&schema.WorkflowGraphGroups{
			ID: group.ID, GraphID: graphID, Title: group.Title, SortOrder: index, CreatedAt: now, UpdatedAt: now,
		}).Error
		if err != nil {
			return err
		}
	}
	for _, node := range applied.Nodes {
		configJSON, err := json.Marshal(nonemptyMap(node.Config))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		configStr := string(configJSON)
		err = tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"node_type":            string(node.NodeType),
				"title":                node.Title,
				"position_x":           node.PositionX,
				"position_y":           node.PositionY,
				"config_json":          configStr,
				"bound_image_asset_id": node.BoundAssetID,
				"group_id":             node.GroupID,
				"document_origin":      documentOriginPtr(node),
				"updated_at":           now,
			}),
		}).Create(&schema.WorkflowGraphNodes{
			ID:                node.ID,
			GraphID:           graphID,
			NodeType:          string(node.NodeType),
			Title:             node.Title,
			PositionX:         node.PositionX,
			PositionY:         node.PositionY,
			ConfigJSON:        configStr,
			BoundImageAssetID: node.BoundAssetID,
			GroupID:           node.GroupID,
			DocumentOrigin:    documentOriginPtr(node),
			CreatedAt:         now,
			UpdatedAt:         now,
		}).Error
		if err != nil {
			return err
		}
	}
	for _, edge := range applied.Edges {
		now := time.Now().UTC()
		err := tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"source_node_id": edge.SourceNodeID,
				"target_node_id": edge.TargetNodeID,
				"data_type":      string(edge.DataType),
				"role":           string(edge.Role),
				"sort_order":     edge.Order,
			}),
		}).Create(&schema.WorkflowGraphEdges{
			ID:           edge.ID,
			GraphID:      graphID,
			SourceNodeID: edge.SourceNodeID,
			TargetNodeID: edge.TargetNodeID,
			DataType:     string(edge.DataType),
			Role:         string(edge.Role),
			SortOrder:    edge.Order,
			CreatedAt:    now,
		}).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteGraphIDExcept(ctx context.Context, tx *gorm.DB, model any, graphID string, keepIDs []string) error {
	q := tx.WithContext(ctx).Where("graph_id = ?", graphID)
	if len(keepIDs) > 0 {
		q = q.Where("id NOT IN ?", keepIDs)
	}
	return q.Delete(model).Error
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
