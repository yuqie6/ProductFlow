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
	Category         *string // nil 表示未填类目
	Price            *string // nil 表示未填价格
	SourceNote       *string // nil 表示未填商品说明
	CurrentFactSetID *string
}

// FactSet 是 graph 编译器用的商品事实版本快照，由 ProductGuard.LoadFactSet 从 product_fact_set_versions 填入。
// Facts 是 payload_json 的 []map，不是 product.Fact HTTP 结构；缺行时守卫返回 nil, nil，不要改成 NotFound。
// 不要和 product.FactSet（GET/PUT /facts 投影）搞混。graph 包不得直接查 products 表。
type FactSet struct {
	ID        string
	ProductID string
	Version   int              // 不可变版本号，对应 product_fact_set_versions.version
	Facts     []map[string]any // payload_json 的 []map；空列表是 [] 不是 nil
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
