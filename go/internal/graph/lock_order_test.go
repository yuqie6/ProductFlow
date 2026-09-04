package graph_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
)

type delayingPromptProvider struct {
	graph.MockPromptProvider
}

func (p delayingPromptProvider) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	time.Sleep(40 * time.Millisecond)
	return p.MockPromptProvider.GeneratePrompt(ctx, req)
}

type delayingImageProvider struct {
	graph.MockImageProvider
}

func (p delayingImageProvider) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	time.Sleep(40 * time.Millisecond)
	return p.MockImageProvider.GenerateImage(ctx, req)
}

func postgresDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func isConflict(err error) bool {
	var e apperr.Error
	return errors.As(err, &e) && e.Status == 409
}

func TestConcurrentCancelExecuteRecoveryDoesNotDeadlock(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	gs.reclaimRun(t, run.ID)

	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: delayingPromptProvider{},
			Image:  delayingImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
		Products: product.GraphGuard{},
	}
	svc := graph.Service{DB: gs.db, Pool: gs.pool, Products: product.GraphGuard{}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type outcome struct {
		op  string
		err error
	}
	results := make(chan outcome, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		err := executor.ExecuteRun(ctx, run.ID)
		if errors.Is(err, queue.ErrBusy) || errors.Is(err, queue.ErrLater) {
			err = nil
		}
		results <- outcome{op: "execute", err: err}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(15 * time.Millisecond)
		_, err := svc.CancelRun(ctx, productID, graphID, run.ID)
		if isConflict(err) {
			err = nil
		}
		results <- outcome{op: "cancel", err: err}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		if _, err := gs.pool.Exec(ctx, `
			UPDATE workflow_graph_runs
			SET execution_lease_expires_at = NOW() - interval '1 minute'
			WHERE id = $1
		`, run.ID); err != nil {
			results <- outcome{op: "expire-lease", err: err}
			return
		}
		_, err := graph.RecoverUnfinishedGraphRuns(ctx, gs.pool, time.Millisecond, product.GraphGuard{})
		results <- outcome{op: "recovery", err: err}
	}()
	wg.Wait()
	close(results)
	for item := range results {
		if postgresDeadlock(item.err) {
			t.Fatalf("%s deadlocked: %v", item.op, item.err)
		}
	}
}
