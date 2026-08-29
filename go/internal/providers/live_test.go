package providers

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
)

const liveProvidersSwitch = "PRODUCTFLOW_RUN_LIVE_PROVIDERS"

type liveBindings struct {
	store  *settings.Store
	prompt settings.ModelBinding
	image  settings.ModelBinding
}

func requireLiveBindings(t *testing.T) liveBindings {
	t.Helper()
	if os.Getenv(liveProvidersSwitch) != "1" {
		t.Skipf("set %s=1 to call live prompt/image providers", liveProvidersSwitch)
	}
	pool := testdb.Pool(t)
	store := settings.NewStore(pool, config.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	prompt, err := store.ResolvePrompt(ctx)
	if err != nil {
		t.Fatalf("resolve prompt: %v", err)
	}
	image, err := store.ResolveImage(ctx)
	if err != nil {
		t.Fatalf("resolve image: %v", err)
	}
	if prompt.Kind == "" || prompt.Kind == "mock" {
		t.Fatalf("prompt purpose is %q; bind a real prompt provider before this gate", prompt.Kind)
	}
	if image.Kind == "" || image.Kind == "mock" {
		t.Fatalf("image purpose is %q; bind a real image provider before this gate", image.Kind)
	}
	if strings.TrimSpace(prompt.APIKey) == "" || strings.TrimSpace(image.APIKey) == "" {
		t.Fatal("live provider profiles are missing API keys")
	}
	t.Logf("live bindings prompt_kind=%s image_kind=%s", prompt.Kind, image.Kind)
	return liveBindings{store: store, prompt: prompt, image: image}
}

func TestLivePromptSuccessFailUnknown(t *testing.T) {
	live := requireLiveBindings(t)
	prompt := LivePrompt{Store: live.store}
	direct := OpenAIPrompt{APIKey: live.prompt.APIKey, BaseURL: live.prompt.BaseURL, Model: live.prompt.Model}
	req := graph.PromptRequest{
		NodeType:  graph.NodeCreativeBrief,
		NodeTitle: "创作要求",
		Facts:     []map[string]any{{"product_name": "暖白釉陶瓷马克杯", "material": "陶瓷"}},
	}

	t.Run("success", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		got, err := prompt.GenerateCreativeBrief(ctx, req)
		if err != nil {
			t.Fatalf("live prompt success: %v", err)
		}
		if len(got.Payload) == 0 {
			t.Fatal("live prompt success returned empty payload")
		}
	})

	t.Run("fail", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := livePromptFail(ctx, direct, req)
		if err == nil {
			t.Fatal("expected live prompt 4xx fail")
		}
		if isUnknown(err) {
			t.Fatalf("want fail, got unknown: %v", err)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		_, err := direct.GenerateCreativeBrief(ctx, req)
		if err == nil {
			t.Fatal("expected live prompt unknown from timed-out request")
		}
		if !isUnknown(err) {
			t.Fatalf("want unknown, got fail: %v", err)
		}
	})
}

func TestLiveImageSuccessFailUnknown(t *testing.T) {
	live := requireLiveBindings(t)
	image := LiveImage{Store: live.store}
	direct := liveImageAdapter(live.image, live.image.Model)
	req := graph.ImageRequest{
		NodeTitle:      "white ceramic mug on wood, ecommerce product photo",
		GenerationSpec: map[string]any{"aspect_ratio": "1:1"},
	}

	t.Run("fail", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := liveImageFail(ctx, live.image, req)
		if err == nil {
			t.Fatal("expected live image 4xx fail")
		}
		if isUnknown(err) {
			t.Fatalf("want fail, got unknown: %v", err)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		_, err := direct.GenerateImage(ctx, req)
		if err == nil {
			t.Fatal("expected live image unknown from timed-out request")
		}
		if !isUnknown(err) {
			t.Fatalf("want unknown, got fail: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		got, err := image.GenerateImage(ctx, req)
		if err != nil {
			t.Fatalf("live image success: %v", err)
		}
		if len(got.Bytes) < 256 || !strings.HasPrefix(got.MIME, "image/") {
			t.Fatalf("live image success is not an image: mime=%s bytes=%d", got.MIME, len(got.Bytes))
		}
	})
}

func livePromptFail(ctx context.Context, good OpenAIPrompt, req graph.PromptRequest) error {
	rejected := good
	rejected.APIKey = "sk-productflow-live-gate-invalid"
	_, err := rejected.GenerateCreativeBrief(ctx, req)
	if err != nil && !isUnknown(err) {
		return err
	}
	malformed := good
	malformed.Transport = liveRefuseTransport()
	_, err = malformed.GenerateCreativeBrief(ctx, req)
	return err
}

func liveImageFail(ctx context.Context, binding settings.ModelBinding, req graph.ImageRequest) error {
	rejected := liveImageAdapter(binding, binding.Model)
	switch img := rejected.(type) {
	case OpenAIImages:
		img.APIKey = "sk-productflow-live-gate-invalid"
		_, err := img.GenerateImage(ctx, req)
		if err != nil && !isUnknown(err) {
			return err
		}
		img.APIKey = binding.APIKey
		img.Transport = liveRefuseTransport()
		_, err = img.GenerateImage(ctx, req)
		return err
	case OpenAIResponses:
		img.APIKey = "sk-productflow-live-gate-invalid"
		_, err := img.GenerateImage(ctx, req)
		if err != nil && !isUnknown(err) {
			return err
		}
		img.APIKey = binding.APIKey
		img.Transport = liveRefuseTransport()
		_, err = img.GenerateImage(ctx, req)
		return err
	default:
		_, err := rejected.GenerateImage(ctx, req)
		return err
	}
}

func liveRefuseTransport() jsonRoundTrip {
	return func(ctx context.Context, method, url, apiKey string, _ []byte) (int, []byte, error) {
		body := []byte(`{"model":"x","messages":"not-an-array"}`)
		if strings.Contains(url, "/images/") || strings.Contains(url, "/responses") {
			body = []byte(`{"model":"x","prompt":false}`)
		}
		return doJSON(ctx, newHTTPClient(), method, url, apiKey, bytes.NewReader(body), "application/json")
	}
}

func liveImageAdapter(binding settings.ModelBinding, model string) graph.ImageProvider {
	base := OpenAIImages{
		Kind: binding.Kind, APIKey: binding.APIKey, BaseURL: binding.BaseURL, Model: model,
		Quality: binding.ImagesQuality, Style: binding.ImagesStyle, MaskEdit: binding.MaskEdit,
	}
	if binding.Kind == "openai_responses" {
		return OpenAIResponses{OpenAIImages: base}
	}
	return base
}
