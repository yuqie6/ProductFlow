package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/prompts"
)

func TestEndpointStripsTrailingV1(t *testing.T) {
	got := endpoint("https://anyrouter.top/v1", "/v1/chat/completions")
	want := "https://anyrouter.top/v1/chat/completions"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if endpoint("https://sub.devbin.de", "/v1/images/generations") != "https://sub.devbin.de/v1/images/generations" {
		t.Fatal(endpoint("https://sub.devbin.de", "/v1/images/generations"))
	}
}

func TestPromptSuccessFailUnknown(t *testing.T) {
	okPayload := `{"goal":"展示商品","key_messages":["主体"],"required_elements":[],"prohibitions":[],"fact_gaps":[]}`
	cases := []struct {
		name   string
		status int
		body   string
		hang   bool
		wantU  bool
		wantOK bool
	}{
		{
			name: "success", status: 200,
			body:   `{"id":"r1","model":"m","output_parsed":` + okPayload + `}`,
			wantOK: true,
		},
		{name: "fail-400", status: 400, body: `{"error":{"message":"bad"}}`},
		{name: "unknown-500", status: 500, body: `oops`, wantU: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/responses" {
					t.Errorf("path %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			p := OpenAIPrompt{APIKey: "sk", BaseURL: srv.URL, Model: "gpt"}
			got, err := p.GenerateCreativeBrief(context.Background(), graph.PromptRequest{NodeTitle: "t"})
			if tc.wantOK {
				if err != nil {
					t.Fatal(err)
				}
				if got.Payload["goal"] != "展示商品" {
					t.Fatalf("payload %+v", got.Payload)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantU != errors.Is(err, graph.ErrProviderUnknown()) && tc.wantU != isUnknown(err) {
				t.Fatalf("unknown=%v err=%v", tc.wantU, err)
			}
			if tc.wantU && !isUnknown(err) {
				t.Fatalf("want unknown, got %v", err)
			}
			if !tc.wantU && isUnknown(err) {
				t.Fatalf("want fail, got unknown %v", err)
			}
		})
	}
}

func TestImageSuccessFailUnknown(t *testing.T) {
	pngB64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	okBody, _ := json.Marshal(map[string]any{
		"id": "img1", "model": "dall-e-3",
		"data": []map[string]any{{"b64_json": pngB64}},
	})
	cases := []struct {
		name   string
		status int
		body   string
		wantU  bool
		wantOK bool
	}{
		{name: "success", status: 200, body: string(okBody), wantOK: true},
		{name: "fail-400", status: 400, body: `{"error":{"message":"bad request"}}`},
		{name: "unknown-502", status: 502, body: `bad gateway`, wantU: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
			got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "hero"})
			if tc.wantOK {
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Bytes) == 0 || !strings.HasPrefix(got.MIME, "image/") {
					t.Fatalf("image %+v", got)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantU && !isUnknown(err) {
				t.Fatalf("want unknown, got %v", err)
			}
			if !tc.wantU && isUnknown(err) {
				t.Fatalf("want fail, got unknown %v", err)
			}
		})
	}
}

func TestChatFourHundredIsFailedNotUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"policy"}}`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Mode: ModeChat})
	if err == nil {
		t.Fatal("expected error")
	}
	if isUnknown(err) {
		t.Fatalf("400 should fail, got unknown: %v", err)
	}
}

func onePixelPNGB64() string {
	return "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
}

func TestResponsesImagePollsUntilResult(t *testing.T) {
	responsesPollInterval = time.Millisecond
	t.Cleanup(func() { responsesPollInterval = 2 * time.Second })

	completed, _ := json.Marshal(map[string]any{
		"id": "resp-1", "status": "completed", "model": "gpt-image",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	creates := 0
	retrieves := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/responses"):
			creates++
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"id":"resp-1","status":"in_progress","output":[{"type":"image_generation_call","status":"generating","result":null}]}`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v1/responses/resp-1"):
			retrieves++
			w.WriteHeader(200)
			_, _ = w.Write(completed)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "gpt-5.6-luna"}}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if creates != 1 || retrieves < 1 {
		t.Fatalf("creates=%d retrieves=%d", creates, retrieves)
	}
	if len(got.Bytes) == 0 || got.ResponseID != "resp-1" {
		t.Fatalf("result %+v", got)
	}
}

