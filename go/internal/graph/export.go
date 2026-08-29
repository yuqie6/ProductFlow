package graph

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// LoadGraph 按商品 + 图 id 读取 live 图行。
func LoadGraph(ctx context.Context, tx pgx.Tx, productID, graphID string) (GraphRow, error) {
	return loadGraph(ctx, tx, productID, graphID)
}

// LoadGraphForUpdate 在保存配方时锁住指定 live 图。
func LoadGraphForUpdate(ctx context.Context, tx pgx.Tx, productID, graphID string) (GraphRow, error) {
	return loadGraphForUpdate(ctx, tx, productID, graphID)
}

// LoadAppliedGraph 把图行展开成节点 / 边 / 分组。
func LoadAppliedGraph(ctx context.Context, tx pgx.Tx, row GraphRow) (AppliedGraph, error) {
	return loadAppliedGraph(ctx, tx, row)
}

// TryLoadActiveGraph 没有 active 图时返回 nil，不把「商品工作流不存在」抛给配方预览。
func TryLoadActiveGraph(ctx context.Context, tx pgx.Tx, productID string) (*GraphRow, error) {
	row, err := loadActiveGraph(ctx, tx, productID)
	if err != nil {
		var e apperr.Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// LoadActiveGraphForUpdate 锁住商品当前 active 图；没有则 nil。
func LoadActiveGraphForUpdate(ctx context.Context, tx pgx.Tx, productID string) (*GraphRow, error) {
	return loadActiveGraphForUpdate(ctx, tx, productID)
}
