package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

func TestRunnableCountOwnsProductDependency(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, gs.db))
	svc := graph.Service{Products: product.GraphGuard{}}
	// DB is deliberately supplied only through the caller's transaction.
	if err := gs.db.Transaction(func(db *gorm.DB) error {
		count, err := svc.CountRunnableNodesTx(ctx, db, productID, graphID)
		if err != nil || count < 1 {
			t.Fatalf("count=%d err=%v", count, err)
		}
		foreign := auth.WithMerchantID(context.Background(), clockid.New())
		if _, err := svc.CountRunnableNodesTx(foreign, db, productID, graphID); !apperr.IsNotFound(err) {
			t.Fatalf("foreign=%v", err)
		}
		var appErr apperr.Error
		if _, err := (graph.Service{}).CountRunnableNodesTx(ctx, db, productID, graphID); !errors.As(err, &appErr) || appErr.Status != 500 {
			t.Fatalf("missing dependency=%v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var runs int
	if err := gs.pool.QueryRow(ctx, "SELECT count(*) FROM workflow_graph_runs WHERE graph_id=$1", graphID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("count created %d runs", runs)
	}
}