func TestResponsesImageRetrievesWhenCreateOmitsBytes(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{
		"id": "resp-2", "status": "completed", "model": "gpt-image",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"id":"resp-2","status":"completed","output":[{"type":"image_generation_call","status":"completed"}]}`)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bytes) == 0 {
		t.Fatal("expected image bytes from retrieve")
	}
}

func TestResponsesImageReadsBodyLargerThanEightMiB(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"id": "resp-big", "status": "completed", "model": "gpt-image",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
		"pad": strings.Repeat("a", 9<<20),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= 8<<20 {
		t.Fatalf("payload %d should exceed old 8MiB cap", len(payload))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bytes) == 0 {
		t.Fatal("truncated body dropped the image")
	}
}

func TestResponsesImageReadsCompletedSSE(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp-sse", "status": "completed",
			"output": []map[string]any{{
				"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
			}},
		},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-sse\",\"status\":\"in_progress\"}}\n\n")
		_, _ = fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", completed)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResponseID != "resp-sse" || len(got.Bytes) == 0 {
		t.Fatalf("sse result %+v", got)
	}
}

func TestResponsesImageSendsGenerateAction(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{
		"id": "resp-action", "status": "completed",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var payload struct {
			Tools      []map[string]any `json:"tools"`
			ToolChoice map[string]any   `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Tools) != 1 || payload.Tools[0]["type"] != "image_generation" || payload.Tools[0]["action"] != "generate" || payload.Tools[0]["size"] != "1024x1024" {
			t.Fatalf("tools %+v", payload.Tools)
		}
		if payload.ToolChoice != nil {
			t.Fatalf("tool_choice must be absent: %+v", payload.ToolChoice)
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	if _, err := img.Generate(context.Background(), GenerateRequest{
		Prompt: "hero", ImageTypeKey: "hero",
		GenerationSpec: map[string]any{"quality_intent": "high"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestResponsesImageTreatsTextOnlyCompletedAsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("did not expect retrieve %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{
			"id":"resp-text","status":"completed",
			"output":[
				{"type":"reasoning"},
				{"type":"message","status":"completed","content":[{"type":"output_text","text":"你好！你想让我帮你做什么？"}]}
			]
		}`)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if !errors.Is(err, ErrTextOutput) {
		t.Fatalf("got %v", err)
	}
	if isUnknown(err) {
		t.Fatalf("text output must be a confirmed fail, got unknown: %v", err)
	}
}

func TestResponsesTerminalFailureWithoutTextIsMissingOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"id":"resp-fail","status":"failed","output":[]}`)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if !errors.Is(err, ErrMissingOutput) {
		t.Fatalf("got %v", err)
	}
	if errors.Is(err, ErrTextOutput) || errors.Is(err, ErrRateLimit) {
		t.Fatalf("missing output must not be confirmed: %v", err)
	}
}

func TestResponsesTerminalFailureWithTextIsTextOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{
			"id":"resp-fail-text","status":"failed",
			"output":[{"type":"message","status":"completed","content":[{"type":"output_text","text":"无法生成图片"}]}]
		}`)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if !errors.Is(err, ErrTextOutput) {
		t.Fatalf("got %v", err)
	}
	if !(errors.Is(err, ErrTextOutput) || errors.Is(err, ErrRateLimit)) {
		t.Fatalf("text output must be confirmed: %v", err)
	}
}

func TestResponsesImageRetriesWithoutToolChoiceOn400(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{
		"id": "resp-retry", "status": "completed",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		posts++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["tool_choice"] != nil {
			t.Fatalf("request must omit tool_choice: %+v", payload)
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 || len(got.Bytes) == 0 {
		t.Fatalf("posts=%d bytes=%d", posts, len(got.Bytes))
	}
}

func isUnknown(err error) bool {
	return errors.Is(err, ErrUnknown) || errors.Is(err, graph.ErrProviderUnknown())
}

func TestImagesEditSendsMultipartSourceMaskAndReferences(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	okBody, _ := json.Marshal(map[string]any{
		"id": "edit1", "model": "gpt-image",
		"data": []map[string]any{{"b64_json": onePixelPNGB64()}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Errorf("path %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("content-type %s", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("prompt") != "去掉水印" {
			t.Errorf("prompt %s", r.FormValue("prompt"))
		}
		if r.FormValue("size") != "1024x1536" {
			t.Errorf("size %s", r.FormValue("size"))
		}
		if r.FormValue("n") != "1" {
			t.Errorf("local edit n %s", r.FormValue("n"))
		}
		if _, _, err := r.FormFile("image[]"); err != nil {
			if _, _, err := r.FormFile("image"); err != nil {
				t.Errorf("missing image: %v", err)
			}
		}
		if _, _, err := r.FormFile("mask"); err != nil {
			t.Errorf("missing mask: %v", err)
		}
		w.WriteHeader(200)
		_, _ = w.Write(okBody)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "gpt-image", MaskEdit: true}
	got, err := img.Edit(context.Background(), EditRequest{
		SourceBytes: png, SourceMIME: "image/png", MaskPNG: png,
		ReferenceBytes: [][]byte{png}, Instruction: "去掉水印", Operation: "remove",
		Size: "768x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bytes) == 0 {
		t.Fatal("expected edited image bytes")
	}
}

func TestResponsesMaskedEditSendsSourceAndProtectedMaskContract(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	completed := completedResponsesImage()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, payload)
		tools, _ := payload["tools"].([]any)
		tool, _ := tools[0].(map[string]any)
		if tool["quality"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unknown parameter: tools[0].quality"}}`)
			return
		}
		_, _ = w.Write(completed)
	}))
	defer srv.Close()
	img := OpenAIResponses{
		OpenAIImages: OpenAIImages{
			Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "reasoning-model", MaskEdit: true,
		},
		ToolRuntime: map[string]any{"model": "gpt-image-1", "quality": "high"},
	}
	got, err := img.Edit(context.Background(), EditRequest{
		SourceBytes: png, SourceMIME: "image/png", MaskPNG: png,
		Instruction: "replace only the masked area", Operation: "inpaint", Size: "1024x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bytes) == 0 || len(bodies) != 2 {
		t.Fatalf("bytes=%d requests=%d", len(got.Bytes), len(bodies))
	}
	for i, payload := range bodies {
		tools, _ := payload["tools"].([]any)
		tool, _ := tools[0].(map[string]any)
		if tool["action"] != "edit" {
			t.Fatalf("request %d lost edit action: %+v", i, tool)
		}
		mask, _ := tool[imageToolInputMaskKey].(map[string]any)
		if !strings.HasPrefix(fmt.Sprint(mask["image_url"]), "data:image/png;base64,") {
			t.Fatalf("request %d lost mask: %+v", i, tool)
		}
		input, _ := payload["input"].([]any)
		if len(input) != 1 {
			t.Fatalf("request %d input=%+v", i, payload["input"])
		}
	}
	lastTools, _ := bodies[1]["tools"].([]any)
	last, _ := lastTools[0].(map[string]any)
	if _, ok := last["quality"]; ok {
		t.Fatalf("retry retained removable quality: %+v", last)
	}
}

