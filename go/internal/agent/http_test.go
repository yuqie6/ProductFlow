package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type mockGateway struct{}

type recordingGateway struct {
	mockGateway
	pageContext any
}

func (g *recordingGateway) StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error) {
	g.pageContext = pageContext
	return g.mockGateway.StartTurn(conversationID, taskID, inputText, assetIDs, idempotencyKey, pageContext)
}

func (mockGateway) Configured() bool { return true }

func (mockGateway) StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error) {
	runID := conversationID
	if taskID != nil && *taskID != "" {
		runID = *taskID
	}
	// harness_turn_id 全局唯一；不能用固定 idempotency 后缀，共享库会撞约束。
	return TurnState{
		APIVersion: "1", RunID: runID, TurnID: "ht-" + clockid.New(),
		Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}, nil
}

func (mockGateway) GetTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	runID := conversationID
	if taskID != nil && *taskID != "" {
		runID = *taskID
	}
	return TurnState{APIVersion: "1", RunID: runID, TurnID: turnID, Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func (mockGateway) CancelTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	st, _ := mockGateway{}.GetTurn(conversationID, turnID, taskID)
	st.Status = "canceled"
	return st, nil
}

func (mockGateway) ResumeTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return mockGateway{}.GetTurn(conversationID, turnID, taskID)
}

func (mockGateway) AnswerQuestion(conversationID, turnID, questionID string, answer map[string]any, taskID *string) (TurnState, error) {
	return mockGateway{}.GetTurn(conversationID, turnID, taskID)
}

func (mockGateway) StreamTurnEvents(ctx context.Context, conversationID, turnID string, taskID *string, after int, w io.Writer) error {
	if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

type questionGateway struct {
	mockGateway
	answerErr       error
	resumeErr       error
	startErr        error
	calls           []string
	startCount      int
	lastStartAssets []string
}

func (g *questionGateway) AnswerQuestion(conversationID, turnID, questionID string, answer map[string]any, taskID *string) (TurnState, error) {
	g.calls = append(g.calls, "answer:"+turnID)
	if g.answerErr != nil {
		return TurnState{}, g.answerErr
	}
	st, _ := mockGateway{}.GetTurn(conversationID, turnID, taskID)
	st.Status = "queued"
	st.Question = nil
	return st, nil
}

func (g *questionGateway) ResumeTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	g.calls = append(g.calls, "resume:"+turnID)
	if g.resumeErr != nil {
		return TurnState{}, g.resumeErr
	}
	st, _ := mockGateway{}.GetTurn(conversationID, turnID, taskID)
	st.Status = "running"
	st.Question = nil
	return st, nil
}

func (g *questionGateway) CancelTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	g.calls = append(g.calls, "cancel:"+turnID)
	return mockGateway{}.CancelTurn(conversationID, turnID, taskID)
}

func (g *questionGateway) StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error) {
	g.calls = append(g.calls, "start")
	g.startCount++
	g.lastStartAssets = append([]string(nil), assetIDs...)
	if g.startErr != nil {
		return TurnState{}, g.startErr
	}
	return g.mockGateway.StartTurn(conversationID, taskID, inputText, assetIDs, idempotencyKey, pageContext)
}

type agentServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	svc     Service
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newAgentServer(t *testing.T, gw Gateway, internalToken string) *agentServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
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
	})
	auth.HTTP{AdminAccessKey: "k", Store: settingsStore}.Register(engine)
	mediaStore := media.Store{Files: storage.Local{Root: root}}
	product.HTTP{Service: product.Service{DB: gdb, Media: mediaStore, Canvas: WriteProductCanvas}, Settings: settingsStore}.Register(engine)
	svc := Service{
		DB: gdb, Graph: graph.Service{DB: gdb, AfterRunStatus: SyncGraphRunToTasks, Products: product.GraphGuard{}},
		Product: product.Service{DB: gdb, Media: mediaStore, Canvas: WriteProductCanvas},
		Library: library.Service{DB: gdb, Media: mediaStore},
		Media:   mediaStore, Settings: settingsStore, Gateway: gw, Poll: time.Millisecond,
	}
	HTTP{Service: svc, Settings: settingsStore, InternalToken: internalToken}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	as := &agentServer{pool: pool, db: gdb, svc: svc, srv: srv, client: &http.Client{}}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	resp, err := as.client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", resp.StatusCode)
	}
	as.cookies = resp.Cookies()
	_, _ = as.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return as
}

