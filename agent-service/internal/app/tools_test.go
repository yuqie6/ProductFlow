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
		createFolderToolName: true,
		renameFolderToolName: true,
		renameAssetToolName:  true,
		moveAssetsToolName:   true,
		requestWorkflowRunToolName: true,
	}
	if !reflect.DeepEqual(durableNames, wantDurable) {
		t.Fatalf("durable catalog = %#v, want %#v", durableNames, wantDurable)
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
