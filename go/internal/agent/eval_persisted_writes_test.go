package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func gradeEvalPersistedWrites(t *testing.T, as *agentServer, seeded seededEvalWorld, expectations []EvalWriteExpect) []string {
	t.Helper()
	var failures []string
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	for _, expected := range expectations {
		var params any
		switch expected.Tool {
		case "apply_graph_change_set_v1":
			live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
			if err != nil {
				t.Fatal(err)
			}
			ref, _ := expected.Match["operations[0].node_ref"].(string)
			var found *graph.NodeView
			for i := range live.Nodes {
				if live.Nodes[i].ID == seeded.NodeIDs[ref] {
					found = &live.Nodes[i]
				}
			}
			if found != nil {
				params = map[string]any{"operations": []any{map[string]any{"op": expected.Match["operations[0].op"], "node_ref": ref, "title": found.Title, "config": found.Config}}}
			}
			if !evalOnlyDeclaredNodeChanges(seeded.InitialGraph, live, seeded.NodeIDs[ref], fmt.Sprint(expected.Match["operations[0].op"])) {
				failures = append(failures, "unexpected persisted graph effect")
			}
		case "propose_graph_change_set_v1":
			var raw []byte
			err := as.pool.QueryRow(ctx, `SELECT change_set_json FROM workflow_graph_proposals WHERE graph_id=$1 AND status='pending' ORDER BY created_at DESC LIMIT 1`, seeded.GraphID).Scan(&raw)
			if err == nil {
				if err := json.Unmarshal(raw, &params); err != nil {
					t.Fatal(err)
				}
			}
		case "propose_global_draft":
			draft, err := as.svc.Library.GetOrganizationDraft(ctx, seeded.ConvID)
			if err == nil && draft.CurrentRevision != nil {
				var payload any
				if err := json.Unmarshal(draft.CurrentRevision.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				params = map[string]any{"draft_kind": "library_organization", "library_payload": payload}
			}
		case "request_workflow_run_v1", "request_global_workflow_run_v1":
			var productID *string
			if expected.Tool == "request_workflow_run_v1" {
				productID = &seeded.ProductID
			}
			request, err := as.svc.GetWorkflowRunRequest(ctx, productID, seeded.ConvID, nil)
			if err == nil && request != nil {
				action := ""
				if request.DocumentAction != nil {
					action = *request.DocumentAction
				}
				params = map[string]any{"scope": request.RunScope, "node_id": request.TargetNodeID, "node_ids": request.TargetNodeIDs,
					"product_id": request.ProductID, "workflow_id": request.WorkflowID, "source_run_id": request.SourceRunID,
					"force": request.Force, "document_action": action}
			}
		case "finalize_product_intake_v1":
			var raw []byte
			if err := as.pool.QueryRow(ctx, `SELECT intake_json FROM products WHERE id=$1`, seeded.ProductID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var intake map[string]any
			if err := json.Unmarshal(raw, &intake); err != nil {
				t.Fatal(err)
			}
			params = map[string]any{"selection": intake, "reference_asset_ids": intake["reference_asset_ids"]}
		case "create_product_workspace_v1":
			if seeded.CreatedProductID == "" {
				failures = append(failures, "created workspace identity was not observed")
				continue
			}
			var name string
			if err := as.pool.QueryRow(ctx, `SELECT name FROM products WHERE id=$1`, seeded.CreatedProductID).Scan(&name); err == nil {
				params = map[string]any{"name": name}
			} else {
				failures = append(failures, "created workspace product was not persisted")
				continue
			}
		case "cancel_workflow_run_v1":
			if seeded.RecentRunID == "" {
				failures = append(failures, "recent run identity was not seeded")
				continue
			}
			var status string
			if err := as.pool.QueryRow(ctx, `SELECT status FROM workflow_graph_runs WHERE id=$1`, seeded.RecentRunID).Scan(&status); err != nil {
				failures = append(failures, "recent run was not persisted")
				continue
			}
			if status != "cancelled" {
				failures = append(failures, "recent run was not cancelled")
			}
			params = map[string]any{"run_id": seeded.RecentRunID}
		default:
			failures = append(failures, "unobservable persisted write: "+expected.Tool)
			continue
		}
		params = normalizeEvalIdentity(t, params, seeded)
		if !evalMatchesPaths(params, expected.Match) {
			failures = append(failures, "persisted content mismatch: "+expected.Tool)
		}
	}
	return failures
}

func evalOnlyDeclaredNodeChanges(before, after graph.Projection, target, operation string) bool {
	if before.ID == "" || len(before.Nodes) != len(after.Nodes) || !reflect.DeepEqual(before.Edges, after.Edges) || !reflect.DeepEqual(before.Groups, after.Groups) {
		return false
	}
	for _, old := range before.Nodes {
		var current *graph.NodeView
		for i := range after.Nodes {
			if after.Nodes[i].ID == old.ID {
				current = &after.Nodes[i]
				break
			}
		}
		if current == nil {
			return false
		}
		if old.ID == target {
			if operation == "rename_node" {
				old.Title = current.Title
			}
			if operation == "update_node_config" {
				old.Config = current.Config
			}
		}
		if old.Title != current.Title || !reflect.DeepEqual(old.Config, current.Config) || !reflect.DeepEqual(old.GroupID, current.GroupID) || old.PositionX != current.PositionX || old.PositionY != current.PositionY || !reflect.DeepEqual(old.BoundAssetID, current.BoundAssetID) {
			return false
		}
	}
	return true
}

func normalizeEvalIdentity(t *testing.T, value any, seeded seededEvalWorld) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	mapping := evalActualToFixtureIDs(seeded)
	for actual, fixture := range mapping {
		if actual != "" {
			text = strings.ReplaceAll(text, strconv.Quote(actual), strconv.Quote(fixture))
		}
	}
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Approval completion must be checked against resulting business content, not only draft status.
func gradeEvalFinalWrites(t *testing.T, as *agentServer, seeded seededEvalWorld, expectations []EvalWriteExpect, before graph.Projection) []string {
	errors := []string{}
	ctx := context.Background()
	for _, expected := range expectations {
		switch expected.Tool {
		case "propose_graph_change_set_v1":
			after, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
			if err != nil {
				t.Fatal(err)
			}
			ops := []map[string]any{}
			oldNodes, oldEdges, oldGroups := map[string]graph.NodeView{}, map[string]bool{}, map[string]bool{}
			for _, node := range before.Nodes {
				oldNodes[node.ID] = node
			}
			for _, edge := range before.Edges {
				oldEdges[edge.ID] = true
			}
			for _, group := range before.Groups {
				oldGroups[group.ID] = true
			}
			for _, group := range after.Groups {
				if !oldGroups[group.ID] {
					ops = append(ops, map[string]any{"op": "create_group", "client_ref": group.ID, "title": group.Title})
				}
			}
			for _, node := range after.Nodes {
				old, exists := oldNodes[node.ID]
				if !exists {
					ops = append(ops, map[string]any{"op": "create_node", "client_ref": node.ID, "node_type": node.NodeType, "group_ref": node.GroupID, "title": node.Title, "config": node.Config})
				} else {
					if old.Title != node.Title {
						ops = append(ops, map[string]any{"op": "rename_node", "node_ref": node.ID, "title": node.Title})
					}
					if !reflect.DeepEqual(old.Config, node.Config) {
						ops = append(ops, map[string]any{"op": "update_node_config", "node_ref": node.ID, "config": node.Config})
					}
					delete(oldNodes, node.ID)
				}
			}
			for id := range oldNodes {
				ops = append(ops, map[string]any{"op": "delete_node", "node_ref": id})
			}
			for _, edge := range after.Edges {
				if !oldEdges[edge.ID] {
					ops = append(ops, map[string]any{"op": "connect_nodes", "source_ref": edge.SourceNodeID, "target_ref": edge.TargetNodeID})
				}
			}
			if after.PendingProposal != nil || !evalMatchesPaths(normalizeEvalIdentity(t, map[string]any{"operations": ops}, seeded), expected.Match) {
				errors = append(errors, "final graph content mismatch")
			}
		case "propose_global_draft":
			draft, err := as.svc.Library.GetOrganizationDraft(ctx, seeded.ConvID)
			if err != nil || draft.Status != "confirmed" {
				errors = append(errors, "library draft not confirmed")
				continue
			}
			var payload struct {
				Operations []struct {
					Operation string         `json:"operation"`
					AssetID   string         `json:"asset_id"`
					Target    map[string]any `json:"target"`
				} `json:"operations"`
			}
			if err := json.Unmarshal(draft.CurrentRevision.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			for _, operation := range payload.Operations {
				asset, err := as.svc.Library.Get(ctx, operation.AssetID)
				if err != nil {
					errors = append(errors, "confirmed asset missing")
					continue
				}
				switch operation.Operation {
				case "rename":
					if asset.DisplayName != operation.Target["display_name"] {
						errors = append(errors, "confirmed asset name mismatch")
					}
				default:
					errors = append(errors, "unobserved final library operation: "+operation.Operation)
				}
			}
			errors = append(errors, gradeEvalPersistedWrites(t, as, seeded, []EvalWriteExpect{expected})...)
		default:
			errors = append(errors, gradeEvalPersistedWrites(t, as, seeded, []EvalWriteExpect{expected})...)
		}
	}
	return errors
}

var evalOperationPath = regexp.MustCompile(`^(operations|library_payload\.operations)\[([0-9]+)\]\.(.+)$`)
var evalPathPart = regexp.MustCompile(`[^.\[\]]+`)

func evalValueAt(value any, path string) any {
	for _, key := range evalPathPart.FindAllString(path, -1) {
		switch item := value.(type) {
		case map[string]any:
			value = item[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(item) {
				return nil
			}
			value = item[i]
		default:
			return nil
		}
	}
	return value
}

func evalMatchesPaths(value any, match map[string]any) bool {
	groups := map[string]map[string]map[string]any{}
	for path, expected := range match {
		if parts := evalOperationPath.FindStringSubmatch(path); parts != nil {
			if groups[parts[1]] == nil {
				groups[parts[1]] = map[string]map[string]any{}
			}
			if groups[parts[1]][parts[2]] == nil {
				groups[parts[1]][parts[2]] = map[string]any{}
			}
			groups[parts[1]][parts[2]][parts[3]] = expected
		} else if !evalEqual(path, evalValueAt(value, path), expected) {
			return false
		}
	}
	for path, patterns := range groups {
		actual, ok := evalValueAt(value, path).([]any)
		if !ok || len(actual) != len(patterns) {
			return false
		}
		constraints := make([]map[string]any, 0, len(patterns))
		for _, pattern := range patterns {
			constraints = append(constraints, pattern)
		}
		var assign func(int, []any, map[string]string) bool
		assign = func(index int, remaining []any, bindings map[string]string) bool {
			if index == len(constraints) {
				return true
			}
			for i, item := range remaining {
				next := map[string]string{}
				for key, value := range bindings {
					next[key] = value
				}
				matched := true
				for key, expected := range constraints[index] {
					actual := evalValueAt(item, key)
					if variable, ok := expected.(string); ok && strings.HasPrefix(variable, "$") {
						text, ok := actual.(string)
						if !ok || text == "" {
							matched = false
							break
						}
						if bound, exists := next[variable]; exists {
							if bound != text {
								matched = false
								break
							}
						} else {
							for _, bound := range next {
								if bound == text {
									matched = false
								}
							}
							next[variable] = text
						}
					} else if !evalEqual(key, actual, expected) {
						matched = false
						break
					}
				}
				if !matched {
					continue
				}
				rest := append([]any{}, remaining[:i]...)
				rest = append(rest, remaining[i+1:]...)
				if assign(index+1, rest, next) {
					return true
				}
			}
			return false
		}
		if !assign(0, actual, map[string]string{}) {
			return false
		}
	}
	return true
}

func evalEqual(path string, actual, expected any) bool {
	encode := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if strings.HasSuffix(path, "node_ids") || strings.HasSuffix(path, "reference_asset_ids") || strings.HasSuffix(path, "tag_names") {
		a, aok := actual.([]any)
		b, bok := expected.([]any)
		if aok && bok {
			aa, bb := []string{}, []string{}
			for _, v := range a {
				aa = append(aa, encode(v))
			}
			for _, v := range b {
				bb = append(bb, encode(v))
			}
			sort.Strings(aa)
			sort.Strings(bb)
			return reflect.DeepEqual(aa, bb)
		}
	}
	return encode(actual) == encode(expected)
}

func TestEvalPersistedConfigRejectsWrongTargetAndValue(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l2")
	if err != nil {
		t.Fatal(err)
	}
	var task EvalTask
	for _, candidate := range tasks {
		if candidate.ID == "graph-editing-update-node-config" {
			task = candidate
		}
	}
	for i, test := range []struct {
		ref, goal, imageType string
		extraDelete, pass    bool
	}{{"node-prompt-2", "白底棚拍", "hero", false, false}, {"node-prompt-1", "错误目标", "hero", false, false}, {"node-prompt-1", "白底棚拍", "detail", false, false}, {"node-prompt-1", "白底棚拍", "hero", true, false}, {"node-prompt-1", "白底棚拍", "hero", false, true}} {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"base_graph_revision": live.Revision, "summary": "eval config", "operations": []any{map[string]any{"op": "update_node_config", "node_ref": seeded.NodeIDs[test.ref], "config": map[string]any{"image_type_key": test.imageType, "prompt": map[string]any{"design_goal": test.goal}}}}})
		if _, err := as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
			t.Fatal(err)
		}
		if test.extraDelete {
			raw, _ := json.Marshal(map[string]any{"base_graph_revision": live.Revision + 1, "summary": "extra delete", "operations": []any{map[string]any{"op": "delete_node", "node_ref": seeded.NodeIDs["node-image-2"]}}})
			if _, err := as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
				t.Fatal(err)
			}
		}
		errors := gradeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
		if (len(errors) == 0) != test.pass {
			t.Fatal(fmt.Sprintf("case %d: %v", i, errors))
		}
	}
}