func TestResponsesSellingPointSendsLowFidelityOnWire(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	completed, _ := json.Marshal(map[string]any{
		"id": "resp-fid", "status": "completed",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&posted)
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()
	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	_, err = img.Generate(context.Background(), GenerateRequest{
		ImageTypeKey: "selling_point",
		GenerationSpec: map[string]any{
			"quality_intent": "high", "reference_fidelity": "high",
		},
		Refs: []ImageRef{{Bytes: png, MIME: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := posted["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("tools %+v", posted["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool["input_fidelity"] != "low" || tool["output_format"] != "png" {
		t.Fatalf("tool %+v", tool)
	}
}

func TestWorkflowInfographicUsesLowInputFidelity(t *testing.T) {
	opts := WorkflowImageToolOptions(GenerateRequest{
		ImageTypeKey: "selling_point",
		GenerationSpec: map[string]any{
			"quality_intent":     "high",
			"reference_fidelity": "high",
			"background_intent":  "auto",
		},
		Refs: []ImageRef{{}},
	}, nil, nil)
	if opts["input_fidelity"] != "low" {
		t.Fatalf("infographic fidelity %+v", opts)
	}
	if opts["output_format"] != "png" {
		t.Fatalf("output_format %+v", opts)
	}
	if opts["action"] != "generate" {
		t.Fatalf("action %+v", opts)
	}
	photo := WorkflowImageToolOptions(GenerateRequest{
		ImageTypeKey: "hero",
		GenerationSpec: map[string]any{
			"quality_intent":     "high",
			"reference_fidelity": "high",
		},
		Refs: []ImageRef{{}},
	}, nil, nil)
	if photo["input_fidelity"] != "high" {
		t.Fatalf("photography fidelity %+v", photo)
	}
}

func TestLocalEditSizeMapsPortraitSource(t *testing.T) {
	if openaiSizeFromPixels("768x1024") != "1024x1536" {
		t.Fatalf("3:4 source mapped to %s", openaiSizeFromPixels("768x1024"))
	}
	if openaiSizeFromPixels("1024x1024") != "1024x1024" {
		t.Fatal(openaiSizeFromPixels("1024x1024"))
	}
}

func TestOpenAIImageSizeMapsFourByFiveToPortrait(t *testing.T) {
	if got := openaiSizeFromSpec(map[string]any{"aspect_ratio": "4:5"}); got != "1024x1536" {
		t.Fatalf("4:5 mapped to %s", got)
	}
}

func TestGeminiUsesResolutionTierPixels(t *testing.T) {
	got := pixelSizeFromSpec(map[string]any{"aspect_ratio": "3:4", "resolution_tier": "high"})
	if got != "1536x2048" {
		t.Fatalf("pixel size %s", got)
	}
	if err := geminiRejectCustomBaseURL("https://example.test"); err == nil {
		t.Fatal("custom base url must be rejected")
	}
}

func TestImagesAPIBatchesCandidateCount(t *testing.T) {
	var posted map[string]any
	okBody, _ := json.Marshal(map[string]any{
		"id": "batch", "model": "dall-e-3",
		"data": []map[string]any{{"b64_json": onePixelPNGB64()}, {"b64_json": onePixelPNGB64()}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(200)
		_, _ = w.Write(okBody)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Count: 2, Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if posted["n"] != float64(2) && posted["n"] != 2 {
		t.Fatalf("n %+v", posted["n"])
	}
	if len(got.Images) != 2 {
		t.Fatalf("images %d", len(got.Images))
	}
}

func TestChatGenerateEditSendsCandidateCount(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	okBody, _ := json.Marshal(map[string]any{
		"id": "edit-batch", "model": "dall-e-3",
		"data": []map[string]any{
			{"b64_json": onePixelPNGB64()},
			{"b64_json": onePixelPNGB64()},
			{"b64_json": onePixelPNGB64()},
		},
	})
	var gotN string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Fatal(err)
		}
		gotN = r.FormValue("n")
		w.WriteHeader(200)
		_, _ = w.Write(okBody)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	got, err := img.Generate(context.Background(), GenerateRequest{
		Prompt: "x", Size: "1024x1024", Count: 3, Mode: ModeChat,
		Refs: []ImageRef{{Bytes: png, Filename: "base.png"}, {Bytes: png, Filename: "reference-1.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotN != "3" {
		t.Fatalf("n %s", gotN)
	}
	if len(got.Images) != 3 {
		t.Fatalf("images %d", len(got.Images))
	}
}

func TestChatGenerateDoesNotFallbackToN1(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var posted map[string]any
		_ = json.NewDecoder(r.Body).Decode(&posted)
		if posted["n"] != float64(4) && posted["n"] != 4 {
			t.Errorf("n %+v", posted["n"])
		}
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"n must be 1"}}`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Count: 4, Mode: ModeChat})
	if err == nil {
		t.Fatal("4xx with n>1 must not succeed via n=1 retry")
	}
	if calls != 1 {
		t.Fatalf("provider calls %d, n=1 fallback must not exist", calls)
	}
}

func TestChatGenerateEditDoesNotFallbackToN1(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("n") != "3" {
			t.Errorf("n %s", r.FormValue("n"))
		}
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"n must be 1"}}`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err = img.Generate(context.Background(), GenerateRequest{
		Prompt: "x", Size: "1024x1024", Count: 3, Mode: ModeChat,
		Refs: []ImageRef{{Bytes: png, Filename: "base.png"}},
	})
	if err == nil {
		t.Fatal("edit 4xx with n>1 must not succeed via n=1 retry")
	}
	if calls != 1 {
		t.Fatalf("provider calls %d, n=1 fallback must not exist", calls)
	}
}

func TestResponsesReconcileApplied(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{
		"id": "resp-1", "status": "completed",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/resp-1") {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()
	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	got, err := img.ReconcileResponse(context.Background(), "resp-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "applied" {
		t.Fatalf("state %s", got)
	}
}

func TestProviderDisplayNamesAreHyphenated(t *testing.T) {
	images := OpenAIImages{Kind: "openai_images"}
	if images.Name() != "openai-images" {
		t.Fatal(images.Name())
	}
	if (OpenAIResponses{}).Name() != "openai-responses" {
		t.Fatal((OpenAIResponses{}).Name())
	}
	if (GeminiImage{}).Name() != "google-gemini-image" {
		t.Fatal((GeminiImage{}).Name())
	}
}

func TestGenerateImageWithReferencesUsesEdits(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	okBody, _ := json.Marshal(map[string]any{
		"id": "img-ref", "model": "dall-e-3",
		"data": []map[string]any{{"b64_json": onePixelPNGB64()}},
	})
	path := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		if r.FormValue("n") != "1" {
			t.Errorf("graph edit n %s", r.FormValue("n"))
		}
		w.WriteHeader(200)
		_, _ = w.Write(okBody)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	got, err := img.Generate(context.Background(), GenerateRequest{
		Prompt: "hero", ImageTypeKey: "hero",
		Refs: []ImageRef{{
			Bytes: png, MIME: "image/png", Filename: "ref.png",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/images/edits" {
		t.Fatalf("path %s", path)
	}
	if got.EffectiveParameters["reference_image_count"] != 1 {
		t.Fatalf("effective %+v", got.EffectiveParameters)
	}
	if !strings.Contains(graph.CompileImageModelPrompt(graph.ImageRequest{NodeTitle: "hero", ImageTypeKey: "hero"}), "封面主图") {
		t.Fatal("compiled prompt should be listing text")
	}
}

func TestResponsesChatBranchOmitsPreviousResponseID(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	completed, _ := json.Marshal(map[string]any{
		"id": "resp-ref", "status": "completed", "model": "gpt-image",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&posted)
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()
	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	_, err = img.Generate(context.Background(), GenerateRequest{
		Prompt: "换成蓝色", Size: "1024x1024", Mode: ModeChat,
		Refs: []ImageRef{{Bytes: png, Filename: "base.png"}, {Bytes: png, Filename: "reference-1.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := posted["previous_response_id"]; ok {
		t.Fatalf("branch request must not send previous_response_id: %+v", posted)
	}
	if countPostedInputImages(posted) != 2 {
		t.Fatalf("expected base and reference input_image, got %+v", posted["input"])
	}
}

func TestResponsesAdapterForwardsPreviousResponseIDAndKeepsBaseImage(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	completed, _ := json.Marshal(map[string]any{
		"id": "resp-ref", "status": "completed", "model": "gpt-image",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&posted)
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()
	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	prev := "prev-1"
	_, err = img.Generate(context.Background(), GenerateRequest{
		Prompt: "继续", Size: "1024x1024", Mode: ModeChat,
		Refs:               []ImageRef{{Bytes: png, Filename: "base.png"}, {Bytes: png, Filename: "reference-1.png"}},
		PreviousResponseID: &prev,
	})
	if err != nil {
		t.Fatal(err)
	}
	if posted["previous_response_id"] != "prev-1" {
		t.Fatalf("payload %+v", posted)
	}
	if countPostedInputImages(posted) != 2 {
		t.Fatalf("previous_response_id must not drop base/reference images, got %+v", posted["input"])
	}
}

func TestResponsesFourHundredDoesNotFallbackToImages(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	imagesHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/images/") {
			imagesHits++
			w.WriteHeader(200)
			_, _ = fmt.Fprintf(w, `{"id":"img1","data":[{"b64_json":%q}]}`, onePixelPNGB64())
			return
		}
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"previous_response_id is not available for this user","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()
	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	prev := "resp-from-other-key"
	_, err = img.Generate(context.Background(), GenerateRequest{
		Prompt: "继续", Size: "1024x1024", Mode: ModeChat,
		Refs:               []ImageRef{{Bytes: png, Filename: "base.png"}},
		PreviousResponseID: &prev,
	})
	if err == nil {
		t.Fatal("expected responses 400")
	}
	if isUnknown(err) {
		t.Fatalf("400 should fail, got unknown: %v", err)
	}
	if imagesHits != 0 {
		t.Fatalf("responses 400 fell back to images %d times", imagesHits)
	}
	_, err = img.Generate(context.Background(), GenerateRequest{
		Prompt: "hero", ImageTypeKey: "hero",
		Refs: []ImageRef{{Bytes: png, MIME: "image/png"}},
	})
	if err == nil {
		t.Fatal("expected responses 400")
	}
	if imagesHits != 0 {
		t.Fatalf("graph responses 400 fell back to images %d times", imagesHits)
	}
}

func TestImagesChatDoesNotPersistResponseID(t *testing.T) {
	okBody, _ := json.Marshal(map[string]any{
		"id": "img-edit-1", "model": "gpt-image",
		"data": []map[string]any{{"b64_json": onePixelPNGB64()}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(okBody)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResponseID != "" {
		t.Fatalf("images chat must not persist response id %q", got.ResponseID)
	}
}

func countPostedInputImages(posted map[string]any) int {
	input, _ := posted["input"].([]any)
	if len(input) == 0 {
		return 0
	}
	msg, _ := input[0].(map[string]any)
	content, _ := msg["content"].([]any)
	n := 0
	for _, item := range content {
		part, _ := item.(map[string]any)
		if part["type"] == "input_image" {
			n++
		}
	}
	return n
}

func TestPromptSendsReferenceImageURL(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	var posted map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"id":"r1","model":"m","output_parsed":{"goal":"展示商品","key_messages":["主体"],"required_elements":["展示杯盖"],"prohibitions":["不得宣称医疗功效"],"fact_gaps":[]}}`)
	}))
	defer srv.Close()
	p := OpenAIPrompt{APIKey: "sk", BaseURL: srv.URL, Model: "gpt"}
	brief, err := p.GenerateCreativeBrief(context.Background(), graph.PromptRequest{
		NodeTitle:  "brief",
		TextPolicy: "none",
		Facts:      []map[string]any{{"key": "product_name", "value": "杯"}},
		References: []graph.ReferenceImage{{
			AssetID: "a1", Bytes: png, MIME: "image/png", Label: "主体", Role: "product_identity",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(brief.Payload["required_elements"]), "展示杯盖") || !strings.Contains(fmt.Sprint(brief.Payload["prohibitions"]), "不得宣称医疗功效") {
		t.Fatalf("no-text incorrectly removed brief requirements: %+v", brief.Payload)
	}
	if path != "/v1/responses" {
		t.Fatalf("path %s", path)
	}
	if _, ok := posted["messages"]; ok {
		t.Fatalf("must not use chat completions messages: %+v", posted)
	}
	if posted["instructions"] == nil || !strings.Contains(fmt.Sprint(posted["instructions"]), "listing_look") {
		t.Fatalf("instructions %+v", posted["instructions"])
	}
	input, _ := posted["input"].([]any)
	if len(input) == 0 {
		t.Fatalf("input %+v", posted["input"])
	}
	user, _ := input[0].(map[string]any)
	content, _ := user["content"].([]any)
	if len(content) < 3 {
		t.Fatalf("user content %+v", user["content"])
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if first["type"] != "input_text" || !strings.Contains(text, `"listing_look"`) || !strings.Contains(text, "generate_ecommerce_context_node") {
		t.Fatalf("first part %+v", first)
	}
	rolePart, _ := content[1].(map[string]any)
	if rolePart["type"] != "input_text" || !strings.Contains(fmt.Sprint(rolePart["text"]), "product_identity") {
		t.Fatalf("role part %+v", rolePart)
	}
	imagePart, _ := content[2].(map[string]any)
	if imagePart["type"] != "input_image" {
		t.Fatalf("image part %+v", imagePart)
	}
	format, _ := posted["text"].(map[string]any)
	inner, _ := format["format"].(map[string]any)
	if inner["type"] != "json_schema" {
		t.Fatalf("text.format %+v", posted["text"])
	}
}

func TestSourceNotePostsResponsesWithStrictSchema(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"id":"r1","model":"m","output_parsed":{"visible":"玻璃瓶","fields":[{"label":"材质","value":"玻璃"},{"label":"容量","value":""}]}}`)
	}))
	defer srv.Close()
	p := OpenAIPrompt{APIKey: "sk", BaseURL: srv.URL, Model: "gpt"}
	got, err := p.GenerateSourceNote(context.Background(), graph.PromptRequest{
		NodeTitle: "密封瓶",
		CurrentDocument: map[string]any{
			"source_note": "手填",
		},
		References: []graph.ReferenceImage{{
			AssetID: "a1", Bytes: png, MIME: "image/png", Label: "主体", Role: "product_identity",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Payload["visible"] != "玻璃瓶" {
		t.Fatalf("payload %+v", got.Payload)
	}
	fields, _ := got.Payload["fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("fields %+v", got.Payload["fields"])
	}
	format, _ := posted["text"].(map[string]any)
	inner, _ := format["format"].(map[string]any)
	if inner["type"] != "json_schema" || inner["strict"] != true || inner["name"] != "generated_source_note" {
		t.Fatalf("text.format %+v", posted["text"])
	}
	schema, _ := inner["schema"].(map[string]any)
	required, _ := schema["required"].([]any)
	joined := fmt.Sprint(required)
	for _, key := range []string{"visible", "fields"} {
		if !strings.Contains(joined, key) {
			t.Fatalf("required %v missing %s", required, key)
		}
	}
	if strings.Contains(joined, "material") || strings.Contains(joined, "certification") {
		t.Fatalf("fixed checklist leaked into required %v", required)
	}
	instr := fmt.Sprint(posted["instructions"])
	if posted["instructions"] == nil || !strings.Contains(instr, "merchant-editable product source note") {
		t.Fatalf("instructions %+v", posted["instructions"])
	}
	if !strings.Contains(instr, "Do not add photography or listing direction") || !strings.Contains(instr, "可重点强调") {
		t.Fatalf("instructions missing listing-direction ban: %v", posted["instructions"])
	}
	if !strings.Contains(instr, "2–4 selling points") {
		t.Fatalf("instructions missing selling-point requirement: %v", posted["instructions"])
	}
	input, _ := posted["input"].([]any)
	user, _ := input[0].(map[string]any)
	content, _ := user["content"].([]any)
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if first["type"] != "input_text" || !strings.Contains(text, `"task":"draft_product_source_note"`) || strings.Contains(text, "listing_look") {
		t.Fatalf("first part %+v", first)
	}
	imagePart, _ := content[2].(map[string]any)
	if imagePart["type"] != "input_image" {
		t.Fatalf("image part %+v", imagePart)
	}
}

func TestSourceNoteSchemaRejectsMissingField(t *testing.T) {
	err := matchJSONSchema(sourceNoteJSONSchema, map[string]any{
		"visible": "瓶",
	})
	if err == nil {
		t.Fatal("expected missing fields")
	}
	if err := matchJSONSchema(sourceNoteJSONSchema, MockSourceNotePayload()); err != nil {
		t.Fatal(err)
	}
}

func TestPromptGenerationBodyHasListingLookAndSeed(t *testing.T) {
	body, err := BuildPromptResponsesBody("gpt", prompts.PromptInstructions(), "listing_prompt_payload", listingPromptJSONSchema, graph.PromptRequest{
		ImageTypeKey:        "selling_point",
		ImageTypeTitle:      "核心卖点图",
		ImageTypeFamily:     "infographic",
		ImageTypeJob:        "详情卖点图",
		TextPolicy:          "required",
		TextLanguage:        "ja-JP",
		GenerateFromContext: true,
		CurrentPrompt:       map[string]any{"design_goal": "卖点"},
		DocumentAction:      graph.DocumentActionRewrite,
		DocumentSection:     "copy",
		CurrentDocument:     map[string]any{"design_goal": "人工目标"},
		Facts:               []map[string]any{{"key": "product_name", "value": "杯"}},
	}, "prompt")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := body["input"].([]map[string]any)
	if len(input) == 0 {
		t.Fatalf("input %+v", body["input"])
	}
	content, _ := input[0]["content"].([]map[string]any)
	text, _ := content[0]["text"].(string)
	for _, needle := range []string{
		`"task":"generate_ecommerce_image_prompt_artifact"`,
		`"generate_from_context":true`,
		`"document_action":"rewrite"`,
		`"document_section":"copy"`,
		`"current_document":{"design_goal":"人工目标"}`,
		`"image_type_key":"selling_point"`,
		`"listing_look"`,
		`"listing_look_rule"`,
		`"current_prompt"`,
		`"text_policy":"required"`,
		`"text_language":"ja-JP"`,
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %s in %s", needle, text)
		}
	}
}

func TestGeminiGenerateContentSendsInlineData(t *testing.T) {
	png, err := decodeB64(onePixelPNGB64())
	if err != nil {
		t.Fatal(err)
	}
	var posted map[string]any
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&posted)
		resp, _ := json.Marshal(map[string]any{
			"responseId": "g1", "modelVersion": "gemini-2.5-flash-image",
			"candidates": []map[string]any{{
				"content": map[string]any{
					"parts": []map[string]any{{
						"inlineData": map[string]any{"mimeType": "image/png", "data": onePixelPNGB64()},
					}},
				},
			}},
		})
		w.WriteHeader(200)
		_, _ = w.Write(resp)
	}))
	defer srv.Close()
	g := GeminiImage{APIKey: "key", BaseURL: srv.URL, Model: "gemini-2.5-flash-image", APIVersion: "v1beta"}
	got, err := g.Generate(context.Background(), GenerateRequest{
		Prompt: "hero", ImageTypeKey: "hero",
		Refs: []ImageRef{{Bytes: png, MIME: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, ":generateContent") {
		t.Fatalf("url %s", gotURL)
	}
	if len(got.Bytes) == 0 {
		t.Fatal("expected gemini image bytes")
	}
	contents, _ := posted["contents"].([]any)
	first, _ := contents[0].(map[string]any)
	parts, _ := first["parts"].([]any)
	if len(parts) < 2 {
		t.Fatalf("parts %+v", parts)
	}
	inline, _ := parts[1].(map[string]any)
	data, _ := inline["inlineData"].(map[string]any)
	if data == nil || data["mimeType"] != "image/png" || data["data"] == nil {
		t.Fatalf("expected inlineData camelCase, got %+v", inline)
	}
	if inline["inline_data"] != nil {
		t.Fatalf("must not send snake_case inline_data: %+v", inline)
	}
}

func completedResponsesImage() []byte {
	raw, _ := json.Marshal(map[string]any{
		"id": "resp-ok", "status": "completed",
		"output": []map[string]any{{
			"type": "image_generation_call", "status": "completed", "result": onePixelPNGB64(),
		}},
	})
	return raw
}

func TestResponsesDropsBackgroundWhenUnsupported(t *testing.T) {
	completed := completedResponsesImage()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, payload)
		if _, ok := payload["background"]; ok {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"message":"Unknown parameter: background is unsupported"}}`)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}, Background: true}
	got, err := img.Generate(context.Background(), GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bytes) == 0 {
		t.Fatal("expected image")
	}
	if len(bodies) < 2 {
		t.Fatalf("posts %d", len(bodies))
	}
	if bodies[0]["background"] != true {
		t.Fatalf("first payload %+v", bodies[0])
	}
	last := bodies[len(bodies)-1]
	if _, ok := last["background"]; ok {
		t.Fatalf("retry must drop background: %+v", last)
	}
}

func TestResponsesRetriesWithTypeSizeOnlyTool(t *testing.T) {
	completed := completedResponsesImage()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, payload)
		tools, _ := payload["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("tools %+v", tools)
		}
		tool, _ := tools[0].(map[string]any)
		if len(tool) > 2 {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"message":"Unknown parameter: tools[0].quality"}}`)
			return
		}
		if tool["type"] != "image_generation" || tool["size"] != "1024x1024" {
			t.Fatalf("minimal tool %+v", tool)
		}
		if _, ok := tool["quality"]; ok {
			t.Fatalf("quality still present %+v", tool)
		}
		w.WriteHeader(200)
		_, _ = w.Write(completed)
	}))
	defer srv.Close()

	img := OpenAIResponses{OpenAIImages: OpenAIImages{Kind: "openai_responses", APIKey: "sk", BaseURL: srv.URL, Model: "m"}}
	got, err := img.Generate(context.Background(), GenerateRequest{
		Prompt: "hero", ImageTypeKey: "hero",
		GenerationSpec: map[string]any{"quality_intent": "high"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bytes) == 0 {
		t.Fatal("expected image")
	}
	if len(bodies) < 2 {
		t.Fatalf("posts %d", len(bodies))
	}
	firstTools, _ := bodies[0]["tools"].([]any)
	first, _ := firstTools[0].(map[string]any)
	if first["quality"] == nil && first["action"] == nil && first["output_format"] == nil {
		t.Fatalf("first tool should include optional fields: %+v", first)
	}
	lastTools, _ := bodies[len(bodies)-1]["tools"].([]any)
	last, _ := lastTools[0].(map[string]any)
	if len(last) != 2 || last["type"] != "image_generation" || last["size"] != "1024x1024" {
		t.Fatalf("later tool must be type+size only: %+v", last)
	}
}

func TestPromptRejectsExtraAndMissingKeys(t *testing.T) {
	t.Run("brief-extra", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"id":"r1","model":"m","output_parsed":{"goal":"展示商品","key_messages":["主体"],"required_elements":[],"prohibitions":[],"fact_gaps":[],"extra":"no"}}`)
		}))
		defer srv.Close()
		p := OpenAIPrompt{APIKey: "sk", BaseURL: srv.URL, Model: "gpt"}
		if _, err := p.GenerateCreativeBrief(context.Background(), graph.PromptRequest{NodeTitle: "t"}); err == nil {
			t.Fatal("expected extra key rejection")
		}
	})
	t.Run("brief-missing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"id":"r1","model":"m","output_parsed":{"goal":"展示商品","key_messages":["主体"]}}`)
		}))
		defer srv.Close()
		p := OpenAIPrompt{APIKey: "sk", BaseURL: srv.URL, Model: "gpt"}
		if _, err := p.GenerateCreativeBrief(context.Background(), graph.PromptRequest{NodeTitle: "t"}); err == nil {
			t.Fatal("expected missing key rejection")
		}
	})
	t.Run("prompt-extra", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"id":"r1","model":"m","output_parsed":{"schema_version":1,"extra":true}}`)
		}))
		defer srv.Close()
		p := OpenAIPrompt{APIKey: "sk", BaseURL: srv.URL, Model: "gpt"}
		if _, err := p.GeneratePrompt(context.Background(), graph.PromptRequest{NodeTitle: "t"}); err == nil {
			t.Fatal("expected extra key rejection")
		}
	})
	t.Run("prompt-missing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"id":"r1","model":"m","output_parsed":{"schema_version":1,"shared_rules":["a"],"design_goal":"g"}}`)
		}))
		defer srv.Close()
		p := OpenAIPrompt{APIKey: "sk", BaseURL: srv.URL, Model: "gpt"}
		if _, err := p.GeneratePrompt(context.Background(), graph.PromptRequest{NodeTitle: "t"}); err == nil {
			t.Fatal("expected missing key rejection")
		}
	})
}

