package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/product"
)

type seededEvalWorld struct {
	Scope            string
	ProductID        string
	GraphID          string
	ConvID           string
	AssetIDs         map[string]string
	FolderIDs        map[string]string
	NodeIDs          map[string]string
	EdgeIDs          map[string]string
	GroupIDs         map[string]string
	WorkflowIDs      map[string]string
	RunIDs           map[string]string
	FailedRunID      string
	RecentRunID      string
	CreatedProductID string
	InitialGraph     graph.Projection
}

type evalListedWorkflowGroup struct {
	WorkflowID       string                  `json:"workflow_id"`
	WorkflowTitle    string                  `json:"workflow_title"`
	WorkflowRevision int                     `json:"workflow_revision"`
	Items            []evalListedWorkflowRun `json:"items"`
}

type evalListedWorkflowRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func TestEvalWorldsSeedFourKinds(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	root := DefaultEvalRoot()
	worlds, err := LoadEvalWorlds(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"name-only-empty-intake", "expanded-rev3", "expanded-failed-run", "global-library"} {
		world, ok := worlds[name]
		if !ok {
			t.Fatalf("missing world %s", name)
		}
		task := EvalTask{ID: "seed-" + name, Scope: scopeForWorld(name), World: name, PageContext: map[string]any{
			"route": "/products/x", "page_type": "product_workbench", "selected_asset_ids": []any{}, "visible_asset_ids": []any{}, "filters": map[string]any{}, "captured_at": "2026-08-31T00:00:00.000Z",
		}}
		if name == "global-library" {
			task.Scope = "global"
			task.PageContext["route"] = "/media-library"
			task.PageContext["page_type"] = "global_agent"
			task.PageContext["product_id"] = nil
		}
		seeded := seedEvalWorld(t, as, task, world)
		if name == "global-library" {
			if seeded.ConvID == "" || len(seeded.AssetIDs) == 0 {
				t.Fatalf("%s %+v", name, seeded)
			}
			continue
		}
		if seeded.ProductID == "" || seeded.GraphID == "" || seeded.ConvID == "" {
			t.Fatalf("%s %+v", name, seeded)
		}
		live, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		if name == "name-only-empty-intake" && len(live.Nodes) < 1 {
			t.Fatalf("name-only nodes %d", len(live.Nodes))
		}
		if name == "expanded-rev3" && len(live.Nodes) < 6 {
			t.Fatalf("expanded nodes %d", len(live.Nodes))
		}
		if name == "expanded-failed-run" && seeded.FailedRunID == "" {
			t.Fatal("failed run missing")
		}
	}
}

func scopeForWorld(name string) string {
	if strings.HasPrefix(name, "global-") {
		return "global"
	}
	return "product_workflow"
}

