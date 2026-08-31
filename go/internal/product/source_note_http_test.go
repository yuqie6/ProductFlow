package product

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
)

type stubSourceNote struct {
	payload map[string]any
	err     error
	req     graph.PromptRequest
}

func (s *stubSourceNote) GenerateSourceNote(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	s.req = req
	if s.err != nil {
		return graph.PromptResult{}, s.err
	}
	return graph.PromptResult{Payload: s.payload}, nil
}

func TestGenerateSourceNoteRequiresImages(t *testing.T) {
	stub := &stubSourceNote{payload: map[string]any{"visible": "瓶"}}
	ps := newProductServerWith(t, Service{SourceNote: stub})
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("product_name", "密封瓶")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := ps.do(t, http.MethodPost, "/api/v2/product-source-notes/generate", &buf, w.FormDataContentType())
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
}

func TestGenerateSourceNoteRejectsTooManyImages(t *testing.T) {
	stub := &stubSourceNote{payload: map[string]any{"visible": "瓶"}}
	ps := newProductServerWith(t, Service{SourceNote: stub})
	body, contentType := multipartPNGs(t, map[string]string{"product_name": "密封瓶"}, 7)
	resp := ps.do(t, http.MethodPost, "/api/v2/product-source-notes/generate", body, contentType)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}
}

func TestGenerateSourceNoteReturnsStructuredFields(t *testing.T) {
	stub := &stubSourceNote{payload: map[string]any{
		"visible": "厚壁玻璃密封瓶",
		"fields": []any{
			map[string]any{"label": "材质", "value": "玻璃"},
			map[string]any{"label": "容量", "value": ""},
		},
	}}
	ps := newProductServerWith(t, Service{SourceNote: stub})
	body, contentType := multipartPNGs(t, map[string]string{
		"product_name": "密封瓶",
		"current_note": "已写的说明",
	}, 1)
	resp := ps.do(t, http.MethodPost, "/api/v2/product-source-notes/generate", body, contentType)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}
	var got GeneratedSourceNote
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Visible != "厚壁玻璃密封瓶" || len(got.Fields) != 2 || got.Fields[0].Label != "材质" || got.Fields[0].Value != "玻璃" || got.Fields[1].Value != "" {
		t.Fatalf("%+v", got)
	}
	if stub.req.NodeTitle != "密封瓶" {
		t.Fatalf("name %q", stub.req.NodeTitle)
	}
	if len(stub.req.References) != 1 || stub.req.References[0].Role != "product_identity" {
		t.Fatalf("refs %+v", stub.req.References)
	}
	note, _ := stub.req.CurrentDocument["source_note"].(string)
	if note != "已写的说明" {
		t.Fatalf("current %+v", stub.req.CurrentDocument)
	}
}

func TestGenerateSourceNoteUnavailableWithoutGenerator(t *testing.T) {
	ps := newProductServer(t)
	body, contentType := multipartPNGs(t, map[string]string{"product_name": "密封瓶"}, 1)
	resp := ps.do(t, http.MethodPost, "/api/v2/product-source-notes/generate", body, contentType)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}
}
