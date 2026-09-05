package imagesession

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type sessionServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	root    string
	media   media.Store
	svc     Service
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newSessionServer(t *testing.T) *sessionServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
	return newSessionServerWithDatabase(t, pool, gdb)
}

func newSessionServerWithDatabase(t *testing.T, pool *pgxpool.Pool, gdb *gorm.DB) *sessionServer {
	t.Helper()
	root := t.TempDir()
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})))
	settingsStore := settings.NewStore(pool, config.Config{
		AdminAccessRequired:      true,
		UploadMaxImageBytes:      10 * 1024 * 1024,
		UploadMaxPixels:          16_000_000,
		UploadAllowedMIMETypes:   "image/png,image/jpeg,image/webp",
		UploadMaxBatchFiles:      20,
		UploadMaxBatchBytes:      50 * 1024 * 1024,
		UploadMaxReferenceImages: 6,
		DeletionEnabled:          false,
	})
	auth.HTTP{AdminAccessKey: "k", Store: settingsStore}.Register(engine)
	mediaStore := media.Store{Files: storage.Local{Root: root}}
	svc := Service{DB: gdb, Pool: pool, Media: mediaStore, Settings: settingsStore}
	product.HTTP{Service: product.Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	HTTP{Service: svc, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	ss := &sessionServer{pool: pool, db: gdb, root: root, media: mediaStore, svc: svc, srv: srv, client: &http.Client{}}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	resp, err := ss.client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", resp.StatusCode)
	}
	ss.cookies = resp.Cookies()
	var previousCapacity *string
	_ = ss.pool.QueryRow(context.Background(), `SELECT value FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`).Scan(&previousCapacity)
	_, _ = ss.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW()),
		       ('generation_max_concurrent_tasks', '20', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	t.Cleanup(func() {
		if previousCapacity == nil {
			_, _ = ss.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`)
			return
		}
		_, _ = ss.pool.Exec(context.Background(), `
			UPDATE app_settings SET value = $1, updated_at = NOW() WHERE key = 'generation_max_concurrent_tasks'
		`, *previousCapacity)
	})
	return ss
}

func (ss *sessionServer) dropDispatch(t *testing.T, aggregateID string) {
	t.Helper()
	if _, err := ss.pool.Exec(context.Background(), `DELETE FROM async_dispatches WHERE aggregate_id = $1`, aggregateID); err != nil {
		t.Fatal(err)
	}
}

func (ss *sessionServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ss.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range ss.cookies {
		req.AddCookie(c)
	}
	resp, err := ss.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (ss *sessionServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ss.do(t, method, path, bytes.NewReader(raw), "application/json")
}

func (ss *sessionServer) decode(t *testing.T, resp *http.Response, dest any) {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if dest == nil {
		return
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func (ss *sessionServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
}

func TestImageSessionListCursorPagination(t *testing.T) {
	ss := newSessionServer(t)
	if _, err := ss.pool.Exec(context.Background(), `DELETE FROM image_sessions`); err != nil {
		t.Fatal(err)
	}
	var sessions []DetailResponse
	for _, title := range []string{"一", "二", "三", "四", "五"} {
		created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": title})
		ss.mustStatus(t, created, http.StatusCreated)
		var session DetailResponse
		ss.decode(t, created, &session)
		sessions = append(sessions, session)
	}
	base := time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(time.Now().UnixNano()))
	for i, session := range sessions {
		if _, err := ss.pool.Exec(context.Background(), `UPDATE image_sessions SET updated_at = $1 WHERE id = $2`, base.Add(time.Duration(len(sessions)-i)*time.Minute), session.ID); err != nil {
			t.Fatal(err)
		}
	}

	readPage := func(path string) ListResponse {
		resp := ss.do(t, http.MethodGet, path, nil, "")
		ss.mustStatus(t, resp, http.StatusOK)
		var page ListResponse
		ss.decode(t, resp, &page)
		return page
	}
	first := readPage("/api/image-sessions?limit=2")
	if len(first.Items) != 2 || first.Items[0].ID != sessions[0].ID || first.Items[1].ID != sessions[1].ID || first.NextCursor == nil {
		t.Fatalf("first page %+v", first)
	}
	second := readPage("/api/image-sessions?limit=2&after=" + url.QueryEscape(*first.NextCursor))
	if len(second.Items) != 2 || second.Items[0].ID != sessions[2].ID || second.Items[1].ID != sessions[3].ID || second.NextCursor == nil {
		t.Fatalf("second page %+v", second)
	}
	third := readPage("/api/image-sessions?limit=2&after=" + url.QueryEscape(*second.NextCursor))
	if len(third.Items) == 0 || third.Items[0].ID != sessions[4].ID {
		t.Fatalf("third page %+v", third)
	}
	seen := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		seen[item.ID] = true
	}
	page := third
	for page.Items != nil {
		for _, item := range page.Items {
			for _, session := range sessions {
				if item.ID == session.ID {
					if seen[item.ID] {
						t.Fatalf("session %s appeared twice", item.ID)
					}
					seen[item.ID] = true
				}
			}
		}
		if page.NextCursor == nil {
			break
		}
		page = readPage("/api/image-sessions?limit=2&after=" + url.QueryEscape(*page.NextCursor))
	}
	if len(seen) != len(sessions) {
		t.Fatalf("cursor pages missed sessions: %v", seen)
	}
	for _, path := range []string{"/api/image-sessions?limit=0", "/api/image-sessions?limit=101", "/api/image-sessions?after=invalid"} {
		resp := ss.do(t, http.MethodGet, path, nil, "")
		ss.mustStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	}
}

func TestImageSessionCreateGenerateAndUnknown(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	if session.Title != "未命名会话" {
		t.Fatalf("title %s", session.Title)
	}
	if session.Assets == nil || session.Rounds == nil || session.GenerationTasks == nil {
		t.Fatalf("null lists %+v", session)
	}
	if session.RoundsCount != 0 || session.HistoryNextAfter != nil {
		t.Fatalf("empty session history %+v", session)
	}

	listed := ss.do(t, http.MethodGet, "/api/image-sessions", nil, "")
	ss.mustStatus(t, listed, http.StatusOK)

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "一只杯子", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	if len(session.GenerationTasks) != 1 {
		t.Fatalf("tasks %d", len(session.GenerationTasks))
	}
	taskID := session.GenerationTasks[0].ID
	var dispatchStatus string
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches WHERE actor_name = 'run_image_session_generation_task' AND aggregate_id = $1
	`, taskID).Scan(&dispatchStatus); err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != "pending" && dispatchStatus != "sent" {
		t.Fatalf("dispatch %s", dispatchStatus)
	}
	ss.dropDispatch(t, taskID)

	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	if session.GenerationTasks[0].Status != "succeeded" {
		t.Fatalf("status %s reason %+v", session.GenerationTasks[0].Status, session.GenerationTasks[0].FailureReason)
	}

	unknown := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "再来一张", "size": "1024x1024", "generation_count": 1,
		"base_asset_id": session.Rounds[0].GeneratedAsset.ID,
	})
	ss.mustStatus(t, unknown, http.StatusAccepted)
	ss.decode(t, unknown, &session)
	failID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, failID)
	failExec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: errors.New("provider crashed")}}
	if err := failExec.Execute(context.Background(), failID); err != nil {
		t.Fatal(err)
	}
	got = ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	var unknownTask *TaskResponse
	for i := range session.GenerationTasks {
		if session.GenerationTasks[i].ID == failID {
			unknownTask = &session.GenerationTasks[i]
		}
	}
	if unknownTask == nil || unknownTask.Status != "unknown" {
		t.Fatalf("unknown %+v", unknownTask)
	}
	if unknownTask.IsRetryable {
		t.Fatal("unknown must not be retryable")
	}
	if unknownTask.FailureReason == nil || *unknownTask.FailureReason != unknownDetail {
		t.Fatalf("reason %+v", unknownTask.FailureReason)
	}
	if unknownTask.ProgressPhase == nil || *unknownTask.ProgressPhase != unknownPhase {
		t.Fatalf("phase %+v", unknownTask.ProgressPhase)
	}
}