func TestMatchJSONSchemaRejectsMaxItems(t *testing.T) {
	schema := boundedStringArraySchema(0, 2)
	if err := matchJSONSchema(schema, []any{"a", "b"}); err != nil {
		t.Fatalf("valid bounded array rejected: %v", err)
	}
	if err := matchJSONSchema(schema, []any{"a", "b", "c"}); err == nil {
		t.Fatal("expected maxItems rejection")
	}
}

func TestListingPromptSchemaAllowsFullInfographicHierarchy(t *testing.T) {
	composition := compositionSchema["properties"].(map[string]any)
	content := contentSchema["properties"].(map[string]any)
	if got, _ := jsonSchemaInt(composition["copy_regions"].(map[string]any)["maxItems"]); got != 4 {
		t.Fatalf("copy_regions maxItems %d", got)
	}
	if got, _ := jsonSchemaInt(content["selling_points"].(map[string]any)["maxItems"]); got != 5 {
		t.Fatalf("selling_points maxItems %d", got)
	}
	if got, _ := jsonSchemaInt(content["decorations"].(map[string]any)["maxItems"]); got != 4 {
		t.Fatalf("decorations maxItems %d", got)
	}
}

func TestImagesChatAppliesToolOptionsModelAndQuality(t *testing.T) {
	okBody, _ := json.Marshal(map[string]any{
		"id": "img1", "model": "gpt-image-1",
		"data": []map[string]any{{"b64_json": onePixelPNGB64()}},
	})
	t.Run("generations", func(t *testing.T) {
		var payload map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/images/generations" {
				t.Errorf("path %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(200)
			_, _ = w.Write(okBody)
		}))
		defer srv.Close()
		img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3", Quality: "standard"}
		if _, err := img.Generate(context.Background(), GenerateRequest{
			Prompt: "x", Size: "1024x1024", Mode: ModeChat,
			ToolOptions: map[string]any{"model": " gpt-image-1 ", "quality": "high"},
		}); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "gpt-image-1" || payload["quality"] != "high" {
			t.Fatalf("payload %+v", payload)
		}
	})
	t.Run("edits", func(t *testing.T) {
		png, err := decodeB64(onePixelPNGB64())
		if err != nil {
			t.Fatal(err)
		}
		var model, quality string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/images/edits" {
				t.Errorf("path %s", r.URL.Path)
			}
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Fatal(err)
			}
			model = r.FormValue("model")
			quality = r.FormValue("quality")
			w.WriteHeader(200)
			_, _ = w.Write(okBody)
		}))
		defer srv.Close()
		img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3", Quality: "standard"}
		if _, err := img.Generate(context.Background(), GenerateRequest{
			Prompt: "x", Size: "1024x1024", Mode: ModeChat,
			Refs:        []ImageRef{{Bytes: png, Filename: "base.png"}},
			ToolOptions: map[string]any{"model": "gpt-image-1", "quality": "low"},
		}); err != nil {
			t.Fatal(err)
		}
		if model != "gpt-image-1" || quality != "low" {
			t.Fatalf("model=%s quality=%s", model, quality)
		}
	})
}

