package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// These snapshots come from Go, not a second TypeScript implementation of graph expansion.
func TestEvalObservationFixtures(t *testing.T) {
	root := DefaultEvalRoot()
	fixture := map[string]any{"node_catalog": graph.CatalogJSON(), "node_catalog_index": graph.CatalogIndexJSON(), "image_type_catalog": graph.ImageTypeCatalogJSON()}
	checkEvalFixture(t, filepath.Join(root, "fixtures", "catalog.json"), fixture)
	as := newAgentServer(t, mockGateway{}, "tok")
	tasks, worlds, err := LoadEvalTasks(root, "l1")
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]any{}
	for _, task := range tasks {
		for _, call := range task.Reference.ScriptedCalls {
			if call.Name != "finalize_product_intake_v1" {
				continue
			}
			seeded := seedEvalWorld(t, as, task, worlds[task.World])
			var input struct {
				Selection json.RawMessage `json:"selection"`
				AssetIDs  []string        `json:"reference_asset_ids"`
			}
			if err := json.Unmarshal(call.Params, &input); err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(input.AssetIDs))
			for _, id := range input.AssetIDs {
				ids = append(ids, seeded.AssetIDs[id])
			}
			out, err := as.svc.FinalizeProductIntake(context.Background(), seeded.ConvID, clockid.New(), input.Selection, ids)
			if err != nil {
				t.Fatalf("%s: %v", task.ID, err)
			}
			live, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
			if err != nil {
				t.Fatal(err)
			}
			sort.Slice(live.Nodes, func(i, j int) bool { return live.Nodes[i].Title < live.Nodes[j].Title })
			identities := map[string]string{}
			nodes := make([]map[string]any, 0, len(live.Nodes))
			for i, node := range live.Nodes {
				if _, err := graph.NormalizeNodeConfig(node.NodeType, node.Config); err != nil {
					t.Fatalf("%s: generated %s config violates current catalog: %v", task.ID, node.NodeType, err)
				}
				if node.NodeType == graph.NodeImagePrompt {
					settings, ok := node.Config["text_settings"].(map[string]any)
					imageType, _ := node.Config["image_type_key"].(string)
					want := map[string]any{"policy": "none", "language": nil}
					if graph.ImageTypeFamily(imageType) == "infographic" {
						want = map[string]any{"policy": "required", "language": "zh-CN"}
					}
					if !ok || !reflect.DeepEqual(settings, want) {
						t.Fatalf("%s: %s prompt text defaults = %#v, want %#v", task.ID, imageType, settings, want)
					}
				}
				id := fmt.Sprintf("expanded-%d", i)
				if node.NodeType == "product_source" {
					id = "source-1"
				}
				identities[node.ID] = id
				if _, ok := node.Config["fact_set_version_id"]; ok {
					node.Config["fact_set_version_id"] = "eval-fact-set"
				}
				nodes = append(nodes, map[string]any{"id": id, "node_type": node.NodeType, "title": node.Title, "config": node.Config})
			}
			sort.Slice(live.Edges, func(i, j int) bool {
				a, b := live.Edges[i], live.Edges[j]
				return identities[a.SourceNodeID]+identities[a.TargetNodeID]+string(a.Role) < identities[b.SourceNodeID]+identities[b.TargetNodeID]+string(b.Role)
			})
			edges := []map[string]any{}
			for i, edge := range live.Edges {
				edges = append(edges, map[string]any{"id": fmt.Sprintf("expanded-edge-%d", i), "source_id": identities[edge.SourceNodeID], "target_id": identities[edge.TargetNodeID], "role": edge.Role, "order": edge.Order})
			}
			sort.Slice(live.Groups, func(i, j int) bool { return live.Groups[i].Title < live.Groups[j].Title })
			groups := []map[string]any{}
			for i, group := range live.Groups {
				members := []string{}
				for _, id := range group.MemberIDs {
					members = append(members, identities[id])
				}
				sort.Strings(members)
				groups = append(groups, map[string]any{"id": fmt.Sprintf("expanded-group-%d", i), "title": group.Title, "member_ids": members})
			}
			value := map[string]any{"selection": json.RawMessage(input.Selection), "intake": out["intake"], "node_count": out["node_count"], "group_count": out["group_count"], "birth_expandable": false, "nodes": nodes, "edges": edges, "groups": groups}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			normalized := strings.ReplaceAll(string(raw), seeded.ProductID, "22222222-2222-4222-8222-222222222222")
			for id, actual := range seeded.AssetIDs {
				normalized = strings.ReplaceAll(normalized, actual, id)
			}
			var stable any
			if err := json.Unmarshal([]byte(normalized), &stable); err != nil {
				t.Fatal(err)
			}
			results[task.ID] = stable
		}
	}
	checkEvalFixture(t, filepath.Join(root, "fixtures", "intake-results.json"), results)
}

func checkEvalFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PRODUCTFLOW_UPDATE_EVAL_FIXTURES") == "1" {
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(stored, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("eval observation fixture drift: %s; regenerate with PRODUCTFLOW_UPDATE_EVAL_FIXTURES=1", path)
	}
}

func TestEvalStructuralWriteFixtures(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	results := []map[string]any{}
	for _, task := range tasks {
		if task.ID != "graph-editing-delete-one-node" && task.ID != "graph-editing-disconnect-edge" {
			continue
		}
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		before, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		var params map[string]any
		for _, call := range task.Reference.ScriptedCalls {
			if call.Name == "apply_graph_change_set_v1" {
				if err := json.Unmarshal(call.Params, &params); err != nil {
					t.Fatal(err)
				}
			}
		}
		original, _ := json.Marshal(params["operations"])
		params["base_graph_revision"] = before.Revision
		for _, item := range params["operations"].([]any) {
			op := item.(map[string]any)
			for _, key := range []string{"node_ref", "edge_ref"} {
				ref, _ := op[key].(string)
				for _, ids := range []map[string]string{seeded.NodeIDs, seeded.EdgeIDs} {
					if actual := ids[ref]; actual != "" {
						op[key] = actual
					}
				}
			}
		}
		raw, _ := json.Marshal(params)
		if _, err := as.svc.ApplyGraphTool(context.Background(), seeded.ConvID, raw, clockid.New()); err != nil {
			t.Fatal(err)
		}
		after, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		nodes, edges, groups := []string{}, []string{}, []string{}
		remaining := map[string]bool{}
		for _, node := range after.Nodes {
			remaining[node.ID] = true
		}
		for fixture, actual := range seeded.NodeIDs {
			if !remaining[actual] {
				nodes = append(nodes, fixture)
			}
		}
		remaining = map[string]bool{}
		for _, edge := range after.Edges {
			remaining[edge.ID] = true
		}
		for fixture, actual := range seeded.EdgeIDs {
			if !remaining[actual] {
				edges = append(edges, fixture)
			}
		}
		remaining = map[string]bool{}
		for _, group := range after.Groups {
			remaining[group.ID] = true
		}
		for fixture, actual := range seeded.GroupIDs {
			if !remaining[actual] {
				groups = append(groups, fixture)
			}
		}
		sort.Strings(nodes)
		sort.Strings(edges)
		sort.Strings(groups)
		results = append(results, map[string]any{"world": task.World, "operations": json.RawMessage(original), "removed_node_ids": nodes, "removed_edge_ids": edges, "removed_group_ids": groups})
	}
	checkEvalFixture(t, filepath.Join(DefaultEvalRoot(), "fixtures", "structural-writes.json"), results)
}
