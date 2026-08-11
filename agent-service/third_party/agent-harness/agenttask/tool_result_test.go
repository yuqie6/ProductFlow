package agenttask_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/durable"
)

func TestMultimodalToolResultSurvivesDurableRestart(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var requests []serviceWireRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var captured serviceWireRequest
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, captured)
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			_, _ = writer.Write([]byte(`{"id":"gallery","status":"completed","output":[{"id":"gallery_call","type":"function_call","call_id":"inspect","name":"inspect_gallery_images","arguments":"{}"}]}`))
		case 2:
			_, _ = writer.Write([]byte(`{"id":"complete","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"gallery complete"}]}]}`))
		default:
			t.Errorf("unexpected provider call %d", calls.Load())
			http.Error(writer, "too many calls", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	png := servicePNG(t)
	database := filepath.Join(t.TempDir(), "tool-result.db")
	workspace := t.TempDir()
	config := agenttask.Config{
		Database: database, Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
		Tools: []agenttask.Tool{{
			Name: "inspect_gallery_images", Description: "Return selected gallery images.",
			ResultHandler: func(context.Context, json.RawMessage) (agenttask.ToolResult, error) {
				return agenttask.ToolResult{
					SchemaVersion: agenttask.ToolResultSchemaVersion,
					Content: []agenttask.ToolResultContent{
						{Type: agenttask.ContentInputText, Text: "two selected images"},
						{Type: agenttask.ContentInputImage, Image: &agenttask.InputImage{
							Data: png, MediaType: "image/png", SizeBytes: int64(len(png)),
							CheckpointMode: agenttask.ImageCheckpointEmbed,
						}},
						{Type: agenttask.ContentInputImage, Image: &agenttask.InputImage{
							URL: "https://cdn.example/gallery.webp", MediaType: "image/webp", SizeBytes: 42,
							CheckpointMode: agenttask.ImageCheckpointReference,
						}},
					},
				}, nil
			},
		}},
	}
	first, err := agenttask.Open(config)
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "inspect gallery")
	if err != nil {
		t.Fatal(err)
	}
	var boundary agenttask.AdvanceResult
	for range 3 {
		boundary, err = first.AdvanceOne(t.Context(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if boundary.Job.Status != durable.JobAwaitingSteps || len(boundary.Job.Steps) != 2 || boundary.Job.Steps[1].Status != durable.StepSucceeded {
		t.Fatalf("tool boundary = %#v", boundary)
	}
	toolResult := boundary.Job.Steps[1].Result
	if !bytes.Contains(toolResult, []byte(`"content_parts"`)) ||
		!bytes.Contains(toolResult, []byte("https://cdn.example/gallery.webp")) ||
		!bytes.Contains(toolResult, []byte(base64.StdEncoding.EncodeToString(png))) {
		t.Fatalf("durable tool result = %s", toolResult)
	}
	if bytes.Contains(toolResult, []byte("data:image/png;base64,")) {
		t.Fatalf("durable result persisted provider wire data URL: %s", toolResult)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := agenttask.Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	result, err := resumed.Resume(t.Context(), job.ID)
	if err != nil || result.Output != "gallery complete" || calls.Load() != 2 {
		t.Fatalf("result = %#v, calls = %d, err = %v", result, calls.Load(), err)
	}
	mu.Lock()
	captured := append([]serviceWireRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("requests = %#v", captured)
	}
	var output []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
	}
	for _, raw := range captured[1].Input {
		var item struct {
			Type   string          `json:"type"`
			CallID string          `json:"call_id"`
			Output json.RawMessage `json:"output"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Type == "function_call_output" && item.CallID == "inspect" {
			if err := json.Unmarshal(item.Output, &output); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(output) != 3 || output[0].Text != "two selected images" ||
		!strings.HasPrefix(output[1].ImageURL, "data:image/png;base64,") || output[2].ImageURL != "https://cdn.example/gallery.webp" {
		t.Fatalf("replayed tool output = %#v", output)
	}
}
