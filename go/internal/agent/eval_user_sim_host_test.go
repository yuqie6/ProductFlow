package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// Node's L3 driver uses this opt-in, isolated PG host for business effects and user decisions.
// L1 graph observation reuses the same host with PRODUCTFLOW_EVAL_HOST_LAYER=l1.
// Pi execution leases remain local to the Node test harness; graph/library transactions are production code.
func TestEvalUserSimHost(t *testing.T) {
	id := os.Getenv("PRODUCTFLOW_EVAL_HOST_TASK")
	if id == "" {
		t.Skip("L3 subprocess host")
	}
	layer := strings.TrimSpace(os.Getenv("PRODUCTFLOW_EVAL_HOST_LAYER"))
	if layer == "" {
		layer = "l3"
	}
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), layer)
	if err != nil {
		t.Fatal(err)
	}
	var task EvalTask
	for _, candidate := range tasks {
		if candidate.ID == id {
			task = candidate
		}
	}
	if task.ID == "" {
		t.Fatal("unknown eval host task")
	}
	as := newEvalHostServer(t)
	seeded := seedEvalWorld(t, as, task, worlds[task.World])
	var mu sync.Mutex
	closed := make(chan struct{})
	var once sync.Once
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	var baseline graph.Projection
	if seeded.GraphID != "" {
		baseline, err = as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/close" {
			once.Do(func() { close(closed) })
			return
		}
		var request struct {
			Method         string         `json:"method"`
			Params         map[string]any `json:"params"`
			IdempotencyKey string         `json:"idempotency_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		raw, _ := json.Marshal(request.Params)
		text := string(raw)
		mapping := map[string]string{"22222222-2222-4222-8222-222222222222": seeded.ProductID, "33333333-3333-4333-8333-333333333333": seeded.GraphID}
		for _, ids := range []map[string]string{seeded.NodeIDs, seeded.EdgeIDs, seeded.GroupIDs, seeded.AssetIDs, seeded.FolderIDs} {
			for fixture, actual := range ids {
				mapping[fixture] = actual
			}
		}
		for fixture, actual := range mapping {
			if actual != "" {
				text = strings.ReplaceAll(text, fmt.Sprintf("%q", fixture), fmt.Sprintf("%q", actual))
			}
		}
		raw = []byte(text)
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		key := request.IdempotencyKey
		var out any
		var callErr error
		switch request.Method {
		case "observe":
			out = map[string]any{"errors": gradeEvalFinalWrites(t, as, seeded, task.Expect.Writes, baseline)}
		case "assets":
			query, _ := p["query"].(string)
			cursor, _ := p["cursor"].(string)
			archived, _ := p["include_archived"].(bool)
			folderQuery, _ := p["folder_query"].(string)
			afterID, _ := p["folders_after_id"].(string)
			workflowID, _ := p["workflow_id"].(string)
			limit, _ := p["limit"].(float64)
			out, callErr = as.svc.ListLibraryAssets(ctx, seeded.ConvID, query, cursor, int(limit), LibraryReadOptions{IncludeArchived: archived, FolderQuery: folderQuery, FoldersAfterID: afterID, WorkflowID: workflowID})
		case "inspect_assets":
			ids := []string{}
			if values, ok := p["asset_ids"].([]any); ok {
				for _, id := range values {
					ids = append(ids, fmt.Sprint(id))
				}
			}
			items, err := as.svc.InspectLibraryAssets(ctx, seeded.ConvID, ids)
			callErr = err
			out = map[string]any{"items": items}
		case "context":
			format, _ := p["response_format"].(string)
			out, callErr = as.svc.ProductContext(ctx, seeded.ConvID, format)
		case "apply":
			out, callErr = as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, key)
		case "propose":
			out, callErr = as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, key)
		case "discard":
			id, _ := p["proposal_id"].(string)
			out, callErr = as.svc.DiscardProposalTool(ctx, seeded.ConvID, id, key)
		case "node":
			out, callErr = as.svc.GetNodeDetail(ctx, seeded.ConvID, fmt.Sprint(p["node_id"]))
		case "intake":
			var input struct {
				Selection json.RawMessage `json:"selection"`
				IDs       []string        `json:"reference_asset_ids"`
			}
			_ = json.Unmarshal(raw, &input)
			out, callErr = as.svc.FinalizeProductIntake(ctx, seeded.ConvID, key, input.Selection, input.IDs)
		case "draft":
			callErr = as.svc.ValidateGlobalDraft(ctx, seeded.ConvID, raw)
			if callErr == nil {
				payload, _ := json.Marshal(p["library_payload"])
				out, callErr = as.svc.Library.AppendOrganizationDraftRevision(ctx, seeded.ConvID, payload, "", "")
			}
		case "prepare", "run":
			var input struct {
				Revision int      `json:"expected_workflow_revision"`
				Scope    string   `json:"scope"`
				NodeID   *string  `json:"node_id"`
				NodeIDs  []string `json:"node_ids"`
				Force    bool     `json:"force"`
				Action   string   `json:"document_action"`
			}
			_ = json.Unmarshal(raw, &input)
			if request.Method == "prepare" {
				out, callErr = as.svc.PrepareWorkflowRunRequest(ctx, seeded.ConvID, input.Revision, nil, nil)
			} else {
				spec, e := parseRunScopeSpec(input.Scope, input.NodeID, input.NodeIDs, input.Force, input.Action)
				callErr = e
				if e == nil {
					out, callErr = as.svc.CreateWorkflowRunRequest(ctx, seeded.ConvID, seeded.GraphID, clockid.New(), clockid.New(), input.Revision, nil, nil, spec)
				}
			}
		case "decision":
			kind, action := fmt.Sprint(p["kind"]), fmt.Sprint(p["action"])
			if action != "confirm" && action != "discard" {
				callErr = fmt.Errorf("invalid decision")
				break
			}
			switch kind {
			case "proposal":
				before, e := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
				callErr = e
				if e == nil && before.PendingProposal == nil {
					callErr = fmt.Errorf("no pending proposal")
				}
				if callErr == nil {
					id := before.PendingProposal.ID
					path := "/api/v3/products/" + seeded.ProductID + "/workflows/" + seeded.GraphID + "/proposals/" + id + "/" + action
					resp := as.do(t, http.MethodPost, path, nil, "", nil)
					if resp.StatusCode != 200 {
						callErr = fmt.Errorf("decision HTTP %d", resp.StatusCode)
					}
					resp.Body.Close()
					after, e := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
					if e != nil || after.PendingProposal != nil || (action == "confirm" && after.Revision <= before.Revision) || (action == "discard" && after.Revision != before.Revision) {
						callErr = fmt.Errorf("proposal decision effect not observed")
					}
					out = map[string]any{"id": id, "action": action, "observed": callErr == nil, "revision": after.Revision}
				}
			case "run_request":
				pending, e := as.svc.GetWorkflowRunRequest(ctx, &seeded.ProductID, seeded.ConvID, nil)
				callErr = e
				if e == nil && pending == nil {
					callErr = fmt.Errorf("no pending run request")
				}
				if callErr == nil {
					if action == "confirm" {
						_, callErr = as.svc.ConfirmWorkflowRunRequest(ctx, &seeded.ProductID, seeded.ConvID, pending.ID)
					} else {
						_, callErr = as.svc.CancelWorkflowRunRequestHTTP(ctx, &seeded.ProductID, seeded.ConvID, pending.ID)
					}
					after, e := as.svc.GetWorkflowRunRequest(ctx, &seeded.ProductID, seeded.ConvID, nil)
					if e != nil || after == nil || (action == "confirm" && after.WorkflowRunID == nil) {
						callErr = fmt.Errorf("run decision effect not observed")
					}
					out = map[string]any{"id": pending.ID, "action": action, "observed": callErr == nil}
				}
			case "library_draft":
				draft, e := as.svc.Library.GetOrganizationDraft(ctx, seeded.ConvID)
				callErr = e
				if e == nil && draft.CurrentRevision == nil {
					callErr = fmt.Errorf("no pending library draft")
				}
				if callErr == nil && action == "confirm" {
					_, callErr = as.svc.ConfirmLibraryDraftHTTP(ctx, seeded.ConvID, draft.CurrentRevision.Version, clockid.New())
					after, e := as.svc.Library.GetOrganizationDraft(ctx, seeded.ConvID)
					if callErr == nil && (e != nil || after.Status != "confirmed") {
						callErr = fmt.Errorf("library decision effect not observed: %v", e)
					}
					out = map[string]any{"id": draft.ID, "action": action, "observed": callErr == nil}
				} else if action != "confirm" {
					callErr = fmt.Errorf("unobservable library discard")
				}
			default:
				callErr = fmt.Errorf("unknown approval kind")
			}
		default:
			callErr = fmt.Errorf("unsupported host method")
		}
		w.Header().Set("Content-Type", "application/json")
		if callErr != nil {
			writeEvalHostError(w, callErr)
			return
		}
		_ = json.NewEncoder(w).Encode(normalizeEvalIdentity(t, out, seeded))
	}))
	defer server.Close()
	fmt.Printf("EVAL_HOST_READY=%s\n", server.URL)
	<-closed
}

func writeEvalHostError(w http.ResponseWriter, err error) {
	var e apperr.Error
	if errors.As(err, &e) {
		body := map[string]any{"detail": e.Detail}
		if e.Code != "" {
			body["code"] = e.Code
		}
		w.WriteHeader(e.Status)
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{"detail": err.Error(), "code": "eval_host"})
}

func TestEvalHostErrorStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeEvalHostError(rec, apperr.Validation("节点配置包含未登记字段: design_goal"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["detail"] != "节点配置包含未登记字段: design_goal" || body["code"] != nil {
		t.Fatalf("%#v", body)
	}

	rec = httptest.NewRecorder()
	writeEvalHostError(rec, apperr.NotFound("节点不存在"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	writeEvalHostError(rec, fmt.Errorf("no pending proposal"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "eval_host" || body["detail"] != "no pending proposal" {
		t.Fatalf("%#v", body)
	}
}
