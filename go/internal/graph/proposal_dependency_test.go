package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/product"
)

func TestCreateAgentProposalOwnsProductDependency(t *testing.T) {
	for _, mode := range []string{"owner", "other_merchant", "insert_failure"} {
		t.Run(mode, func(t *testing.T) {
			gs := newGraphServer(t)
			ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, gs.db))
			productID := gs.createProduct(t, "proposal dependency")
			svc := graph.Service{DB: gs.db, Products: product.GraphGuard{}}
			view, err := svc.CreateEmpty(ctx, productID)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(map[string]any{
				"base_graph_revision": view.Revision, "summary": "proposal", "actor_type": "agent",
				"operations": []map[string]any{{"op": "create_node", "client_ref": "n1", "node_type": "product_source", "title": "商品资料", "config": map[string]any{"source_product_id": productID}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			change, err := graph.ParseChangeSet(raw)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "other_merchant" {
				ctx = auth.WithMerchantID(context.Background(), clockid.New())
			}
			const constraint = "test_proposal_dependency"
			if mode == "insert_failure" {
				if _, err := gs.pool.Exec(ctx, "ALTER TABLE workflow_graph_proposals ADD CONSTRAINT "+constraint+" CHECK (graph_id <> '"+view.ID+"')"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := gs.pool.Exec(context.Background(), "ALTER TABLE workflow_graph_proposals DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
						t.Error(err)
					}
				})
			}
			proposal, err := svc.CreateAgentProposal(ctx, productID, "", change)
			if mode == "owner" && err != nil {
				t.Fatal(err)
			}
			if mode == "other_merchant" && !apperr.IsNotFound(err) {
				t.Fatalf("cross merchant error=%v", err)
			}
			if mode == "insert_failure" {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
					t.Fatalf("expected constraint failure, got %v", err)
				}
			}
			var proposals, revision, nodes int
			if err := gs.pool.QueryRow(context.Background(), "SELECT count(*) FROM workflow_graph_proposals WHERE graph_id=$1", view.ID).Scan(&proposals); err != nil {
				t.Fatal(err)
			}
			if err := gs.pool.QueryRow(context.Background(), "SELECT revision FROM workflow_graphs WHERE id=$1", view.ID).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			if err := gs.pool.QueryRow(context.Background(), "SELECT count(*) FROM workflow_graph_nodes WHERE graph_id=$1", view.ID).Scan(&nodes); err != nil {
				t.Fatal(err)
			}
			expected := 0
			if mode == "owner" {
				expected = 1
				if proposal.GraphID != view.ID || proposal.ID == "" {
					t.Fatalf("proposal=%+v", proposal)
				}
			}
			if proposals != expected || revision != view.Revision || nodes != 0 {
				t.Fatalf("proposals=%d revision=%d nodes=%d", proposals, revision, nodes)
			}
		})
	}
}
