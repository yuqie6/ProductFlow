package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

func TestProductContextToolUsesExplicitStrictEmptyObjectSchema(t *testing.T) {
	tools := scopedReadTools(nil, Scope{})
	for _, tool := range tools {
		if tool.Name != productContextToolName {
			continue
		}
		want := map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		}
		if !tool.Strict || !reflect.DeepEqual(tool.Parameters, want) {
			t.Fatalf("product context tool strict schema = %#v, want %#v", tool.Parameters, want)
		}
		return
	}
	t.Fatalf("tool %q is not registered", productContextToolName)
}

func TestScopedToolCatalogContainsOnlyCurrentGalleryTools(t *testing.T) {
	readNames := make(map[string]bool)
	for _, tool := range scopedReadTools(nil, Scope{}) {
		readNames[tool.Name] = true
		if strings.Contains(tool.Name, "delete") {
			t.Fatalf("read catalog unexpectedly grants delete tool %q", tool.Name)
		}
	}
	wantRead := map[string]bool{
		productContextToolName:       true,
		inspectWorkflowRunsToolName:  true,
		listLegacyArchivesToolName:   true,
		inspectLegacyArchiveToolName: true,
		listAssetsToolName:           true,
		inspectAssetsToolName:        true,
	}
	if !reflect.DeepEqual(readNames, wantRead) || !readNames["list_product_image_assets_v2"] {
		t.Fatalf("read catalog = %#v, want %#v", readNames, wantRead)
	}

	durableNames := make(map[string]bool)
	for _, tool := range scopedDurableTools(nil, Scope{}) {
		name := tool.Tool.Name()
		durableNames[name] = true
		if strings.Contains(name, "delete") {
			t.Fatalf("durable catalog unexpectedly grants delete tool %q", name)
		}
		if tool.Tool.Effect() != "reconcilable" {
			t.Fatalf("durable tool %q effect = %q", name, tool.Tool.Effect())
		}
	}
	wantDurable := map[string]bool{
		createFolderToolName:       true,
		renameFolderToolName:       true,
		renameAssetToolName:        true,
		moveAssetsToolName:         true,
		requestWorkflowRunToolName: true,
	}
	if !reflect.DeepEqual(durableNames, wantDurable) {
		t.Fatalf("durable catalog = %#v, want %#v", durableNames, wantDurable)
	}
}

func TestGlobalToolCatalogContainsProductWorkspaceCreatorAndWorkflowRunner(t *testing.T) {
	tools := scopedGlobalDurableTools(nil, Scope{ConversationID: testConversationID})
	if len(tools) != 2 || tools[0].Tool.Name() != createProductWorkspaceToolName || tools[1].Tool.Name() != requestWorkflowRunToolName {
		t.Fatalf("global durable tools = %#v", tools)
	}
	for _, tool := range tools {
		if tool.Tool.Effect() != durable.EffectReconcilable {
			t.Fatalf("global durable tool %q effect = %q", tool.Tool.Name(), tool.Tool.Effect())
		}
	}
}

