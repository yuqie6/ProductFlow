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
		productContextToolName: true,
		listAssetsToolName:     true,
		inspectAssetsToolName:  true,
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
	}
	if !reflect.DeepEqual(durableNames, wantDurable) {
		t.Fatalf("durable catalog = %#v, want %#v", durableNames, wantDurable)
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