func (as *agentServer) do(t *testing.T, method, path string, body io.Reader, contentType string, extra http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, as.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for _, c := range as.cookies {
		req.AddCookie(c)
	}
	resp, err := as.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (as *agentServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return as.do(t, method, path, bytes.NewReader(raw), "application/json", nil)
}

func (as *agentServer) decode(t *testing.T, resp *http.Response, dest any) {
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

func (as *agentServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
}

func (as *agentServer) mustDetail(t *testing.T, resp *http.Response, wantStatus int, wantDetail string) {
	t.Helper()
	as.mustStatus(t, resp, wantStatus)
	var body map[string]any
	as.decode(t, resp, &body)
	if body["detail"] != wantDetail {
		t.Fatalf("detail %v want %s", body["detail"], wantDetail)
	}
}

func TestAgentSessionCRUD(t *testing.T) {
	as := newAgentServer(t, HTTPGateway{}, "")
	created := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, created, http.StatusCreated)
	var session SessionResponse
	as.decode(t, created, &session)
	if session.Title != "新会话" {
		t.Fatalf("title %s", session.Title)
	}
	if len(session.Conversations) == 0 || session.Conversations[0].ProductName != "全局 Agent" {
		t.Fatalf("conversations %+v", session.Conversations)
	}

	listed := as.do(t, http.MethodGet, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, listed, http.StatusOK)
	var page SessionListResponse
	as.decode(t, listed, &page)
	if len(page.Items) == 0 {
		t.Fatal("empty list")
	}

	renamed := as.doJSON(t, http.MethodPatch, "/api/v2/agent-sessions/"+session.ID, map[string]any{"title": "工作会话"})
	as.mustStatus(t, renamed, http.StatusOK)
	as.decode(t, renamed, &session)
	if session.Title != "工作会话" {
		t.Fatalf("rename %s", session.Title)
	}

	empty := as.doJSON(t, http.MethodPatch, "/api/v2/agent-sessions/"+session.ID, map[string]any{"title": "  "})
	as.mustStatus(t, empty, http.StatusBadRequest)
	empty.Body.Close()

	archived := as.do(t, http.MethodPost, "/api/v2/agent-sessions/"+session.ID+"/archive", nil, "", nil)
	as.mustStatus(t, archived, http.StatusOK)
	as.decode(t, archived, &session)
	if session.Status != "archived" {
		t.Fatalf("archive %s", session.Status)
	}
}

func TestAgentUnauthorizedAndInternalAuth(t *testing.T) {
	as := newAgentServer(t, HTTPGateway{}, "internal-token")
	req, err := http.NewRequest(http.MethodGet, as.srv.URL+"/api/v2/agent-sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	as.mustStatus(t, resp, http.StatusUnauthorized)
	var detail map[string]string
	as.decode(t, resp, &detail)
	if detail["detail"] != "请先登录" {
		t.Fatalf("detail %v", detail)
	}

	unauth := as.do(t, http.MethodGet, "/api/internal/v1/agent-runtime/provider-config", nil, "", nil)
	as.mustStatus(t, unauth, http.StatusUnauthorized)
	if unauth.Header.Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("www-authenticate %s", unauth.Header.Get("WWW-Authenticate"))
	}
	unauth.Body.Close()

	wrong := as.do(t, http.MethodGet, "/api/internal/v1/agent-runtime/provider-config", nil, "", http.Header{"Authorization": []string{"Bearer nope"}})
	as.mustStatus(t, wrong, http.StatusUnauthorized)
	wrong.Body.Close()
}

func TestAgentInternalNotConfigured(t *testing.T) {
	as := newAgentServer(t, HTTPGateway{}, "")
	resp := as.do(t, http.MethodGet, "/api/internal/v1/agent-runtime/provider-config", nil, "", http.Header{"Authorization": []string{"Bearer x"}})
	as.mustStatus(t, resp, http.StatusServiceUnavailable)
	var detail map[string]string
	as.decode(t, resp, &detail)
	if detail["detail"] != "Agent 内部服务尚未配置" {
		t.Fatalf("detail %v", detail)
	}
}

func TestAgentWorkbenchMissingAndTurnGateway(t *testing.T) {
	as := newAgentServer(t, HTTPGateway{}, "")
	productID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '没有工作区的商品', NOW(), NOW())
	`, productID); err != nil {
		t.Fatal(err)
	}
	wb := as.do(t, http.MethodGet, "/api/v2/products/"+productID+"/agent-workbench", nil, "", nil)
	as.mustStatus(t, wb, http.StatusConflict)
	var detail map[string]string
	as.decode(t, wb, &detail)
	if detail["detail"] != "商品还没有 Agent 工作区" {
		t.Fatalf("detail %v", detail)
	}

	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "开始", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, turn, http.StatusServiceUnavailable)
	as.decode(t, turn, &detail)
	if detail["detail"] != "Agent 服务尚未配置或暂时不可用" {
		t.Fatalf("detail %v", detail)
	}

	noKey := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", nil)
	as.mustStatus(t, noKey, http.StatusBadRequest)
	as.decode(t, noKey, &detail)
	if detail["detail"] != "Idempotency-Key 不能为空" {
		t.Fatalf("detail %v", detail)
	}
	noGraph := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, noGraph, http.StatusConflict)
	as.decode(t, noGraph, &detail)
	if detail["detail"] != "当前商品还没有可执行的工作流" {
		t.Fatalf("detail %v", detail)
	}
}

func TestAgentTurnPassesPageContextToGateway(t *testing.T) {
	gw := &recordingGateway{}
	as := newAgentServer(t, gw, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text":      "检查当前商品素材",
		"idempotency_key": clockid.New(),
		"page_context": map[string]any{
			"route":              "/products/p1",
			"page_type":          "product_workbench",
			"product_id":         "p1",
			"selected_asset_ids": []string{"asset-1"},
			"visible_asset_ids":  []string{"asset-1", "asset-2"},
			"filters":            map[string]string{"tab": "agent"},
			"workflow_revision":  2,
			"library_revision":   3,
			"captured_at":        "2026-08-17T12:00:00+00:00",
		},
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	payload, ok := gw.pageContext.(map[string]any)
	if !ok || payload == nil {
		t.Fatalf("gateway page context %+v", gw.pageContext)
	}
	if payload["route"] != "/products/p1" || payload["page_type"] != "product_workbench" {
		t.Fatalf("route %+v", payload)
	}
	if payload["snapshot_id"] == nil || payload["digest"] == nil {
		t.Fatalf("snapshot %+v", payload)
	}
	if payload["workflow_revision"] != 2 || payload["library_revision"] != 3 {
		t.Fatalf("revisions %+v", payload)
	}
	selected, _ := payload["selected_asset_ids"].([]string)
	if len(selected) != 1 || selected[0] != "asset-1" {
		t.Fatalf("selected %+v", payload["selected_asset_ids"])
	}
	var storedRoute, storedDigest string
	var workflowRev, libraryRev *int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT route, digest, workflow_revision, library_revision
		FROM agent_page_context_snapshots WHERE id = $1
	`, payload["snapshot_id"]).Scan(&storedRoute, &storedDigest, &workflowRev, &libraryRev); err != nil {
		t.Fatal(err)
	}
	if storedRoute != "/products/p1" || storedDigest == "" || workflowRev == nil || *workflowRev != 2 || libraryRev == nil || *libraryRev != 3 {
		t.Fatalf("stored route=%s digest=%s wr=%v lr=%v", storedRoute, storedDigest, workflowRev, libraryRev)
	}
}

