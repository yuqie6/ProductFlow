package graph

import (
	"context"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

// NodeConfigJSON 供交付排队读取节点 config，避免 delivery 直接查 workflow_graph_nodes。
func NodeConfigJSON(ctx context.Context, tx *gorm.DB, nodeID string) ([]byte, error) {
	var raw []byte
	err := pfdb.QueryRow(ctx, tx, `SELECT config_json FROM workflow_graph_nodes WHERE id = $1`, nodeID).Scan(&raw)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, err
	}
	return raw, err
}

// HasImageArtifactForAsset 判断该商品图是否来自成功的工作流 image artifact。
func HasImageArtifactForAsset(ctx context.Context, tx *gorm.DB, assetID string) (bool, error) {
	var id string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id FROM workflow_graph_artifacts
		WHERE product_image_asset_id = $1 AND artifact_type = 'image'
		LIMIT 1
	`, assetID).Scan(&id)
	if errors.Is(err, sqldb.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ImageNodeTarget 是局部编辑需要的当前 image_generation 节点快照。
type ImageNodeTarget struct {
	GraphID     string
	NodeID      string
	Revision    int
	ArtifactID  string
	AssetID     string
	InputDigest string
}

// LockImageNodeTarget 锁住商品 active 图上的 image_generation 节点及其当前 artifact。
func LockImageNodeTarget(ctx context.Context, tx *gorm.DB, productID, nodeID string) (ImageNodeTarget, error) {
	var graphID, nodeType string
	var artifactID *string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT n.graph_id, n.node_type, n.current_artifact_id
		FROM workflow_graph_nodes n
		JOIN workflow_graphs g ON g.id = n.graph_id
		WHERE n.id = $1 AND g.product_id = $2 AND g.active = TRUE
		FOR UPDATE OF n
	`, nodeID, productID).Scan(&graphID, &nodeType, &artifactID)
	if errors.Is(err, sqldb.ErrNoRows) || nodeType != "image_generation" {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 必须是当前商品 active graph 的 image_generation 节点")
	}
	if err != nil {
		return ImageNodeTarget{}, err
	}
	if artifactID == nil {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 必须有当前 image artifact")
	}
	var artifactType string
	var assetID *string
	var digest *string
	err = pfdb.QueryRow(ctx, tx, `
		SELECT artifact_type, product_image_asset_id, input_digest FROM workflow_graph_artifacts WHERE id = $1
	`, *artifactID).Scan(&artifactType, &assetID, &digest)
	if err != nil || artifactType != "image" || assetID == nil {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 必须有当前 image artifact")
	}
	if digest == nil || len(*digest) != 64 {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target 当前 artifact 缺少 input digest")
	}
	var revision int
	if err := pfdb.QueryRow(ctx, tx, `SELECT revision FROM workflow_graphs WHERE id = $1`, graphID).Scan(&revision); err != nil {
		return ImageNodeTarget{}, apperr.Conflict("局部编辑 target graph 不存在")
	}
	return ImageNodeTarget{
		GraphID: graphID, NodeID: nodeID, Revision: revision,
		ArtifactID: *artifactID, AssetID: *assetID, InputDigest: *digest,
	}, nil
}

// ArtifactLineage 是局部编辑 adoption payload 需要的源 artifact 摘要。
type ArtifactLineage struct {
	InputDigest   *string
	GraphRevision int
}

// LoadArtifactLineage 读取 artifact 的 input_digest 与 graph_revision。
func LoadArtifactLineage(ctx context.Context, tx *gorm.DB, artifactID string) (ArtifactLineage, error) {
	var out ArtifactLineage
	err := pfdb.QueryRow(ctx, tx, `
		SELECT input_digest, graph_revision FROM workflow_graph_artifacts WHERE id = $1
	`, artifactID).Scan(&out.InputDigest, &out.GraphRevision)
	if err != nil {
		return ArtifactLineage{}, apperr.Conflict("局部编辑 lineage 记录不完整")
	}
	return out, nil
}
