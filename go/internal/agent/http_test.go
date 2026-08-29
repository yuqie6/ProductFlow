package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
)

type mockGateway struct{}

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

type agentServer struct {
	pool    *pgxpool.Pool
	svc     Service
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newAgentServer(t *testing.T, gw Gateway, internalToken string) *agentServer {
	t.Helper()
	pool := testdb.Pool(t)
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
	product.HTTP{Service: product.Service{Pool: pool, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	svc := Service{
		Pool: pool, Graph: graph.Service{Pool: pool},
		Product: product.Service{Pool: pool, Media: mediaStore},
		Library: library.Service{Pool: pool, Media: mediaStore},
		Media: mediaStore, Settings: settingsStore, Gateway: gw, Poll: time.Millisecond,
	}
	HTTP{Service: svc, Settings: settingsStore, InternalToken: internalToken}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	as := &agentServer{pool: pool, svc: svc, srv: srv, client: &http.Client{}}
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

	if err := (Executor{Service: as.svc}).Execute(context.Background(), submitted.Turn.ID); err != nil {
		t.Fatal(err)
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
			"Authorization":     []string{"Bearer tok"},
			"Idempotency-Key":   []string{clockid.New()},
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
