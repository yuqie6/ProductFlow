package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestEvalGraphWriteAuthority(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	task := evalTaskByID(t, tasks, "graph-editing-update-node-config")
	reorder := evalTaskByID(t, tasks, "graph-editing-dissolve-and-reorder")
	ctx := context.Background()

	t.Run("dissolve_group is observable", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, reorder, worlds[reorder.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyEvalGraph(t, as, seeded, before.Revision, "dissolve only", []map[string]any{
			{"op": "dissolve_group", "group_ref": "group-main"},
		}); err != nil {
			t.Fatal(err)
		}
		after, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision <= before.Revision {
			t.Fatalf("revision %d -> %d", before.Revision, after.Revision)
		}
		if groupExists(after.Groups, seeded.GroupIDs["group-main"]) {
			t.Fatal("group still present")
		}
		if !nodeExists(after.Nodes, seeded.NodeIDs["node-prompt-1"]) {
			t.Fatal("dissolved members must remain")
		}
	})

	t.Run("scripted dissolve and reorder", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, reorder, worlds[reorder.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		raw := marshalEvalChangeSet(t, seeded, before.Revision, "解散分组并重排输入", []map[string]any{
			{"op": "dissolve_group", "group_ref": "group-main"},
			{"op": "reorder_edges", "node_ref": "node-prompt-1", "role": "facts", "edge_refs": []string{"edge-second-source-prompt", "edge-source-prompt"}},
		})
		if _, err := as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
			t.Fatal(err)
		}
		pending, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil || pending.PendingProposal == nil {
			t.Fatal(err)
		}
		if _, err := as.svc.Graph.ConfirmProposal(ctx, seeded.ProductID, seeded.GraphID, pending.PendingProposal.ID); err != nil {
			t.Fatal(err)
		}
		after, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if groupExists(after.Groups, seeded.GroupIDs["group-main"]) {
			t.Fatal("group still present")
		}
	})

	t.Run("legal nested prompt design_goal", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyEvalGraph(t, as, seeded, before.Revision, "更新主图提示词", []map[string]any{
			{"op": "update_node_config", "node_ref": "node-prompt-1", "config": map[string]any{
				"image_type_key": "hero",
				"prompt":         map[string]any{"design_goal": "白底棚拍"},
			}},
		}); err != nil {
			t.Fatal(err)
		}
		detail, err := as.svc.GetNodeDetail(ctx, seeded.ConvID, seeded.NodeIDs["node-prompt-1"])
		if err != nil {
			t.Fatal(err)
		}
		config, _ := detail["config"].(map[string]any)
		prompt, _ := config["prompt"].(map[string]any)
		if prompt["design_goal"] != "白底棚拍" {
			t.Fatalf("config %#v", config)
		}
		if _, ok := config["design_goal"]; ok {
			t.Fatal("top-level design_goal must not appear")
		}
	})

	t.Run("wrong-path design_goal is rejected", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		assertRejectedUnchanged(t, as, seeded, []map[string]any{
			{"op": "update_node_config", "node_ref": "node-prompt-1", "config": map[string]any{"design_goal": "白底棚拍"}},
		}, "design_goal")
	})

	t.Run("retired creative_brief design_goals", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		assertRejectedUnchanged(t, as, seeded, []map[string]any{
			{"op": "update_node_config", "node_ref": "node-brief-1", "config": map[string]any{"design_goals": []any{"retired"}}},
		}, "design_goals")
	})

	t.Run("retired generation_spec text fields", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		assertRejectedUnchanged(t, as, seeded, []map[string]any{
			{"op": "update_node_config", "node_ref": "node-image-1", "config": map[string]any{
				"image_type_key":  "hero",
				"generation_spec": map[string]any{"aspect_ratio": "1:1", "text_policy": "none", "text_language": "zh-CN"},
			}},
		}, "generation_spec")
	})

	t.Run("legal generation_spec aspect_ratio", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyEvalGraph(t, as, seeded, before.Revision, "legal spec", []map[string]any{
			{"op": "update_node_config", "node_ref": "node-image-1", "config": map[string]any{
				"image_type_key":  "hero",
				"generation_spec": map[string]any{"aspect_ratio": "3:4"},
			}},
		}); err != nil {
			t.Fatal(err)
		}
		detail, err := as.svc.GetNodeDetail(ctx, seeded.ConvID, seeded.NodeIDs["node-image-1"])
		if err != nil {
			t.Fatal(err)
		}
		spec, _ := detail["config"].(map[string]any)["generation_spec"].(map[string]any)
		if spec["aspect_ratio"] != "3:4" {
			t.Fatalf("%#v", spec)
		}
		if _, ok := spec["text_policy"]; ok {
			t.Fatal("retired text_policy must not persist")
		}
	})

	t.Run("failed multi-op apply is rejected without partial effect", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, reorder, worlds[reorder.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = applyEvalGraph(t, as, seeded, before.Revision, "rejected", []map[string]any{
			{"op": "dissolve_group", "group_ref": "group-main"},
			{"op": "update_node_config", "node_ref": "node-brief-1", "config": map[string]any{"design_goals": []any{"x"}}},
		})
		requireEvalStatus(t, err, 400, "立即写入")
		after, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision != before.Revision || !groupExists(after.Groups, seeded.GroupIDs["group-main"]) {
			t.Fatal("failed changeset mutated the graph")
		}
	})

	t.Run("delete node removes member and edges then second write uses new revision", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyEvalGraph(t, as, seeded, before.Revision, "delete prompt", []map[string]any{
			{"op": "delete_node", "node_ref": "node-prompt-1"},
		}); err != nil {
			t.Fatal(err)
		}
		mid, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if nodeExists(mid.Nodes, seeded.NodeIDs["node-prompt-1"]) {
			t.Fatal("node still present")
		}
		if edgeExists(mid.Edges, seeded.EdgeIDs["edge-source-prompt"]) || edgeExists(mid.Edges, seeded.EdgeIDs["edge-brief-prompt"]) {
			t.Fatal("connected edges still present")
		}
		if _, err := applyEvalGraph(t, as, seeded, mid.Revision, "rename remaining", []map[string]any{
			{"op": "rename_node", "node_ref": "node-prompt-2", "title": "新细节提示词"},
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("propose rejects retired fields", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		raw := marshalEvalChangeSet(t, seeded, before.Revision, "retired", []map[string]any{
			{"op": "update_node_config", "node_ref": "node-brief-1", "config": map[string]any{"design_goals": []any{"x"}}},
		})
		_, err = as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, clockid.New())
		requireEvalStatus(t, err, 409, "design_goals")
		after, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision != before.Revision || after.PendingProposal != nil {
			t.Fatal("rejected propose mutated graph")
		}
	})

	t.Run("failed multi-op proposal has no partial effect", func(t *testing.T) {
		seeded := seedEvalWorld(t, as, reorder, worlds[reorder.World])
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		raw := marshalEvalChangeSet(t, seeded, before.Revision, "rejected", []map[string]any{
			{"op": "dissolve_group", "group_ref": "group-main"},
			{"op": "update_node_config", "node_ref": "node-brief-1", "config": map[string]any{"design_goals": []any{"x"}}},
		})
		_, err = as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, clockid.New())
		requireEvalStatus(t, err, 409, "design_goals")
		after, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision != before.Revision || after.PendingProposal != nil || !groupExists(after.Groups, seeded.GroupIDs["group-main"]) {
			t.Fatal("failed proposal mutated graph")
		}
	})
}