func TestChat429IsConfirmedRetryableFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit"}}`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Mode: ModeChat})
	if err == nil {
		t.Fatal("expected 429")
	}
	if isUnknown(err) {
		t.Fatalf("chat 429 should not be unknown: %v", err)
	}
	if !(errors.Is(err, ErrTextOutput) || errors.Is(err, ErrRateLimit)) {
		t.Fatalf("chat 429 should be confirmed: %v", err)
	}
	if !(errors.Is(err, ErrRateLimit) || errors.Is(err, ErrTimeout) || errors.Is(err, ErrConnection) || errors.Is(err, ErrProvider5xx)) {
		t.Fatalf("chat 429 should be retryable: %v", err)
	}
}

func TestGraph429StaysUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit"}}`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "hero"})
	if err == nil {
		t.Fatal("expected 429")
	}
	if !isUnknown(err) {
		t.Fatalf("graph 429 should stay unknown: %v", err)
	}
}

func TestChat503IsProvider5xxNotUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `service unavailable`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Mode: ModeChat})
	if err == nil {
		t.Fatal("expected 503")
	}
	if isUnknown(err) {
		t.Fatalf("chat 503 should not be unknown: %v", err)
	}
	if !errors.Is(err, ErrProvider5xx) {
		t.Fatalf("got %v", err)
	}
	if !(errors.Is(err, ErrRateLimit) || errors.Is(err, ErrTimeout) || errors.Is(err, ErrConnection) || errors.Is(err, ErrProvider5xx)) {
		t.Fatalf("chat 503 should be retryable: %v", err)
	}
}