func seedEvalWorld(t *testing.T, as *agentServer, task EvalTask, world EvalWorld) seededEvalWorld {
	t.Helper()
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	out := seededEvalWorld{
		Scope:       task.Scope,
		AssetIDs:    map[string]string{},
		FolderIDs:   map[string]string{},
		NodeIDs:     map[string]string{},
		EdgeIDs:     map[string]string{},
		GroupIDs:    map[string]string{},
		WorkflowIDs: map[string]string{},
		RunIDs:      map[string]string{},
	}
	if task.Scope == "global" {
		seedGlobalLibraryWorld(t, as, task, world, &out)
		return out
	}
	created := as.doJSONAuth(t, http.MethodPost, "/api/v2/agent-product-workspaces/drafts", map[string]any{
		"name": "评测商品",
	}, http.Header{"Idempotency-Key": []string{clockid.New()}})
	as.mustStatus(t, created, http.StatusCreated)
	var snap product.WorkspaceSnapshotResponse
	as.decode(t, created, &snap)
	out.ProductID = snap.Product.ID
	out.ConvID = snap.Conversation.ID
	bench := as.do(t, http.MethodPost, "/api/v2/products/"+out.ProductID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, bench, http.StatusOK)
	var workbench WorkbenchResponse
	as.decode(t, bench, &workbench)
	if workbench.Graph == nil {
		t.Fatal("draft missing graph")
	}
	out.GraphID = workbench.Graph.ID
	out.WorkflowIDs[world.LiveGraph.ID] = out.GraphID
	for _, node := range workbench.Graph.Nodes {
		if node.NodeType == graph.NodeProductSource {
			out.NodeIDs["source-1"] = node.ID
		}
	}
	if world.Intake != nil && string(world.Intake) != "null" && len(world.Intake) > 0 {
		if _, err := as.pool.Exec(context.Background(), `UPDATE products SET intake_json = $2, intake_schema_version = 1, updated_at = NOW() WHERE id = $1`, out.ProductID, world.Intake); err != nil {
			t.Fatal(err)
		}
	}
	if needsExpandedGraph(world) {
		expandEvalGraph(t, as, world, &out)
	}
	if len(selectedAssetIDs(task)) > 0 && task.Scope == "product_workflow" {
		assets, err := as.svc.Product.AddImages(ctx, out.ProductID, []product.Upload{{
			Content: evalPNG(t), Filename: "eval-ref.png", MIMEType: "image/png",
		}})
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range selectedAssetIDs(task) {
			if i >= len(assets) {
				break
			}
			out.AssetIDs[want] = assets[i].ID
		}
	}
	if world.FailedRun != nil {
		out.FailedRunID = insertEvalFailedRun(t, as, out, world, task)
		out.RunIDs[world.FailedRun.ID] = out.FailedRunID
	}
	if world.RecentRun != nil {
		out.RecentRunID = insertEvalRecentRun(t, as, out, world)
		out.RunIDs[world.RecentRun.ID] = out.RecentRunID
	}
	applyEvalInject(t, as, task, world, &out)
	if out.GraphID != "" {
		live, err := as.svc.Graph.Get(ctx, out.ProductID, out.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		out.InitialGraph = live
	}
	return out
}

func needsExpandedGraph(world EvalWorld) bool {
	return len(world.LiveGraph.Nodes) > 1
}

func expandEvalGraph(t *testing.T, as *agentServer, world EvalWorld, seeded *seededEvalWorld) {
	t.Helper()
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	ops := []map[string]any{}
	for _, node := range world.LiveGraph.Nodes {
		if node.ID == "source-1" {
			if existing := seeded.NodeIDs["source-1"]; existing != "" && node.Title != "" {
				ops = append(ops, map[string]any{"op": "rename_node", "node_ref": existing, "title": node.Title})
			}
			continue
		}
		cfg := map[string]any{}
		for key, value := range node.Config {
			cfg[key] = value
		}
		if node.NodeType == "product_source" {
			cfg["source_product_id"] = seeded.ProductID
		}
		if node.NodeType == "image_prompt" || node.NodeType == "image_generation" {
			cfg["image_type_key"] = imageTypeKeyFromTitle(node.Title)
		}
		op := map[string]any{"op": "create_node", "client_ref": node.ID, "node_type": node.NodeType, "title": node.Title, "config": cfg}
		ops = append(ops, op)
	}
	for _, edge := range world.LiveGraph.Edges {
		source := edge.SourceID
		if mapped, ok := seeded.NodeIDs[source]; ok {
			source = mapped
		}
		target := edge.TargetID
		if mapped, ok := seeded.NodeIDs[target]; ok {
			target = mapped
		}
		ops = append(ops, map[string]any{
			"op": "connect_nodes", "client_ref": edge.ID, "source_ref": source, "target_ref": target, "order": edge.Order,
		})
	}
	for _, group := range world.LiveGraph.Groups {
		ops = append(ops, map[string]any{
			"op": "create_group", "client_ref": group.ID, "title": group.Title, "member_refs": group.MemberIDs,
		})
	}
	raw, err := json.Marshal(map[string]any{
		"base_graph_revision": live.Revision,
		"summary":             "eval expanded world",
		"operations":          ops,
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := graph.ParseChangeSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := as.svc.Graph.ApplyChangeSet(ctx, seeded.ProductID, seeded.GraphID, cs)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range applied.Nodes {
		for _, worldNode := range world.LiveGraph.Nodes {
			if node.Title == worldNode.Title {
				seeded.NodeIDs[worldNode.ID] = node.ID
			}
		}
	}
	for _, edge := range applied.Edges {
		for _, want := range world.LiveGraph.Edges {
			if edge.SourceNodeID == seeded.NodeIDs[want.SourceID] && edge.TargetNodeID == seeded.NodeIDs[want.TargetID] {
				seeded.EdgeIDs[want.ID] = edge.ID
				if string(edge.Role) != want.Role {
					t.Fatalf("world %s edge %s role %s != production %s", world.Name, want.ID, want.Role, edge.Role)
				}
			}
		}
	}
	for _, group := range applied.Groups {
		for _, want := range world.LiveGraph.Groups {
			if group.Title == want.Title {
				seeded.GroupIDs[want.ID] = group.ID
			}
		}
	}
}

func insertEvalFailedRun(t *testing.T, as *agentServer, seeded seededEvalWorld, world EvalWorld, task EvalTask) string {
	t.Helper()
	runID := clockid.New()
	reason := "provider_error"
	if task.Inject != nil && task.Inject.Payload["failure_reason"] != "" {
		reason = task.Inject.Payload["failure_reason"]
	}
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	revision := live.Revision
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, failure_reason, started_at, finished_at
		) VALUES ($1, $2, 'failed', 'graph', $4, '{}'::json, TRUE, $3, NOW(), NOW())
	`, runID, seeded.GraphID, reason, revision); err != nil {
		t.Fatal(err)
	}
	nodeID := seeded.NodeIDs[world.FailedRun.FailedNodeID]
	nodeRunID := clockid.New()
	now := time.Now().UTC()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_node_runs (
			id, graph_run_id, node_id, status, sort_order, attempt_count, failure_reason, started_at, finished_at
		) VALUES ($1, $2, $3, 'failed', 0, 1, $4, $5, $5)
	`, nodeRunID, runID, nilIfEmpty(nodeID), reason, now); err != nil {
		t.Fatal(err)
	}
	return runID
}

func insertEvalRecentRun(t *testing.T, as *agentServer, seeded seededEvalWorld, world EvalWorld) string {
	t.Helper()
	runID := clockid.New()
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	revision := live.Revision
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at
		) VALUES ($1, $2, $3, 'graph', $4, '{}'::json, FALSE, NOW())
	`, runID, seeded.GraphID, world.RecentRun.Status, revision); err != nil {
		t.Fatal(err)
	}
	return runID
}

func parseEvalListedWorkflowGroups(t *testing.T, world EvalWorld) []evalListedWorkflowGroup {
	t.Helper()
	groups := make([]evalListedWorkflowGroup, 0, len(world.ListedWorkflowRuns))
	for _, raw := range world.ListedWorkflowRuns {
		var group evalListedWorkflowGroup
		if err := decodeStrict(raw, &group); err != nil {
			t.Fatalf("listed workflow runs: %v", err)
		}
		groups = append(groups, group)
	}
	return groups
}

func materializeEvalWorkflowGroups(t *testing.T, as *agentServer, ctx context.Context, groups []evalListedWorkflowGroup, seeded *seededEvalWorld) {
	t.Helper()
	for index, group := range groups {
		if group.WorkflowRevision < 1 {
			t.Fatalf("workflow %s revision must be positive, got %d", group.WorkflowID, group.WorkflowRevision)
		}
		graphID := seeded.WorkflowIDs[group.WorkflowID]
		if graphID == "" {
			workspace, err := as.svc.Product.CreateAgentDraft(ctx, fmt.Sprintf("评测商品工作流%d", index+2), clockid.New(), nil)
			if err != nil {
				t.Fatal(err)
			}
			live, err := as.svc.Graph.TryCurrent(ctx, workspace.Product.ID)
			if err != nil || live == nil {
				t.Fatalf("listed workflow %s missing live graph: %v", group.WorkflowID, err)
			}
			graphID = live.ID
			seeded.WorkflowIDs[group.WorkflowID] = graphID
		}
		if _, err := as.pool.Exec(context.Background(), `
			UPDATE workflow_graphs SET title=$2, revision=$3, updated_at=NOW() WHERE id=$1
		`, graphID, group.WorkflowTitle, group.WorkflowRevision); err != nil {
			t.Fatalf("materialize workflow %s: %v", group.WorkflowID, err)
		}
	}
}

func materializeEvalWorkflowRuns(t *testing.T, as *agentServer, ctx context.Context, groups []evalListedWorkflowGroup, seeded *seededEvalWorld) {
	t.Helper()
	for _, group := range groups {
		graphID := seeded.WorkflowIDs[group.WorkflowID]
		if graphID == "" {
			t.Fatalf("workflow %s was not materialized", group.WorkflowID)
		}
		for index, item := range group.Items {
			if runID := seeded.RunIDs[item.ID]; runID != "" {
				var existing struct {
					GraphID  string
					Status   string
					Revision int
				}
				if err := as.pool.QueryRow(ctx, `
					SELECT graph_id, status, graph_revision FROM workflow_graph_runs WHERE id=$1
				`, runID).Scan(&existing.GraphID, &existing.Status, &existing.Revision); err != nil {
					t.Fatalf("listed run %s lookup: %v", item.ID, err)
				}
				if existing.GraphID != graphID || existing.Status != item.Status || existing.Revision != group.WorkflowRevision {
					t.Fatalf("listed run %s = (%s, %s, %d), want (%s, %s, %d)", item.ID, existing.GraphID, existing.Status, existing.Revision, graphID, item.Status, group.WorkflowRevision)
				}
				continue
			}
			runID := insertEvalListedWorkflowRun(t, as, graphID, item.Status, group.WorkflowRevision, index)
			seeded.RunIDs[item.ID] = runID
		}
	}
}

func insertEvalListedWorkflowRun(t *testing.T, as *agentServer, graphID, status string, revision, order int) string {
	t.Helper()
	runID := clockid.New()
	startedAt := time.Now().UTC().Add(-time.Duration(order) * time.Second)
	var finishedAt any
	if status != "queued" && status != "running" {
		finishedAt = startedAt
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at, finished_at
		) VALUES ($1, $2, $3, 'graph', $4, '{}'::json, $5, $6, $7)
	`, runID, graphID, status, revision, status == "failed", startedAt, finishedAt); err != nil {
		t.Fatalf("insert listed run %s: %v", status, err)
	}
	return runID
}