func evalTaskByID(t *testing.T, tasks []EvalTask, id string) EvalTask {
	t.Helper()
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("missing task %s", id)
	return EvalTask{}
}

func applyEvalGraph(t *testing.T, as *agentServer, seeded seededEvalWorld, revision int, summary string, ops []map[string]any) (map[string]any, error) {
	t.Helper()
	return as.svc.ApplyGraphTool(context.Background(), seeded.ConvID, marshalEvalChangeSet(t, seeded, revision, summary, ops), clockid.New())
}

func assertRejectedUnchanged(t *testing.T, as *agentServer, seeded seededEvalWorld, ops []map[string]any, substr string) {
	t.Helper()
	before, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyEvalGraph(t, as, seeded, before.Revision, "rejected", ops)
	requireEvalValidation(t, err, substr)
	after, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision {
		t.Fatalf("revision %d -> %d", before.Revision, after.Revision)
	}
}

func requireEvalValidation(t *testing.T, err error, substr string) {
	t.Helper()
	requireEvalStatus(t, err, 400, substr)
}

func requireEvalStatus(t *testing.T, err error, status int, substr string) {
	t.Helper()
	var e apperr.Error
	if !errors.As(err, &e) || e.Status != status {
		t.Fatalf("got %#v", err)
	}
	if substr != "" && !strings.Contains(e.Detail, substr) {
		t.Fatalf("detail %q want %q", e.Detail, substr)
	}
}

func marshalEvalChangeSet(t *testing.T, seeded seededEvalWorld, revision int, summary string, ops []map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"base_graph_revision": revision, "summary": summary, "operations": ops})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	mapping := map[string]string{
		"22222222-2222-4222-8222-222222222222": seeded.ProductID,
		"33333333-3333-4333-8333-333333333333": seeded.GraphID,
	}
	for _, ids := range []map[string]string{seeded.NodeIDs, seeded.EdgeIDs, seeded.GroupIDs, seeded.AssetIDs, seeded.FolderIDs} {
		for fixture, actual := range ids {
			mapping[fixture] = actual
		}
	}
	for fixture, actual := range mapping {
		if actual != "" {
			text = strings.ReplaceAll(text, strconv.Quote(fixture), strconv.Quote(actual))
		}
	}
	return json.RawMessage(text)
}

func groupExists(groups []graph.GroupView, id string) bool {
	for _, group := range groups {
		if group.ID == id {
			return true
		}
	}
	return false
}

func nodeExists(nodes []graph.NodeView, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func edgeExists(edges []graph.EdgeView, id string) bool {
	for _, edge := range edges {
		if edge.ID == id {
			return true
		}
	}
	return false
}