func TestAgentTurnSubmitStagesDispatch(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "整理素材", "idempotency_key": key, "asset_ids": []string{},
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	if !submitted.Created || submitted.Turn.ID == "" || submitted.Turn.HarnessTurnID == nil {
		t.Fatalf("submit %+v", submitted)
	}
	if submitted.Turn.Status != "queued" {
		t.Fatalf("status %s", submitted.Turn.Status)
	}
	var dispatchStatus string
	err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches WHERE actor_name = $1 AND aggregate_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, queue.ActorAgentTurnSync, submitted.Turn.ID).Scan(&dispatchStatus)
	if err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != queue.StatusPending {
		t.Fatalf("dispatch %s", dispatchStatus)
	}

	replay := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "整理素材", "idempotency_key": key,
	})
	as.mustStatus(t, replay, http.StatusAccepted)
	var again SubmitTurnResponse
	as.decode(t, replay, &again)
	if again.Created || again.Turn.ID != submitted.Turn.ID {
		t.Fatalf("replay %+v", again)
	}

	conflict := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "另一段", "idempotency_key": key,
	})
	as.mustStatus(t, conflict, http.StatusConflict)
	conflict.Body.Close()

	missing := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/turns/"+clockid.New(), nil, "", nil)
	as.mustStatus(t, missing, http.StatusNotFound)
	var missingDetail map[string]string
	as.decode(t, missing, &missingDetail)
	if missingDetail["detail"] != "Agent turn 不存在" {
		t.Fatalf("detail %v", missingDetail)
	}

	events := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/events", nil, "", http.Header{"Last-Event-ID": []string{"abc"}})
	as.mustStatus(t, events, http.StatusBadRequest)
	var detail map[string]string
	as.decode(t, events, &detail)
	if detail["detail"] != "Last-Event-ID 无效" {
		t.Fatalf("detail %v", detail)
	}

	listed := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/turns", nil, "", nil)
	as.mustStatus(t, listed, http.StatusOK)
	var page TurnPageResponse
	as.decode(t, listed, &page)
	if len(page.Items) != 1 || page.Items[0].ID != submitted.Turn.ID {
		t.Fatalf("list %+v", page)
	}

	got := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID, nil, "", nil)
	as.mustStatus(t, got, http.StatusOK)
	var loaded TurnResponse
	as.decode(t, got, &loaded)
	if loaded.ID != submitted.Turn.ID {
		t.Fatalf("get %+v", loaded)
	}

	if err := (Executor{Service: as.svc}).Execute(context.Background(), submitted.Turn.ID); !errors.Is(err, queue.ErrLater) {
		t.Fatalf("in-flight turn must return ErrLater, got %v", err)
	}
	if _, err := RecoverUnfinishedTurns(context.Background(), as.svc); err != nil {
		t.Fatal(err)
	}
	var dispatchCount int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM async_dispatches WHERE actor_name = $1 AND aggregate_id = $2
	`, queue.ActorAgentTurnSync, submitted.Turn.ID).Scan(&dispatchCount); err != nil {
		t.Fatal(err)
	}
	if dispatchCount != 1 {
		t.Fatalf("dispatch rows %d", dispatchCount)
	}
}

func (as *agentServer) submitAndParkQuestion(t *testing.T, questionID string) (convID, turnID, harnessID string) {
	t.Helper()
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID = sess.Conversations[0].ConversationID
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "可以帮我创建商品工作流吗", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	if submitted.Turn.HarnessTurnID == nil {
		t.Fatal("missing harness turn")
	}
	turnID = submitted.Turn.ID
	harnessID = *submitted.Turn.HarnessTurnID
	question := `{"id":"` + questionID + `","header":"商品名","question":"这个商品叫什么名字？","options":[{"label":"还没想好"},{"label":"稍后再说"}]}`
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET status = 'requires_input', question_json = $2::json, updated_at = NOW()
		WHERE id = $1
	`, turnID, question); err != nil {
		t.Fatal(err)
	}
	return convID, turnID, harnessID
}

func (as *agentServer) conversationTurnCount(t *testing.T, convID string) int {
	t.Helper()
	var n int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_turn_projections WHERE conversation_id = $1
	`, convID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAnswerQuestionResumesLiveWaiter(t *testing.T) {
	gw := &questionGateway{}
	as := newAgentServer(t, gw, "")
	convID, turnID, harnessID := as.submitAndParkQuestion(t, "question-name-1")
	gw.calls = nil
	gw.startCount = 0

	resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/question-name-1/answer", map[string]any{
		"text": "筋膜枪",
	})
	as.mustStatus(t, resp, http.StatusOK)
	var body QuestionAnswerResponse
	as.decode(t, resp, &body)
	if body.AnsweredTurn.ID != turnID || body.ContinuationTurn.ID != turnID {
		t.Fatalf("expected same-turn resume, answered=%s continuation=%s", body.AnsweredTurn.ID, body.ContinuationTurn.ID)
	}
	if body.AnsweredTurn.Status != "running" || body.AnsweredTurn.ContinuationTurnID != nil {
		t.Fatalf("answered %+v", body.AnsweredTurn)
	}
	if len(body.AnsweredTurn.Question) > 0 && string(body.AnsweredTurn.Question) != "null" {
		t.Fatalf("question still set %s", body.AnsweredTurn.Question)
	}
	if as.conversationTurnCount(t, convID) != 1 {
		t.Fatalf("created a continuation turn")
	}
	if gw.startCount != 0 {
		t.Fatalf("StartTurn calls %d", gw.startCount)
	}
	if len(gw.calls) < 2 || gw.calls[0] != "answer:"+harnessID || gw.calls[1] != "resume:"+harnessID {
		t.Fatalf("gateway calls %v want answer+resume %s", gw.calls, harnessID)
	}
}

func TestAnswerQuestionSkipResumesSameTurn(t *testing.T) {
	gw := &questionGateway{}
	as := newAgentServer(t, gw, "")
	convID, turnID, harnessID := as.submitAndParkQuestion(t, "question-skip-1")
	gw.calls = nil
	gw.startCount = 0

	resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/question-skip-1/answer", map[string]any{
		"skip": true,
	})
	as.mustStatus(t, resp, http.StatusOK)
	var body QuestionAnswerResponse
	as.decode(t, resp, &body)
	if body.AnsweredTurn.ID != turnID || body.ContinuationTurn.ID != turnID {
		t.Fatalf("expected same-turn skip, answered=%s continuation=%s", body.AnsweredTurn.ID, body.ContinuationTurn.ID)
	}
	if as.conversationTurnCount(t, convID) != 1 {
		t.Fatalf("created a continuation turn")
	}
	if gw.startCount != 0 {
		t.Fatalf("StartTurn calls %d", gw.startCount)
	}
	if len(gw.calls) < 2 || gw.calls[0] != "answer:"+harnessID || gw.calls[1] != "resume:"+harnessID {
		t.Fatalf("gateway calls %v want answer+resume %s", gw.calls, harnessID)
	}
}

func TestAnswerQuestionFallsBackWhenWaiterGone(t *testing.T) {
	gw := &questionGateway{}
	as := newAgentServer(t, gw, "")
	convID, turnID, harnessID := as.submitAndParkQuestion(t, "question-name-2")
	gw.calls = nil
	gw.startCount = 0

	resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/question-name-2/answer", map[string]any{
		"text": "筋膜枪",
	})
	as.mustStatus(t, resp, http.StatusOK)
	var body QuestionAnswerResponse
	as.decode(t, resp, &body)
	if body.AnsweredTurn.ID != turnID || body.ContinuationTurn.ID != turnID {
		t.Fatalf("expected same-turn resume, answered=%s continuation=%s", body.AnsweredTurn.ID, body.ContinuationTurn.ID)
	}
	if body.AnsweredTurn.Status != "running" || body.AnsweredTurn.ContinuationTurnID != nil {
		t.Fatalf("answered %+v", body.AnsweredTurn)
	}
	if as.conversationTurnCount(t, convID) != 1 {
		t.Fatalf("turn count %d", as.conversationTurnCount(t, convID))
	}
	if gw.startCount != 0 {
		t.Fatalf("StartTurn calls %d", gw.startCount)
	}
	if len(gw.calls) < 2 || gw.calls[0] != "answer:"+harnessID || gw.calls[1] != "resume:"+harnessID {
		t.Fatalf("gateway calls %v want answer+resume %s", gw.calls, harnessID)
	}
}

func TestAnswerQuestionTaskBoundContinuationCopiesAssetsAndCancelsWaiter(t *testing.T) {
	gw := &questionGateway{}
	as := newAgentServer(t, gw, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	createdTask := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "提问续跑", "goal": "回答商品名后继续", "conversation_id": convID,
	})
	as.mustStatus(t, createdTask, http.StatusCreated)
	var task TaskResponse
	as.decode(t, createdTask, &task)
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "可以帮我创建商品工作流吗", "idempotency_key": clockid.New(), "task_id": task.ID,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	if submitted.Turn.HarnessTurnID == nil || submitted.Turn.TaskID == nil {
		t.Fatal("task-bound turn missing harness or task")
	}
	turnID := submitted.Turn.ID
	harnessID := *submitted.Turn.HarnessTurnID
	questionID := "question-task-bound"
	question := `{"id":"` + questionID + `","header":"商品名","question":"这个商品叫什么名字？","options":[{"label":"还没想好"},{"label":"稍后再说"}]}`
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET status = 'requires_input', question_json = $2::json, input_asset_ids_json = $3::json, updated_at = NOW()
		WHERE id = $1
	`, turnID, question, `["asset-copy-1"]`); err != nil {
		t.Fatal(err)
	}
	gw.calls = nil
	gw.startCount = 0
	gw.lastStartAssets = nil

	resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/"+questionID+"/answer", map[string]any{
		"text": "筋膜枪",
	})
	as.mustStatus(t, resp, http.StatusOK)
	var body QuestionAnswerResponse
	as.decode(t, resp, &body)
	if body.ContinuationTurn.ID != turnID || body.AnsweredTurn.ID != turnID {
		t.Fatalf("expected same-turn resume, answered=%s continuation=%s", body.AnsweredTurn.ID, body.ContinuationTurn.ID)
	}
	if body.AnsweredTurn.Status != "running" {
		t.Fatalf("original status %s", body.AnsweredTurn.Status)
	}
	if as.conversationTurnCount(t, convID) != 1 {
		t.Fatalf("turn count %d", as.conversationTurnCount(t, convID))
	}
	if gw.startCount != 0 {
		t.Fatalf("StartTurn calls %d", gw.startCount)
	}
	if len(gw.calls) < 2 || gw.calls[0] != "answer:"+harnessID || gw.calls[1] != "resume:"+harnessID {
		t.Fatalf("gateway calls %v want answer+resume %s", gw.calls, harnessID)
	}
	if len(body.AnsweredTurn.InputAssetIDs) != 1 || body.AnsweredTurn.InputAssetIDs[0] != "asset-copy-1" {
		t.Fatalf("assets %+v", body.AnsweredTurn.InputAssetIDs)
	}
	if body.AnsweredTurn.TaskID == nil || *body.AnsweredTurn.TaskID != task.ID {
		t.Fatalf("task %+v", body.AnsweredTurn.TaskID)
	}
}

