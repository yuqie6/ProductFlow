package providers

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/config"
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
	pool := openLiveProviderPool(t)
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

func openLiveProviderPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if raw == "" {
		t.Fatal("DATABASE_URL is required for the live provider gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.NormalizePostgresURL(raw))
	if err != nil {
		t.Fatalf("open live provider database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping live provider database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
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
	req := GenerateRequest{
		Prompt:         "white ceramic mug on wood, ecommerce product photo",
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
		_, err := direct.Generate(ctx, req)
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
		got, err := image.Generate(ctx, req)
		if err != nil {
			t.Fatalf("live image success: %v", err)
		}
		if len(got.Bytes) < 256 || !strings.HasPrefix(got.MIME, "image/") {
			t.Fatalf("live image success is not an image: mime=%s bytes=%d", got.MIME, len(got.Bytes))
		}
	})
}

func TestLiveChatSessionGenerate(t *testing.T) {
	live := requireLiveBindings(t)
	image := LiveImage{Store: live.store}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	start := time.Now()
	got, err := image.Generate(ctx, GenerateRequest{Prompt: "小猫", Size: "1024x1024", Mode: ModeChat})
	t.Logf("elapsed=%s err=%v bytes=%d mime=%s id=%s unknown=%v", time.Since(start), err, len(got.Bytes), got.MIME, got.ResponseID, err != nil && isUnknown(err))
	if err != nil {
		t.Fatalf("live chat generate: %v", err)
	}
	if len(got.Bytes) < 256 || !strings.HasPrefix(got.MIME, "image/") {
		t.Fatalf("live chat generate is not an image: mime=%s bytes=%d", got.MIME, len(got.Bytes))
	}
}

func TestLiveLocalImageEdit(t *testing.T) {
	live := requireLiveBindings(t)
	if live.image.Kind != "openai_images" && live.image.Kind != "openai_responses" {
		t.Skipf("image provider %s does not use the OpenAI-compatible images edit contract", live.image.Kind)
	}
	source, mask := liveLocalEditPNGs(t)
	provider := LiveImage{Store: live.store}
	capability := provider.Capability()
	if !capability.Supported {
		t.Fatalf("live local image edit capability is disabled: %s", capability.Reason)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	request := EditRequest{
		SourceBytes: source, SourceMIME: "image/png", MaskPNG: mask,
		Instruction: "Keep the cup unchanged and replace only the masked background with plain white.",
		Operation:   "inpaint", Size: "256x256",
	}
	got, err := provider.Edit(ctx, request)
	if err != nil {
		t.Fatalf("live local image edit: %v", err)
	}
	if len(got.Bytes) < 256 || !strings.HasPrefix(got.MIME, "image/") {
		t.Fatalf("live local image edit is not an image: mime=%s bytes=%d", got.MIME, len(got.Bytes))
	}
}

func liveLocalEditPNGs(t *testing.T) ([]byte, []byte) {
	t.Helper()
	sourceImage := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	maskImage := image.NewNRGBA(sourceImage.Bounds())
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			sourceImage.SetNRGBA(x, y, color.NRGBA{R: 235, G: 222, B: 194, A: 255})
			alpha := uint8(255)
			if x >= 160 && y < 96 {
				alpha = 0
			}
			maskImage.SetNRGBA(x, y, color.NRGBA{A: alpha})
		}
	}
	encode := func(value image.Image) []byte {
		var output bytes.Buffer
		if err := png.Encode(&output, value); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	return encode(sourceImage), encode(maskImage)
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

func liveImageFail(ctx context.Context, binding settings.ModelBinding, req GenerateRequest) error {
	rejected := liveImageAdapter(binding, binding.Model)
	switch img := rejected.(type) {
	case OpenAIImages:
		img.APIKey = "sk-productflow-live-gate-invalid"
		_, err := img.Generate(ctx, req)
		if err != nil && !isUnknown(err) {
			return err
		}
		img.APIKey = binding.APIKey
		img.Transport = liveRefuseTransport()
		_, err = img.Generate(ctx, req)
		return err
	case OpenAIResponses:
		img.APIKey = "sk-productflow-live-gate-invalid"
		_, err := img.Generate(ctx, req)
		if err != nil && !isUnknown(err) {
			return err
		}
		img.APIKey = binding.APIKey
		img.Transport = liveRefuseTransport()
		_, err = img.Generate(ctx, req)
		return err
	default:
		_, err := rejected.Generate(ctx, req)
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

func liveImageAdapter(binding settings.ModelBinding, model string) ImageClient {
	base := OpenAIImages{
		Kind: binding.Kind, APIKey: binding.APIKey, BaseURL: binding.BaseURL, Model: model,
		Quality: binding.ImagesQuality, Style: binding.ImagesStyle, MaskEdit: binding.MaskEdit,
	}
	if binding.Kind == "openai_responses" {
		return OpenAIResponses{OpenAIImages: base}
	}
	return base
}
