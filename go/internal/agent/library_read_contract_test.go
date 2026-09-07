package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func libraryReadAsset(t *testing.T, as *agentServer) library.Asset {
	t.Helper()
	var content bytes.Buffer
	if err := png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	items, err := as.svc.Library.Upload(ctx, []library.UploadItem{{Content: content.Bytes(), Filename: "read-contract-" + clockid.New() + ".png", MIMEType: "image/png"}}, nil, clockid.New())
	if err != nil {
		t.Fatal(err)
	}
	return items[0].Asset
}

func libraryReadConversation(t *testing.T, as *agentServer) string {
	t.Helper()
	r := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, r, http.StatusCreated)
	var session SessionResponse
	as.decode(t, r, &session)
	return session.Conversations[0].ConversationID
}

func libraryRenamePayload(t *testing.T, as *agentServer) []byte {
	t.Helper()
	asset := libraryReadAsset(t, as)
	item := LibraryAssetMetadata{ID: asset.ID, DisplayName: asset.DisplayName, Revision: asset.Revision, FolderID: asset.FolderID, TagNames: []string{}, IsArchived: asset.IsArchived}
	raw, err := json.Marshal(map[string]any{"schema_version": 1, "confirmation_summary": "重命名素材", "operations": []any{libraryReadOperation(item, "rename", map[string]any{"display_name": "确认后名称"})}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func libraryReadPage(t *testing.T, as *agentServer, conversation string, query url.Values) LibraryAssetListResponse {
	t.Helper()
	r := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+conversation+"/media-library?"+query.Encode(), nil, "", http.Header{"Authorization": {"Bearer tok"}})
	as.mustStatus(t, r, http.StatusOK)
	var page LibraryAssetListResponse
	as.decode(t, r, &page)
	return page
}

func libraryReadOperation(item LibraryAssetMetadata, kind string, target map[string]any) map[string]any {
	return map[string]any{"operation": kind, "asset_id": item.ID, "expected_revision": item.Revision, "reason": "用户指定整理",
		"before": map[string]any{"revision": item.Revision, "display_name": item.DisplayName, "folder_id": item.FolderID, "tag_names": item.TagNames, "is_archived": item.IsArchived}, "target": target}
}

func TestLibraryReadFactsDriveConfirmedOperations(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	asset := libraryReadAsset(t, as)
	folder, err := as.svc.Library.CreateFolder(ctx, "目标目录-"+clockid.New())
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := as.svc.Product.CreateAgentDraft(ctx, "关联目标", clockid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Discover the target workflow through the existing global product-context tool.
	conversation := libraryReadConversation(t, as)
	contextResult, err := as.svc.GlobalWorkflowContext(ctx, conversation, workspace.Product.ID, "concise")
	if err != nil {
		t.Fatal(err)
	}
	live := contextResult["live_graph"].(map[string]any)
	workflowID := live["id"].(string)
	for _, kind := range []string{"rename", "move", "set_tags", "archive", "restore", "link_workflow"} {
		t.Run(kind, func(t *testing.T) {
			conv := libraryReadConversation(t, as)
			q := url.Values{"query": {asset.OriginalFilename}, "include_archived": {"true"}, "folder_query": {folder.Name}, "workflow_id": {workflowID}, "limit": {"10"}}
			page := libraryReadPage(t, as, conv, q)
			if len(page.Items) != 1 || page.Items[0].ID != asset.ID || len(page.Folders) != 1 || page.Folders[0].ID != folder.ID {
				t.Fatalf("missing visible targets: %+v", page)
			}
			item := page.Items[0]
			target := map[string]any{}
			switch kind {
			case "rename":
				target["display_name"] = "用户新名"
			case "move":
				target["folder_id"] = page.Folders[0].ID
			case "set_tags":
				target["tag_names"] = []string{"夏季", "已选"}
			case "archive":
				target["is_archived"] = true
			case "restore":
				target["is_archived"] = false
			case "link_workflow":
				if page.Workflow == nil {
					t.Fatal("missing workflow observation")
				}
				target = map[string]any{"workflow_id": page.Workflow.WorkflowID, "workflow_title": page.Workflow.WorkflowTitle, "expected_workflow_revision": page.Workflow.WorkflowRevision, "expected_linked": page.Workflow.Linked[item.ID]}
			}
			payload := map[string]any{"schema_version": 1, "confirmation_summary": "按用户指令整理", "operations": []any{libraryReadOperation(item, kind, target)}}
			value := map[string]any{"schema_version": 1, "draft_kind": "library_organization", "library_payload": payload}
			validated := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+conv+"/global-draft/validate", map[string]any{"value": value}, http.Header{"Authorization": {"Bearer tok"}})
			as.mustStatus(t, validated, http.StatusOK)
			validated.Body.Close()
			raw, _ := json.Marshal(payload)
			draft, err := as.svc.Library.AppendOrganizationDraftRevision(ctx, conv, raw, "", "")
			if err != nil {
				t.Fatal(err)
			}
			before := libraryReadPage(t, as, conv, q).Items[0]
			if !reflect.DeepEqual(before, item) {
				t.Fatal("draft modified an asset before confirmation")
			}
			confirmed := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+conv+"/library-organization-draft/confirm", map[string]any{"expected_draft_version": draft.CurrentRevision.Version, "idempotency_key": clockid.New()})
			as.mustStatus(t, confirmed, http.StatusOK)
			confirmed.Body.Close()
			afterPage := libraryReadPage(t, as, conv, q)
			after := afterPage.Items[0]
			if kind != "link_workflow" && after.Revision != item.Revision+1 {
				t.Fatalf("revision %d -> %d", item.Revision, after.Revision)
			}
			switch kind {
			case "rename":
				if after.DisplayName != target["display_name"] {
					t.Fatal(after)
				}
			case "move":
				if after.FolderID == nil || *after.FolderID != target["folder_id"] {
					t.Fatal(after)
				}
			case "set_tags":
				sort.Strings(after.TagNames)
				want := append([]string(nil), target["tag_names"].([]string)...)
				sort.Strings(want)
				if !reflect.DeepEqual(after.TagNames, want) {
					t.Fatal(after)
				}
			case "archive", "restore":
				if after.IsArchived != target["is_archived"] {
					t.Fatal(after)
				}
			case "link_workflow":
				if !afterPage.Workflow.Linked[item.ID] {
					t.Fatal("link not persisted")
				}
			}
			if kind == "archive" {
				q.Del("include_archived")
				if len(libraryReadPage(t, as, conv, q).Items) != 0 {
					t.Fatal("default list exposed archived asset")
				}
			}
		})
	}
}

func TestLibraryReadWrongFactsCannotConfirm(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	asset := libraryReadAsset(t, as)
	other := libraryReadAsset(t, as)
	for _, fault := range []string{"revision", "zero_revision", "before", "other_asset", "missing_folder"} {
		t.Run(fault, func(t *testing.T) {
			conv := libraryReadConversation(t, as)
			q := url.Values{"query": {asset.OriginalFilename}, "limit": {"10"}}
			item := libraryReadPage(t, as, conv, q).Items[0]
			op := libraryReadOperation(item, "rename", map[string]any{"display_name": "错误写入"})
			switch fault {
			case "revision":
				op["expected_revision"] = item.Revision + 1
			case "zero_revision":
				op["expected_revision"] = 0
			case "before":
				op["before"].(map[string]any)["display_name"] = "错误旧值"
			case "other_asset":
				op["asset_id"] = other.ID
			case "missing_folder":
				op["operation"] = "move"
				op["target"] = map[string]any{"folder_id": clockid.New()}
			}
			raw, _ := json.Marshal(map[string]any{"schema_version": 1, "confirmation_summary": "错误输入", "operations": []any{op}})
			draft, err := as.svc.Library.AppendOrganizationDraftRevision(ctx, conv, raw, "", "")
			if err != nil {
				t.Fatal(err)
			}
			r := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+conv+"/library-organization-draft/confirm", map[string]any{"expected_draft_version": draft.CurrentRevision.Version, "idempotency_key": clockid.New()})
			want := http.StatusConflict
			if fault == "missing_folder" {
				want = http.StatusNotFound
			}
			as.mustStatus(t, r, want)
			r.Body.Close()
			if got := libraryReadPage(t, as, conv, q).Items[0]; !reflect.DeepEqual(got, item) {
				t.Fatalf("failed confirmation changed asset: %+v", got)
			}
		})
	}
}

