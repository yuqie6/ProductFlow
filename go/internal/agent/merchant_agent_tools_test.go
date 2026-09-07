package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// B7：浏览器+internal+Pi scope 商家字段；本商 turn/tool 成功；伪造 scope / 跨商 content / 撤销后确认拒绝；关键工具越权 harness。
func TestMerchantAgentToolsIsolation(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	authHdr := http.Header{"Authorization": []string{"Bearer tok"}}
	devMerchant := auth.MustDevMerchantID(t, as.db)

	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	globalConv := sess.Conversations[0].ConversationID

	contract := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+globalConv+"/contract", nil, "", authHdr)
	as.mustStatus(t, contract, http.StatusOK)
	var contractBody ContractResponse
	as.decode(t, contract, &contractBody)
	if contractBody.MerchantID != devMerchant {
		t.Fatalf("contract merchant_id=%q want %q", contractBody.MerchantID, devMerchant)
	}

	runtime := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+globalConv+"/runtime-context", nil, "", authHdr)
	as.mustStatus(t, runtime, http.StatusOK)
	var runtimeBody RuntimeContextResponse
	as.decode(t, runtime, &runtimeBody)
	if runtimeBody.MerchantID != devMerchant {
		t.Fatalf("runtime merchant_id=%q want %q", runtimeBody.MerchantID, devMerchant)
	}

	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+globalConv+"/turns", map[string]any{
		"input_text": "列出本商家商品", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	turn.Body.Close()

	products := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+globalConv+"/products?limit=10", nil, "", authHdr)
	as.mustStatus(t, products, http.StatusOK)
	products.Body.Close()

	foreign := seedForeignAgentFixture(t, as)

	assertCross404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
		var body struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Detail != auth.CrossMerchantDetail {
			t.Fatalf("%s detail=%q want %q body=%s", name, body.Detail, auth.CrossMerchantDetail, raw)
		}
	}

	// 内部调用声明的商家必须与持久化会话归属一致，伪造目标返回统一404。
	forgedHdr := http.Header{
		"Authorization":             []string{"Bearer tok"},
		"X-ProductFlow-Merchant-Id": []string{foreign.merchantID},
	}
	assertCross404("forged merchant assertion", as.do(t, http.MethodGet,
		"/api/internal/v1/agent-conversations/"+globalConv+"/contract", nil, "", forgedHdr))

	assertCross404("foreign conversation contract", as.do(t, http.MethodGet,
		"/api/internal/v1/agent-conversations/"+foreign.globalConvID+"/contract", nil, "", authHdr))
	assertCross404("own global inspect foreign product", as.doJSONAuth(t, http.MethodPost,
		"/api/internal/v1/agent-conversations/"+globalConv+"/products/inspect",
		map[string]any{"product_ids": []string{foreign.productID}}, authHdr))
	assertCross404("own global library content", as.do(t, http.MethodGet,
		"/api/internal/v1/agent-conversations/"+globalConv+"/media-library/"+foreign.libraryAssetID+"/content", nil, "", authHdr))

	listed := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+globalConv+"/products?limit=100", nil, "", authHdr)
	as.mustStatus(t, listed, http.StatusOK)
	var productPage GlobalProductListResponse
	as.decode(t, listed, &productPage)
	for _, item := range productPage.Items {
		if item.ID == foreign.productID {
			t.Fatal("list_products_v1 leaked foreign merchant product")
		}
	}

	runToolOverreachHarness(t, as, authHdr, globalConv, foreign)

	task, pendingReq := createProductGoalRunRequest(t, as, false)
	if task.ConversationID == nil || task.ProductID == nil {
		t.Fatal("pending task missing conversation/product")
	}

	now := time.Now().UTC()
	if err := as.db.Model(&schema.Users{}).
		Where("merchant_id = ? AND status = ?", devMerchant, "active").
		Updates(map[string]any{"status": "disabled", "updated_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = as.db.Model(&schema.Users{}).
			Where("merchant_id = ?", devMerchant).
			Updates(map[string]any{"status": "active", "updated_at": time.Now().UTC()}).Error
	})

	confirm := as.do(t, http.MethodPost,
		"/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pendingReq.ID+"/confirm",
		nil, "", nil)
	defer confirm.Body.Close()
	if confirm.StatusCode != http.StatusUnauthorized {
		raw, _ := io.ReadAll(confirm.Body)
		t.Fatalf("confirm after user disabled want 401 got %d %s", confirm.StatusCode, raw)
	}
}

