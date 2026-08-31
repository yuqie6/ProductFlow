package graph

import (
	"context"
	"errors"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// NodeConfigJSON 供交付排队读取节点 config，避免 delivery 直接查 workflow_graph_nodes。
// 找不到节点返回 RecordNotFound；其余库错误原样返回。
func NodeConfigJSON(ctx context.Context, tx *gorm.DB, nodeID string) ([]byte, error) {
	var rec schema.WorkflowGraphNodes
	err := tx.WithContext(ctx).Select("config_json").Where("id = ?", nodeID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return []byte(rec.ConfigJSON), nil
}

// HasImageArtifactForAsset 判断该商品图是否来自成功的工作流 image artifact。
// 找不到 artifact 返回 false, nil；库查询失败原样返回。
func HasImageArtifactForAsset(ctx context.Context, tx *gorm.DB, assetID string) (bool, error) {
	var rec schema.WorkflowGraphArtifacts
	err := tx.WithContext(ctx).Select("id").
		Where("product_image_asset_id = ? AND artifact_type = ?", assetID, "image").
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ImageNodeTarget 是局部编辑需要的当前 image_generation 节点快照。
type ImageNodeTarget struct {
	GraphID    string
	NodeID     string
	Revision   int // 锁定时 active 图 revision
	ArtifactID string
	AssetID    string
	// InputDigest 是当前 image artifact 的编译 digest，长度必须为 64。
	InputDigest string
}

// LockImageNodeTarget 锁住商品 active 图上的 image_generation 节点及其当前 artifact。
// 节点不是当前 active 图的 image_generation、缺当前 image artifact 或 digest 返回 Conflict。
func LockImageNodeTarget(ctx context.Context, tx *gorm.DB, productID, nodeID string) (ImageNodeTarget, error) {
	var node schema.WorkflowGraphNodes
	err := tx.WithContext(ctx).
		Model(&schema.WorkflowGraphNodes{}).
		Select("workflow_graph_nodes.*").
		Clauses(pfdb.ForUpdateOf("workflow_graph_nodes")).
		Joins("JOIN workflow_graphs g ON g.id = workflow_graph_nodes.graph_id").
		Where("workflow_graph_nodes.id = ? AND g.product_id = ? AND g.active = ?", nodeID, productID, true).
		Take(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || node.NodeType != "image_generation" {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 必须是当前商品 active graph 的 image_generation 节点")
	}
	if err != nil {
		return ImageNodeTarget{}, err
	}
	if node.CurrentArtifactID == nil {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 必须有当前 image artifact")
	}
	var artifact schema.WorkflowGraphArtifacts
	err = tx.WithContext(ctx).Where("id = ?", *node.CurrentArtifactID).Take(&artifact).Error
	if err != nil || artifact.ArtifactType != "image" || artifact.ProductImageAssetID == nil {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 必须有当前 image artifact")
	}
	if len(artifact.InputDigest) != 64 {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 当前 artifact 缺少 input digest")
	}
	var graph schema.WorkflowGraphs
	if err := tx.WithContext(ctx).Select("revision").Where("id = ?", node.GraphID).Take(&graph).Error; err != nil {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target graph 不存在")
	}
	return ImageNodeTarget{
		GraphID: node.GraphID, NodeID: nodeID, Revision: graph.Revision,
		ArtifactID: *node.CurrentArtifactID, AssetID: *artifact.ProductImageAssetID, InputDigest: artifact.InputDigest,
	}, nil
}

// ArtifactLineage 是局部编辑 adoption payload 需要的源 artifact 摘要。
type ArtifactLineage struct {
	// InputDigest 来自源 artifact；供 adoption payload 带上编译签名。
	InputDigest   *string
	GraphRevision int // 源 artifact 写入时的图 revision
}

// LoadArtifactLineage 读取 artifact 的 input_digest 与 graph_revision。
// 找不到记录返回 Conflict。
func LoadArtifactLineage(ctx context.Context, tx *gorm.DB, artifactID string) (ArtifactLineage, error) {
	var rec schema.WorkflowGraphArtifacts
	err := tx.WithContext(ctx).Select("input_digest", "graph_revision").Where("id = ?", artifactID).Take(&rec).Error
	if err != nil {
		return ArtifactLineage{}, apperr.Conflict("局部编辑 lineage 记录不完整")
	}
	digest := rec.InputDigest
	return ArtifactLineage{InputDigest: &digest, GraphRevision: rec.GraphRevision}, nil
}