func TestAnswerQuestionUnavailableDoesNotCreateContinuation(t *testing.T) {
	gw := &questionGateway{answerErr: GatewayError{Status: 503, Code: "unavailable", Detail: "Agent 服务暂时不可用"}}
	as := newAgentServer(t, gw, "")
	convID, turnID, _ := as.submitAndParkQuestion(t, "question-name-3")
	gw.startCount = 0

	resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/question-name-3/answer", map[string]any{
		"text": "筋膜枪",
	})
	as.mustStatus(t, resp, http.StatusServiceUnavailable)
	resp.Body.Close()
	if as.conversationTurnCount(t, convID) != 1 {
		t.Fatalf("continuation created during 503")
	}
	var status string
	if err := as.pool.QueryRow(context.Background(), `SELECT status FROM agent_turn_projections WHERE id = $1`, turnID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "requires_input" {
		t.Fatalf("status %s", status)
	}
}

func TestAnswerQuestionNotResumableDoesNotCreateContinuation(t *testing.T) {
	gw := &questionGateway{answerErr: GatewayError{Status: 409, Code: "not_resumable", Detail: "this question is no longer attached to a live Pi turn"}}
	as := newAgentServer(t, gw, "")
	convID, turnID, _ := as.submitAndParkQuestion(t, "question-name-4")
	gw.startCount = 0

	resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/question-name-4/answer", map[string]any{
		"text": "筋膜枪",
	})
	as.mustStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	if as.conversationTurnCount(t, convID) != 1 {
		t.Fatalf("continuation created during not_resumable")
	}
}

