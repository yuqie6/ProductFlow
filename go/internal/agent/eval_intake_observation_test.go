package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
)

// This exercises the real intake service through the same seeded world used by
// the eval host. It deliberately includes inputs absent from intake-results.json.
func TestEvalIntakeObservationContract(t *testing.T) {
	as := newEvalHostServer(t)
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	task := evalIntakeTaskByID(t, tasks, "product-intake-finalize-explicit-minimal-set")
	world := worlds[task.World]

	t.Run("accepts trimmed image type and persists follow-up context", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, world)
		ctx := evalMerchantContext(t, as)
		beforeGraph := evalGraphProjection(t, as, ctx, seeded)
		selection := json.RawMessage(`{"schema_version":1,"image_types":[{"key":" hero ","quantity":1,"order":0},{"key":"detail","quantity":1,"order":1}]}`)
		assetID := seeded.AssetIDs["11111111-1111-4111-8111-111111111111"]
		if assetID == "" {
			t.Fatal("seeded reference asset missing")
		}
		key := "eval-intake-valid"

		result, err := as.svc.FinalizeProductIntake(ctx, seeded.ConvID, key, selection, []string{assetID})
		if err != nil {
			t.Fatal(err)
		}
		if result["accepted"] != true || result["intake_finalized"] != true || result["graph_expanded"] != true {
			t.Fatalf("unexpected intake result: %#v", result)
		}
		if revision, ok := result["revision"].(int); !ok || revision <= beforeGraph.Revision {
			t.Fatalf("revision did not advance: %#v before=%d", result["revision"], beforeGraph.Revision)
		}
		intake := evalJSONMap(t, result["intake"])
		imageTypes, ok := intake["image_types"].([]any)
		if !ok || len(imageTypes) != 2 {
			t.Fatalf("normalized intake image_types: %#v", intake["image_types"])
		}
		first, _ := imageTypes[0].(map[string]any)
		if first["key"] != "hero" {
			t.Fatalf("trimmed key was not persisted: %#v", first)
		}

		afterContext := evalProductContext(t, as, ctx, seeded.ConvID)
		afterGraph := evalGraphProjection(t, as, ctx, seeded)
		if !reflect.DeepEqual(evalJSONMap(t, afterContext["intake"]), intake) {
			t.Fatalf("follow-up context intake does not match write: %#v", afterContext["intake"])
		}
		if afterContext["birth_expandable"] != false || len(afterGraph.Nodes) <= len(beforeGraph.Nodes) {
			t.Fatalf("follow-up context does not expose expanded graph: birth=%v nodes=%d before=%d", afterContext["birth_expandable"], len(afterGraph.Nodes), len(beforeGraph.Nodes))
		}

		replayed, err := as.svc.FinalizeProductIntake(ctx, seeded.ConvID, key, selection, []string{assetID})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(evalJSONMap(t, result), evalJSONMap(t, replayed)) {
			t.Fatalf("same-key replay changed result: first=%#v replay=%#v", result, replayed)
		}
		replayedGraph := evalGraphProjection(t, as, ctx, seeded)
		if replayedGraph.Revision != afterGraph.Revision || !reflect.DeepEqual(afterGraph, replayedGraph) {
			t.Fatalf("same-key replay changed graph: first=%#v replay=%#v", afterGraph, replayedGraph)
		}

		differentSelection := json.RawMessage(`{"schema_version":1,"image_types":[{"key":"hero","quantity":2,"order":0},{"key":"detail","quantity":1,"order":1}]}`)
		_, err = as.svc.FinalizeProductIntake(ctx, seeded.ConvID, key, differentSelection, []string{assetID})
		requireEvalStatus(t, err, 409, "同一工具 idempotency key")
		conflictGraph := evalGraphProjection(t, as, ctx, seeded)
		if !reflect.DeepEqual(afterGraph, conflictGraph) {
			t.Fatalf("same-key conflict changed graph: before=%#v after=%#v", afterGraph, conflictGraph)
		}
	})

	invalidCases := []struct {
		name      string
		selection json.RawMessage
		assetIDs  func(seededEvalWorld) []string
	}{
		{
			name:      "transcript wrong order",
			selection: json.RawMessage(`{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0},{"key":"detail","quantity":1,"order":3}]}`),
			assetIDs: func(seeded seededEvalWorld) []string {
				return []string{seeded.AssetIDs["11111111-1111-4111-8111-111111111111"]}
			},
		},
		{
			name:      "unknown delivery preset",
			selection: json.RawMessage(`{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}],"delivery_preset_key":"recommended_set"}`),
			assetIDs: func(seeded seededEvalWorld) []string {
				return []string{seeded.AssetIDs["11111111-1111-4111-8111-111111111111"]}
			},
		},
		{
			name:      "missing reference",
			selection: json.RawMessage(`{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`),
			assetIDs:  func(seededEvalWorld) []string { return []string{} },
		},
		{
			name:      "unknown reference",
			selection: json.RawMessage(`{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`),
			assetIDs:  func(seededEvalWorld) []string { return []string{"99999999-9999-4999-8999-999999999999"} },
		},
	}
	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			seeded := seedEvalWorld(t, as, task, world)
			ctx := evalMerchantContext(t, as)
			beforeContext := evalProductContext(t, as, ctx, seeded.ConvID)
			beforeGraph := evalGraphProjection(t, as, ctx, seeded)
			_, err := as.svc.FinalizeProductIntake(ctx, seeded.ConvID, "eval-intake-invalid-"+testCase.name, testCase.selection, testCase.assetIDs(seeded))
			requireEvalStatus(t, err, 400, "")
			afterContext := evalProductContext(t, as, ctx, seeded.ConvID)
			afterGraph := evalGraphProjection(t, as, ctx, seeded)
			if !reflect.DeepEqual(beforeContext, afterContext) {
				t.Fatalf("rejected intake changed context: before=%#v after=%#v", beforeContext, afterContext)
			}
			if !reflect.DeepEqual(beforeGraph, afterGraph) {
				t.Fatalf("rejected intake changed graph: before=%#v after=%#v", beforeGraph, afterGraph)
			}
		})
	}

	t.Run("empty idempotency key is rejected without fallback", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, world)
		ctx := evalMerchantContext(t, as)
		beforeContext := evalProductContext(t, as, ctx, seeded.ConvID)
		beforeGraph := evalGraphProjection(t, as, ctx, seeded)
		selection := json.RawMessage(`{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`)
		assetID := seeded.AssetIDs["11111111-1111-4111-8111-111111111111"]
		_, err := as.svc.FinalizeProductIntake(ctx, seeded.ConvID, "", selection, []string{assetID})
		requireEvalStatus(t, err, 400, "")
		if !reflect.DeepEqual(beforeContext, evalProductContext(t, as, ctx, seeded.ConvID)) || !reflect.DeepEqual(beforeGraph, evalGraphProjection(t, as, ctx, seeded)) {
			t.Fatal("empty key rejection changed persisted state")
		}
	})
}

func evalIntakeTaskByID(t *testing.T, tasks []EvalTask, id string) EvalTask {
	t.Helper()
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("missing eval task %s", id)
	return EvalTask{}
}

func evalMerchantContext(t *testing.T, as *agentServer) context.Context {
	t.Helper()
	return auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
}

func evalProductContext(t *testing.T, as *agentServer, ctx context.Context, conversationID string) map[string]any {
	t.Helper()
	value, err := as.svc.ProductContext(ctx, conversationID, "detailed")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func evalGraphProjection(t *testing.T, as *agentServer, ctx context.Context, seeded seededEvalWorld) graph.Projection {
	t.Helper()
	value, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func evalJSONMap(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