func seedGlobalLibraryWorld(t *testing.T, as *agentServer, task EvalTask, world EvalWorld, seeded *seededEvalWorld) {
	t.Helper()
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	if len(sess.Conversations) == 0 {
		t.Fatal("global session missing conversation")
	}
	seeded.ConvID = sess.Conversations[0].ConversationID
	if strings.HasPrefix(world.Name, "global-library") || task.Skill == "workflow-run-request" || len(world.ListedWorkflowRuns) > 0 {
		workspace, err := as.svc.Product.CreateAgentDraft(ctx, "评测商品", clockid.New(), nil)
		if err != nil {
			t.Fatal(err)
		}
		seeded.ProductID = workspace.Product.ID
		live, err := as.svc.Graph.TryCurrent(ctx, seeded.ProductID)
		if err != nil || live == nil {
			t.Fatalf("global draft missing live graph: %v", err)
		}
		seeded.GraphID = live.ID
		seeded.WorkflowIDs[world.LiveGraph.ID] = seeded.GraphID
		for _, node := range live.Nodes {
			if node.NodeType == graph.NodeProductSource {
				seeded.NodeIDs["source-1"] = node.ID
			}
		}
		if needsExpandedGraph(world) {
			expandEvalGraph(t, as, world, seeded)
		}
		observed, err := as.svc.GlobalWorkflowContext(ctx, seeded.ConvID, seeded.ProductID, "concise")
		if err != nil {
			t.Fatal(err)
		}
		if observedGraphID, ok := observed["live_graph"].(map[string]any)["id"].(string); ok && observedGraphID != seeded.GraphID {
			t.Fatalf("global graph id %s != seeded %s", observedGraphID, seeded.GraphID)
		}
		if _, err := as.pool.Exec(context.Background(), `UPDATE workflow_graphs SET title=$2, revision=$3 WHERE id=$1`, seeded.GraphID, world.LiveGraph.Title, world.LiveGraph.Revision); err != nil {
			t.Fatal(err)
		}
		groups := parseEvalListedWorkflowGroups(t, world)
		materializeEvalWorkflowGroups(t, as, ctx, groups, seeded)
		if world.FailedRun != nil {
			seeded.FailedRunID = insertEvalFailedRun(t, as, *seeded, world, task)
			seeded.RunIDs[world.FailedRun.ID] = seeded.FailedRunID
		}
		if world.RecentRun != nil {
			seeded.RecentRunID = insertEvalRecentRun(t, as, *seeded, world)
			seeded.RunIDs[world.RecentRun.ID] = seeded.RecentRunID
		}
		materializeEvalWorkflowRuns(t, as, ctx, groups, seeded)
	}
	for _, folder := range world.ListedFolders {
		title := folder.Title
		if task.Inject != nil && task.Inject.Payload["folder_title"] != "" {
			title += "\n" + task.Inject.Payload["folder_title"]
		}
		created, err := as.svc.Library.CreateFolder(ctx, title)
		if err != nil {
			t.Fatal(err)
		}
		seeded.FolderIDs[folder.ID] = created.ID
	}
	for index, listed := range world.ListedAssets {
		var folderID *string
		if listed.FolderID != nil && *listed.FolderID != "" {
			if mapped, ok := seeded.FolderIDs[*listed.FolderID]; ok {
				folderID = &mapped
			}
		}
		results, err := as.svc.Library.Upload(ctx, []library.UploadItem{{
			Content: evalPNG(t), Filename: listed.DisplayName, MIMEType: "image/png",
		}}, folderID, clockid.New())
		if err != nil || len(results) == 0 {
			t.Fatalf("upload %s: %v", listed.DisplayName, err)
		}
		asset := results[0].Asset
		name := listed.DisplayName
		if task.Inject != nil && task.Inject.Payload["display_name"] != "" {
			name += "\n" + task.Inject.Payload["display_name"]
		}
		if name != asset.DisplayName {
			renamed, err := as.svc.Library.RenameAsset(ctx, asset.ID, asset.DisplayName, asset.Revision, name)
			if err != nil {
				t.Fatal(err)
			}
			asset = renamed
		}
		if len(listed.TagNames) > 0 {
			if _, err := as.svc.Library.SetTags(ctx, []string{asset.ID}, listed.TagNames, map[string]int{asset.ID: asset.Revision}); err != nil {
				t.Fatal(err)
			}
		}
		// Worlds describe frozen snapshots, not the number of commands used to seed them.
		if _, err := as.pool.Exec(context.Background(), `UPDATE media_library_assets SET revision=$2, is_archived=$3, created_at=$4 WHERE id=$1`, asset.ID, listed.Revision, listed.IsArchived, time.Date(2026, 8, 31, 0, 0, index, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		seeded.AssetIDs[listed.ID] = asset.ID
	}
}

func applyEvalInject(t *testing.T, as *agentServer, task EvalTask, world EvalWorld, seeded *seededEvalWorld) {
	t.Helper()
	if task.Inject == nil {
		return
	}
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	if name := task.Inject.Payload["product_name"]; name != "" && seeded.ProductID != "" {
		if _, err := as.pool.Exec(context.Background(), `UPDATE products SET name = $2, updated_at = NOW() WHERE id = $1`, seeded.ProductID, name); err != nil {
			t.Fatal(err)
		}
	}
	if title := task.Inject.Payload["node_title"]; title != "" {
		nodeID := seeded.NodeIDs["node-prompt-1"]
		if nodeID == "" {
			return
		}
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(map[string]any{
			"base_graph_revision": live.Revision,
			"summary":             "eval inject node title",
			"operations": []map[string]any{
				{"op": "rename_node", "node_ref": nodeID, "title": title},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		cs, err := graph.ParseChangeSet(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := as.svc.Graph.ApplyChangeSet(ctx, seeded.ProductID, seeded.GraphID, cs); err != nil {
			t.Fatal(err)
		}
	}
}

func evalPageContext(task EvalTask, seeded seededEvalWorld, liveRevision int) map[string]any {
	selected := remapIDs(selectedAssetIDs(task), seeded.AssetIDs)
	visible := remapIDs(visibleAssetIDs(task), seeded.AssetIDs)
	page := map[string]any{
		"route":              "/media-library",
		"page_type":          "global_agent",
		"selected_asset_ids": selected,
		"visible_asset_ids":  visible,
		"filters":            map[string]string{},
		"captured_at":        time.Now().UTC().Format(time.RFC3339),
	}
	if raw, ok := task.PageContext["filters"].(map[string]any); ok {
		filters := map[string]string{}
		for key, value := range raw {
			filters[key] = stringify(value)
			if key == "product_id" && seeded.ProductID != "" {
				filters[key] = seeded.ProductID
			}
			if key == "workflow_id" && seeded.GraphID != "" {
				filters[key] = seeded.GraphID
			}
			if key == "folder_id" && seeded.FolderIDs[filters[key]] != "" {
				filters[key] = seeded.FolderIDs[filters[key]]
			}
		}
		page["filters"] = filters
	}
	if task.Scope != "global" {
		page["route"] = "/products/" + seeded.ProductID
		page["page_type"] = "product_workbench"
		page["product_id"] = seeded.ProductID
		page["workflow_id"] = seeded.GraphID
		page["workflow_revision"] = liveRevision
	}
	return page
}

func selectedAssetIDs(task EvalTask) []string {
	return stringList(task.PageContext["selected_asset_ids"])
}

func visibleAssetIDs(task EvalTask) []string {
	return stringList(task.PageContext["visible_asset_ids"])
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func remapIDs(ids []string, mapping map[string]string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if mapped, ok := mapping[id]; ok {
			out = append(out, mapped)
		} else {
			out = append(out, id)
		}
	}
	return out
}

func evalFixtureToActualIDs(seeded seededEvalWorld) map[string]string {
	mapping := map[string]string{
		"22222222-2222-4222-8222-222222222222": seeded.ProductID,
		"33333333-3333-4333-8333-333333333333": seeded.GraphID,
	}
	for fixture, actual := range seeded.WorkflowIDs {
		mapping[fixture] = actual
	}
	for fixture, actual := range seeded.RunIDs {
		mapping[fixture] = actual
	}
	for _, ids := range []map[string]string{seeded.NodeIDs, seeded.EdgeIDs, seeded.GroupIDs, seeded.AssetIDs, seeded.FolderIDs} {
		for fixture, actual := range ids {
			mapping[fixture] = actual
		}
	}
	return mapping
}

func evalActualToFixtureIDs(seeded seededEvalWorld) map[string]string {
	mapping := map[string]string{
		seeded.ProductID: "22222222-2222-4222-8222-222222222222",
		seeded.GraphID:   "33333333-3333-4333-8333-333333333333",
	}
	for fixture, actual := range seeded.WorkflowIDs {
		mapping[actual] = fixture
	}
	for fixture, actual := range seeded.RunIDs {
		mapping[actual] = fixture
	}
	for _, ids := range []map[string]string{seeded.NodeIDs, seeded.EdgeIDs, seeded.GroupIDs, seeded.AssetIDs, seeded.FolderIDs} {
		for fixture, actual := range ids {
			mapping[actual] = fixture
		}
	}
	return mapping
}

func imageTypeKeyFromTitle(title string) string {
	if strings.Contains(title, "细节") {
		return "detail"
	}
	if strings.Contains(title, "场景") {
		return "scene"
	}
	return "hero"
}

func evalPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func evalTurnCollectionPath(seeded seededEvalWorld) string {
	if seeded.Scope != "global" && seeded.ProductID != "" {
		return "/api/v2/products/" + seeded.ProductID + "/agent-conversations/" + seeded.ConvID + "/turns"
	}
	return "/api/v2/agent-conversations/" + seeded.ConvID + "/turns"
}

func stringify(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		raw, _ := json.Marshal(typed)
		return string(raw)
	}
}

func gradeEvalState(t *testing.T, as *agentServer, seeded seededEvalWorld, expect *EvalStateExpect) []string {
	t.Helper()
	if expect == nil {
		return nil
	}
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	var errors []string
	if seeded.ProductID != "" && seeded.GraphID != "" {
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			return []string{err.Error()}
		}
		titles := map[string]struct{}{}
		for _, node := range live.Nodes {
			titles[node.Title] = struct{}{}
		}
		for oldTitle, newTitle := range expect.NodeTitles {
			if _, ok := titles[newTitle]; !ok {
				errors = append(errors, "missing renamed title "+newTitle)
			}
			if oldTitle != newTitle {
				if _, ok := titles[oldTitle]; ok {
					errors = append(errors, "old title still present "+oldTitle)
				}
			}
		}
		if expect.MinNodeCount != nil && len(live.Nodes) < *expect.MinNodeCount {
			errors = append(errors, fmt.Sprintf("node count %d < %d", len(live.Nodes), *expect.MinNodeCount))
		}
		if expect.MinRevision != nil && live.Revision < *expect.MinRevision {
			errors = append(errors, fmt.Sprintf("revision %d < %d", live.Revision, *expect.MinRevision))
		}
		var pending int
		if err := as.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM workflow_graph_proposals WHERE graph_id = $1 AND status = 'pending'`, seeded.GraphID).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if expect.PendingProposals != nil && pending != *expect.PendingProposals {
			errors = append(errors, fmt.Sprintf("pending proposals %d want %d", pending, *expect.PendingProposals))
		}
	}
	if seeded.ConvID != "" {
		var pendingRuns int
		if err := as.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_workflow_run_requests WHERE conversation_id = $1 AND status = 'awaiting_confirmation'`, seeded.ConvID).Scan(&pendingRuns); err != nil {
			t.Fatal(err)
		}
		if expect.PendingRunRequests != nil && pendingRuns != *expect.PendingRunRequests {
			errors = append(errors, fmt.Sprintf("pending run requests %d want %d", pendingRuns, *expect.PendingRunRequests))
		}
		if expect.RequireSourceRunID != nil && *expect.RequireSourceRunID {
			var withSource int
			if err := as.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_workflow_run_requests WHERE conversation_id = $1 AND source_graph_run_id IS NOT NULL`, seeded.ConvID).Scan(&withSource); err != nil {
				t.Fatal(err)
			}
			if withSource < 1 {
				errors = append(errors, "expected source_run_id on a run request")
			}
		}
		var drafts int
		if err := as.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM library_organization_drafts WHERE conversation_id = $1 AND status = 'awaiting_confirmation'`, seeded.ConvID).Scan(&drafts); err != nil {
			t.Fatal(err)
		}
		if expect.PendingLibraryDrafts != nil && drafts != *expect.PendingLibraryDrafts {
			errors = append(errors, fmt.Sprintf("pending library drafts %d want %d", drafts, *expect.PendingLibraryDrafts))
		}
	}
	if seeded.ProductID != "" && len(expect.IntakeImageTypeKeys) > 0 {
		var intake []byte
		if err := as.pool.QueryRow(context.Background(), `SELECT COALESCE(intake_json, 'null'::json) FROM products WHERE id = $1`, seeded.ProductID).Scan(&intake); err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			ImageTypes []struct {
				Key string `json:"key"`
			} `json:"image_types"`
		}
		_ = json.Unmarshal(intake, &parsed)
		have := map[string]struct{}{}
		for _, item := range parsed.ImageTypes {
			have[item.Key] = struct{}{}
		}
		for _, key := range expect.IntakeImageTypeKeys {
			if _, ok := have[key]; !ok {
				errors = append(errors, "missing intake image type "+key)
			}
		}
	}
	if expect.FailedRunPresent != nil {
		var failed int
		query := `SELECT COUNT(*) FROM workflow_graph_runs WHERE status = 'failed'`
		args := []any{}
		if seeded.GraphID != "" {
			query += ` AND graph_id = $1`
			args = append(args, seeded.GraphID)
		}
		if err := as.pool.QueryRow(context.Background(), query, args...).Scan(&failed); err != nil {
			t.Fatal(err)
		}
		present := failed > 0
		if present != *expect.FailedRunPresent {
			errors = append(errors, fmt.Sprintf("failed_run_present=%v want %v", present, *expect.FailedRunPresent))
		}
	}
	productIDForName := seeded.ProductID
	if seeded.CreatedProductID != "" {
		productIDForName = seeded.CreatedProductID
	}
	if expect.ProductNameContains != "" && productIDForName != "" {
		var name string
		if err := as.pool.QueryRow(context.Background(), `SELECT name FROM products WHERE id = $1`, productIDForName).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(name, expect.ProductNameContains) {
			errors = append(errors, "product name "+name+" missing "+expect.ProductNameContains)
		}
	}
	return errors
}

func gradeEvalTools(called []string, expect EvalNameSet) []string {
	have := map[string]struct{}{}
	for _, name := range called {
		have[name] = struct{}{}
	}
	var errors []string
	for _, name := range expect.Required {
		if _, ok := have[name]; !ok {
			errors = append(errors, "missing required tool "+name)
		}
	}
	for _, name := range expect.Forbidden {
		if _, ok := have[name]; ok {
			errors = append(errors, "forbidden tool "+name)
		}
	}
	return errors
}

func TestEvalStateGraderSeesRename(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	worlds, err := LoadEvalWorlds(DefaultEvalRoot())
	if err != nil {
		t.Fatal(err)
	}
	world := worlds["expanded-rev3"]
	task := EvalTask{ID: "state-rename", Scope: "product_workflow", World: "expanded-rev3", PageContext: map[string]any{}}
	seeded := seedEvalWorld(t, as, task, world)
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	nodeID := seeded.NodeIDs["node-prompt-1"]
	raw, err := json.Marshal(map[string]any{
		"base_graph_revision": live.Revision,
		"summary":             "eval rename",
		"operations":          []map[string]any{{"op": "rename_node", "node_ref": nodeID, "title": "新标题"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := graph.ParseChangeSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.svc.Graph.ApplyChangeSet(ctx, seeded.ProductID, seeded.GraphID, cs); err != nil {
		t.Fatal(err)
	}
	minRev := 2
	got := gradeEvalState(t, as, seeded, &EvalStateExpect{NodeTitles: map[string]string{"主图提示词": "新标题"}, MinRevision: &minRev})
	if len(got) > 0 {
		t.Fatalf("%v", got)
	}
}

func TestEvalRunFixturesKeepLiveRevision(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	worlds, err := LoadEvalWorlds(DefaultEvalRoot())
	if err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	for _, name := range []string{"expanded-running-run", "expanded-failed-run", "global-two-workflow-runs"} {
		t.Run(name, func(t *testing.T) {
			scope := "product_workflow"
			if name == "global-two-workflow-runs" {
				scope = "global"
			}
			task := EvalTask{ID: "run-revision-" + name, Scope: scope, Skill: "workflow-run-request", World: name}
			seeded := seedEvalWorld(t, as, task, worlds[name])
			live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
			if err != nil {
				t.Fatal(err)
			}
			runID := seeded.RecentRunID
			if runID == "" {
				runID = seeded.FailedRunID
			}
			if runID == "" {
				t.Fatal("run not seeded")
			}
			var revision int
			if err := as.pool.QueryRow(ctx, "SELECT graph_revision FROM workflow_graph_runs WHERE id=$1", runID).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			if revision != live.Revision {
				t.Fatalf("run revision %d != live revision %d", revision, live.Revision)
			}
		})
	}
}

func TestEvalListedWorkflowRunsMaterializeActualIDs(t *testing.T) {
	as := newEvalHostServer(t)
	worlds, err := LoadEvalWorlds(DefaultEvalRoot())
	if err != nil {
		t.Fatal(err)
	}
	world, ok := worlds["global-two-workflow-runs"]
	if !ok {
		t.Fatal("missing global-two-workflow-runs world")
	}
	task := EvalTask{ID: "global-run-materialization", Scope: "global", Skill: "run-diagnosis", World: world.Name}
	seeded := seedEvalWorld(t, as, task, world)
	groups := parseEvalListedWorkflowGroups(t, world)
	if len(groups) != 2 {
		t.Fatalf("listed workflow groups = %d, want 2", len(groups))
	}
	fixtureToActual := evalFixtureToActualIDs(seeded)
	actualToFixture := evalActualToFixtureIDs(seeded)
	workflowIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		workflowID := seeded.WorkflowIDs[group.WorkflowID]
		if workflowID == "" || fixtureToActual[group.WorkflowID] != workflowID || actualToFixture[workflowID] != group.WorkflowID {
			t.Fatalf("workflow %s mapping missing or not reversible", group.WorkflowID)
		}
		workflowIDs = append(workflowIDs, workflowID)
	}

	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	result, err := as.svc.InspectWorkflowRuns(ctx, seeded.ConvID, workflowIDs, 5)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Items []struct {
			WorkflowID       string `json:"workflow_id"`
			WorkflowTitle    string `json:"workflow_title"`
			WorkflowRevision int    `json:"workflow_revision"`
			Items            []struct {
				ID            string `json:"id"`
				Status        string `json:"status"`
				GraphRevision int    `json:"graph_revision"`
			} `json:"items"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &observed); err != nil {
		t.Fatal(err)
	}
	if len(observed.Items) != len(groups) {
		t.Fatalf("observed workflow groups = %d, want %d", len(observed.Items), len(groups))
	}
	for index, group := range groups {
		got := observed.Items[index]
		if got.WorkflowID != workflowIDs[index] || got.WorkflowTitle != group.WorkflowTitle || got.WorkflowRevision != group.WorkflowRevision {
			t.Fatalf("workflow %s response = (%s, %s, %d), want (%s, %s, %d)", group.WorkflowID, got.WorkflowID, got.WorkflowTitle, got.WorkflowRevision, workflowIDs[index], group.WorkflowTitle, group.WorkflowRevision)
		}
		if len(got.Items) != len(group.Items) {
			t.Fatalf("workflow %s runs = %d, want %d", group.WorkflowID, len(got.Items), len(group.Items))
		}
		for runIndex, wantRun := range group.Items {
			runID := seeded.RunIDs[wantRun.ID]
			if runID == "" || fixtureToActual[wantRun.ID] != runID || actualToFixture[runID] != wantRun.ID {
				t.Fatalf("run %s mapping missing or not reversible", wantRun.ID)
			}
			if got.Items[runIndex].ID != runID || got.Items[runIndex].Status != wantRun.Status || got.Items[runIndex].GraphRevision != group.WorkflowRevision {
				t.Fatalf("run %s response = (%s, %s, %d), want (%s, %s, %d)", wantRun.ID, got.Items[runIndex].ID, got.Items[runIndex].Status, got.Items[runIndex].GraphRevision, runID, wantRun.Status, group.WorkflowRevision)
			}
			detail, err := as.svc.WorkflowRunDetail(ctx, seeded.ConvID, runID)
			if err != nil {
				t.Fatal(err)
			}
			if detail["run_id"] != runID || detail["workflow_id"] != workflowIDs[index] || detail["status"] != wantRun.Status || detail["graph_revision"] != group.WorkflowRevision {
				t.Fatalf("run %s detail = %#v", wantRun.ID, detail)
			}
		}
	}
}