func TestSyncTurnResumesParentInsteadOfStartingOrphanContinuation(t *testing.T) {
	gw := &questionGateway{}
	as := newAgentServer(t, gw, "")
	convID, turnID, harnessID := as.submitAndParkQuestion(t, "question-name-4")
	childID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, '[]', 'queued', false, '[]', NOW(), NOW()
		)
	`, childID, convID, "question-continuation-orphan", strings.Repeat("a", 64), "继续当前 Agent 任务。用户回答：筋膜枪。"); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET question_answer_json = $2::json, continuation_turn_id = $3, updated_at = NOW()
		WHERE id = $1
	`, turnID, `{"text":"筋膜枪"}`, childID); err != nil {
		t.Fatal(err)
	}
	gw.calls = nil
	gw.startCount = 0
	if err := as.svc.SyncTurn(context.Background(), childID); err != nil {
		t.Fatal(err)
	}
	if gw.startCount != 0 {
		t.Fatalf("StartTurn calls %d", gw.startCount)
	}
	if len(gw.calls) < 2 || gw.calls[0] != "answer:"+harnessID || gw.calls[1] != "resume:"+harnessID {
		t.Fatalf("gateway calls %v", gw.calls)
	}
	var parentStatus, childStatus string
	if err := as.pool.QueryRow(context.Background(), `SELECT status FROM agent_turn_projections WHERE id = $1`, turnID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if err := as.pool.QueryRow(context.Background(), `SELECT status FROM agent_turn_projections WHERE id = $1`, childID).Scan(&childStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "running" {
		t.Fatalf("parent status %s", parentStatus)
	}
	if childStatus != "canceled" {
		t.Fatalf("child status %s", childStatus)
	}
}

func TestSyncTurnCancelsBoundOrphanContinuation(t *testing.T) {
	gw := &questionGateway{}
	as := newAgentServer(t, gw, "")
	convID, turnID, harnessID := as.submitAndParkQuestion(t, "question-name-5")
	childID := clockid.New()
	childHarness := "ht-orphan-" + clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, harness_turn_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, '[]', 'queued', false, '[]', NOW(), NOW()
		)
	`, childID, convID, childHarness, "question-continuation-bound", strings.Repeat("b", 64), "继续当前 Agent 任务。用户回答：筋膜枪。"); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET question_answer_json = $2::json, continuation_turn_id = $3, updated_at = NOW()
		WHERE id = $1
	`, turnID, `{"text":"筋膜枪"}`, childID); err != nil {
		t.Fatal(err)
	}
	gw.calls = nil
	gw.startCount = 0
	if err := as.svc.SyncTurn(context.Background(), childID); err != nil {
		t.Fatal(err)
	}
	if gw.startCount != 0 {
		t.Fatalf("StartTurn calls %d", gw.startCount)
	}
	foundCancelChild := false
	if len(gw.calls) < 3 || gw.calls[0] != "answer:"+harnessID || gw.calls[1] != "resume:"+harnessID {
		t.Fatalf("gateway calls %v", gw.calls)
	}
	for _, call := range gw.calls {
		if call == "cancel:"+childHarness {
			foundCancelChild = true
		}
	}
	if !foundCancelChild {
		t.Fatalf("expected cancel of bound continuation %v", gw.calls)
	}
	var parentStatus, childStatus string
	if err := as.pool.QueryRow(context.Background(), `SELECT status FROM agent_turn_projections WHERE id = $1`, turnID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if err := as.pool.QueryRow(context.Background(), `SELECT status FROM agent_turn_projections WHERE id = $1`, childID).Scan(&childStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "running" {
		t.Fatalf("parent status %s", parentStatus)
	}
	if childStatus != "canceled" {
		t.Fatalf("child status %s", childStatus)
	}
}

func TestRecoverUnfinishedTurnsRestagesConsumedDispatch(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "整理素材", "idempotency_key": clockid.New(), "asset_ids": []string{},
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	if submitted.Turn.ID == "" {
		t.Fatal("missing turn id")
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE async_dispatches
		SET status = $1, consumed_at = NOW(), updated_at = NOW()
		WHERE actor_name = $2 AND aggregate_id = $3
	`, queue.StatusConsumed, queue.ActorAgentTurnSync, submitted.Turn.ID); err != nil {
		t.Fatal(err)
	}
	summary, err := RecoverUnfinishedTurns(context.Background(), as.svc)
	if err != nil {
		t.Fatal(err)
	}
	if summary.EnqueuedTurns < 1 {
		t.Fatalf("expected restage of consumed in-flight turn, summary %+v", summary)
	}
	var dispatchStatus string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches
		WHERE actor_name = $1 AND aggregate_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, queue.ActorAgentTurnSync, submitted.Turn.ID).Scan(&dispatchStatus); err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != queue.StatusPending {
		t.Fatalf("recovery left consumed dispatch as %s", dispatchStatus)
	}
}

func TestRecoverUnfinishedTurnsDoesNotRestagePendingDispatch(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "整理素材", "idempotency_key": clockid.New(), "asset_ids": []string{},
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	summary, err := RecoverUnfinishedTurns(context.Background(), as.svc)
	if err != nil {
		t.Fatal(err)
	}
	if summary.EnqueuedTurns != 0 {
		t.Fatalf("pending dispatch must not recount as enqueue, summary %+v", summary)
	}
	var dispatchStatus string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches
		WHERE actor_name = $1 AND aggregate_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, queue.ActorAgentTurnSync, submitted.Turn.ID).Scan(&dispatchStatus); err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != queue.StatusPending && dispatchStatus != queue.StatusSent {
		t.Fatalf("status %s", dispatchStatus)
	}
}

func TestAgentTaskPauseComplete(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	created := as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{
		"session_id": sess.ID, "title": "整理", "goal": "整理全局素材",
	})
	as.mustStatus(t, created, http.StatusCreated)
	var task TaskResponse
	as.decode(t, created, &task)
	if task.Status != "queued" && task.Status != "running" {
		t.Fatalf("status %s", task.Status)
	}
	pause := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/pause", nil, "", nil)
	if pause.StatusCode == http.StatusConflict {
		pause.Body.Close()
		cancel := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/cancel", nil, "", nil)
		as.mustStatus(t, cancel, http.StatusOK)
		as.decode(t, cancel, &task)
		return
	}
	as.mustStatus(t, pause, http.StatusOK)
	as.decode(t, pause, &task)
	complete := as.do(t, http.MethodPost, "/api/v2/agent-tasks/"+task.ID+"/complete", nil, "", nil)
	as.mustStatus(t, complete, http.StatusOK)
	as.decode(t, complete, &task)
	if task.Status != "succeeded" {
		t.Fatalf("complete %s", task.Status)
	}
}

func TestAgentClaimRequiresTurn(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	claim := as.doJSON(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim", map[string]any{
		"idempotency_key": "missing", "harness_turn_id": "ht-1", "owner_id": "worker-1",
	})
	// 内部鉴权
	claim.Body.Close()
	claim = as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(`{"idempotency_key":"missing","harness_turn_id":"ht-1","owner_id":"worker-1"}`),
		"application/json", http.Header{"Authorization": []string{"Bearer tok"}})
	if claim.StatusCode != http.StatusNotFound && claim.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(claim.Body)
		claim.Body.Close()
		t.Fatalf("claim %d %s", claim.StatusCode, raw)
	}
	claim.Body.Close()
}

func TestAgentUnknownJSONRejected(t *testing.T) {
	as := newAgentServer(t, HTTPGateway{}, "")
	resp := as.doJSON(t, http.MethodPatch, "/api/v2/agent-sessions/x", map[string]any{"title": "a", "extra": 1})
	as.mustStatus(t, resp, http.StatusBadRequest)
	var detail map[string]string
	as.decode(t, resp, &detail)
	if detail["detail"] != "请求体无效" {
		t.Fatalf("detail %v", detail)
	}
}

func TestAgentTurnSSEHeartbeat(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "听心跳", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, as.srv.URL+"/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range as.cookies {
		req.AddCookie(c)
	}
	resp, err := as.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("sse %d %s", resp.StatusCode, raw)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type %s", resp.Header.Get("Content-Type"))
	}
	buf := make([]byte, 256)
	n, err := resp.Body.Read(buf)
	if n == 0 && err != nil {
		t.Fatalf("read sse: %v", err)
	}
	body := string(buf[:n])
	if !strings.Contains(body, ": heartbeat") {
		t.Fatalf("sse body %q", body)
	}
	cancel()
}

func TestAgentTurnSSEProxiesRuntimeCursor(t *testing.T) {
	gw := &scriptedStreamGateway{}
	as := newAgentServer(t, gw, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "转发直播", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, as.srv.URL+"/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/events?after=4", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "7")
	for _, c := range as.cookies {
		req.AddCookie(c)
	}
	resp, err := as.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("sse %d %s", resp.StatusCode, raw)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "event: text.delta") || !strings.Contains(body, `"sequence":8`) {
		t.Fatalf("proxied sse %q", body)
	}
	if gw.after != 7 {
		t.Fatalf("runtime cursor %d", gw.after)
	}
}

type scriptedStreamGateway struct {
	mockGateway
	after int
}

func (g *scriptedStreamGateway) StreamTurnEvents(ctx context.Context, conversationID, turnID string, taskID *string, after int, w io.Writer) error {
	g.after = after
	event := `{"schema_version":1,"run_id":"` + conversationID + `","turn_id":"` + turnID + `","sequence":` + strconv.Itoa(after+1) + `,"created_at":"2026-08-30T00:00:00.000Z","kind":"text.delta","payload":{"delta":"hi","step_id":"s","attempt_id":"a"}}`
	_, err := io.WriteString(w, "id: "+strconv.Itoa(after+1)+"\nevent: text.delta\ndata: "+event+"\n\n")
	return err
}

func TestAgentClaimHeartbeatAndContract(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "claim 这条", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)

	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	contract := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/contract", nil, "", auth)
	as.mustStatus(t, contract, http.StatusOK)
	var contractBody ContractResponse
	as.decode(t, contract, &contractBody)
	if contractBody.ToolContractVersion != toolContractVersion || contractBody.ScopeType != "global" {
		t.Fatalf("contract %+v", contractBody)
	}

	products := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/products", nil, "", auth)
	as.mustStatus(t, products, http.StatusOK)
	products.Body.Close()

	focus := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/canvas/focus",
		strings.NewReader(`{"node_ids":["n1"]}`), "application/json", http.Header{
			"Authorization":   []string{"Bearer tok"},
			"Idempotency-Key": []string{clockid.New()},
		})
	as.mustStatus(t, focus, http.StatusOK)
	var focusBody map[string]any
	as.decode(t, focus, &focusBody)
	if focusBody["accepted"] != true {
		t.Fatalf("focus %+v", focusBody)
	}

	claim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-1",
		})), "application/json", auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)
	if lease.Phase != "claimed" || lease.LeaseToken == "" || lease.ProjectionID != submitted.Turn.ID {
		t.Fatalf("lease %+v", lease)
	}
	var checkpointSeq int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT last_checkpoint_sequence FROM agent_turn_executions WHERE id = $1
	`, lease.ExecutionID).Scan(&checkpointSeq); err != nil {
		t.Fatal(err)
	}
	if checkpointSeq != 0 {
		t.Fatalf("last_checkpoint_sequence %d", checkpointSeq)
	}

	heartbeat := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/heartbeat", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "phase": "model",
	}, auth)
	as.mustStatus(t, heartbeat, http.StatusOK)
	as.decode(t, heartbeat, &lease)
	if lease.Phase != "model" {
		t.Fatalf("heartbeat %s", lease.Phase)
	}

	recon := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/effect-reconciliation", map[string]any{
		"tool_call_id": "tc-1",
	})
	as.mustStatus(t, recon, http.StatusConflict)
	var reconDetail map[string]string
	as.decode(t, recon, &reconDetail)
	if reconDetail["detail"] != "只有 unknown Agent Turn 才能执行副作用对账" {
		t.Fatalf("detail %v", reconDetail)
	}
}

