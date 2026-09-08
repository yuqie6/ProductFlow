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

func TestEvalPersistedOperationCoverageMatrix(t *testing.T) {
	tasks, _, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 75 {
		t.Fatalf("frozen L1 task count = %d, want 75", len(tasks))
	}
	supportedTools := map[string]bool{
		"apply_graph_change_set_v1":      true,
		"cancel_workflow_run_v1":         true,
		"create_product_workspace_v1":    true,
		"discard_workflow_proposal_v1":   true,
		"finalize_product_intake_v1":     true,
		"propose_global_draft":           true,
		"propose_graph_change_set_v1":    true,
		"request_global_workflow_run_v1": true,
		"request_workflow_run_v1":        true,
	}
	supportedGraphOps := evalObservedGraphOperations
	supportedLibraryOps := map[string]bool{
		"archive": true, "link_workflow": true, "move": true,
		"rename": true, "restore": true, "set_tags": true,
	}
	type row struct {
		TaskID       string `json:"task_id"`
		WriteIndex   int    `json:"write_index"`
		Tool         string `json:"tool"`
		Path         string `json:"path"`
		Operation    string `json:"operation,omitempty"`
		Expected     any    `json:"expected,omitempty"`
		Observer     string `json:"observer"`
		ActualGoTest string `json:"actual_go_test"`
		ActualCase   string `json:"actual_case,omitempty"`
		Evidence     string `json:"evidence_status"`
	}
	graphEvidence := map[string]struct {
		caseName string
		status   string
	}{
		"graph-editing-delete-one-node":                        {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-delete-one-node", "positive-write-readback"},
		"graph-editing-disconnect-edge":                        {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-disconnect-edge", "positive-write-readback"},
		"graph-editing-move-node-positions":                    {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-move-node-positions", "positive-write-readback"},
		"graph-editing-move-node-to-group":                     {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-move-node-to-group", "positive-write-readback"},
		"graph-editing-rename-group":                           {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-rename-group", "positive-write-readback"},
		"graph-editing-rename-node":                            {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-rename-node", "positive-write-readback"},
		"graph-editing-update-node-config":                     {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-update-node-config", "positive-write-readback"},
		"graph-editing-discard-pending-proposal":               {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-discard-pending-proposal", "discard-final-state-readback"},
		"graph-editing-dissolve-and-reorder":                   {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-dissolve-and-reorder", "pending-and-confirmed-write-readback"},
		"graph-editing-negative-batch-direct-apply":            {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-negative-batch-direct-apply", "pending-write-readback"},
		"graph-editing-negative-delete-all-nodes":              {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-negative-delete-all-nodes", "pending-write-readback"},
		"graph-editing-propose-scene-shot":                     {"TestEvalPersistedOperationL1GraphCoverage/graph-editing-propose-scene-shot", "pending-and-confirmed-write-readback"},
		"workflow-run-request-negative-off-topic-delete-graph": {"TestEvalPersistedOperationL1GraphCoverage/workflow-run-request-negative-off-topic-delete-graph", "pending-write-readback"},
	}
	libraryEvidence := map[string]struct {
		caseName string
		status   string
	}{
		"media-library-organization-archive-asset":            {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-archive-asset", "pending-and-confirmed-write-readback"},
		"media-library-organization-batch-rename":             {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-batch-rename", "pending-and-confirmed-write-readback"},
		"media-library-organization-inspect-and-rename-asset": {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-inspect-and-rename-asset", "pending-and-confirmed-write-readback"},
		"media-library-organization-inspect-before-archive":   {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-inspect-before-archive", "pending-and-confirmed-write-readback"},
		"media-library-organization-link-workflow":            {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-link-workflow", "pending-and-confirmed-write-readback"},
		"media-library-organization-move-selected-asset":      {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-move-selected-asset", "pending-and-confirmed-write-readback"},
		"media-library-organization-move-to-root":             {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-move-to-root", "pending-and-confirmed-write-readback"},
		"media-library-organization-rename-listed-asset":      {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-rename-listed-asset", "pending-and-confirmed-write-readback"},
		"media-library-organization-restore-asset":            {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-restore-asset", "pending-and-confirmed-write-readback"},
		"media-library-organization-set-tags":                 {"TestEvalPersistedLibraryOperationCoverage/media-library-organization-set-tags", "pending-and-confirmed-write-readback"},
	}
	nonGraphEvidence := map[string]struct {
		caseName string
		status   string
	}{
		"workflow-run-request-run-current-workflow":    {"TestEvalPersistedNonGraphOperationCoverage/request_workflow_run", "positive-write-readback"},
		"workflow-run-request-global-node-run":         {"TestEvalPersistedNonGraphOperationCoverage/request_global_workflow_run", "positive-write-readback"},
		"product-intake-finalize-explicit-minimal-set": {"TestEvalPersistedNonGraphOperationCoverage/finalize_product_intake", "positive-write-readback"},
		"product-intake-create-named-workspace":        {"TestEvalPersistedNonGraphOperationCoverage/create_product_workspace", "positive-write-readback"},
		"workflow-run-request-cancel-running-run":      {"TestEvalPersistedNonGraphOperationCoverage/cancel_workflow_run", "positive-write-readback"},
	}
	for _, referenceCase := range evalPersistedReferenceCases {
		nonGraphEvidence[referenceCase.taskID] = struct {
			caseName string
			status   string
		}{"TestEvalPersistedNonGraphOperationCoverage/" + referenceCase.taskID, referenceCase.status}
	}
	rows := []row{}
	for _, task := range tasks {
		for writeIndex, write := range task.Expect.Writes {
			if !supportedTools[write.Tool] {
				t.Fatalf("%s write %d uses unsupported observer tool %s", task.ID, writeIndex, write.Tool)
			}
			keys := make([]string, 0, len(write.Match))
			for path := range write.Match {
				keys = append(keys, path)
			}
			sort.Strings(keys)
			if len(keys) == 0 {
				keys = []string{""}
			}
			for _, path := range keys {
				operation := ""
				operationPath := false
				if parts := evalOperationPath.FindStringSubmatch(path); parts != nil {
					if (parts[1] == "operations" && parts[3] == "op") ||
						(parts[1] == "library_payload.operations" && parts[3] == "operation") {
						if value, ok := write.Match[path].(string); ok {
							operation, operationPath = value, true
						}
					}
				} else if strings.HasSuffix(path, ".operation") {
					if value, ok := write.Match[path].(string); ok {
						operation, operationPath = value, true
					}
				}
				if write.Tool == "apply_graph_change_set_v1" || write.Tool == "propose_graph_change_set_v1" {
					if operationPath && !supportedGraphOps[operation] {
						t.Fatalf("%s write %d path %s operation %q is not observed", task.ID, writeIndex, path, operation)
					}
				}
				if write.Tool == "propose_global_draft" && operationPath && !supportedLibraryOps[operation] {
					t.Fatalf("%s write %d library operation %q is not observed", task.ID, writeIndex, operation)
				}
				actualTest := ""
				actualCase := ""
				evidenceStatus := "observer-support-only"
				if write.Tool == "apply_graph_change_set_v1" || write.Tool == "propose_graph_change_set_v1" || write.Tool == "discard_workflow_proposal_v1" {
					actualTest = "TestEvalPersistedOperationL1GraphCoverage"
					if evidence, ok := graphEvidence[task.ID]; ok {
						actualCase, evidenceStatus = evidence.caseName, evidence.status
					}
				} else if write.Tool == "propose_global_draft" {
					actualTest = "TestEvalPersistedLibraryOperationCoverage"
					if evidence, ok := libraryEvidence[task.ID]; ok {
						actualCase, evidenceStatus = evidence.caseName, evidence.status
					}
				} else if evidence, ok := nonGraphEvidence[task.ID]; ok {
					actualTest, actualCase, evidenceStatus = "TestEvalPersistedNonGraphOperationCoverage", evidence.caseName, evidence.status
				}
				if (write.Tool == "apply_graph_change_set_v1" || write.Tool == "propose_graph_change_set_v1" || write.Tool == "discard_workflow_proposal_v1" || write.Tool == "propose_global_draft" || write.Tool == "request_workflow_run_v1" || write.Tool == "request_global_workflow_run_v1" || write.Tool == "finalize_product_intake_v1") && actualCase == "" {
					t.Fatalf("%s write %d is missing a real operation evidence case", task.ID, writeIndex)
				}
				rows = append(rows, row{TaskID: task.ID, WriteIndex: writeIndex, Tool: write.Tool, Path: path, Operation: operation, Expected: write.Match[path], Observer: "database-final-state", ActualGoTest: actualTest, ActualCase: actualCase, Evidence: evidenceStatus})
			}
		}
	}
	if len(rows) != 188 {
		t.Fatalf("frozen persisted operation/path rows = %d, want 188", len(rows))
	}
	result := map[string]any{"schema_version": 1, "layer": "l1", "task_count": len(tasks), "row_count": len(rows), "rows": rows}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if evidenceRoot := os.Getenv("PRODUCTFLOW_EVAL_PERSISTED_EVIDENCE_DIR"); evidenceRoot != "" {
		if err := os.MkdirAll(evidenceRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evidenceRoot, "coverage-matrix.json"), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("persisted operation coverage tasks=%d rows=%d", len(tasks), len(rows))
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