func TestChat400QuotaIsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Mode: ModeChat})
	if !errors.Is(err, ErrRateLimit) {
		t.Fatalf("got %v", err)
	}
}

func TestChat400QuotaWordIsNotRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"prompt mentions product quota"}}`)
	}))
	defer srv.Close()
	img := OpenAIImages{Kind: "openai_images", APIKey: "sk", BaseURL: srv.URL, Model: "dall-e-3"}
	_, err := img.Generate(context.Background(), GenerateRequest{Prompt: "x", Size: "1024x1024", Mode: ModeChat})
	if err == nil {
		t.Fatal("expected 400")
	}
	if errors.Is(err, ErrRateLimit) {
		t.Fatal("bare quota in a 400 message is not a rate limit")
	}
}

func TestMapTransportClassifiesTimeoutAndConnection(t *testing.T) {
	timeoutErr := mapTransport(context.DeadlineExceeded)
	if !errors.Is(timeoutErr, ErrTimeout) {
		t.Fatalf("timeout %v", timeoutErr)
	}
	if !errors.Is(timeoutErr, ErrUnknown) {
		t.Fatalf("graph path must still see unknown: %v", timeoutErr)
	}
	connErr := mapTransport(fmt.Errorf("read: connection reset by peer"))
	if !errors.Is(connErr, ErrConnection) {
		t.Fatalf("connection %v", connErr)
	}
	if !errors.Is(timeoutErr, ErrTimeout) || !errors.Is(connErr, ErrConnection) {
		t.Fatal("timeout/connection should be retryable")
	}
	other := mapTransport(fmt.Errorf("tls handshake failed"))
	if !errors.Is(other, ErrUnknown) {
		t.Fatalf("unclassifiable %v", other)
	}
	if errors.Is(other, ErrTimeout) || errors.Is(other, ErrConnection) || errors.Is(other, ErrRateLimit) || errors.Is(other, ErrProvider5xx) {
		t.Fatalf("unclassifiable must not be retryable: %v", other)
	}
}