func TestEvalPersistedProposalAndLibraryContent(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l2")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"graph-editing-propose-scene-shot", "media-library-organization-rename-listed-asset"} {
		var task EvalTask
		for _, candidate := range tasks {
			if candidate.ID == id {
				task = candidate
			}
		}
		if task.ID == "" {
			t.Fatal("missing task", id)
		}
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		for _, valid := range []bool{false, true} {
			var params map[string]any
			for _, call := range task.Reference.ScriptedCalls {
				if call.Name == task.Expect.Writes[0].Tool {
					if err := json.Unmarshal(call.Params, &params); err != nil {
						t.Fatal(err)
					}
				}
			}
			if task.Skill == "graph-editing" {
				live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
				if err != nil {
					t.Fatal(err)
				}
				params["base_graph_revision"] = live.Revision
				ops := params["operations"].([]any)
				if !valid {
					ops[1].(map[string]any)["config"].(map[string]any)["image_type_key"] = "detail"
				}
				for _, operation := range ops {
					op := operation.(map[string]any)
					for _, key := range []string{"source_ref", "target_ref"} {
						ref, _ := op[key].(string)
						if actual := seeded.NodeIDs[ref]; actual != "" {
							op[key] = actual
						}
					}
				}
				raw, _ := json.Marshal(params)
				if _, err := as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
					t.Fatal(err)
				}
			} else {
				payload := params["library_payload"].(map[string]any)
				op := payload["operations"].([]any)[0].(map[string]any)
				op["asset_id"] = seeded.AssetIDs[op["asset_id"].(string)]
				if !valid {
					op["target"].(map[string]any)["display_name"] = "错误名称"
				}
				raw, _ := json.Marshal(payload)
				if _, err := as.svc.Library.AppendOrganizationDraftRevision(ctx, seeded.ConvID, raw, "", ""); err != nil {
					t.Fatal(err)
				}
			}
			failures := gradeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
			if (len(failures) == 0) != valid {
				t.Fatalf("%s valid=%t errors=%v", id, valid, failures)
			}
			if task.Skill == "graph-editing" {
				if _, err := as.svc.DiscardProposalTool(ctx, seeded.ConvID, "", clockid.New()); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
