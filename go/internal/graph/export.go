package graph

import (
	"context"
	"errors"
	"net/http"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

// LoadGraph 按商品 + 图 id 读取 live 图身份。
func LoadGraph(ctx context.Context, tx *gorm.DB, productID, graphID string) (Identity, error) {
	row, err := loadGraph(ctx, tx, productID, graphID)
	return row.Identity, err
}

// LoadGraphForUpdate 在保存配方时锁住指定 live 图。
func LoadGraphForUpdate(ctx context.Context, tx *gorm.DB, productID, graphID string) (Identity, error) {
	row, err := loadGraphForUpdate(ctx, tx, productID, graphID)
	return row.Identity, err
}

// LoadAppliedGraph 把图身份展开成节点 / 边 / 分组。
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
func LoadActiveGraphForUpdate(ctx context.Context, tx *gorm.DB, productID string) (*Identity, error) {
	row, err := loadActiveGraphForUpdate(ctx, tx, productID)
	if err != nil || row == nil {
		return nil, err
	}
	id := row.Identity
	return &id, nil
}
