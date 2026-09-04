package graph

import (
	"context"

	"gorm.io/gorm"
)

// LoadGraph 给 Agent 工作流请求按 product_id + graph_id 读 workflow_graphs 身份。
// 返回 Identity（revision/active），不展开节点/边，也不 FOR UPDATE。
// 对不上返回 NotFound。画布 HTTP 请走 Service.Get；配方保存请走 LoadLiveForUpdate。
func LoadGraph(ctx context.Context, tx *gorm.DB, productID, graphID string) (Identity, error) {
	row, err := loadGraph(ctx, tx, productID, graphID)
	return row.Identity, err
}

// LoadAppliedGraph 把图身份展开成节点 / 边 / 分组。
// config JSON 损坏或查库失败原样返回。跨包预览请优先 TryLive。
func LoadAppliedGraph(ctx context.Context, tx *gorm.DB, id Identity) (AppliedGraph, error) {
	return loadAppliedGraph(ctx, tx, graphRow{Identity: id})
}
