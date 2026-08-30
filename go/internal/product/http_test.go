package product

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
)

func pngFile(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNormalizePrice(t *testing.T) {
	if _, err := normalizePrice("abc"); err == nil {
		t.Fatal("expected error")
	}
	got, err := normalizePrice("79.00")
	if err != nil || got == nil || *got != "79.00" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestBirthCommands(t *testing.T) {
	pool, gdb := testdb.Open(t)
	root := t.TempDir()
	engine := httpx.NewEngine(nil)
	store := httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})
	engine.Use(httpx.Session(store))
	settingsStore := settings.NewStore(pool, config.Config{
		AdminAccessRequired:      true,
		UploadMaxImageBytes:      10 * 1024 * 1024,
		UploadMaxPixels:          16_000_000,
		UploadAllowedMIMETypes:   "image/png,image/jpeg,image/webp",
		UploadMaxBatchFiles:      20,
		UploadMaxBatchBytes:      50 * 1024 * 1024,
		UploadMaxReferenceImages: 6,
	})
	auth.HTTP{AdminAccessKey: "k", Store: settingsStore}.Register(engine)
	HTTP{
		Service: Service{
			DB:    gdb,
			Media: media.Store{Files: storage.Local{Root: root}},
		},
		Settings: settingsStore,
	}.Register(engine)
	srv := httptest.NewServer(engine)
	defer srv.Close()

	client := &http.Client{}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	loginResp, err := client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", loginResp.StatusCode)
	}
	cookies := loginResp.Cookies()

	body, contentType := multipartPNG(t, map[string]string{
		"name":     "露营保温杯",
		"category": "户外",
		"price":    "79.00",
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/products", body)
	req.Header.Set("Content-Type", contentType)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("v2 create %d %s", resp.StatusCode, raw)
	}
	var created CreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Product.CoverImageAssetID == nil || len(created.CreatedAssets) != 1 {
		t.Fatalf("%+v", created)
	}
	var graphCount int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM workflow_graphs WHERE product_id = $1`, created.Product.ID).Scan(&graphCount); err != nil {
		t.Fatal(err)
	}
	if graphCount != 0 {
		t.Fatalf("v2 should not write graph: %d", graphCount)
	}

	preview, _ := http.NewRequest(http.MethodGet, srv.URL+created.CreatedAssets[0].PreviewURL, nil)
	for _, c := range cookies {
		preview.AddCookie(c)
	}
	previewResp, err := client.Do(preview)
	if err != nil {
		t.Fatal(err)
	}
	previewResp.Body.Close()
	if previewResp.StatusCode != http.StatusOK {
		t.Fatalf("preview %d", previewResp.StatusCode)
	}

	v3Body, v3Type := multipartPNG(t, map[string]string{
		"name":        "直接创建演示商品",
		"image_types": `[{"key":"hero","quantity":1}]`,
	})
	v3, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v3/products", v3Body)
	v3.Header.Set("Content-Type", v3Type)
	for _, c := range cookies {
		v3.AddCookie(c)
	}
	v3Resp, err := client.Do(v3)
	if err != nil {
		t.Fatal(err)
	}
	defer v3Resp.Body.Close()
	if v3Resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(v3Resp.Body)
		t.Fatalf("v3 create %d %s", v3Resp.StatusCode, raw)
	}
	var direct DirectCreateResponse
	if err := json.NewDecoder(v3Resp.Body).Decode(&direct); err != nil {
		t.Fatal(err)
	}
	if direct.Graph["schema_version"] != float64(3) && direct.Graph["schema_version"] != 3 {
		t.Fatalf("graph %+v", direct.Graph)
	}
	if direct.Product.CoverImageAssetID == nil {
		t.Fatal("v3 missing cover")
	}

	presetBody, presetType := multipartPNG(t, map[string]string{
		"name":                "带平台默认交付商品",
		"image_types":         `[{"key":"hero","quantity":1}]`,
		"delivery_preset_key": "jd_hero",
	})
	presetReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v3/products", presetBody)
	presetReq.Header.Set("Content-Type", presetType)
	for _, c := range cookies {
		presetReq.AddCookie(c)
	}
	presetResp, err := client.Do(presetReq)
	if err != nil {
		t.Fatal(err)
	}
	defer presetResp.Body.Close()
	if presetResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(presetResp.Body)
		t.Fatalf("v3 preset %d %s", presetResp.StatusCode, raw)
	}
	var withPreset DirectCreateResponse
	if err := json.NewDecoder(presetResp.Body).Decode(&withPreset); err != nil {
		t.Fatal(err)
	}
	nodes, _ := withPreset.Graph["nodes"].([]any)
	found := 0
	for _, rawNode := range nodes {
		node, _ := rawNode.(map[string]any)
		if node["node_type"] != "image_generation" {
			continue
		}
		found++
		cfg, _ := node["config"].(map[string]any)
		spec, _ := cfg["delivery_spec"].(map[string]any)
		if spec["width"] != float64(1200) || spec["height"] != float64(1200) || spec["format"] != "png" || spec["fit"] != "contain" {
			t.Fatalf("delivery_spec %+v", spec)
		}
	}
	if found == 0 {
		t.Fatal("missing image_generation node")
	}

	unknownBody, unknownType := multipartPNG(t, map[string]string{
		"name":                "未知交付预设商品",
		"image_types":         `[{"key":"hero","quantity":1}]`,
		"delivery_preset_key": "unknown-preset",
	})
	unknownReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v3/products", unknownBody)
	unknownReq.Header.Set("Content-Type", unknownType)
	for _, c := range cookies {
		unknownReq.AddCookie(c)
	}
	unknownResp, err := client.Do(unknownReq)
	if err != nil {
		t.Fatal(err)
	}
	defer unknownResp.Body.Close()
	if unknownResp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(unknownResp.Body)
		t.Fatalf("unknown preset %d %s", unknownResp.StatusCode, raw)
	}
	var detail map[string]any
	if err := json.NewDecoder(unknownResp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail["detail"] != "未知 DeliverySpec 预设: unknown-preset" {
		t.Fatalf("%+v", detail)
	}
}

func multipartPNG(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	part, err := w.CreateFormFile("images", "cup.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngFile(t, 8, 6)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}
