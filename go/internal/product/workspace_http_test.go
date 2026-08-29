package product_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

func TestAgentWorkspaceBirthWritesCanvas(t *testing.T) {
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
	product.HTTP{
		Service: product.Service{
			DB:     gdb,
			Media:  media.Store{Files: storage.Local{Root: root}},
			Canvas: agent.WriteProductCanvas,
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

	draft := `{"name":"名称草稿"}`
	dreq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces/drafts", strings.NewReader(draft))
	dreq.Header.Set("Content-Type", "application/json")
	dreq.Header.Set("Idempotency-Key", "draft-key-1")
	for _, c := range cookies {
		dreq.AddCookie(c)
	}
	dresp, err := client.Do(dreq)
	if err != nil {
		t.Fatal(err)
	}
	defer dresp.Body.Close()
	if dresp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(dresp.Body)
		t.Fatalf("draft %d %s", dresp.StatusCode, raw)
	}
	var snap product.WorkspaceSnapshotResponse
	if err := json.NewDecoder(dresp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.Product.CoverImageAssetID != nil || snap.IntakeFinalized {
		t.Fatalf("%+v", snap)
	}
	if snap.Conversation.ProductID == nil || *snap.Conversation.ProductID == "" {
		t.Fatal("missing conversation product")
	}

	wsBody, wsType := workspacePNG(t, map[string]string{
		"name":      "表单齐商品",
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`,
	})
	ws, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces", wsBody)
	ws.Header.Set("Content-Type", wsType)
	ws.Header.Set("Idempotency-Key", "workspace-key-1")
	for _, c := range cookies {
		ws.AddCookie(c)
	}
	wsResp, err := client.Do(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer wsResp.Body.Close()
	if wsResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(wsResp.Body)
		t.Fatalf("workspace %d %s", wsResp.StatusCode, raw)
	}
	var wsCreated product.WorkspaceCreateResponse
	if err := json.NewDecoder(wsResp.Body).Decode(&wsCreated); err != nil {
		t.Fatal(err)
	}
	if wsCreated.Product.CoverImageAssetID != nil {
		t.Fatal("agent form should not set cover")
	}
	if len(wsCreated.Product.Intake) == 0 || string(wsCreated.Product.Intake) == "null" {
		t.Fatal("intake missing")
	}

	got, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v2/agent-product-workspaces/"+snap.Conversation.ID, nil)
	for _, c := range cookies {
		got.AddCookie(c)
	}
	gotResp, err := client.Do(got)
	if err != nil {
		t.Fatal(err)
	}
	defer gotResp.Body.Close()
	if gotResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(gotResp.Body)
		t.Fatalf("get workspace %d %s", gotResp.StatusCode, raw)
	}
	var restored product.WorkspaceSnapshotResponse
	if err := json.NewDecoder(gotResp.Body).Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if restored.Created || restored.IntakeFinalized {
		t.Fatalf("%+v", restored)
	}

	intakeBody, intakeType := workspacePNG(t, map[string]string{
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`,
	})
	intake, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces/"+snap.Conversation.ID+"/intake", intakeBody)
	intake.Header.Set("Content-Type", intakeType)
	intake.Header.Set("Idempotency-Key", "intake-key-1")
	for _, c := range cookies {
		intake.AddCookie(c)
	}
	intakeResp, err := client.Do(intake)
	if err != nil {
		t.Fatal(err)
	}
	defer intakeResp.Body.Close()
	if intakeResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(intakeResp.Body)
		t.Fatalf("intake %d %s", intakeResp.StatusCode, raw)
	}
	var finalized product.WorkspaceSnapshotResponse
	if err := json.NewDecoder(intakeResp.Body).Decode(&finalized); err != nil {
		t.Fatal(err)
	}
	if !finalized.Created || !finalized.IntakeFinalized || len(finalized.CreatedAssets) != 1 {
		t.Fatalf("%+v", finalized)
	}
	if finalized.Product.CoverImageAssetID != nil {
		t.Fatal("intake must not set cover")
	}

	replayBody, replayType := workspacePNG(t, map[string]string{
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`,
	})
	replay, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces/"+snap.Conversation.ID+"/intake", replayBody)
	replay.Header.Set("Content-Type", replayType)
	replay.Header.Set("Idempotency-Key", "intake-key-1")
	for _, c := range cookies {
		replay.AddCookie(c)
	}
	replayResp, err := client.Do(replay)
	if err != nil {
		t.Fatal(err)
	}
	defer replayResp.Body.Close()
	if replayResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(replayResp.Body)
		t.Fatalf("intake replay %d %s", replayResp.StatusCode, raw)
	}
	var replayed product.WorkspaceSnapshotResponse
	if err := json.NewDecoder(replayResp.Body).Decode(&replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.Created || !replayed.IntakeFinalized {
		t.Fatalf("%+v", replayed)
	}
	if replayed.CreatedAssets[0].ID != finalized.CreatedAssets[0].ID {
		t.Fatalf("replay assets %+v %+v", replayed.CreatedAssets, finalized.CreatedAssets)
	}
}

func workspacePNG(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	part, err := w.CreateFormFile("images", "cup.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}
