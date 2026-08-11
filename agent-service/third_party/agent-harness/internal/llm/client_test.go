package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
)

type capturedRequest struct {
	Model       string              `json:"model"`
	Input       []json.RawMessage   `json:"input"`
	Tools       []json.RawMessage   `json:"tools"`
	Reasoning   *responsesReasoning `json:"reasoning"`
	Text        *responsesText      `json:"text"`
	ServiceTier string              `json:"service_tier"`
	Store       *bool               `json:"store"`
	Background  *bool               `json:"background"`
	Stream      *bool               `json:"stream"`
}

func TestClientChatUsesResponsesWireFormat(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %q, want /responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var request capturedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_1",
			"status":"completed",
			"output":[
				{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"},
				{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"完成","annotations":[]}]}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	client := New("test-key", server.URL, "test-model")
	client.HTTP = server.Client()
	client.ReasoningEffort = "max"
	client.ReasoningSummary = "detailed"
	client.TextVerbosity = "low"
	client.ServiceTier = "default"
	system := "system"
	user := "hello"
	message, status, err := client.Chat(context.Background(), []Message{
		{Role: "system", Content: &system},
		{Role: "user", Content: &user},
	}, []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "read",
			Parameters:  map[string]any{"type": "object"},
		},
	}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status = %q", status)
	}
	if got := message.String(); got != "完成" {
		t.Fatalf("content = %q", got)
	}
	if len(message.ResponseItems) != 2 {
		t.Fatalf("ResponseItems = %d, want 2", len(message.ResponseItems))
	}

	request := <-requests
	if request.Model != "test-model" || len(request.Input) != 2 || len(request.Tools) != 1 {
		t.Fatalf("unexpected request: %+v", request)
	}
	if request.Store == nil || *request.Store {
		t.Fatalf("store = %v, want false", request.Store)
	}
	if request.Reasoning == nil || request.Reasoning.Effort != "max" || request.Reasoning.Summary != "detailed" {
		t.Fatalf("reasoning = %#v", request.Reasoning)
	}
	if request.Text == nil || request.Text.Verbosity != "low" || request.ServiceTier != "default" {
		t.Fatalf("text = %#v, service tier = %q", request.Text, request.ServiceTier)
	}
	var tool map[string]any
	if err := json.Unmarshal(request.Tools[0], &tool); err != nil {
		t.Fatal(err)
	}
	if tool["name"] != "read_file" || tool["type"] != "function" {
		t.Fatalf("tool = %#v", tool)
	}
	if _, nested := tool["function"]; nested {
		t.Fatalf("Responses tool must be flat: %#v", tool)
	}
}

func TestMessagesToInputEncodesNativeMultimodalPartsAndStrictTool(t *testing.T) {
	input, err := messagesToInput([]Message{{
		Role: "user",
		ContentParts: []ContentPart{
			{Type: "input_text", Text: "inspect"},
			{Type: "input_image", ImageURL: "https://cdn.example/a.png", MediaType: "image/png", SizeBytes: 10, CheckpointMode: "reference", Detail: "high"},
			{Type: "input_image", EmbeddedData: []byte("png"), MediaType: "image/png", SizeBytes: 3, CheckpointMode: "embed"},
		},
	}})
	if err != nil || len(input) != 1 {
		t.Fatalf("input = %#v, err = %v", input, err)
	}
	var message struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL string `json:"image_url"`
			Detail   string `json:"detail"`
		} `json:"content"`
	}
	if err := json.Unmarshal(input[0], &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 3 || message.Content[0].Type != "input_text" ||
		message.Content[1].ImageURL != "https://cdn.example/a.png" || message.Content[1].Detail != "high" ||
		message.Content[2].ImageURL != "data:image/png;base64,cG5n" {
		t.Fatalf("wire message = %#v", message)
	}
	tools := responseTools([]Tool{{Function: ToolFunction{Name: "artifact", Strict: true}}})
	if len(tools) != 1 || !tools[0].Strict {
		t.Fatalf("wire tools = %#v", tools)
	}
}