func TestAppendEventRejectsLiveDeltasAndAllowsGaps(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "控制事件", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	claim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-1",
		})), "application/json", auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)

	live := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/events", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 1,
		"schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
		"kind": "text.delta", "payload": json.RawMessage(`{"delta":"no"}`), "created_at": time.Now().UTC(),
	}, auth)
	as.mustStatus(t, live, http.StatusBadRequest)
	live.Body.Close()

	started := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/events", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 1,
		"schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
		"kind": "turn.started", "payload": json.RawMessage(`{}`), "created_at": time.Now().UTC(),
	}, auth)
	as.mustStatus(t, started, http.StatusOK)
	started.Body.Close()

	gapped := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/events", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 5,
		"schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
		"kind": "tool.step", "payload": json.RawMessage(`{"step_id":"s1","kind":"inspect_context","summary":"读取上下文","status":"succeeded"}`),
		"created_at": time.Now().UTC(),
	}, auth)
	as.mustStatus(t, gapped, http.StatusOK)
	gapped.Body.Close()
}

func TestSettledTurnSSEReplaysGappedControlEvents(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "结束后重放控制事件", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	claim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-1",
		})), "application/json", auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)

	started := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/events", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 1,
		"schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
		"kind": "turn.started", "payload": json.RawMessage(`{}`), "created_at": time.Now().UTC(),
	}, auth)
	as.mustStatus(t, started, http.StatusOK)
	started.Body.Close()
	gapped := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/events", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "sequence": 5,
		"schema_version": 1, "run_id": submitted.Turn.HarnessRunID, "turn_id": *submitted.Turn.HarnessTurnID,
		"kind": "tool.step", "payload": json.RawMessage(`{"step_id":"s1","kind":"inspect_context","summary":"读取上下文","status":"succeeded"}`),
		"created_at": time.Now().UTC(),
	}, auth)
	as.mustStatus(t, gapped, http.StatusOK)
	gapped.Body.Close()

	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET status = 'succeeded', output_text = 'ok', updated_at = NOW()
		WHERE id = $1
	`, submitted.Turn.ID); err != nil {
		t.Fatal(err)
	}

	resp := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/events", nil, "", nil)
	as.mustStatus(t, resp, http.StatusOK)
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "event: turn.started") || !strings.Contains(body, "event: tool.step") {
		t.Fatalf("settled sse %q", body)
	}
}

func TestAgentClaimAllowsExpiredWaitingInput(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "提问后续跑", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	claim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-1",
		})), "application/json", auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)

	heartbeat := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/heartbeat", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "phase": "waiting_input",
	}, auth)
	as.mustStatus(t, heartbeat, http.StatusOK)

	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions
		SET lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW()
		WHERE id = $1
	`, lease.ExecutionID); err != nil {
		t.Fatal(err)
	}

	reclaim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-2",
		})), "application/json", auth)
	as.mustStatus(t, reclaim, http.StatusOK)
	var next ExecutionLeaseResponse
	as.decode(t, reclaim, &next)
	if next.OwnerID != "worker-2" || next.Phase != "claimed" {
		t.Fatalf("reclaim %+v", next)
	}
}

