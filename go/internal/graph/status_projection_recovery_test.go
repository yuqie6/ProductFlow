package graph_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
)

type projectionImageProvider struct {
	graph.MockImageProvider
	calls atomic.Int32
}

func (p *projectionImageProvider) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	p.calls.Add(1)
	return p.MockImageProvider.GenerateImage(ctx, req)
}

func TestGraphProjectionFailureRetainsDispatchWithoutRepeatingProvider(t *testing.T) {
	for _, terminal := range []string{"succeeded", "unknown"} {
		t.Run(terminal, func(t *testing.T) {
			gs := newIsolatedGraphServer(t)
			productID, graphID := gs.createDirectGraph(t)
			ctx := context.Background()
			resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
			gs.mustStatus(t, resp, 201)
			var run graph.GraphRunResponse
			gs.decode(t, resp, &run)
			now := time.Now().UTC()
			convID, requestID := clockid.New(), clockid.New()
			merchantID := auth.MustDevMerchantID(t, gs.db)
			if err := gs.db.Create(&schema.AgentConversations{ID: convID, MerchantID: merchantID, ProductID: &productID, HarnessRunID: convID, ScopeType: "product_workflow", Status: "collecting", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
				t.Fatal(err)
			}
			if err := gs.db.Create(&schema.AgentWorkflowRunRequests{ID: requestID, ConversationID: convID, ProductID: productID, GraphID: graphID, GraphRunID: &run.ID, ExpectedWorkflowRevision: 1, IdempotencyKey: requestID, RequestHash: strings.Repeat("a", 64), SourceStepID: "projection-recovery", Status: "confirmed", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
				t.Fatal(err)
			}
			if err := gs.db.Exec("ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT test_graph_projection_failure CHECK (status='confirmed')").Error; err != nil {
				t.Fatal(err)
			}
			provider := &projectionImageProvider{}
			if terminal == "unknown" {
				provider.MockImageProvider.Err = graph.ErrProviderUnknown()
			}
			exec := graph.Executor{DB: gs.db, Products: product.GraphGuard{}, AfterRunStatus: agent.SyncGraphRunToTasks, Deps: graph.Dependencies{Prompt: graph.MockPromptProvider{}, Image: provider, Assets: product.Service{DB: gs.db, Media: gs.media}}}
			job := loadGraphJob(t, gs, run.ID)
			worker := queue.NewWorker(map[string]queue.ActorFunc{queue.ActorGraphRun: exec.ExecuteRun})
			err := worker.Work(ctx, job)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "test_graph_projection_failure" {
				t.Errorf("projection error consumed: %v", err)
			}
			var runStatus, requestStatus string
			read := func() {
				t.Helper()
				if err := gs.pool.QueryRow(ctx, `SELECT r.status,q.status FROM workflow_graph_runs r JOIN agent_workflow_run_requests q ON q.graph_run_id=r.id WHERE r.id=$1`, run.ID).Scan(&runStatus, &requestStatus); err != nil {
					t.Fatal(err)
				}
			}
			read()
			if runStatus != terminal || requestStatus != "confirmed" {
				t.Fatalf("run=%s request=%s", runStatus, requestStatus)
			}
			before := loadQuotaAccount(t, gs.db, merchantID)
			calls := provider.calls.Load()
			if calls < 1 {
				t.Fatal("provider was never invoked")
			}
			// Repeated delivery while the fault persists must still fail, even for an already-terminal run.
			err = worker.Work(ctx, job)
			pgErr = nil
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("terminal replay ignored projection failure: %v", err)
			}
			if err := gs.db.Exec("ALTER TABLE agent_workflow_run_requests DROP CONSTRAINT test_graph_projection_failure").Error; err != nil {
				t.Fatal(err)
			}
			if err := worker.Work(ctx, job); err != nil {
				t.Fatal(err)
			}
			read()
			if runStatus != terminal || requestStatus != terminal {
				t.Fatalf("recovery run=%s request=%s", runStatus, requestStatus)
			}
			after := loadQuotaAccount(t, gs.db, merchantID)
			if provider.calls.Load() != calls || after.AvailableUnits != before.AvailableUnits || after.ReservedUnits != before.ReservedUnits {
				t.Fatal("projection recovery repeated generation or quota effects")
			}
		})
	}
}