func TestGlobalWorkflowRunRequestToolUsesExplicitTargetAndConfirmationRequest(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID
	productID := "77777777-7777-4777-8777-777777777777"
	workflowID := "88888888-8888-4888-8888-888888888888"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testInternalToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case basePath + "/global-workflow-run-requests/prepare":
			if request.Method != http.MethodPost {
				t.Fatalf("global workflow prepare method = %s", request.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["product_id"] != productID || body["workflow_id"] != workflowID || body["expected_workflow_revision"] != float64(7) {
				t.Fatalf("global workflow prepare body = %#v", body)
			}
			writeFixtureJSON(writer, map[string]any{
				"product_id": productID, "workflow_id": workflowID, "workflow_title": "主图工作流",
				"workflow_revision": 7, "runnable_node_count": 3, "task_id": "99999999-9999-4999-8999-999999999999",
			})
		case basePath + "/global-workflow-run-requests":
			if request.Method != http.MethodPost || request.Header.Get("Idempotency-Key") != "global-run-key" {
				t.Fatalf("global workflow execute request = %s %s", request.Method, request.URL.String())
			}
			writeFixtureJSON(writer, map[string]any{
				"id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "product_id": productID, "workflow_id": workflowID,
				"status": "awaiting_confirmation",
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var tool *requestGlobalWorkflowRunTool
	for _, candidate := range scopedGlobalDurableTools(client, Scope{
		ConversationID: testConversationID,
		ScopeType:      scopeTypeGlobal,
		TaskID:         "99999999-9999-4999-8999-999999999999",
	}) {
		if candidate.Tool.Name() == requestWorkflowRunToolName {
			tool, _ = candidate.Tool.(*requestGlobalWorkflowRunTool)
			break
		}
	}
	if tool == nil {
		t.Fatalf("global workflow run tool is not registered")
	}
	prepared, err := tool.Prepare(context.Background(), json.RawMessage(`{"product_id":"`+productID+`","workflow_id":"`+workflowID+`","expected_workflow_revision":7}`))
	if err != nil || !strings.Contains(string(prepared), workflowID) {
		t.Fatalf("global workflow prepared = %s, %v", prepared, err)
	}
	result, err := tool.Execute(context.Background(), durable.Invocation{
		Prepared:       prepared,
		IdempotencyKey: "global-run-key",
		StepID:         "global-run-step",
	})
	if err != nil || !strings.Contains(string(result), "awaiting_confirmation") {
		t.Fatalf("global workflow execute result = %s, %v", result, err)
	}
}

func TestCreateProductWorkspaceToolUsesGlobalConversationAndIdempotency(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID + "/product-workspaces"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testInternalToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != basePath {
			t.Fatalf("product workspace request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Idempotency-Key") != "product-workspace-key" {
			t.Fatalf("idempotency key = %q", request.Header.Get("Idempotency-Key"))
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "春季商品" {
			t.Fatalf("request body = %#v", body)
		}
		writeFixtureJSON(writer, map[string]any{
			"schema_version":          1,
			"created":                 true,
			"session_id":              "11111111-1111-4111-8111-111111111111",
			"global_conversation_id":  testConversationID,
			"product_conversation_id": "22222222-2222-4222-8222-222222222222",
			"product_id":              testProductID,
			"product_name":            "春季商品",
			"workflow_draft_id":       "33333333-3333-4333-8333-333333333333",
			"task_id":                 "44444444-4444-4444-8444-444444444444",
			"intake_finalized":        false,
			"navigation_path":         "/products/new?workspace=22222222-2222-4222-8222-222222222222&agent_task_id=44444444-4444-4444-8444-444444444444",
		})
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	tool := &createProductWorkspaceTool{
		client: client,
		scope:  Scope{ConversationID: testConversationID, ScopeType: scopeTypeGlobal},
	}
	prepared, err := tool.Prepare(context.Background(), json.RawMessage(`{"name":" 春季商品 "}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(prepared) != `{"name":"春季商品"}` {
		t.Fatalf("prepared = %s", prepared)
	}
	result, err := tool.Execute(context.Background(), durable.Invocation{
		Prepared:       prepared,
		IdempotencyKey: "product-workspace-key",
	})
	if err != nil || !strings.Contains(string(result), "22222222-2222-4222-8222-222222222222") {
		t.Fatalf("execute result = %s, %v", result, err)
	}
}

func TestWorkflowRunReadToolUsesBoundedProductFlowEndpoint(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID + "/workflow-runs"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != basePath || request.URL.Query().Get("limit") != "5" {
			t.Fatalf("workflow run request = %s %s", request.Method, request.URL.String())
		}
		writeFixtureJSON(writer, map[string]any{
			"workflow_id":       "55555555-5555-4555-8555-555555555555",
			"workflow_revision": 7,
			"items": []map[string]any{{
				"id": "66666666-6666-4666-8666-666666666666", "status": "running",
				"node_runs": []map[string]any{{"status": "running"}},
			}},
		})
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var tool *agenttask.Tool
	for _, candidate := range scopedReadTools(client, Scope{ConversationID: testConversationID}) {
		if candidate.Name == inspectWorkflowRunsToolName {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatalf("tool %q is not registered", inspectWorkflowRunsToolName)
	}
	result, err := tool.Handler(context.Background(), json.RawMessage(`{"limit":5}`))
	if err != nil || !strings.Contains(result, "66666666-6666-4666-8666-666666666666") {
		t.Fatalf("workflow run result = %q, %v", result, err)
	}
	if _, err := tool.Handler(context.Background(), json.RawMessage(`{"limit":21}`)); err == nil {
		t.Fatal("unbounded workflow run inspection was accepted")
	}
}

func TestGlobalProductToolsUseBoundedProductFlowEndpoints(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		switch request.URL.Path {
		case basePath + "/products":
			if request.Method != http.MethodGet || request.URL.Query().Get("query") != "春季" ||
				request.URL.Query().Get("cursor") != "cursor-1" || request.URL.Query().Get("limit") != "10" {
				t.Fatalf("global product list request = %s %s", request.Method, request.URL.String())
			}
			writeFixtureJSON(writer, map[string]any{
				"items": []map[string]any{{
					"id": "77777777-7777-4777-8777-777777777777", "name": "春季商品", "category": "收纳",
					"updated_at": "2026-08-18T00:00:00Z",
					"active_workflow": map[string]any{
						"id": "88888888-8888-4888-8888-888888888888", "title": "主图工作流",
						"revision": 4, "edit_version": 2, "node_count": 3,
					},
				}},
				"next_cursor": "cursor-2",
			})
		case basePath + "/products/inspect":
			if request.Method != http.MethodPost {
				t.Fatalf("global product inspect method = %s", request.Method)
			}
			var body struct {
				ProductIDs []string `json:"product_ids"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body.ProductIDs, []string{testProductID}) {
				t.Fatalf("global product inspect body = %#v", body)
			}
			writeFixtureJSON(writer, map[string]any{
				"items": []map[string]any{{
					"id": testProductID, "name": "商品 A", "category": nil,
					"updated_at": "2026-08-18T00:00:00Z", "active_workflow": nil,
				}},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(name, arguments string) (string, error) {
		t.Helper()
		for _, tool := range scopedGlobalReadTools(client, Scope{ConversationID: testConversationID}) {
			if tool.Name == name {
				return tool.Handler(context.Background(), json.RawMessage(arguments))
			}
		}
		t.Fatalf("tool %q is not registered", name)
		return "", nil
	}

	listed, err := invoke(
		listGlobalProductsToolName,
		`{"query":"春季","cursor":"cursor-1","limit":10}`,
	)
	if err != nil || !strings.Contains(listed, "主图工作流") || strings.Contains(listed, "image_url") {
		t.Fatalf("global product list result = %q, %v", listed, err)
	}
	inspected, err := invoke(
		inspectGlobalProductsToolName,
		`{"product_ids":["`+testProductID+`"]}`,
	)
	if err != nil || !strings.Contains(inspected, testProductID) {
		t.Fatalf("global product inspect result = %q, %v", inspected, err)
	}
	if _, err := invoke(
		listGlobalProductsToolName,
		`{"query":"","cursor":"","limit":101}`,
	); err == nil {
		t.Fatal("unbounded global product list was accepted")
	}
	if _, err := invoke(
		inspectGlobalProductsToolName,
		`{"product_ids":["a","b","c","d","e","f","g","h","i","j","k","l","m","n","o","p","q","r","s","t","u"]}`,
	); err == nil {
		t.Fatal("unbounded global product inspection was accepted")
	}
	if requestCount != 2 {
		t.Fatalf("ProductFlow request count = %d, want 2", requestCount)
	}
}

func TestGlobalWorkflowContextToolUsesExplicitProductScope(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID
	productID := "77777777-7777-4777-8777-777777777777"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != basePath+"/global-workflow-context" ||
			request.URL.Query().Get("product_id") != productID {
			t.Fatalf("global workflow context request = %s %s", request.Method, request.URL.String())
		}
		writeFixtureJSON(writer, map[string]any{
			"target": map[string]any{
				"product_id": productID, "workflow_draft_id": "88888888-8888-4888-8888-888888888888",
			},
			"workflow_draft": map[string]any{"version": 3, "status": "draft"},
		})
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var tool *agenttask.Tool
	for _, candidate := range scopedGlobalReadTools(client, Scope{ConversationID: testConversationID}) {
		if candidate.Name == inspectGlobalWorkflowContextToolName {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatalf("tool %q is not registered", inspectGlobalWorkflowContextToolName)
	}
	result, err := tool.Handler(
		context.Background(),
		json.RawMessage(`{"product_id":"`+productID+`"}`),
	)
	if err != nil || !strings.Contains(result, productID) || !strings.Contains(result, `"version":3`) {
		t.Fatalf("global workflow context result = %q, %v", result, err)
	}
	if _, err := tool.Handler(context.Background(), json.RawMessage(`{"product_id":""}`)); err == nil {
		t.Fatal("empty product scope was accepted")
	}
}

func TestGlobalWorkflowRunToolUsesBoundedProductFlowEndpoint(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID
	workflowID := "88888888-8888-4888-8888-888888888888"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != basePath+"/workflow-runs/inspect" {
			t.Fatalf("global workflow run request = %s %s", request.Method, request.URL.String())
		}
		var body struct {
			WorkflowIDs []string `json:"workflow_ids"`
			Limit       int      `json:"limit"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(body.WorkflowIDs, []string{workflowID}) || body.Limit != 3 {
			t.Fatalf("global workflow run body = %#v", body)
		}
		writeFixtureJSON(writer, map[string]any{
			"items": []map[string]any{{
				"product_id": testProductID, "product_name": "商品 A", "workflow_id": workflowID,
				"workflow_title": "主图工作流", "workflow_revision": 7, "active": true,
				"runs": []map[string]any{{
					"id": "99999999-9999-4999-8999-999999999999", "status": "running",
					"failure_reason": nil, "started_at": "2026-08-18T00:00:00Z", "finished_at": nil,
					"node_status_counts": map[string]int{"running": 1, "queued": 2},
				}},
			}},
		})
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var tool *agenttask.Tool
	for _, candidate := range scopedGlobalReadTools(client, Scope{ConversationID: testConversationID}) {
		if candidate.Name == inspectGlobalWorkflowRunsToolName {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatalf("tool %q is not registered", inspectGlobalWorkflowRunsToolName)
	}
	result, err := tool.Handler(
		context.Background(),
		json.RawMessage(`{"workflow_ids":["`+workflowID+`"],"limit":3}`),
	)
	if err != nil || !strings.Contains(result, "running") || strings.Contains(result, "node_config") {
		t.Fatalf("global workflow run result = %q, %v", result, err)
	}
	if _, err := tool.Handler(context.Background(), json.RawMessage(`{"workflow_ids":["a"],"limit":11}`)); err == nil {
		t.Fatal("unbounded global workflow run inspection was accepted")
	}
}

func TestLegacyArchiveReadToolsUseBoundedProductFlowEndpoints(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testInternalToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		requestCount++
		switch request.URL.Path {
		case basePath + "/legacy-archives":
			if request.Method != http.MethodGet || request.URL.Query().Get("kind") != "workflow" ||
				request.URL.Query().Get("query") != "主图" || request.URL.Query().Get("after") != "cursor-1" ||
				request.URL.Query().Get("limit") != "25" {
				t.Fatalf("legacy archive list request = %s %s", request.Method, request.URL.String())
			}
			writeFixtureJSON(writer, map[string]any{
				"schema_version": 1,
				"items": []map[string]any{{
					"kind": "workflow", "id": "archive-1", "title": "主图工作流",
				}},
				"next_cursor": nil,
			})
		case basePath + "/legacy-archives/inspect":
			if request.Method != http.MethodPost {
				t.Fatalf("legacy archive inspect method = %s", request.Method)
			}
			var body struct {
				Kind      string `json:"kind"`
				ArchiveID string `json:"archive_id"`
				Section   string `json:"section"`
				Offset    int    `json:"offset"`
				Limit     int    `json:"limit"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Kind != "workflow" || body.ArchiveID != "archive-1" || body.Section != "assets" ||
				body.Offset != 10 || body.Limit != 10 {
				t.Fatalf("legacy archive inspect body = %#v", body)
			}
			writeFixtureJSON(writer, map[string]any{
				"schema_version": 1, "archive_kind": "workflow", "archive_id": "archive-1",
				"section": "assets", "offset": 10, "limit": 10, "total": 11,
				"items": []map[string]any{{
					"product_image_asset_id": testAssetID, "mime_type": "image/png", "byte_size": 68,
				}},
				"has_more": false,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(name, arguments string) (string, error) {
		t.Helper()
		for _, tool := range scopedReadTools(client, Scope{ConversationID: testConversationID}) {
			if tool.Name == name {
				return tool.Handler(context.Background(), json.RawMessage(arguments))
			}
		}
		t.Fatalf("tool %q is not registered", name)
		return "", nil
	}

	listed, err := invoke(
		listLegacyArchivesToolName,
		`{"kind":"workflow","query":"主图","after":"cursor-1","limit":25}`,
	)
	if err != nil || !strings.Contains(listed, `"archive-1"`) || strings.Contains(listed, "payload") {
		t.Fatalf("legacy archive list result = %q, %v", listed, err)
	}
	inspected, err := invoke(
		inspectLegacyArchiveToolName,
		`{"kind":"workflow","archive_id":"archive-1","section":"assets","offset":10,"limit":10}`,
	)
	if err != nil || !strings.Contains(inspected, testAssetID) || strings.Contains(inspected, "image_url") {
		t.Fatalf("legacy archive inspect result = %q, %v", inspected, err)
	}
	if _, err := invoke(
		listLegacyArchivesToolName,
		`{"kind":"workflow","query":"","after":"","limit":51}`,
	); err == nil {
		t.Fatal("unbounded legacy archive list was accepted")
	}
	if _, err := invoke(
		inspectLegacyArchiveToolName,
		`{"kind":"workflow","archive_id":"archive-1","section":"nodes","offset":0,"limit":11}`,
	); err == nil {
		t.Fatal("unbounded legacy archive inspection was accepted")
	}
	if requestCount != 2 {
		t.Fatalf("ProductFlow request count = %d, want 2", requestCount)
	}
}

func TestDurableExecutionResultClassifiesHTTPOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantError error
	}{
		{name: "conflict", err: &productflow.HTTPError{StatusCode: http.StatusConflict}, wantError: durable.ErrConflict},
		{name: "validation", err: &productflow.HTTPError{StatusCode: http.StatusUnprocessableEntity}},
		{name: "server error", err: &productflow.HTTPError{StatusCode: http.StatusServiceUnavailable}, wantError: durable.ErrOutcomeUnknown},
		{name: "transport error", err: errors.New("connection reset"), wantError: durable.ErrOutcomeUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := durableExecutionResult(nil, test.err, "test mutation")
			if test.wantError == nil {
				if err == nil || errors.Is(err, durable.ErrConflict) || errors.Is(err, durable.ErrOutcomeUnknown) {
					t.Fatalf("validation error classification = %v", err)
				}
				return
			}
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want errors.Is(..., %v)", err, test.wantError)
			}
		})
	}

	want := json.RawMessage(`{"applied":true}`)
	got, err := durableExecutionResult(want, nil, "test mutation")
	if err != nil || string(got) != string(want) {
		t.Fatalf("successful result = %s, %v", got, err)
	}
}

func TestMoveAssetsToolReconcilesUnknownHTTPOutcome(t *testing.T) {
	basePath := "/api/internal/v1/agent-conversations/" + testConversationID + "/asset-moves"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testInternalToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case basePath + "/prepare":
			writeFixtureJSON(writer, map[string]any{
				"moves":            []map[string]any{{"asset_id": testAssetID, "expected_folder_id": nil}},
				"target_folder_id": "55555555-5555-4555-8555-555555555555",
			})
		case basePath:
			if request.Header.Get("Idempotency-Key") != "move-key" {
				t.Fatalf("execute idempotency key = %q", request.Header.Get("Idempotency-Key"))
			}
			http.Error(writer, "outcome unavailable", http.StatusServiceUnavailable)
		case basePath + "/reconcile":
			if request.Header.Get("Idempotency-Key") != "move-key" {
				t.Fatalf("reconcile idempotency key = %q", request.Header.Get("Idempotency-Key"))
			}
			writeFixtureJSON(writer, map[string]any{
				"state": "applied",
				"result": map[string]any{
					"asset_ids": []string{testAssetID},
					"folder_id": "55555555-5555-4555-8555-555555555555",
					"applied":   true,
				},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := productflow.NewClient(server.URL, testInternalToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	tool := &moveAssetsTool{client: client, scope: Scope{ConversationID: testConversationID}}
	prepared, err := tool.Prepare(
		context.Background(),
		json.RawMessage(`{"asset_ids":["`+testAssetID+`"],"folder_id":"55555555-5555-4555-8555-555555555555"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	invocation := durable.Invocation{Prepared: prepared, IdempotencyKey: "move-key"}
	if _, err := tool.Execute(context.Background(), invocation); !errors.Is(err, durable.ErrOutcomeUnknown) {
		t.Fatalf("execute error = %v, want ErrOutcomeUnknown", err)
	}
	reconciled, err := tool.Reconcile(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State != durable.ReconcileApplied || !strings.Contains(string(reconciled.Result), testAssetID) {
		t.Fatalf("reconciled = %#v", reconciled)
	}
}
