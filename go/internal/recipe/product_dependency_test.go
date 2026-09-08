package recipe_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/recipe"
	"gorm.io/gorm"
)

type observedProductGuard struct {
	product.GraphGuard
	root           *gorm.DB
	loaded, locked bool
	lockError      error
}

func (g *observedProductGuard) Lock(ctx context.Context, db *gorm.DB, id string) error {
	g.locked = true
	return g.GraphGuard.Lock(ctx, db, id)
}

func (g *observedProductGuard) LoadSource(ctx context.Context, db *gorm.DB, id string) (*graph.SourceProduct, error) {
	g.loaded = true
	if g.locked {
		// A separate real PostgreSQL transaction must be unable to acquire the
		// row while recipe still owns its composition transaction.
		g.lockError = g.root.WithContext(ctx).Transaction(func(other *gorm.DB) error {
			return other.Exec("SELECT id FROM products WHERE id = ? FOR UPDATE NOWAIT", id).Error
		})
	}
	return g.GraphGuard.LoadSource(ctx, db, id)
}

func TestRecipeUsesProductOwnerAndKeepsItsTransactionLock(t *testing.T) {
	rs := newRecipeServer(t)
	id, _ := rs.createDirectGraph(t, "recipe product owner")
	guard := &observedProductGuard{root: rs.db}
	svc := recipe.Service{DB: rs.db, Products: guard}
	// The missing recipe ends the use case after product access. Existing HTTP
	// integration tests cover successful create/preview/apply and merchant scope.
	_, err := svc.Preview(context.Background(), id, clockid.New(), 1)
	if err == nil || !guard.loaded || guard.locked {
		t.Fatalf("preview did not use read dependency: loaded=%v locked=%v err=%v", guard.loaded, guard.locked, err)
	}
	guard.loaded = false
	_, err = svc.Apply(context.Background(), recipe.ApplyInput{
		ProductID: id, RecipeID: clockid.New(), ExpectedRecipeVersion: 1,
		PreviewDigest: strings.Repeat("a", 64), IdempotencyKey: clockid.New(),
	})
	if err == nil || !guard.loaded || !guard.locked {
		t.Fatalf("apply did not use locked dependency: loaded=%v locked=%v err=%v", guard.loaded, guard.locked, err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(guard.lockError, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("product lock not retained in recipe transaction: %v", guard.lockError)
	}
	if err := rs.db.Exec("SELECT id FROM products WHERE id = ? FOR UPDATE NOWAIT", id).Error; err != nil {
		t.Fatalf("failed apply retained product lock: %v", err)
	}
}
