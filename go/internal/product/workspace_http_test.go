package product_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/auth"
	_ "github.com/yuqie6/productflow/internal/delivery"
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

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	draftKey := "draft-key-" + t.Name() + "-" + suffix
	workspaceKey := "workspace-key-" + t.Name() + "-" + suffix
	intakeKey := "intake-key-" + t.Name() + "-" + suffix
	presetKey := "workspace-preset-key-" + t.Name() + "-" + suffix
	badPresetKey := "workspace-bad-preset-" + t.Name() + "-" + suffix

	draft := `{"name":"名称草稿"}`
	dreq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces/drafts", strings.NewReader(draft))
	dreq.Header.Set("Content-Type", "application/json")
	dreq.Header.Set("Idempotency-Key", draftKey)
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
	if snap.Conversation.SessionID == nil {
		t.Fatal("missing conversation session")
	}
	if _, err := pool.Exec(context.Background(), `UPDATE agent_sessions SET summary = 'stale-summary' WHERE id = $1`, *snap.Conversation.SessionID); err != nil {
		t.Fatal(err)
	}

	wsBody, wsType := workspacePNG(t, map[string]string{
		"name":      "表单齐商品",
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`,
	})
	ws, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces", wsBody)
	ws.Header.Set("Content-Type", wsType)
	ws.Header.Set("Idempotency-Key", workspaceKey)
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
		"task_id":   "task-from-create-page",
	})
	intake, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces/"+snap.Conversation.ID+"/intake", intakeBody)
	intake.Header.Set("Content-Type", intakeType)
	intake.Header.Set("Idempotency-Key", intakeKey)
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
	var summary string
	if err := pool.QueryRow(context.Background(), `SELECT summary FROM agent_sessions WHERE id = $1`, *snap.Conversation.SessionID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "暂无 Agent Task" {
		t.Fatalf("session summary %q", summary)
	}

	replayBody, replayType := workspacePNG(t, map[string]string{
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`,
	})
	replay, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces/"+snap.Conversation.ID+"/intake", replayBody)
	replay.Header.Set("Content-Type", replayType)
	replay.Header.Set("Idempotency-Key", intakeKey)
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

	presetBody, presetType := workspacePNG(t, map[string]string{
		"name":      "带交付预设工作区",
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}],"delivery_preset_key":"jd_hero"}`,
	})
	presetReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces", presetBody)
	presetReq.Header.Set("Content-Type", presetType)
	presetReq.Header.Set("Idempotency-Key", presetKey)
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
		t.Fatalf("workspace preset %d %s", presetResp.StatusCode, raw)
	}
	var presetCreated product.WorkspaceCreateResponse
	if err := json.NewDecoder(presetResp.Body).Decode(&presetCreated); err != nil {
		t.Fatal(err)
	}
	var intakeDoc map[string]any
	if err := json.Unmarshal(presetCreated.Product.Intake, &intakeDoc); err != nil {
		t.Fatal(err)
	}
	if intakeDoc["delivery_preset_key"] != "jd_hero" {
		t.Fatalf("intake %+v", intakeDoc)
	}
	spec, _ := intakeDoc["delivery_spec"].(map[string]any)
	if spec["width"] != float64(1200) || spec["format"] != "png" {
		t.Fatalf("delivery_spec %+v", spec)
	}

	badBody, badType := workspacePNG(t, map[string]string{
		"name":      "未知预设工作区",
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}],"delivery_preset_key":"not-a-real-preset"}`,
	})
	badReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/agent-product-workspaces", badBody)
	badReq.Header.Set("Content-Type", badType)
	badReq.Header.Set("Idempotency-Key", badPresetKey)
	for _, c := range cookies {
		badReq.AddCookie(c)
	}
	badResp, err := client.Do(badReq)
	if err != nil {
		t.Fatal(err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(badResp.Body)
		t.Fatalf("unknown workspace preset %d %s", badResp.StatusCode, raw)
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