func TestLibraryReadPaginationScopeAndWorkflowConflicts(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	asset := libraryReadAsset(t, as)
	conv := libraryReadConversation(t, as)
	marker := "paged-" + clockid.New()
	for _, suffix := range []string{"a", "b", "c"} {
		if _, err := as.svc.Library.CreateFolder(ctx, marker+suffix); err != nil {
			t.Fatal(err)
		}
	}
	q := url.Values{"query": {asset.OriginalFilename}, "folder_query": {marker}, "limit": {"1"}}
	seen := map[string]bool{}
	for {
		page := libraryReadPage(t, as, conv, q)
		if len(page.Folders) != 1 || seen[page.Folders[0].ID] {
			t.Fatalf("invalid folder page: %+v", page)
		}
		seen[page.Folders[0].ID] = true
		if page.FoldersNextAfterID == nil {
			break
		}
		q.Set("folders_after_id", *page.FoldersNextAfterID)
	}
	if len(seen) != 3 {
		t.Fatal(seen)
	}
	workspace, err := as.svc.Product.CreateAgentDraft(ctx, "读权限与关联", clockid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bench, err := as.svc.EnsureWorkbench(ctx, workspace.Product.ID, clockid.New(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	productConv := bench.Conversation.ID
	for _, query := range []string{"", "?include_archived=true"} {
		r := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+productConv+"/media-library"+query, nil, "", http.Header{"Authorization": {"Bearer tok"}})
		as.mustStatus(t, r, http.StatusConflict)
		r.Body.Close()
	}
	productList := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+productConv+"/assets", nil, "", http.Header{"Authorization": {"Bearer tok"}})
	as.mustStatus(t, productList, http.StatusOK)
	productList.Body.Close()
	contextResult, err := as.svc.GlobalWorkflowContext(ctx, conv, workspace.Product.ID, "concise")
	if err != nil {
		t.Fatal(err)
	}
	workflowID := contextResult["live_graph"].(map[string]any)["id"].(string)
	q = url.Values{"query": {asset.OriginalFilename}, "workflow_id": {workflowID}, "limit": {"10"}}
	for _, fault := range []string{"revision", "title", "linked"} {
		t.Run(fault, func(t *testing.T) {
			conversation := libraryReadConversation(t, as)
			page := libraryReadPage(t, as, conversation, q)
			item := page.Items[0]
			target := map[string]any{"workflow_id": page.Workflow.WorkflowID, "workflow_title": page.Workflow.WorkflowTitle,
				"expected_workflow_revision": page.Workflow.WorkflowRevision, "expected_linked": page.Workflow.Linked[item.ID]}
			switch fault {
			case "revision":
				target["expected_workflow_revision"] = page.Workflow.WorkflowRevision + 1
			case "title":
				target["workflow_title"] = "错误标题"
			case "linked":
				target["expected_linked"] = true
			}
			raw, _ := json.Marshal(map[string]any{"schema_version": 1, "confirmation_summary": "关联", "operations": []any{libraryReadOperation(item, "link_workflow", target)}})
			draft, err := as.svc.Library.AppendOrganizationDraftRevision(ctx, conversation, raw, "", "")
			if err != nil {
				t.Fatal(err)
			}
			r := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+conversation+"/library-organization-draft/confirm", map[string]any{"expected_draft_version": draft.CurrentRevision.Version, "idempotency_key": clockid.New()})
			as.mustStatus(t, r, http.StatusConflict)
			r.Body.Close()
			if libraryReadPage(t, as, conversation, q).Workflow.Linked[item.ID] {
				t.Fatal("conflicting link persisted")
			}
		})
	}
}
