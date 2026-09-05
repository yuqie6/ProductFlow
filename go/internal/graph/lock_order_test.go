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

func skippableConcurrentErr(err error) bool {
	return err == nil || errors.Is(err, queue.ErrBusy) || errors.Is(err, queue.ErrLater) || isConflict(err)
}

func TestConcurrentAdoptCancelMutateDoesNotDeadlock(t *testing.T) {
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
	results := make(chan outcome, 4)
	var wg sync.WaitGroup
	wg.Add(4)
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
		time.Sleep(55 * time.Millisecond)
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
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		view, err := svc.Get(ctx, productID, graphID)
		if err != nil {
			results <- outcome{op: "mutate", err: err}
			return
		}
		var imageNode graph.NodeView
		for _, node := range view.Nodes {
			if node.NodeType == graph.NodeImageGeneration {
				imageNode = node
				break
			}
		}
		if imageNode.ID == "" {
			results <- outcome{op: "mutate", err: errors.New("missing image_generation node")}
			return
		}
		_, err = svc.ApplyChangeSet(ctx, productID, graphID, graph.ChangeSet{
			BaseGraphRevision: view.Revision,
			Summary:           "并发抢 graph 锁",
			Operations: []graph.Operation{
				graph.RenameNodeOp{NodeRef: imageNode.ID, Title: imageNode.Title + "-lock"},
			},
		})
		if isConflict(err) {
			err = nil
		}
		results <- outcome{op: "mutate", err: err}
	}()
	wg.Wait()
	close(results)
	for item := range results {
		if postgresDeadlock(item.err) {
			t.Fatalf("%s deadlocked: %v", item.op, item.err)
		}
		if !skippableConcurrentErr(item.err) {
			t.Fatalf("%s: %v", item.op, item.err)
		}
	}

	got, err := svc.GetRun(ctx, productID, graphID, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status == graph.RunStatusRunning {
		if drainErr := executor.ExecuteRun(ctx, run.ID); !skippableConcurrentErr(drainErr) {
			t.Fatalf("drain execute: %v", drainErr)
		}
		got, err = svc.GetRun(ctx, productID, graphID, run.ID)
		if err != nil {
			t.Fatalf("get run after drain: %v", err)
		}
	}

	view := loadProjection(t, gs, productID, graphID)
	contentTypes := []graph.NodeType{graph.NodeCreativeBrief, graph.NodeVisualSystem, graph.NodeImagePrompt}
	generated := 0
	for _, nodeType := range contentTypes {
		node := nodeOfType(t, view, nodeType)
		if node.DocumentOrigin == nil {
			t.Fatalf("%s missing document_origin", nodeType)
		}
		origin := *node.DocumentOrigin
		if origin != graph.OriginSeed && origin != graph.OriginGenerated {
			t.Fatalf("%s origin %q, want seed or generated", nodeType, origin)
		}
		if origin == graph.OriginGenerated {
			generated++
			if node.ConfigStatus != graph.ConfigReady {
				t.Fatalf("generated %s must remain ready, got %s", nodeType, node.ConfigStatus)
			}
		}
	}
	if got.Status == graph.RunStatusSucceeded {
		if generated != len(contentTypes) {
			t.Fatalf("succeeded run must adopt all content nodes, generated=%d status=%s", generated, got.Status)
		}
	}
}