func TestMessagesToInputEncodesNativeToolResultWithoutChangingLegacyStrings(t *testing.T) {
	legacy := "legacy output"
	input, err := messagesToInput([]Message{
		{Role: "tool", ToolCallID: "legacy", Content: &legacy},
		{Role: "tool", ToolCallID: "multimodal", ContentParts: []ContentPart{
			{Type: "input_text", Text: "selected image"},
			{Type: "input_image", ImageURL: "https://cdn.example/gallery.webp", Detail: "low"},
			{Type: "input_image", EmbeddedData: []byte("png"), MediaType: "image/png"},
		}},
	})
	if err != nil || len(input) != 2 {
		t.Fatalf("input = %#v, err = %v", input, err)
	}
	var legacyOutput struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(input[0], &legacyOutput); err != nil {
		t.Fatal(err)
	}
	if legacyOutput.Type != "function_call_output" || legacyOutput.CallID != "legacy" || legacyOutput.Output != legacy {
		t.Fatalf("legacy output = %#v", legacyOutput)
	}
	var multimodalOutput struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Output []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL string `json:"image_url"`
			Detail   string `json:"detail"`
		} `json:"output"`
	}
	if err := json.Unmarshal(input[1], &multimodalOutput); err != nil {
		t.Fatal(err)
	}
	if multimodalOutput.Type != "function_call_output" || multimodalOutput.CallID != "multimodal" ||
		len(multimodalOutput.Output) != 3 || multimodalOutput.Output[0].Text != "selected image" ||
		multimodalOutput.Output[1].ImageURL != "https://cdn.example/gallery.webp" || multimodalOutput.Output[1].Detail != "low" ||
		multimodalOutput.Output[2].ImageURL != "data:image/png;base64,cG5n" {
		t.Fatalf("multimodal output = %#v", multimodalOutput)
	}
}

func TestClientOmitsDisabledOptionalResponsesSettings(t *testing.T) {
	client := New("test-key", "https://provider.example", "test-model")
	client.ReasoningSummary = "none"
	if got := client.reasoningConfig(); got != nil {
		t.Fatalf("reasoning config = %#v, want nil", got)
	}
	if got := client.textConfig(); got != nil {
		t.Fatalf("text config = %#v, want nil", got)
	}
}

func TestClientChatClassifiesTransportAmbiguity(t *testing.T) {
	for _, test := range []struct {
		name      string
		wrote     bool
		ambiguous bool
	}{
		{name: "dial failure is definite"},
		{name: "failure after headers is ambiguous", wrote: true, ambiguous: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := New("key", "https://provider.invalid", "model")
			client.HTTP = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if test.wrote {
					httptrace.ContextClientTrace(request.Context()).WroteHeaders()
				}
				return nil, errors.New("connection lost")
			})}
			user := "work"
			_, _, err := client.Chat(t.Context(), []Message{{Role: "user", Content: &user}}, nil)
			if err == nil || IsAmbiguousRequest(err) != test.ambiguous {
				t.Fatalf("err = %v, ambiguous = %v", err, IsAmbiguousRequest(err))
			}
		})
	}
}

func TestMessageJSONPreservesResponseItems(t *testing.T) {
	message := Message{
		Role: "assistant",
		ResponseItems: []json.RawMessage{
			json.RawMessage(`{"type":"reasoning","encrypted_content":"opaque"}`),
			json.RawMessage(`{"type":"message","content":[]}`),
		},
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Message
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.ResponseItems) != 2 || string(decoded.ResponseItems[0]) != string(message.ResponseItems[0]) {
		t.Fatalf("decoded response items = %s", decoded.ResponseItems)
	}
}