// runToolOverreachHarness 覆盖矩阵 I9 关键工具路径的跨商/越权拒绝（含 content）。
// 合成工具 skill/ask/context_injection 无 Go 资源 id，由 contract.merchant_id 覆盖。
func runToolOverreachHarness(t *testing.T, as *agentServer, authHdr http.Header, ownGlobalConv string, foreign foreignAgentFixture) {
	t.Helper()
	assertReject := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			t.Fatalf("%s leaked success %d %s", name, resp.StatusCode, raw)
		}
		if resp.StatusCode == http.StatusNotFound {
			var body struct {
				Detail string `json:"detail"`
			}
			_ = json.Unmarshal(raw, &body)
			if body.Detail != auth.CrossMerchantDetail && !strings.Contains(body.Detail, "不存在") {
				t.Fatalf("%s detail=%q body=%s", name, body.Detail, raw)
			}
		}
	}

	idem := func() http.Header {
		h := authHdr.Clone()
		h.Set("Idempotency-Key", clockid.New())
		return h
	}

	productTask := seedProductGoalTask(t, as)
	if productTask.ConversationID == nil || productTask.ProductID == nil {
		t.Fatal("product task incomplete")
	}
	ownProductConv := *productTask.ConversationID

	cases := []struct {
		tool string
		call func() *http.Response
	}{
		{"get_product_workflow_context_v1", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/product-context", nil, "", authHdr)
		}},
		{"inspect_workflow_runs_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+ownProductConv+"/workflow-runs/inspect",
				map[string]any{"run_ids": []string{foreign.runID}}, authHdr)
		}},
		{"list_product_image_assets_v2", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/assets", nil, "", authHdr)
		}},
		{"inspect_product_image_assets_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+ownProductConv+"/assets/inspect",
				map[string]any{"asset_ids": []string{foreign.assetID}}, authHdr)
		}},
		{"request_workflow_run_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/workflow-run-requests",
				map[string]any{"workflow_id": foreign.graphID, "expected_workflow_revision": 1, "scope": "graph"}, idem())
		}},
		{"request_global_workflow_run_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+ownGlobalConv+"/global-workflow-run-requests",
				map[string]any{"product_id": foreign.productID, "workflow_id": foreign.graphID, "expected_workflow_revision": 1, "scope": "graph"}, idem())
		}},
		{"finalize_product_intake_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/product-intake",
				map[string]any{"selection": map[string]any{}, "reference_asset_ids": []string{}}, idem())
		}},
		{"list_products_v1", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+foreign.globalConvID+"/products", nil, "", authHdr)
		}},
		{"inspect_products_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+ownGlobalConv+"/products/inspect",
				map[string]any{"product_ids": []string{foreign.productID}}, authHdr)
		}},
		{"inspect_global_workflow_context_v1", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+ownGlobalConv+"/global-workflow-context?product_id="+foreign.productID, nil, "", authHdr)
		}},
		{"inspect_global_workflow_runs_v1", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+foreign.globalConvID+"/workflow-runs", nil, "", authHdr)
		}},
		{"list_global_media_library_assets_v1", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+foreign.globalConvID+"/media-library", nil, "", authHdr)
		}},
		{"inspect_global_media_library_assets_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+ownGlobalConv+"/media-library/inspect",
				map[string]any{"asset_ids": []string{foreign.libraryAssetID}}, authHdr)
		}},
		{"create_product_workspace_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.globalConvID+"/product-workspaces",
				map[string]any{"name": "越权工作区"}, idem())
		}},
		{"propose_global_draft", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.globalConvID+"/global-draft/validate",
				map[string]any{"value": map[string]any{"draft_kind": "library_organization", "library_payload": map[string]any{"operations": []any{}}}}, authHdr)
		}},
		{"get_node_detail_v1", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+ownProductConv+"/graph/nodes/"+foreign.nodeID, nil, "", authHdr)
		}},
		{"get_workflow_run_detail_v1", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+ownProductConv+"/workflow-runs/"+foreign.runID, nil, "", authHdr)
		}},
		{"apply_graph_change_set_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/graph/apply-change-set",
				map[string]any{"change_set": map[string]any{"base_graph_revision": 1, "operations": []any{}}}, idem())
		}},
		{"propose_graph_change_set_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/graph/proposals",
				map[string]any{"change_set": map[string]any{"base_graph_revision": 1, "operations": []any{}}}, idem())
		}},
		{"discard_workflow_proposal_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/graph/proposals/discard",
				map[string]any{"proposal_id": foreign.proposalID}, idem())
		}},
		{"cancel_workflow_run_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+ownProductConv+"/workflow-runs/"+foreign.runID+"/cancel",
				map[string]any{}, idem())
		}},
		{"focus_canvas_items_v1", func() *http.Response {
			return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+foreign.productConvID+"/canvas/focus",
				map[string]any{"node_ids": []string{foreign.nodeID}}, idem())
		}},
		{"asset_content", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+ownProductConv+"/assets/"+foreign.assetID+"/content", nil, "", authHdr)
		}},
		{"library_content", func() *http.Response {
			return as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+ownGlobalConv+"/media-library/"+foreign.libraryAssetID+"/content", nil, "", authHdr)
		}},
	}

	covered := map[string]struct{}{
		"load_productflow_skill": {}, "ask_user": {}, "productflow_context_injection": {},
	}
	for _, tc := range cases {
		covered[tc.tool] = struct{}{}
		assertReject(tc.tool, tc.call())
	}
	manifestTools := []string{
		"load_productflow_skill", "ask_user", "productflow_context_injection",
		"get_product_workflow_context_v1", "inspect_workflow_runs_v1", "list_product_image_assets_v2",
		"inspect_product_image_assets_v1", "request_workflow_run_v1", "request_global_workflow_run_v1",
		"finalize_product_intake_v1", "list_products_v1", "inspect_products_v1",
		"inspect_global_workflow_context_v1", "inspect_global_workflow_runs_v1",
		"list_global_media_library_assets_v1", "inspect_global_media_library_assets_v1",
		"create_product_workspace_v1", "propose_global_draft", "get_node_detail_v1",
		"get_workflow_run_detail_v1", "apply_graph_change_set_v1", "propose_graph_change_set_v1",
		"discard_workflow_proposal_v1", "cancel_workflow_run_v1", "focus_canvas_items_v1",
	}
	for _, name := range manifestTools {
		if _, ok := covered[name]; !ok {
			t.Fatalf("harness missing tool path %s", name)
		}
	}
	if len(manifestTools) != 25 {
		t.Fatalf("manifest tool count %d want 25", len(manifestTools))
	}
}

