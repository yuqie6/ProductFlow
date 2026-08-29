package graph

import (
	"context"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

// AdoptImageArtifactInput 把局部编辑结果写成当前节点的 image artifact。
type AdoptImageArtifactInput struct {
	ProductID                 string
	GraphID                   string
	NodeID                    string
	ExpectedCurrentArtifactID string
	SourceArtifactID          string
	ExpectedSourceAssetID     string
	ResultAssetID             string
	PayloadJSON               []byte
	PayloadHash               string
	ProviderName              string
	ProviderModel             *string
}

// AdoptImageArtifact 锁定 active 图与节点，写入 image artifact 并切换 current_artifact_id。
func AdoptImageArtifact(ctx context.Context, tx *gorm.DB, in AdoptImageArtifactInput) (string, error) {
	var revision int
	err := pfdb.QueryRow(ctx, tx, `
		SELECT revision FROM workflow_graphs
		WHERE id = $1 AND product_id = $2 AND active = TRUE
		FOR UPDATE
	`, in.GraphID, in.ProductID).Scan(&revision)
	if errors.Is(err, sqldb.ErrNoRows) {
		return "", apperr.Conflict("局部编辑目标 graph 已变化或不再 active")
	}
	if err != nil {
		return "", err
	}
	var nodeType string
	var currentArtifact *string
	err = pfdb.QueryRow(ctx, tx, `
		SELECT node_type, current_artifact_id FROM workflow_graph_nodes
		WHERE id = $1 AND graph_id = $2 FOR UPDATE
	`, in.NodeID, in.GraphID).Scan(&nodeType, &currentArtifact)
	if errors.Is(err, sqldb.ErrNoRows) || nodeType != "image_generation" {
		return "", apperr.Conflict("局部编辑目标节点已变化")
	}
	if err != nil {
		return "", err
	}
	if currentArtifact == nil || *currentArtifact != in.ExpectedCurrentArtifactID || *currentArtifact != in.SourceArtifactID {
		return "", apperr.Conflict("节点当前结果已变化，不能 adoption")
	}
	var sourceGraphID, sourceNodeID string
	var sourceAssetID *string
	var sourceDigest *string
	err = pfdb.QueryRow(ctx, tx, `
		SELECT graph_id, node_id, product_image_asset_id, input_digest
		FROM workflow_graph_artifacts WHERE id = $1
	`, in.SourceArtifactID).Scan(&sourceGraphID, &sourceNodeID, &sourceAssetID, &sourceDigest)
	if err != nil {
		return "", apperr.Conflict("局部编辑 lineage 记录不完整")
	}
	if sourceGraphID != in.GraphID || sourceNodeID != in.NodeID ||
		sourceAssetID == nil || *sourceAssetID != in.ExpectedSourceAssetID {
		return "", apperr.Conflict("局部编辑 source artifact 与任务快照不一致")
	}
	artifactID := clockid.New()
	if _, err := pfdb.Exec(ctx, tx, `
		INSERT INTO workflow_graph_artifacts (
			id, graph_id, node_id, node_run_id, artifact_type, schema_version, graph_revision,
			payload_json, payload_hash, input_digest, product_image_asset_id, provider_name, provider_model, created_at
		) VALUES ($1, $2, $3, NULL, 'image', 3, $4, $5, $6, $7, $8, $9, $10, NOW())
	`, artifactID, in.GraphID, in.NodeID, revision, in.PayloadJSON, in.PayloadHash, sourceDigest, in.ResultAssetID, in.ProviderName, in.ProviderModel); err != nil {
		return "", err
	}
	if _, err := pfdb.Exec(ctx, tx, `UPDATE workflow_graph_nodes SET current_artifact_id = $1 WHERE id = $2`, artifactID, in.NodeID); err != nil {
		return "", err
	}
	return artifactID, nil
}

// RevertNodeCurrentArtifact 把节点 current_artifact_id 从已 adoption 的结果改回 source。
func RevertNodeCurrentArtifact(ctx context.Context, tx *gorm.DB, productID, graphID, nodeID, expectedArtifactID, adoptedArtifactID, restoreArtifactID string) error {
	var liveGraph string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id FROM workflow_graphs WHERE id = $1 AND product_id = $2 AND active = TRUE FOR UPDATE
	`, graphID, productID).Scan(&liveGraph)
	if errors.Is(err, sqldb.ErrNoRows) {
		return apperr.Conflict("adoption 所属 graph 已变化或不再 active")
	}
	if err != nil {
		return err
	}
	var current *string
	err = pfdb.QueryRow(ctx, tx, `
		SELECT current_artifact_id FROM workflow_graph_nodes WHERE id = $1 AND graph_id = $2 FOR UPDATE
	`, nodeID, graphID).Scan(&current)
	if err != nil || current == nil || *current != expectedArtifactID || *current != adoptedArtifactID {
		return apperr.Conflict("节点当前结果已变化，不能 revert")
	}
	_, err = pfdb.Exec(ctx, tx, `UPDATE workflow_graph_nodes SET current_artifact_id = $1 WHERE id = $2`, restoreArtifactID, nodeID)
	return err
}