func TestClientChatStreamEmitsDeltasAndReturnsCompleteMessage(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request capturedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"流\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"式\"}\n\n")
		_, _ = fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"resp_stream","status":"completed","output":[{"id":"msg_stream","type":"message","role":"assistant","content":[{"type":"output_text","text":"流式"}]}]}}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	client := New("test-key", server.URL, "test-model")
	client.HTTP = server.Client()
	user := "hello"
	var deltas strings.Builder
	message, status, err := client.ChatStream(context.Background(), []Message{{Role: "user", Content: &user}}, nil, func(delta string) {
		deltas.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if status != "completed" || message.String() != "流式" || deltas.String() != "流式" {
		t.Fatalf("status = %q, message = %q, deltas = %q", status, message.String(), deltas.String())
	}
	if len(message.ResponseItems) != 1 {
		t.Fatalf("ResponseItems = %d, want 1", len(message.ResponseItems))
	}
	request := <-requests
	if request.Stream == nil || !*request.Stream {
		t.Fatalf("stream = %v, want true", request.Stream)
	}
}

func TestClientChatStreamDurableClassifiesDeltaSinkFailureAsAmbiguous(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		_, _ = fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"resp_stream","status":"completed","output":[{"id":"msg_stream","type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]}}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	client := New("test-key", server.URL, "test-model")
	client.HTTP = server.Client()
	user := "hello"
	want := errors.New("event store unavailable")
	_, _, err := client.ChatStreamDurable(
		t.Context(), []Message{{Role: "user", Content: &user}}, nil,
		func(string) error { return want },
	)
	if !errors.Is(err, want) || !IsAmbiguousRequest(err) {
		t.Fatalf("ChatStreamDurable error = %v, ambiguous = %t", err, IsAmbiguousRequest(err))
	}
}

func TestReadResponseStreamSurfacesErrorEvent(t *testing.T) {
	_, status, err := readResponseStream(strings.NewReader("data: {\"type\":\"error\",\"code\":\"bad_request\",\"message\":\"boom\"}\n\n"), nil)
	if err == nil || !strings.Contains(err.Error(), "boom") || status != "failed" {
		t.Fatalf("status = %q, err = %v", status, err)
	}
}

func TestReadResponseStreamRejectsIncompleteResponse(t *testing.T) {
	stream := `data: {"type":"response.incomplete","response":{"id":"resp_incomplete","status":"incomplete","output":[{"id":"msg_partial","type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]}}` + "\n\n"
	_, status, err := readResponseStream(strings.NewReader(stream), nil)
	if err == nil || !strings.Contains(err.Error(), "未完整结束") || status != "incomplete" {
		t.Fatalf("status = %q, err = %v", status, err)
	}
}

func TestClientChatReplaysOutputItemsAndToolResult(t *testing.T) {
	requests := make(chan capturedRequest, 2)
	requestNumber := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request capturedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- request
		requestNumber++
		w.Header().Set("Content-Type", "application/json")
		if requestNumber == 1 {
			_, _ = w.Write([]byte(`{
				"id":"resp_1",
				"status":"completed",
				"output":[
					{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"},
					{"id":"fc_1","type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"printf ok\"}","status":"completed"}
				]
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":"resp_2",
			"status":"completed",
			"output":[{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]
		}`))
	}))
	t.Cleanup(server.Close)

	client := New("test-key", server.URL, "test-model")
	client.HTTP = server.Client()
	user := "run"
	history := []Message{{Role: "user", Content: &user}}
	first, _, err := client.Chat(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool calls = %#v", first.ToolCalls)
	}
	var arguments string
	if err := json.Unmarshal(first.ToolCalls[0].Function.Arguments, &arguments); err != nil {
		t.Fatalf("arguments: %v", err)
	}
	if arguments != `{"command":"printf ok"}` {
		t.Fatalf("arguments = %q", arguments)
	}

	toolOutput := "ok"
	history = append(history, first, Message{Role: "tool", ToolCallID: "call_1", Content: &toolOutput})
	second, _, err := client.Chat(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	if second.String() != "ok" {
		t.Fatalf("content = %q", second.String())
	}

	<-requests
	secondRequest := <-requests
	if len(secondRequest.Input) != 4 {
		t.Fatalf("second input items = %d, want 4", len(secondRequest.Input))
	}
	wantTypes := []string{"", "reasoning", "function_call", "function_call_output"}
	for i, raw := range secondRequest.Input {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			t.Fatal(err)
		}
		if header.Type != wantTypes[i] {
			t.Fatalf("input[%d].type = %q, want %q", i, header.Type, wantTypes[i])
		}
	}
}
