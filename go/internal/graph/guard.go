package graph

import (
	"context"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

type productGuardCtxKey struct{}

// SourceProduct 是商品资料节点需要的身份摘要；SQL 留在 product 包。
type SourceProduct struct {
	ID               string
	Name             string
	Category         *string
	Price            *string
	SourceNote       *string
	CurrentFactSetID *string
}

// FactSet 是商品事实版本；缺行时守卫返回 nil。
type FactSet struct {
	ID        string
	ProductID string
	Version   int
	Facts     []map[string]any
}

// ProductGuard 把商品行锁、绑定资产和资料快照留在 product 包，避免 graph 查询 products。
type ProductGuard interface {
	Lock(ctx context.Context, tx *gorm.DB, productID string) error
	HasAssets(ctx context.Context, tx *gorm.DB, productID string, ids []string) error
	LoadSource(ctx context.Context, tx *gorm.DB, productID string) (*SourceProduct, error)
	LoadFactSet(ctx context.Context, tx *gorm.DB, factSetID, productID string) (*FactSet, error)
	BoundAssetMeta(ctx context.Context, tx *gorm.DB, productID, assetID string) (displayName, mimeType string, err error)
}

// WithProductGuard 把守卫挂到 ctx 上，供 StageNew / Mutate / CreateEmpty / Project 使用。
func WithProductGuard(ctx context.Context, g ProductGuard) context.Context {
	if g == nil {
		return ctx
	}
	return context.WithValue(ctx, productGuardCtxKey{}, g)
}

func productGuardFrom(ctx context.Context) ProductGuard {
	if ctx == nil {
		return nil
	}
	g, _ := ctx.Value(productGuardCtxKey{}).(ProductGuard)
	return g
}

func requireProductGuard(ctx context.Context) (ProductGuard, error) {
	g := productGuardFrom(ctx)
	if g == nil {
		return nil, apperr.Internal("图命令缺少商品守卫")
	}
	return g, nil
}