func TestRecoverUnfinishedTurnsLeavesExpiredWaitingInput(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text": "提问后续跑", "idempotency_key": key,
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}
	claim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-1",
		})), "application/json", auth)
	as.mustStatus(t, claim, http.StatusOK)
	var lease ExecutionLeaseResponse
	as.decode(t, claim, &lease)

	heartbeat := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/"+lease.ExecutionID+"/heartbeat", map[string]any{
		"owner_id": "worker-1", "lease_token": lease.LeaseToken, "phase": "waiting_input",
	}, auth)
	as.mustStatus(t, heartbeat, http.StatusOK)
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections SET status = 'requires_input', updated_at = NOW() WHERE id = $1
	`, submitted.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions
		SET lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW()
		WHERE id = $1
	`, lease.ExecutionID); err != nil {
		t.Fatal(err)
	}

	summary, err := RecoverUnfinishedTurns(context.Background(), as.svc)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownExecutions != 0 {
		t.Fatalf("parked question marked unknown: %+v", summary)
	}
	var status string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM agent_turn_projections WHERE id = $1
	`, submitted.Turn.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "requires_input" {
		t.Fatalf("status %s", status)
	}

	reclaim := as.do(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/turn-executions/claim",
		strings.NewReader(mustJSON(t, map[string]any{
			"idempotency_key": key,
			"harness_turn_id": *submitted.Turn.HarnessTurnID,
			"owner_id":        "worker-2",
		})), "application/json", auth)
	as.mustStatus(t, reclaim, http.StatusOK)
}

func TestAgentWorkbenchEnsureWithGraph(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	productID := clockid.New()
	graphID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '有图的商品', NOW(), NOW())
	`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, '商品创意工作流', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID); err != nil {
		t.Fatal(err)
	}
	key := clockid.New()
	ensure := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{key},
	})
	as.mustStatus(t, ensure, http.StatusOK)
	var bench WorkbenchResponse
	as.decode(t, ensure, &bench)
	if bench.Mode != "agent" || bench.Conversation.ID == "" || bench.Conversation.ScopeType != "product_workflow" {
		t.Fatalf("workbench %+v", bench)
	}
	replay := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{key},
	})
	as.mustStatus(t, replay, http.StatusOK)
	var again WorkbenchResponse
	as.decode(t, replay, &again)
	if again.Conversation.ID != bench.Conversation.ID {
		t.Fatalf("replay %s vs %s", again.Conversation.ID, bench.Conversation.ID)
	}
	got := as.do(t, http.MethodGet, "/api/v2/products/"+productID+"/agent-workbench", nil, "", nil)
	as.mustStatus(t, got, http.StatusOK)
	got.Body.Close()
}

func mustJSON(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (as *agentServer) doJSONAuth(t *testing.T, method, path string, payload any, extra http.Header) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return as.do(t, method, path, bytes.NewReader(raw), "application/json", extra)
}

func TestInternalProductIntakeReconcileAcceptsTaskID(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	selection := map[string]any{
		"schema_version": 1,
		"image_types":    []any{map[string]any{"key": "hero", "quantity": 1, "order": 0}},
	}
	refIDs := []string{clockid.New()}

	nullAuth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	nullResp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/product-intake/reconcile", map[string]any{
		"selection": selection, "reference_asset_ids": refIDs, "task_id": nil,
	}, nullAuth)
	as.mustStatus(t, nullResp, http.StatusOK)
	var nullOut ReconcileResponse
	as.decode(t, nullResp, &nullOut)
	if nullOut.State != "not_applied" {
		t.Fatalf("null task_id state %s", nullOut.State)
	}

	strAuth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	strResp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/product-intake/reconcile", map[string]any{
		"selection": selection, "reference_asset_ids": refIDs, "task_id": clockid.New(),
	}, strAuth)
	as.mustStatus(t, strResp, http.StatusOK)
	var strOut ReconcileResponse
	as.decode(t, strResp, &strOut)
	if strOut.State != "not_applied" {
		t.Fatalf("string task_id state %s", strOut.State)
	}
}

func TestInternalProductIntakeRejectsInvalidBody(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	validSelection := map[string]any{
		"schema_version": 1,
		"image_types":    []any{map[string]any{"key": "hero", "quantity": 1, "order": 0}},
	}
	intakePath := "/api/internal/v1/agent-conversations/" + convID + "/product-intake"
	reconcilePath := intakePath + "/reconcile"
	auth := func() http.Header {
		return http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	}

	emptyRefs := as.doJSONAuth(t, http.MethodPost, reconcilePath, map[string]any{
		"selection": validSelection, "reference_asset_ids": []string{},
	}, auth())
	as.mustDetail(t, emptyRefs, http.StatusBadRequest, "至少选择一张参考图")

	emptyApply := as.doJSONAuth(t, http.MethodPost, intakePath, map[string]any{
		"selection": validSelection, "reference_asset_ids": []string{},
	}, auth())
	as.mustDetail(t, emptyApply, http.StatusBadRequest, "至少选择一张参考图")

	invalidSel := as.doJSONAuth(t, http.MethodPost, reconcilePath, map[string]any{
		"selection": map[string]any{"schema_version": 1, "image_types": []any{}}, "reference_asset_ids": []string{clockid.New()},
	}, auth())
	as.mustDetail(t, invalidSel, http.StatusBadRequest, "图片类型选择不符合 AgentProductSelectionV1")
}

func TestInternalAssetMoveReconcileRejectsInvalidBody(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	path := "/api/internal/v1/agent-conversations/" + convID + "/asset-moves/reconcile"
	auth := func() http.Header {
		return http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}}
	}

	emptyObj := as.doJSONAuth(t, http.MethodPost, path, map[string]any{}, auth())
	as.mustDetail(t, emptyObj, http.StatusBadRequest, "单次必须移动 1 到 100 张图片")

	emptyMoves := as.doJSONAuth(t, http.MethodPost, path, map[string]any{"moves": []any{}}, auth())
	as.mustDetail(t, emptyMoves, http.StatusBadRequest, "单次必须移动 1 到 100 张图片")

	valid := as.doJSONAuth(t, http.MethodPost, path, map[string]any{
		"moves": []any{map[string]any{"asset_id": clockid.New(), "expected_folder_id": nil}},
	}, auth())
	as.mustStatus(t, valid, http.StatusOK)
	var out ReconcileResponse
	as.decode(t, valid, &out)
	if out.State != "not_applied" {
		t.Fatalf("valid-shaped unknown mutation state %s", out.State)
	}

	unknownConv := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+clockid.New()+"/asset-moves/reconcile", map[string]any{
		"moves": []any{map[string]any{"asset_id": clockid.New()}},
	}, auth())
	if unknownConv.StatusCode != http.StatusOK && unknownConv.StatusCode != http.StatusNotFound && unknownConv.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(unknownConv.Body)
		unknownConv.Body.Close()
		t.Fatalf("unknown conversation status %d %s", unknownConv.StatusCode, raw)
	}
	if unknownConv.StatusCode == http.StatusOK {
		var unknownOut ReconcileResponse
		as.decode(t, unknownConv, &unknownOut)
		if unknownOut.State != "not_applied" {
			t.Fatalf("unknown conversation state %s", unknownOut.State)
		}
	} else {
		unknownConv.Body.Close()
	}
}

