package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
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
			body:   `{"id":"r1","model":"m","choices":[{"message":{"content":"{\"goal\":\"展示商品\",\"design_goals\":[\"主体\"],\"required_copy\":[],\"prohibitions\":[]}"}}]}`,
			wantOK: true,
		},
		{name: "fail-400", status: 400, body: `{"error":{"message":"bad"}}`},
		{name: "unknown-500", status: 500, body: `oops`, wantU: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
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
			got, err := img.GenerateImage(context.Background(), graph.ImageRequest{NodeTitle: "hero"})
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
	_, err := img.Generate(context.Background(), imagesession.ChatRequest{Prompt: "x", Size: "1024x1024"})
	if err == nil {
		t.Fatal("expected error")
	}
	if isUnknown(err) {
		t.Fatalf("400 should fail, got unknown: %v", err)
	}
}

func isUnknown(err error) bool {
	return errors.Is(err, graph.ErrProviderUnknown()) || errors.Is(err, imagesession.ErrUnknown())
}
