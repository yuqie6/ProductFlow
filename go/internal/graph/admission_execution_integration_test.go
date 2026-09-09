package graph_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
)

func TestRiverGraphAdmissionUsesRealRuns(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	fixture := auth.SeedDualMerchants(t, gs.db, gs.client, gs.srv.URL)
	mustSeedMerchantQuota(t, gs.db, fixture.MerchantAID, 10000)
	mustSeedMerchantQuota(t, gs.db, fixture.MerchantBID, 10000)
	if err := gs.db.Exec("UPDATE app_settings SET value='1' WHERE key=?", generation.MaxConcurrentSettingKey).Error; err != nil {
		t.Fatal(err)
	}
	original := gs.cookies
	gs.cookies = fixture.CookiesA
	a, ga := gs.createDirectGraphWithImageTypes(t, `[{"key":"hero","quantity":1},{"key":"detail","quantity":1},{"key":"scene","quantity":1}]`)
	ra := submitAdmissionGraphRun(t, gs, a, ga)
	gs.cookies = fixture.CookiesB
	b, gb := gs.createDirectGraphWithImageTypes(t, `[{"key":"hero","quantity":1}]`)
	rb := submitAdmissionGraphRun(t, gs, b, gb)
	gs.cookies = original
	ja, jb := loadGraphJob(t, gs, ra.ID), loadGraphJob(t, gs, rb.ID)
	if ja.Args.MerchantID != fixture.MerchantAID || jb.Args.MerchantID != fixture.MerchantBID {
		t.Fatal("job merchant snapshot drift")
	}
	provider := &projectionImageProvider{}
	executor := graph.Executor{DB: gs.db, Products: product.GraphGuard{}, Deps: graph.Dependencies{Prompt: graph.MockPromptProvider{}, Image: provider, Assets: product.Service{DB: gs.db, Media: gs.media}}}
	worker, err := queue.NewClient(gs.pool, map[string]queue.ActorFunc{queue.ActorGraphRun: executor.ExecuteRun}, queue.WorkerConfig{GenerationWorkers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	bProgressedBeforeAFinished := false
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		var countB int64
		if err := gs.db.Table("workflow_graph_node_runs").Where("graph_run_id=? AND attempt_count>0", rb.ID).Count(&countB).Error; err != nil {
			t.Fatal(err)
		}
		var stateA string
		if err := gs.db.Table("workflow_graph_runs").Select("status").Where("id=?", ra.ID).Scan(&stateA).Error; err != nil {
			t.Fatal(err)
		}
		if countB > 0 && stateA != "succeeded" {
			bProgressedBeforeAFinished = true
		}
		if loadGraphJob(t, gs, ra.ID).State == rivertype.JobStateCompleted && loadGraphJob(t, gs, rb.ID).State == rivertype.JobStateCompleted {
			if !bProgressedBeforeAFinished {
				t.Fatal("B waited behind the entire A graph")
			}
			for _, id := range []string{ra.ID, rb.ID} {
				var bad int64
				if err := gs.db.Table("workflow_graph_node_runs").Where("graph_run_id=? AND (status<>'succeeded' OR attempt_count<>1)", id).Count(&bad).Error; err != nil {
					t.Fatal(err)
				}
				if bad != 0 {
					t.Fatalf("run %s unfinished/repeated nodes=%d", id, bad)
				}
			}
			if provider.calls.Load() != 4 {
				t.Fatalf("provider calls=%d want4", provider.calls.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("graph jobs did not finish: A=%s B=%s", loadGraphJob(t, gs, ra.ID).State, loadGraphJob(t, gs, rb.ID).State)
}

func submitAdmissionGraphRun(t *testing.T, gs *graphServer, productID, graphID string) graph.GraphRunResponse {
	t.Helper()
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": graph.RunScopeGraph})
	gs.mustStatus(t, resp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	return run
}
func loadGraphJob(t *testing.T, gs *graphServer, runID string) *river.Job[queue.TaskArgs] {
	t.Helper()
	var row struct {
		ID    int64
		State rivertype.JobState
		Args  []byte
	}
	if err := gs.db.Table("river_job").Select("id,state,args").Where("args ->> 'actor'=? AND args ->> 'aggregate_id'=?", queue.ActorGraphRun, runID).Order("id DESC").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	var args queue.TaskArgs
	if err := json.Unmarshal(row.Args, &args); err != nil {
		t.Fatal(err)
	}
	return &river.Job[queue.TaskArgs]{JobRow: &rivertype.JobRow{ID: row.ID, State: row.State}, Args: args}
}