func TestAgentFinalizeIntakeExpandsBirthGraph(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	draft := as.do(t, http.MethodPost, "/api/v2/agent-product-workspaces/drafts", strings.NewReader(`{"name":"名称草稿"}`), "application/json", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, draft, http.StatusCreated)
	var snap product.WorkspaceSnapshotResponse
	as.decode(t, draft, &snap)
	productID := snap.Product.ID
	convID := snap.Conversation.ID
	authTok := http.Header{"Authorization": []string{"Bearer tok"}}

	before := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/product-context", nil, "", authTok)
	as.mustStatus(t, before, http.StatusOK)
	var beforeCtx map[string]any
	as.decode(t, before, &beforeCtx)
	if beforeCtx["birth_expandable"] != false {
		t.Fatalf("name-only without intake birth_expandable=%+v", beforeCtx["birth_expandable"])
	}

	body, contentType := workspaceIntakePNG(t, map[string]string{})
	add := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-assets", body, contentType, nil)
	as.mustStatus(t, add, http.StatusCreated)
	var added product.AssetListResponse
	as.decode(t, add, &added)
	if len(added.Items) != 1 {
		t.Fatalf("assets %+v", added)
	}
	assetID := added.Items[0].ID
	selection := map[string]any{
		"schema_version": 1,
		"image_types": []any{
			map[string]any{"key": "hero", "quantity": 2, "order": 0},
			map[string]any{"key": "selling_point", "quantity": 2, "order": 1},
			map[string]any{"key": "scene", "quantity": 1, "order": 2},
			map[string]any{"key": "detail", "quantity": 2, "order": 3},
			map[string]any{"key": "sku", "quantity": 1, "order": 4},
			map[string]any{"key": "dimensions", "quantity": 1, "order": 5},
		},
	}
	stuckIntake, err := json.Marshal(map[string]any{
		"schema_version": 1, "image_types": selection["image_types"], "reference_asset_ids": []string{assetID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE products SET intake_schema_version = 1, intake_json = $2, updated_at = NOW() WHERE id = $1
	`, productID, string(stuckIntake)); err != nil {
		t.Fatal(err)
	}

	stuck := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/product-context", nil, "", authTok)
	as.mustStatus(t, stuck, http.StatusOK)
	var stuckCtx map[string]any
	as.decode(t, stuck, &stuckCtx)
	if stuckCtx["birth_expandable"] != true {
		t.Fatalf("stuck birth_expandable=%+v", stuckCtx["birth_expandable"])
	}

	unknownOp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/graph/proposals", map[string]any{
		"change_set": map[string]any{
			"base_graph_revision": 1,
			"summary":             "猜操作",
			"operations":          []any{map[string]any{"op": "add_node", "title": "节点"}},
		},
	}, http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}})
	as.mustDetail(t, unknownOp, http.StatusBadRequest, "不支持的 Graph 操作 add_node。operations[].op 必须是: "+strings.Join(graph.GraphCommandOpNames, ", "))

	apply := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/product-intake", map[string]any{
		"selection": selection, "reference_asset_ids": []string{assetID},
	}, http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}})
	as.mustStatus(t, apply, http.StatusOK)
	var expanded struct {
		GraphExpanded bool `json:"graph_expanded"`
		Revision      int  `json:"revision"`
		NodeCount     int  `json:"node_count"`
		GroupCount    int  `json:"group_count"`
	}
	as.decode(t, apply, &expanded)
	if !expanded.GraphExpanded || expanded.GroupCount != 6 || expanded.NodeCount != 19 {
		t.Fatalf("expand %+v", expanded)
	}

	after := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/product-context", nil, "", authTok)
	as.mustStatus(t, after, http.StatusOK)
	var afterCtx map[string]any
	as.decode(t, after, &afterCtx)
	if afterCtx["birth_expandable"] != false {
		t.Fatalf("expanded birth_expandable=%+v", afterCtx["birth_expandable"])
	}
	live, _ := afterCtx["live_graph"].(map[string]any)
	counts := map[string]int{}
	for _, item := range live["nodes"].([]any) {
		node, _ := item.(map[string]any)
		key, _ := node["node_type"].(string)
		counts[key]++
	}
	if counts["product_source"] != 1 || counts["visual_system"] != 1 || counts["creative_brief"] != 1 ||
		counts["image_asset"] != 1 || counts["prompt_generation"] != 6 || counts["image_generation"] != 9 {
		t.Fatalf("nodes %+v", counts)
	}
	if groups, _ := live["groups"].([]any); len(groups) != 6 {
		t.Fatalf("groups %+v", live["groups"])
	}

	againSelection := map[string]any{
		"schema_version": 1,
		"image_types": []any{
			map[string]any{"key": "hero", "quantity": 3, "order": 0},
			map[string]any{"key": "selling_point", "quantity": 2, "order": 1},
			map[string]any{"key": "scene", "quantity": 1, "order": 2},
			map[string]any{"key": "detail", "quantity": 2, "order": 3},
			map[string]any{"key": "sku", "quantity": 1, "order": 4},
			map[string]any{"key": "dimensions", "quantity": 1, "order": 5},
		},
	}
	again := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+convID+"/product-intake", map[string]any{
		"selection": againSelection, "reference_asset_ids": []string{assetID},
	}, http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{clockid.New()}})
	as.mustStatus(t, again, http.StatusOK)
	var noop struct {
		GraphExpanded bool `json:"graph_expanded"`
		NodeCount     int  `json:"node_count"`
		GroupCount    int  `json:"group_count"`
	}
	as.decode(t, again, &noop)
	if noop.GraphExpanded || noop.NodeCount != 19 || noop.GroupCount != 6 {
		t.Fatalf("second finalize should not rebuild %+v", noop)
	}
	updated := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+convID+"/product-context", nil, "", authTok)
	as.mustStatus(t, updated, http.StatusOK)
	var updatedCtx map[string]any
	as.decode(t, updated, &updatedCtx)
	intake, _ := updatedCtx["intake"].(map[string]any)
	types, _ := intake["image_types"].([]any)
	if len(types) == 0 {
		t.Fatalf("updated intake %+v", updatedCtx["intake"])
	}
	hero, _ := types[0].(map[string]any)
	if hero["key"] != "hero" || hero["quantity"] != float64(3) {
		t.Fatalf("expanded graph must still accept intake updates %+v", intake)
	}
}

func TestBrowserWorkspaceIntakeAcceptsTaskID(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	draft := as.do(t, http.MethodPost, "/api/v2/agent-product-workspaces/drafts", strings.NewReader(`{"name":"草稿商品"}`), "application/json", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, draft, http.StatusCreated)
	var snap product.WorkspaceSnapshotResponse
	as.decode(t, draft, &snap)
	if snap.Conversation.ID == "" {
		t.Fatal("draft missing conversation")
	}

	body, contentType := workspaceIntakePNG(t, map[string]string{
		"selection": `{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0}]}`,
		"task_id":   "task-from-create-page",
	})
	resp := as.do(t, http.MethodPost, "/api/v2/agent-product-workspaces/"+snap.Conversation.ID+"/intake", body, contentType, http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, resp, http.StatusOK)
	as.decode(t, resp, &snap)
	if !snap.IntakeFinalized {
		t.Fatalf("intake %+v", snap)
	}
}

func workspaceIntakePNG(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
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
