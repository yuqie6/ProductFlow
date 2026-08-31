package graph

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
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
	PayloadJSON               []byte // adoption 事件 JSON，写入 artifact.payload_json
	// PayloadHash 是 PayloadJSON 的 SHA-256 hex，写入 artifact.payload_hash。
	PayloadHash  string
	ProviderName string // 写入 artifact.provider_name；局部编辑常为 local_edit
	// ProviderModel 为 nil 表示未记录模型。
	ProviderModel *string
}

// AdoptImageArtifact 锁定 active 图与节点，写入 image artifact 并切换 current_artifact_id。
func AdoptImageArtifact(ctx context.Context, tx *gorm.DB, in AdoptImageArtifactInput) (string, error) {
	var graph schema.WorkflowGraphs
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND product_id = ? AND active = ?", in.GraphID, in.ProductID, true).
		Take(&graph).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", apperr.Conflict("局部编辑目标 graph 已变化或不再 active")
	}
	if err != nil {
		return "", err
	}
	var node schema.WorkflowGraphNodes
	err = tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND graph_id = ?", in.NodeID, in.GraphID).
		Take(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || node.NodeType != "image_generation" {
		return "", apperr.Conflict("局部编辑目标节点已变化")
	}
	if err != nil {
		return "", err
	}
	if node.CurrentArtifactID == nil || *node.CurrentArtifactID != in.ExpectedCurrentArtifactID || *node.CurrentArtifactID != in.SourceArtifactID {
		return "", apperr.Conflict("节点当前结果已变化，不能 adoption")
	}
	var source schema.WorkflowGraphArtifacts
	err = tx.WithContext(ctx).Where("id = ?", in.SourceArtifactID).Take(&source).Error
	if err != nil {
		return "", apperr.Conflict("局部编辑 lineage 记录不完整")
	}
	sourceNodeID := ""
	if source.NodeID != nil {
		sourceNodeID = *source.NodeID
	}
	if source.GraphID != in.GraphID || sourceNodeID != in.NodeID ||
		source.ProductImageAssetID == nil || *source.ProductImageAssetID != in.ExpectedSourceAssetID {
		return "", apperr.Conflict("局部编辑 source artifact 与任务快照不一致")
	}
	artifactID := clockid.New()
	nodeID := in.NodeID
	resultAssetID := in.ResultAssetID
	providerName := in.ProviderName
	err = tx.WithContext(ctx).Create(&schema.WorkflowGraphArtifacts{
		ID:                  artifactID,
		GraphID:             in.GraphID,
		NodeID:              &nodeID,
		ArtifactType:        "image",
		SchemaVersion:       3,
		GraphRevision:       graph.Revision,
		PayloadJSON:         string(in.PayloadJSON),
		PayloadHash:         in.PayloadHash,
		InputDigest:         source.InputDigest,
		ProductImageAssetID: &resultAssetID,
		ProviderName:        &providerName,
		ProviderModel:       in.ProviderModel,
		CreatedAt:           time.Now().UTC(),
	}).Error
	if err != nil {
		return "", err
	}
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).Where("id = ?", in.NodeID).
		Select("current_artifact_id").
		Updates(map[string]any{"current_artifact_id": artifactID}).Error; err != nil {
		return "", err
	}
	return artifactID, nil
}

// RevertNodeCurrentArtifact 把节点 current_artifact_id 从已 adoption 的结果改回 source。
// graph 不再 active、节点缺失或当前 artifact 已变返回 Conflict；其余库错误原样返回。
func RevertNodeCurrentArtifact(ctx context.Context, tx *gorm.DB, productID, graphID, nodeID, expectedArtifactID, adoptedArtifactID, restoreArtifactID string) error {
	var graph schema.WorkflowGraphs
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND product_id = ? AND active = ?", graphID, productID, true).
		Take(&graph).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.Conflict("adoption 所属 graph 已变化或不再 active")
	}
	if err != nil {
		return err
	}
	var node schema.WorkflowGraphNodes
	err = tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND graph_id = ?", nodeID, graphID).
		Take(&node).Error
	if err != nil || node.CurrentArtifactID == nil || *node.CurrentArtifactID != expectedArtifactID || *node.CurrentArtifactID != adoptedArtifactID {
		return apperr.Conflict("节点当前结果已变化，不能 revert")
	}
	return tx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).Where("id = ?", nodeID).
		Select("current_artifact_id").
		Updates(map[string]any{"current_artifact_id": restoreArtifactID}).Error
}