type foreignAgentFixture struct {
	merchantID     string
	productID      string
	graphID        string
	nodeID         string
	runID          string
	proposalID     string
	assetID        string
	libraryAssetID string
	productConvID  string
	globalConvID   string
}

func seedForeignAgentFixture(t *testing.T, as *agentServer) foreignAgentFixture {
	t.Helper()
	now := time.Now().UTC()
	merchantID := clockid.New()
	if err := as.db.Create(&schema.Merchants{
		ID: merchantID, Name: "夹具他商-B7-agent", Status: "active", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	productID := clockid.New()
	if err := as.db.Create(&schema.Products{
		ID: productID, MerchantID: merchantID, Name: "他商商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	graphID := clockid.New()
	if err := as.db.Create(&schema.WorkflowGraphs{
		ID: graphID, ProductID: productID, Title: "他商图", Active: true, SchemaVersion: 3, Revision: 1,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	nodeID := clockid.New()
	if err := as.db.Exec(`
		INSERT INTO workflow_graph_nodes (
			id, graph_id, node_type, title, position_x, position_y, config_json, document_origin, created_at, updated_at
		) VALUES (?, ?, 'creative_brief', '他商节点', 0, 0, '{}', 'seed', ?, ?)
	`, nodeID, graphID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	runID := clockid.New()
	if err := as.db.Exec(`
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at, finished_at
		) VALUES (?, ?, 'succeeded', 'graph', 1, '{}'::json, TRUE, ?, ?)
	`, runID, graphID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	proposalID := clockid.New()

	byteSize := int64(64)
	width, height := 32, 32
	sha := strings.Repeat("b", 64)
	mediaID := clockid.New()
	if err := as.db.Create(&schema.MediaObjects{
		ID: mediaID, StoragePath: "b7-foreign/" + mediaID + ".png", MIMEType: "image/png",
		ByteSize: &byteSize, Width: &width, Height: &height, SHA256: &sha,
		VerificationStatus: "verified", CreatedAt: now, VerifiedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	assetID := clockid.New()
	if err := as.db.Create(&schema.ProductImageAssets{
		ID: assetID, ProductID: productID, MediaObjectID: mediaID, OriginType: "upload",
		DisplayName: "他商图", OriginalFilename: "x.png", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	libraryAssetID := clockid.New()
	if err := as.db.Create(&schema.MediaLibraryAssets{
		ID: libraryAssetID, MerchantID: merchantID, MediaObjectID: mediaID,
		SourceType: "direct_upload", SourceID: clockid.New(), ProvenanceJSON: "{}", ProvenanceHash: strings.Repeat("c", 64),
		Revision: 1, DisplayName: "他商库图", OriginalFilename: "x.png", IsArchived: false,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	sessionID := clockid.New()
	if err := as.db.Create(&schema.AgentSessions{
		ID: sessionID, MerchantID: merchantID, Title: "他商会话", Status: "active",
		CreatedAt: now, UpdatedAt: now, ActivityAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	productConvID := clockid.New()
	if err := as.db.Create(&schema.AgentConversations{
		ID: productConvID, MerchantID: merchantID, ProductID: &productID, SessionID: &sessionID,
		HarnessRunID: productConvID, Status: "collecting", ScopeType: "product_workflow",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	globalConvID := clockid.New()
	if err := as.db.Create(&schema.AgentConversations{
		ID: globalConvID, MerchantID: merchantID, SessionID: &sessionID,
		HarnessRunID: globalConvID, Status: "collecting", ScopeType: "global",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	return foreignAgentFixture{
		merchantID: merchantID, productID: productID, graphID: graphID, nodeID: nodeID,
		runID: runID, proposalID: proposalID, assetID: assetID, libraryAssetID: libraryAssetID,
		productConvID: productConvID, globalConvID: globalConvID,
	}
}