func TestImageSessionTextOutputFailedNotUnknown(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "小猫", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	taskID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, taskID)
	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: ErrTextOutput}}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	task := session.GenerationTasks[0]
	if task.Status != "failed" {
		t.Fatalf("status %s reason %+v", task.Status, task.FailureReason)
	}
	if task.IsRetryable {
		t.Fatal("confirmed provider failure must not auto-retry")
	}
	if task.Attempts != 1 {
		t.Fatalf("attempts %d", task.Attempts)
	}
	if task.FailureReason == nil || *task.FailureReason != ErrTextOutput.Error() {
		t.Fatalf("reason %+v", task.FailureReason)
	}
}

func TestImageSessionAttachToProduct(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "附加会话"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "杯子", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	ss.dropDispatch(t, session.GenerationTasks[0].ID)
	if err := (Executor{DB: ss.db, Media: ss.media}).Execute(context.Background(), session.GenerationTasks[0].ID); err != nil {
		t.Fatal(err)
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	assetID := session.Rounds[0].GeneratedAsset.ID

	body, ctype := productMultipart(t)
	prod := ss.do(t, http.MethodPost, "/api/v2/products", body, ctype)
	ss.mustStatus(t, prod, http.StatusCreated)
	var createdProd product.CreateResponse
	ss.decode(t, prod, &createdProd)

	attached := ss.doJSON(t, http.MethodPost, "/api/v2/image-sessions/"+session.ID+"/assets/"+assetID+"/attach-to-product", map[string]any{
		"product_id": createdProd.Product.ID,
	})
	ss.mustStatus(t, attached, http.StatusOK)
	var asset product.AssetResponse
	ss.decode(t, attached, &asset)
	if asset.OriginType != "image_session_attach" {
		t.Fatalf("origin %s", asset.OriginType)
	}

	again := ss.doJSON(t, http.MethodPost, "/api/v2/image-sessions/"+session.ID+"/assets/"+assetID+"/attach-to-product", map[string]any{
		"product_id": createdProd.Product.ID,
	})
	ss.mustStatus(t, again, http.StatusOK)
	var replay product.AssetResponse
	ss.decode(t, again, &replay)
	if replay.ID != asset.ID {
		t.Fatalf("idempotent %s vs %s", replay.ID, asset.ID)
	}
}

func TestImageSessionMissingIs404(t *testing.T) {
	ss := newSessionServer(t)
	resp := ss.do(t, http.MethodGet, "/api/image-sessions/00000000-0000-4000-8000-000000000001", nil, "")
	ss.mustStatus(t, resp, http.StatusNotFound)
	var body struct {
		Detail string `json:"detail"`
	}
	ss.decode(t, resp, &body)
	if body.Detail != "连续生图会话不存在" {
		t.Fatalf("%s", body.Detail)
	}
}

func TestImageSessionUpdateRejectsEmptyTitle(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "原标题"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	for _, title := range []string{"", "   "} {
		resp := ss.doJSON(t, http.MethodPatch, "/api/image-sessions/"+session.ID, map[string]any{"title": title})
		ss.mustStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	if session.Title != "原标题" {
		t.Fatalf("title rewritten to %q", session.Title)
	}
}

func TestImageSessionGenerateRejectsZeroCount(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	resp := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "数量非法", "size": "1024x1024", "generation_count": 0,
	})
	ss.mustStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

func TestImageSessionGenerateRejectsLongPromptAndInvalidToolOptions(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	long := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": strings.Repeat("字", 4001), "size": "1024x1024",
	})
	ss.mustStatus(t, long, http.StatusBadRequest)
	var longBody struct {
		Detail string `json:"detail"`
	}
	ss.decode(t, long, &longBody)
	if longBody.Detail != "提示词不能超过 4000 个字符" {
		t.Fatalf("detail %s", longBody.Detail)
	}
	paddedEmpty := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": strings.Repeat(" ", 4001), "size": "1024x1024",
	})
	ss.mustStatus(t, paddedEmpty, http.StatusBadRequest)
	var emptyBody struct {
		Detail string `json:"detail"`
	}
	ss.decode(t, paddedEmpty, &emptyBody)
	if emptyBody.Detail != "提示词不能为空" {
		t.Fatalf("padded empty detail %s", emptyBody.Detail)
	}
	badTool := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "杯子", "size": "1024x1024", "tool_options": map[string]any{"quality": "ultra"},
	})
	ss.mustStatus(t, badTool, http.StatusBadRequest)
	badTool.Body.Close()
	withN := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "杯子", "size": "1024x1024", "tool_options": map[string]any{"n": 2},
	})
	ss.mustStatus(t, withN, http.StatusAccepted)
	var generated DetailResponse
	ss.decode(t, withN, &generated)
	if len(generated.GenerationTasks) != 1 || generated.GenerationTasks[0].GenerationCount != 1 {
		t.Fatalf("tool_options.n must not become generation_count: %+v", generated.GenerationTasks)
	}
}

