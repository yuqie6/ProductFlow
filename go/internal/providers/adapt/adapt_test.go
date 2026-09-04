package adapt

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/providers"
)

type stubClient struct {
	last providers.GenerateRequest
	out  providers.GenerateResult
	err  error
}

func (s *stubClient) Name() string { return "stub" }

func (s *stubClient) Generate(_ context.Context, req providers.GenerateRequest) (providers.GenerateResult, error) {
	s.last = req
	return s.out, s.err
}

func (s *stubClient) Edit(context.Context, providers.EditRequest) (providers.EditResult, error) {
	return providers.EditResult{}, nil
}

func (s *stubClient) Capability() providers.EditCapability {
	return providers.UnsupportedEditCapability("stub")
}

func (s *stubClient) ReconcileResponse(context.Context, string) (string, error) {
	return "unsupported", nil
}

func TestGraphImageCompilesPromptAndMapsUnknown(t *testing.T) {
	stub := &stubClient{out: providers.GenerateResult{Bytes: []byte("png"), MIME: "image/png"}}
	got, err := GraphImage(stub).GenerateImage(context.Background(), graph.ImageRequest{
		NodeTitle: "hero", ImageTypeKey: "hero",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stub.last.Prompt, "封面主图") {
		t.Fatalf("prompt %q", stub.last.Prompt)
	}
	if stub.last.Mode != providers.ModeWorkflow {
		t.Fatalf("mode %q", stub.last.Mode)
	}
	if string(got.Bytes) != "png" {
		t.Fatalf("bytes %q", got.Bytes)
	}

	stub.err = providers.ErrUnknown
	_, err = GraphImage(stub).GenerateImage(context.Background(), graph.ImageRequest{NodeTitle: "hero"})
	if !errors.Is(err, graph.ErrProviderUnknown()) {
		t.Fatalf("want graph unknown, got %v", err)
	}
}

func TestChatMapsModeAndErrors(t *testing.T) {
	stub := &stubClient{out: providers.GenerateResult{Bytes: []byte("png"), Images: [][]byte{[]byte("png")}, MIME: "image/png"}}
	got, err := Chat(stub, nil).Generate(context.Background(), imagesession.ChatRequest{
		Prompt: "小猫", Size: "1024x1024", Count: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.last.Mode != providers.ModeChat || stub.last.Prompt != "小猫" {
		t.Fatalf("last %+v", stub.last)
	}
	if len(got.Bytes) == 0 {
		t.Fatal("expected chat bytes")
	}
	stub.err = providers.ErrRateLimit
	_, err = Chat(stub, nil).Generate(context.Background(), imagesession.ChatRequest{Prompt: "x"})
	if !errors.Is(err, imagesession.ErrRateLimit) {
		t.Fatalf("want rate limit, got %v", err)
	}
}
