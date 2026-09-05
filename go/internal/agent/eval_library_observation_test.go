package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

// Normalize identities structurally; names and other business facts remain untouched.
func normalizeEvalLibraryValue(t *testing.T, value any, seeded seededEvalWorld) any {
	t.Helper()
	ids := map[string]string{seeded.ProductID: "22222222-2222-4222-8222-222222222222", seeded.GraphID: "33333333-3333-4333-8333-333333333333"}
	for _, mappings := range []map[string]string{seeded.AssetIDs, seeded.FolderIDs} {
		for fixture, actual := range mappings {
			ids[actual] = fixture
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(v any) any {
		switch v := v.(type) {
		case string:
			if id, ok := ids[v]; ok && v != "" {
				return id
			}
			return v
		case []any:
			for i := range v {
				v[i] = walk(v[i])
			}
			return v
		case map[string]any:
			out := map[string]any{}
			for key, child := range v {
				if id, ok := ids[key]; ok {
					key = id
				}
				out[key] = walk(child)
			}
			return out
		default:
			return v
		}
	}
	return walk(decoded)
}

func newEvalLibraryServer(t *testing.T) *agentServer {
	t.Helper()
	pool, db := testdb.IsolatedMigrated(t, "eval_library_"+strings.ReplaceAll(clockid.New(), "-", ""))
	return newAgentServerOnDB(t, mockGateway{}, "tok", pool, db)
}

func TestEvalLibraryObservationFixtures(t *testing.T) {
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "")
	if err != nil {
		t.Fatal(err)
	}
	snapshots := map[string]any{}
	for _, task := range tasks {
		if task.Skill != "media-library-organization" || task.Scope != "global" {
			continue
		}
		t.Run(task.ID, func(t *testing.T) {
			as := newEvalLibraryServer(t)
			seeded := seedEvalWorld(t, as, task, worlds[task.World])
			if evalTurnCollectionPath(seeded) != "/api/v2/agent-conversations/"+seeded.ConvID+"/turns" {
				t.Fatal("global fixture routed through product scope")
			}
			pageContext := evalPageContext(task, seeded, worlds[task.World].LiveGraph.Revision)
			filters := pageContext["filters"].(map[string]string)
			if original, ok := task.PageContext["filters"].(map[string]any); ok {
				if id, ok := original["folder_id"].(string); ok && seeded.FolderIDs[id] != "" && filters["folder_id"] != seeded.FolderIDs[id] {
					t.Fatal("folder filter retains fixture identity")
				}
			}
			if (seeded.GraphID != "" && filters["workflow_id"] != "" && filters["workflow_id"] != seeded.GraphID) || (seeded.ProductID != "" && filters["product_id"] != "" && filters["product_id"] != seeded.ProductID) {
				t.Fatal("global target filters retain fixture identities")
			}
			page, err := as.svc.ListLibraryAssets(context.Background(), seeded.ConvID, "", "", 100, LibraryReadOptions{IncludeArchived: true, WorkflowID: seeded.GraphID})
			if err != nil {
				t.Fatal(err)
			}
			stable := normalizeEvalLibraryValue(t, page, seeded).(map[string]any)
			folders := stable["folders"].([]any)
			sort.Slice(folders, func(i, j int) bool {
				return folders[i].(map[string]any)["id"].(string) < folders[j].(map[string]any)["id"].(string)
			})
			snapshots[task.ID] = stable
			for _, call := range task.Reference.ScriptedCalls {
				if call.Name != "propose_global_draft" {
					continue
				}
				var params map[string]any
				if err := json.Unmarshal(call.Params, &params); err != nil {
					t.Fatal(err)
				}
				payload := params["library_payload"].(map[string]any)
				for _, raw := range payload["operations"].([]any) {
					op := raw.(map[string]any)
					fixtureID := op["asset_id"].(string)
					var item LibraryAssetMetadata
					for _, current := range page.Items {
						if current.ID == seeded.AssetIDs[fixtureID] {
							item = current
						}
					}
					if item.ID == "" {
						t.Fatal("reference asset absent from Go read")
					}
					before := libraryReadOperation(item, "", nil)["before"]
					if !reflect.DeepEqual(op["before"], normalizeEvalLibraryValue(t, before, seeded)) {
						t.Fatalf("reference before does not match actual read: %v vs %v", op["before"], before)
					}
					op["asset_id"], op["expected_revision"], op["before"] = item.ID, item.Revision, before
					target := op["target"].(map[string]any)
					if id, ok := target["folder_id"].(string); ok {
						target["folder_id"] = seeded.FolderIDs[id]
					}
					if op["operation"] == "link_workflow" {
						target["workflow_id"] = page.Workflow.WorkflowID
						target["workflow_title"] = page.Workflow.WorkflowTitle
						target["expected_workflow_revision"] = page.Workflow.WorkflowRevision
						target["expected_linked"] = page.Workflow.Linked[item.ID]
					}
				}
				raw, _ := json.Marshal(payload)
				draft, err := as.svc.Library.AppendOrganizationDraftRevision(context.Background(), seeded.ConvID, raw, "", "")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := as.svc.ConfirmLibraryDraftHTTP(context.Background(), seeded.ConvID, draft.CurrentRevision.Version, clockid.New()); err != nil {
					t.Fatal(err)
				}
				for _, raw := range payload["operations"].([]any) {
					op := raw.(map[string]any)
					asset, err := as.svc.Library.Get(context.Background(), op["asset_id"].(string))
					if err != nil {
						t.Fatal(err)
					}
					target := op["target"].(map[string]any)
					switch op["operation"] {
					case "rename":
						if asset.DisplayName != target["display_name"] {
							t.Fatal("rename not persisted")
						}
					case "move":
						var folder any
						if asset.FolderID != nil {
							folder = *asset.FolderID
						}
						if folder != target["folder_id"] {
							t.Fatal("move not persisted")
						}
					case "archive", "restore":
						if asset.IsArchived != target["is_archived"] {
							t.Fatal("archive state not persisted")
						}
					case "set_tags":
						names, want := []string{}, []string{}
						for _, tag := range asset.Tags {
							names = append(names, tag.Name)
						}
						for _, tag := range target["tag_names"].([]any) {
							want = append(want, tag.(string))
						}
						sort.Strings(names)
						sort.Strings(want)
						if !reflect.DeepEqual(names, want) {
							t.Fatal("tags not persisted")
						}
					case "link_workflow":
						observed, err := as.svc.Library.ObserveWorkflowLinks(context.Background(), seeded.GraphID, []string{asset.ID})
						if err != nil || !observed.Linked[asset.ID] {
							t.Fatal("link not persisted")
						}
					default:
						t.Fatal("unsupported observation")
					}
				}
			}
		})
	}
	t.Run("pagination-and-linked", func(t *testing.T) {
		as := newEvalLibraryServer(t)
		var task EvalTask
		for _, candidate := range tasks {
			if candidate.ID == "media-library-organization-batch-rename" {
				task = candidate
			}
		}
		world := worlds[task.World]
		world.ListedFolders = []EvalListedFolder{
			{ID: "55555555-5555-4555-8555-555555555555", Title: "目录甲"},
			{ID: "55555555-5555-4555-8555-555555555556", Title: "目录乙"},
			{ID: "55555555-5555-4555-8555-555555555557", Title: "目录丙"},
		}
		seeded := seedEvalWorld(t, as, task, world)
		linkedID := seeded.AssetIDs[world.ListedAssets[0].ID]
		if _, err := as.svc.Library.SyncWorkflow(context.Background(), seeded.ProductID, seeded.GraphID, []string{linkedID}); err != nil {
			t.Fatal(err)
		}
		full, err := as.svc.ListLibraryAssets(context.Background(), seeded.ConvID, "", "", 100, LibraryReadOptions{WorkflowID: seeded.GraphID})
		if err != nil {
			t.Fatal(err)
		}
		stable := normalizeEvalLibraryValue(t, full, seeded).(map[string]any)
		folders := stable["folders"].([]any)
		sort.Slice(folders, func(i, j int) bool {
			return folders[i].(map[string]any)["id"].(string) < folders[j].(map[string]any)["id"].(string)
		})
		snapshots["_pagination"] = stable
		seen := map[string]bool{}
		after := ""
		for {
			page, err := as.svc.ListLibraryAssets(context.Background(), seeded.ConvID, "", "", 1, LibraryReadOptions{FoldersAfterID: after, WorkflowID: seeded.GraphID})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Folders) != 1 || seen[page.Folders[0].ID] {
				t.Fatal("invalid folder continuation")
			}
			seen[page.Folders[0].ID] = true
			if len(page.Workflow.Linked) != 1 || page.Workflow.Linked[page.Items[0].ID] != (page.Items[0].ID == linkedID) {
				t.Fatal("membership outside observed page")
			}
			if page.FoldersNextAfterID == nil {
				break
			}
			after = *page.FoldersNextAfterID
		}
		if len(seen) != 3 {
			t.Fatal(seen)
		}
		page, err := as.svc.ListLibraryAssets(context.Background(), seeded.ConvID, "", "", 1)
		if err != nil || page.NextCursor == nil {
			t.Fatalf("missing asset continuation: %v", err)
		}
		next, err := as.svc.ListLibraryAssets(context.Background(), seeded.ConvID, "", *page.NextCursor, 1)
		if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.NextCursor != nil {
			t.Fatalf("invalid asset continuation: %v", err)
		}
		if _, err := as.svc.ListLibraryAssets(context.Background(), seeded.ConvID, "changed", *page.NextCursor, 1); err == nil {
			t.Fatal("cursor reused across filters")
		}
	})
	checkEvalFixture(t, filepath.Join(DefaultEvalRoot(), "fixtures", "library-observations.json"), snapshots)
}
