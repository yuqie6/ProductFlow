package graph

import (
	"context"
	"errors"
	"net/http"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

// LoadGraph 在配方保存、Agent 工作流请求里按 product_id + graph_id 读 workflow_graphs 身份。
// 返回 Identity（revision/active），不展开节点/边，也不 FOR UPDATE。
// 对不上返回 NotFound。画布 HTTP 请走 Service.Get（会 Project）；要锁行用 LoadGraphForUpdate。
func LoadGraph(ctx context.Context, tx *gorm.DB, productID, graphID string) (Identity, error) {
	row, err := loadGraph(ctx, tx, productID, graphID)
	return row.Identity, err
}

// LoadGraphForUpdate 在保存配方时锁住指定 live 图。
// 对不上返回 NotFound。
func LoadGraphForUpdate(ctx context.Context, tx *gorm.DB, productID, graphID string) (Identity, error) {
	row, err := loadGraphForUpdate(ctx, tx, productID, graphID)
	return row.Identity, err
}

// LoadAppliedGraph 把图身份展开成节点 / 边 / 分组。
// config JSON 损坏或查库失败原样返回。
func LoadAppliedGraph(ctx context.Context, tx *gorm.DB, id Identity) (AppliedGraph, error) {
	return loadAppliedGraph(ctx, tx, graphRow{Identity: id})
}

// TryLoadActiveGraph 没有 active 图时返回 nil，不把「商品工作流不存在」抛给配方预览。
func TryLoadActiveGraph(ctx context.Context, tx *gorm.DB, productID string) (*Identity, error) {
	row, err := loadActiveGraph(ctx, tx, productID)
	if err != nil {
		var e apperr.Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	id := row.Identity
	return &id, nil
}

// LoadActiveGraphForUpdate 锁住商品当前 active 图；没有则 nil。
// 找不到 active 图返回 nil, nil，不报 NotFound；锁行查询失败原样返回。
func LoadActiveGraphForUpdate(ctx context.Context, tx *gorm.DB, productID string) (*Identity, error) {
	row, err := loadActiveGraphForUpdate(ctx, tx, productID)
	if err != nil || row == nil {
		return nil, err
	}
	id := row.Identity
	return &id, nil
}