func TestImageSessionCreateRejectsUnknownFields(t *testing.T) {
	ss := newSessionServer(t)
	resp := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "a", "foo": 1})
	ss.mustStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

func (ss *sessionServer) seedGeneratedRounds(t *testing.T, sessionID string, n int, start time.Time) []string {
	t.Helper()
	ids := make([]string, 0, n)
	byteSize := int64(1024)
	width, height := 8, 8
	sha := strings.Repeat("a", 64)
	for i := 0; i < n; i++ {
		created := start.Add(time.Duration(i) * time.Second)
		mediaID := clockid.New()
		assetID := clockid.New()
		roundID := clockid.New()
		storagePath := "image-session-seed/" + mediaID + ".png"
		verifiedAt := created
		if err := ss.db.Create(&schema.MediaObjects{
			ID: mediaID, StoragePath: storagePath, MIMEType: "image/png", ByteSize: &byteSize,
			Width: &width, Height: &height, SHA256: &sha, VerificationStatus: "verified",
			CreatedAt: created, VerifiedAt: &verifiedAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := ss.db.Create(&schema.ImageSessionAssets{
			ID: assetID, SessionID: sessionID, Kind: kindGenerated, OriginalFilename: "seed.png",
			MIMEType: "image/png", StoragePath: storagePath, CreatedAt: created, MediaObjectID: mediaID,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := ss.db.Create(&schema.ImageSessionRounds{
			ID: roundID, SessionID: sessionID, Prompt: "种子轮次", AssistantMessage: defaultAssistant,
			Size: "1024x1024", ModelName: "mock", ProviderName: "mock", PromptVersion: "seed",
			GeneratedAssetID: assetID, CreatedAt: created, CandidateIndex: 1, CandidateCount: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, roundID)
	}
	return ids
}

func TestImageSessionGetReturnsFirstScreenAndHistoryPages(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "分页会话"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	ids := ss.seedGeneratedRounds(t, session.ID, 25, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	if session.RoundsCount != 25 {
		t.Fatalf("rounds_count %d", session.RoundsCount)
	}
	if len(session.Rounds) != 20 || session.HistoryNextAfter == nil {
		t.Fatalf("first screen rounds %d next %+v", len(session.Rounds), session.HistoryNextAfter)
	}
	for _, asset := range session.Assets {
		if asset.Kind != kindReference {
			t.Fatalf("detail assets must be references only: %+v", asset)
		}
	}
	latestIDs := ids[len(ids)-20:]
	gotIDs := make([]string, 0, len(session.Rounds))
	for i, round := range session.Rounds {
		gotIDs = append(gotIDs, round.ID)
		want := latestIDs[len(latestIDs)-1-i]
		if round.ID != want {
			t.Fatalf("round %d got %s want %s", i, round.ID, want)
		}
	}

	statusResp := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID+"/status", nil, "")
	ss.mustStatus(t, statusResp, http.StatusOK)
	var status StatusResponse
	ss.decode(t, statusResp, &status)
	if status.RoundsCount != 25 || status.LatestRoundID == nil || *status.LatestRoundID != ids[len(ids)-1] {
		t.Fatalf("status %+v latest want %s", status, ids[len(ids)-1])
	}
	if len(status.GenerationTasks) != 0 {
		t.Fatalf("status must not dump historical tasks: %+v", status.GenerationTasks)
	}

	readHistory := func(path string) HistoryResponse {
		t.Helper()
		resp := ss.do(t, http.MethodGet, path, nil, "")
		ss.mustStatus(t, resp, http.StatusOK)
		var page HistoryResponse
		ss.decode(t, resp, &page)
		return page
	}
	older := readHistory("/api/image-sessions/" + session.ID + "/history?after=" + url.QueryEscape(*session.HistoryNextAfter) + "&limit=20")
	if len(older.Items) != 5 || older.NextAfter != nil {
		t.Fatalf("older page %+v", older)
	}
	seen := map[string]bool{}
	for _, round := range session.Rounds {
		seen[round.ID] = true
	}
	for _, round := range older.Items {
		if seen[round.ID] {
			t.Fatalf("round %s duplicated across pages", round.ID)
		}
		seen[round.ID] = true
	}
	if len(seen) != 25 {
		t.Fatalf("pages missed rounds: %d", len(seen))
	}
	for i, round := range older.Items {
		want := ids[len(ids)-21-i]
		if round.ID != want {
			t.Fatalf("older %d got %s want %s", i, round.ID, want)
		}
	}

	for _, path := range []string{
		"/api/image-sessions/" + session.ID + "/history?limit=0",
		"/api/image-sessions/" + session.ID + "/history?limit=101",
		"/api/image-sessions/" + session.ID + "/history?after=invalid",
	} {
		resp := ss.do(t, http.MethodGet, path, nil, "")
		ss.mustStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	}
}

func TestImageSessionHistoryMissingSessionIs404(t *testing.T) {
	ss := newSessionServer(t)
	resp := ss.do(t, http.MethodGet, "/api/image-sessions/00000000-0000-4000-8000-000000000001/history", nil, "")
	ss.mustStatus(t, resp, http.StatusNotFound)
	var body struct {
		Detail string `json:"detail"`
	}
	ss.decode(t, resp, &body)
	if body.Detail != "连续生图会话不存在" {
		t.Fatalf("%s", body.Detail)
	}
}

func TestImageSessionGetBoundsTasksWithoutCrowdingActiveOrFirstScreen(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	roundIDs := ss.seedGeneratedRounds(t, session.ID, 25, start)
	wantIDs := map[string]bool{}
	var rows []schema.ImageSessionGenerationTasks
	for i, roundID := range roundIDs {
		groupID := clockid.New()
		if err := ss.db.Model(&schema.ImageSessionRounds{}).Where("id = ?", roundID).
			Update("generation_group_id", groupID).Error; err != nil {
			t.Fatal(err)
		}
		row := schema.ImageSessionGenerationTasks{
			ID: clockid.New(), SessionID: session.ID, Status: "succeeded", Prompt: "first screen",
			Size: "1024x1024", GenerationCount: 1, CompletedCandidates: 1,
			ResultGenerationGroupID: &groupID, CreatedAt: start.Add(time.Duration(i) * time.Second),
		}
		// 此任务同时属于活动集和首屏轮次集，结果必须去重。
		if i == 24 {
			row.Status = "running"
			row.CreatedAt = start.Add(200 * time.Second)
			attemptID := clockid.New()
			row.ActiveAttemptID, row.StartedAt = &attemptID, &row.CreatedAt
		}
		rows = append(rows, row)
		if i >= 5 {
			wantIDs[row.ID] = true
		}
	}
	for i := 0; i < 25; i++ {
		row := schema.ImageSessionGenerationTasks{
			ID: clockid.New(), SessionID: session.ID, Status: imageSessionActiveTaskStatuses[i%2],
			Prompt: "active", Size: "1024x1024", GenerationCount: 1,
			CreatedAt: start.Add(time.Duration(100+i) * time.Second),
		}
		if row.Status == "running" {
			attemptID := clockid.New()
			row.ActiveAttemptID, row.StartedAt = &attemptID, &row.CreatedAt
		}
		rows = append(rows, row)
		if i >= 6 {
			wantIDs[row.ID] = true
		}
	}
	statuses := []string{"failed", "unknown", "cancelled", "succeeded", "succeeded"}
	terminalPrefix := clockid.New()[:24]
	retryID := fmt.Sprintf("%s-%02d", terminalPrefix, 25)
	for i := 0; i < 30; i++ {
		row := schema.ImageSessionGenerationTasks{
			ID: fmt.Sprintf("%s-%02d", terminalPrefix, i), SessionID: session.ID, Status: statuses[i%len(statuses)],
			Prompt: "recent terminal", Size: "1024x1024", GenerationCount: 1,
			CreatedAt: start.Add(300 * time.Second), IsRetryable: statuses[i%len(statuses)] == "failed",
		}
		if i%len(statuses) == 4 {
			missingGroup := clockid.New()
			row.ResultGenerationGroupID = &missingGroup
		}
		rows = append(rows, row)
		if i >= 10 {
			wantIDs[row.ID] = true
		}
	}
	if err := ss.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	otherSession := schema.ImageSessions{ID: clockid.New(), Title: "other", CreatedAt: start, UpdatedAt: start}
	if err := ss.db.Create(&otherSession).Error; err != nil {
		t.Fatal(err)
	}
	otherTask := rows[24]
	otherTask.ID, otherTask.SessionID = clockid.New(), otherSession.ID
	if err := ss.db.Create(&otherTask).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ss.dropDispatch(t, retryID)
		if err := ss.db.Where("session_id IN ?", []string{session.ID, otherSession.ID}).Delete(&schema.ImageSessionGenerationTasks{}).Error; err != nil {
			t.Error(err)
		}
	})

	response := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, response, http.StatusOK)
	ss.decode(t, response, &session)
	if len(session.GenerationTasks) != 59 {
		t.Fatalf("tasks=%d, want 59 (three bounded sets with one overlap)", len(session.GenerationTasks))
	}
	for i, task := range session.GenerationTasks {
		if !wantIDs[task.ID] {
			t.Fatalf("unexpected or duplicate task: %+v", task)
		}
		delete(wantIDs, task.ID)
		if i > 0 {
			previous := session.GenerationTasks[i-1]
			if previous.CreatedAt.Before(task.CreatedAt) || (previous.CreatedAt.Equal(task.CreatedAt) && previous.ID < task.ID) {
				t.Fatalf("unstable task ordering: %s before %s", previous.ID, task.ID)
			}
		}
	}
	if len(wantIDs) != 0 {
		t.Fatalf("missing active/terminal/first-screen tasks: %v", wantIDs)
	}
	if len(session.Rounds) != 20 || session.RoundsCount != 25 || session.HistoryNextAfter == nil {
		t.Fatalf("task bound changed round pagination: %+v", session)
	}
	statusResponse := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID+"/status", nil, "")
	ss.mustStatus(t, statusResponse, http.StatusOK)
	var status StatusResponse
	ss.decode(t, statusResponse, &status)
	if !status.HasActiveGenerationTask || len(status.GenerationTasks) != 26 {
		t.Fatalf("detail limit must not change status/SSE active set: %+v", status)
	}

	// 重试旧任务不改变 created_at，仍须进入单独的活动集。
	retry := ss.do(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generation-tasks/"+retryID+"/retry", nil, "")
	ss.mustStatus(t, retry, http.StatusAccepted)
	ss.decode(t, retry, &session)
	foundRetried := false
	for _, task := range session.GenerationTasks {
		if task.ID == retryID {
			foundRetried = task.Status == "queued"
		}
	}
	if !foundRetried {
		t.Fatal("retry response lost queued task")
	}
}

func productMultipart(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "附加商品")
	part, err := w.CreateFormFile("images", "cup.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes(t, 8, 6)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestImageSessionEventsStreamTerminalStatus(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "一只杯子", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	if len(session.GenerationTasks) != 1 {
		t.Fatalf("tasks %d", len(session.GenerationTasks))
	}
	taskID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, taskID)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ss.srv.URL+"/api/image-sessions/"+session.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range ss.cookies {
		req.AddCookie(cookie)
	}
	resp, err := ss.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("events status %d %s", resp.StatusCode, raw)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type %s", resp.Header.Get("Content-Type"))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	first, err := readSessionStatusEvent(scanner)
	if err != nil {
		t.Fatalf("first status event: %v", err)
	}
	if first.ID != session.ID || !first.HasActiveGenerationTask {
		t.Fatalf("first status %+v", first)
	}

	execErr := make(chan error, 1)
	go func() {
		exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}
		execErr <- exec.Execute(context.Background(), taskID)
	}()

	for {
		status, err := readSessionStatusEvent(scanner)
		if err != nil {
			select {
			case exec := <-execErr:
				if exec != nil {
					t.Fatalf("execute: %v", exec)
				}
			default:
			}
			t.Fatalf("terminal status event: %v", err)
		}
		if status.HasActiveGenerationTask {
			continue
		}
		if status.RoundsCount < 1 || status.LatestRoundID == nil {
			t.Fatalf("terminal rounds %d latest %+v", status.RoundsCount, status.LatestRoundID)
		}
		for _, task := range status.GenerationTasks {
			if task.ID == taskID && (task.Status == "queued" || task.Status == "running") {
				t.Fatalf("terminal status still lists active task %+v", task)
			}
		}
		break
	}
	if err := <-execErr; err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func readSessionStatusEvent(scanner *bufio.Scanner) (StatusResponse, error) {
	var event, data string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case line == "":
			if event != "session.status" || data == "" {
				event, data = "", ""
				continue
			}
			var status StatusResponse
			if err := json.Unmarshal([]byte(data), &status); err != nil {
				return StatusResponse{}, err
			}
			return status, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return StatusResponse{}, err
	}
	return StatusResponse{}, io.EOF
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
