package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

const gopgPiInternalToken = "0123456789abcdef0123456789abcdef"

type liveGateway struct {
	mu    sync.RWMutex
	inner Gateway
}

func (g *liveGateway) set(inner Gateway) {
	g.mu.Lock()
	g.inner = inner
	g.mu.Unlock()
}

func (g *liveGateway) current() Gateway {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.inner == nil {
		return mockGateway{}
	}
	return g.inner
}

func (g *liveGateway) Configured() bool {
	cur := g.current()
	type configured interface{ Configured() bool }
	if c, ok := cur.(configured); ok {
		return c.Configured()
	}
	return true
}

func (g *liveGateway) StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error) {
	return g.current().StartTurn(conversationID, taskID, inputText, assetIDs, idempotencyKey, pageContext)
}

func (g *liveGateway) CancelTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.current().CancelTurn(conversationID, turnID, taskID)
}

func (g *liveGateway) ResumeTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.current().ResumeTurn(conversationID, turnID, taskID)
}

func (g *liveGateway) AnswerQuestion(conversationID, turnID, questionID string, answer map[string]any, taskID *string) (TurnState, error) {
	return g.current().AnswerQuestion(conversationID, turnID, questionID, answer, taskID)
}

type askUserProvider struct {
	mu        sync.Mutex
	count     int
	bodies    []string
	questions int
}

func (p *askUserProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.count++
	n := p.count
	p.bodies = append(p.bodies, string(body))
	p.mu.Unlock()

	if !strings.Contains(r.URL.Path, "responses") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	var request struct {
		Input []struct {
			Type string `json:"type"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		panic(err)
	}
	answered := 0
	for _, item := range request.Input {
		if item.Type == "function_call_output" {
			answered++
		}
	}
	if answered >= p.questions {
		writeProviderTextSSE(w, n)
	} else {
		writeProviderAskUserSSE(w, answered+1)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (p *askUserProvider) snapshot() (int, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append([]string(nil), p.bodies...)
	return p.count, out
}

func writeSSE(w http.ResponseWriter, eventType string, payload any) {
	raw, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, raw)
}

func writeProviderAskUserSSE(w http.ResponseWriter, number int) {
	itemID, callID, responseID := fmt.Sprintf("fc-question-%d", number), fmt.Sprintf("call-question-%d", number), fmt.Sprintf("resp-question-%d", number)
	toolCall := map[string]any{
		"type": "function_call", "id": itemID, "call_id": callID,
		"name": "ask_user", "arguments": `{"header":"确认","question":"是否继续？","options":[{"label":"继续"},{"label":"停止"}]}`,
		"status": "completed",
	}
	responseBody := map[string]any{
		"id": responseID, "object": "response", "status": "completed",
		"output": []any{toolCall},
		"usage":  map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
	}
	writeSSE(w, "response.created", map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": responseID, "object": "response", "status": "in_progress", "output": []any{}},
	})
	writeSSE(w, "response.output_item.added", map[string]any{
		"type": "response.output_item.added", "output_index": 0,
		"item": map[string]any{"type": "function_call", "id": itemID, "call_id": callID, "name": "ask_user", "arguments": "", "status": "in_progress"},
	})
	writeSSE(w, "response.function_call_arguments.delta", map[string]any{
		"type": "response.function_call_arguments.delta", "output_index": 0, "delta": toolCall["arguments"],
	})
	writeSSE(w, "response.function_call_arguments.done", map[string]any{
		"type": "response.function_call_arguments.done", "output_index": 0, "arguments": toolCall["arguments"],
	})
	writeSSE(w, "response.output_item.done", map[string]any{
		"type": "response.output_item.done", "output_index": 0, "item": toolCall,
	})
	writeSSE(w, "response.completed", map[string]any{"type": "response.completed", "response": responseBody})
}

func writeProviderTextSSE(w http.ResponseWriter, n int) {
	id := fmt.Sprintf("msg-question-resume-%d", n)
	respID := fmt.Sprintf("resp-question-resume-%d", n)
	message := map[string]any{
		"type": "message", "id": id, "role": "assistant", "status": "completed", "phase": "final_answer",
		"content": []any{map[string]any{"type": "output_text", "text": "已根据你的回答继续", "annotations": []any{}}},
	}
	responseBody := map[string]any{
		"id": respID, "object": "response", "status": "completed",
		"output": []any{message},
		"usage":  map[string]any{"input_tokens": 1, "output_tokens": 3, "total_tokens": 4},
	}
	writeSSE(w, "response.created", map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": respID, "object": "response", "status": "in_progress", "output": []any{}},
	})
	writeSSE(w, "response.output_item.added", map[string]any{
		"type": "response.output_item.added", "output_index": 0,
		"item": map[string]any{"type": "message", "id": id, "role": "assistant", "status": "in_progress", "content": []any{}},
	})
	writeSSE(w, "response.output_text.delta", map[string]any{
		"type": "response.output_text.delta", "output_index": 0, "delta": "已根据你的回答继续",
	})
	writeSSE(w, "response.output_text.done", map[string]any{
		"type": "response.output_text.done", "output_index": 0, "text": "已根据你的回答继续",
	})
	writeSSE(w, "response.output_item.done", map[string]any{
		"type": "response.output_item.done", "output_index": 0, "item": message,
	})
	writeSSE(w, "response.completed", map[string]any{"type": "response.completed", "response": responseBody})
}

func TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult(t *testing.T) {
	for _, questions := range []int{1, 2} {
		t.Run(fmt.Sprintf("question-%d", questions), func(t *testing.T) { testDurableQuestionAnswer(t, questions) })
	}
}

func testDurableQuestionAnswer(t *testing.T, questions int) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required to spawn the Pi Agent")
	}
	agentDir := agentServiceDir(t)
	if _, err := os.Stat(filepath.Join(agentDir, "src", "main.ts")); err != nil {
		t.Skip("agent-service src/main.ts missing")
	}

	gw := &liveGateway{}
	as := newAgentServer(t, gw, gopgPiInternalToken)
	seedFakeAgentProvider(t, as)
	provider := &askUserProvider{questions: questions}
	llm := httptest.NewServer(provider)
	t.Cleanup(llm.Close)

	dataRoot := t.TempDir()
	first := spawnPiAgent(t, dataRoot, as.srv.URL, llm.URL+"/v1", gopgPiInternalToken)
	gw.set(HTTPGateway{BaseURL: first.baseURL, Token: gopgPiInternalToken, ReadTimeout: 30 * time.Second})

	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID

	turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
		"input_text":      "需要时向我提问",
		"idempotency_key": clockid.New(),
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)
	if submitted.Turn.HarnessTurnID == nil {
		t.Fatal("missing harness turn ID")
	}
	parkedAgent := waitAgentTurnStatus(t, first.baseURL, convID, *submitted.Turn.HarnessTurnID, "requires_input", 45*time.Second, first)
	parked := waitGoTurnStatus(t, as, convID, submitted.Turn.ID, "requires_input", 10*time.Second)
	if len(parked.Question) == 0 {
		t.Fatalf("parked turn missing question: %+v agent=%+v", parked, parkedAgent)
	}
	var question struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(parked.Question, &question); err != nil || question.ID == "" {
		t.Fatalf("question %+v err=%v", parked.Question, err)
	}
	if questions == 2 {
		firstID := question.ID
		answer := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/questions/"+firstID+"/answer", map[string]any{"option": 1})
		as.mustStatus(t, answer, http.StatusOK)
		answer.Body.Close()
		waitAgentTurnStatus(t, first.baseURL, convID, *submitted.Turn.HarnessTurnID, "requires_input", 45*time.Second, first)
		deadline := time.Now().Add(10 * time.Second)
		for {
			parked = waitGoTurnStatus(t, as, convID, submitted.Turn.ID, "requires_input", 10*time.Second)
			if err := json.Unmarshal(parked.Question, &question); err != nil {
				t.Fatal(err)
			}
			if question.ID != "" && question.ID != firstID {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("second question was not projected")
			}
			time.Sleep(20 * time.Millisecond)
		}
		var answerMissing bool
		if err := as.pool.QueryRow(context.Background(), `SELECT question_answer_json IS NULL FROM agent_turn_projections WHERE id = $1`, submitted.Turn.ID).Scan(&answerMissing); err != nil {
			t.Fatal(err)
		}
		if !answerMissing {
			t.Fatal("second question inherited the first answer")
		}
	}

	killPiAgent(t, first.cmd)
	expireWaitingInputLease(t, as, submitted.Turn.ID)

	answer := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/questions/"+question.ID+"/answer", map[string]any{
		"option": 0,
	})
	if answer.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(answer.Body)
		answer.Body.Close()
		t.Fatalf("expected persist-then-unavailable, got %d %s", answer.StatusCode, body)
	}
	answer.Body.Close()
	var storedAnswer string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COALESCE(question_answer_json::text, '') FROM agent_turn_projections WHERE id = $1
	`, submitted.Turn.ID).Scan(&storedAnswer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(storedAnswer, `"option": 0`) && !strings.Contains(storedAnswer, `"option":0`) {
		t.Fatalf("PG answer missing: %s", storedAnswer)
	}

	second := spawnPiAgent(t, dataRoot, as.srv.URL, llm.URL+"/v1", gopgPiInternalToken)
	gw.set(HTTPGateway{BaseURL: second.baseURL, Token: gopgPiInternalToken, ReadTimeout: 30 * time.Second})

	deadline := time.Now().Add(45 * time.Second)
	var lastSync error
	for {
		lastSync = as.svc.SyncTurn(context.Background(), submitted.Turn.ID)
		var status string
		if err := as.pool.QueryRow(context.Background(), `SELECT status FROM agent_turn_projections WHERE id = $1`, submitted.Turn.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "requires_input" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("answered question still requires_input after Agent restart sync=%v\n%s", lastSync, second.logs())
		}
		time.Sleep(200 * time.Millisecond)
	}

	waitAgentTurnStatus(t, second.baseURL, convID, *submitted.Turn.HarnessTurnID, "succeeded", 45*time.Second, second)
	waitDurableTurnTerminal(t, as, submitted.Turn.ID, 45*time.Second, second)
	terminal := syncTurnFromAgent(t, as, convID, submitted.Turn.ID)
	if terminal.Status != "succeeded" {
		t.Fatalf("status %s logs=%s", terminal.Status, second.logs())
	}
	assertProductionHarnessAttribution(t, as, submitted.Turn.ID, second.baseURL)

	var attempt int
	var kinds []string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT attempt FROM agent_turn_executions WHERE turn_projection_id = $1
	`, submitted.Turn.ID).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	if attempt < 2 {
		t.Fatalf("expected new attempt after durable answer, got %d", attempt)
	}
	if err := as.db.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", submitted.Turn.ID).
		Order("sequence").Pluck("kind", &kinds).Error; err != nil {
		t.Fatal(err)
	}
	answered := 0
	for i, kind := range kinds {
		if i > 0 && kinds[i] == "" {
			t.Fatalf("empty journal kind at %d", i)
		}
		if kind == "question/answered" {
			answered++
		}
	}
	if answered != questions {
		t.Fatalf("question/answered count %d kinds=%v", answered, kinds)
	}
	var journal []schema.AgentTurnEvents
	if err := as.db.Where("turn_projection_id = ?", submitted.Turn.ID).Order("sequence").Find(&journal).Error; err != nil {
		t.Fatal(err)
	}
	for i, event := range journal {
		if event.Sequence != i+1 {
			t.Fatalf("journal sequence gap at %d: %d", i, event.Sequence)
		}
	}
	count, bodies := provider.snapshot()
	if count < 2 {
		t.Fatalf("provider requests %d, want a continue after inject; logs=%s", count, second.logs())
	}
	injected := false
	for i := 1; i < len(bodies); i++ {
		if strings.Contains(bodies[i], "function_call_output") || strings.Contains(bodies[i], "call-question") {
			injected = true
			break
		}
	}
	if !injected {
		t.Fatalf("Pi continue request did not include injected ask_user tool result; bodies=%v logs=%s", bodies, second.logs())
	}
	var finalRequest struct {
		Input []struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal([]byte(bodies[len(bodies)-1]), &finalRequest); err != nil {
		t.Fatal(err)
	}
	results := 0
	for _, item := range finalRequest.Input {
		if item.Type != "function_call_output" {
			continue
		}
		results++
		if item.CallID != fmt.Sprintf("call-question-%d", results) {
			t.Fatalf("wrong resumed tool identity: %s", item.CallID)
		}
		expected := `{"accepted":true,"answer":{"option":0}}`
		if results < questions {
			expected = `{"schema_version":1,"data":{"accepted":true,"answer":{"option":1}}}`
		}
		if !sameJSON(item.Output, json.RawMessage(expected)) {
			t.Fatalf("wrong question result %d: %s", results, item.Output)
		}
	}
	if results != questions {
		t.Fatalf("model received %d question results, want %d", results, questions)
	}
}

type piAgentProc struct {
	cmd     *exec.Cmd
	baseURL string
	buf     *syncBuffer
}

func (p piAgentProc) logs() string {
	if p.buf == nil {
		return ""
	}
	return p.buf.String()
}

func spawnPiAgent(t *testing.T, dataRoot, productFlowURL, providerURL, token string) piAgentProc {
	return spawnPiAgentWithEnv(t, dataRoot, productFlowURL, providerURL, token, nil)
}

func spawnPiAgentWithEnv(t *testing.T, dataRoot, productFlowURL, providerURL, token string, extra map[string]string) piAgentProc {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	buf := &syncBuffer{}
	cmd := exec.Command("node", "--import", "tsx/esm", "src/main.ts")
	cmd.Dir = agentServiceDir(t)
	env := map[string]string{
		"AGENT_LISTEN_ADDRESS":           addr,
		"AGENT_DATA_ROOT":                dataRoot,
		"PRODUCTFLOW_INTERNAL_BASE_URL":  productFlowURL,
		"AGENT_SERVICE_INTERNAL_TOKEN":   token,
		"PRODUCTFLOW_REQUEST_TIMEOUT":    "30s",
		"AGENT_PROVIDER_REQUEST_TIMEOUT": "15s",
		"AGENT_QUESTION_TIMEOUT":         "900s",
		"AGENT_MAX_CONCURRENT_TURNS":     "1",
		"AGENT_MODEL_CONTEXT_WINDOW":     "128000",
		"AGENT_AUTO_COMPACT_TOKEN_LIMIT": "96000",
		"AGENT_MAX_ITERATIONS":           "4",
	}
	if providerURL != "" {
		env["AGENT_PROVIDER_API_KEY"] = "fake-provider-key"
		env["AGENT_PROVIDER_BASE_URL"] = providerURL
		env["AGENT_PROVIDER_MODEL"] = "fake-model"
	}
	for key, value := range extra {
		if strings.TrimSpace(value) != "" {
			env[key] = value
		}
	}
	if env["AGENT_PROVIDER_API_KEY"] == "" {
		env["AGENT_PROVIDER_API_KEY"] = "fake-provider-key"
	}
	cmd.Env = mergeEnv(os.Environ(), env)
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "ProductFlow Pi Agent listening") {
			return piAgentProc{cmd: cmd, baseURL: "http://" + addr, buf: buf}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("Agent exited before listen: %s", buf.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	t.Fatalf("Agent did not listen: %s", buf.String())
	return piAgentProc{}
}

func killPiAgent(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGKILL)
	_, _ = cmd.Process.Wait()
}

func expireWaitingInputLease(t *testing.T, as *agentServer, projectionID string) {
	t.Helper()
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions
		SET lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW()
		WHERE turn_projection_id = $1
	`, projectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := as.pool.QueryRow(context.Background(), `SELECT status FROM agent_turn_projections WHERE id = $1`, projectionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "requires_input" {
		var phase string
		_ = as.pool.QueryRow(context.Background(), `
			SELECT phase FROM agent_turn_executions WHERE turn_projection_id = $1
		`, projectionID).Scan(&phase)
		t.Fatalf("scanner must keep parked question, got %s phase=%s", status, phase)
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	env := map[string]string{}
	for _, kv := range base {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			env[k] = v
		}
	}
	for k, v := range overrides {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func waitAgentTurnStatus(t *testing.T, agentURL, convID, harnessTurnID, want string, timeout time.Duration, agent piAgentProc) map[string]any {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var last map[string]any
	seen := []string{}
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, agentURL+"/internal/v1/conversations/"+convID+"/turns/"+harnessTurnID, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+gopgPiInternalToken)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			last = map[string]any{}
			if json.Unmarshal(raw, &last) == nil {
				status, _ := last["status"].(string)
				if len(seen) == 0 || seen[len(seen)-1] != status {
					seen = append(seen, status)
				}
				if status == want {
					return last
				}
				if status == "canceled" || status == "failed" || status == "unknown" || status == "succeeded" {
					t.Fatalf("Agent turn %s status=%s want=%s seen=%v body=%s\n%s", harnessTurnID, status, want, seen, raw, agent.logs())
				}
			}
		} else if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Agent turn %s last=%v want=%s\n%s", harnessTurnID, last, want, agent.logs())
	return last
}

func syncTurnFromAgent(t *testing.T, as *agentServer, convID, turnID string) TurnResponse {
	t.Helper()
	resp := as.do(t, http.MethodGet, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID, nil, "", nil)
	as.mustStatus(t, resp, http.StatusOK)
	var out TurnResponse
	as.decode(t, resp, &out)
	return out
}

func waitGoTurnStatus(t *testing.T, as *agentServer, convID, turnID, want string, timeout time.Duration) TurnResponse {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last TurnResponse
	for time.Now().Before(deadline) {
		last = syncTurnFromAgent(t, as, convID, turnID)
		if last.Status == want {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Go turn %s status=%s want=%s", turnID, last.Status, want)
	return last
}

func waitDurableTurnTerminal(t *testing.T, as *agentServer, projectionID string, timeout time.Duration, agent piAgentProc) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var status, phase string
	var turnEnds int
	for time.Now().Before(deadline) {
		err := as.pool.QueryRow(context.Background(), `
			SELECT p.status, e.phase,
			       (SELECT COUNT(*) FROM agent_turn_events ev
			        WHERE ev.turn_projection_id = p.id AND ev.kind = 'turn/end')
			FROM agent_turn_projections p
			JOIN agent_turn_executions e ON e.turn_projection_id = p.id
			WHERE p.id = $1
		`, projectionID).Scan(&status, &phase, &turnEnds)
		if err != nil {
			t.Fatal(err)
		}
		if status == "succeeded" && phase == "terminal" && turnEnds == 1 {
			return
		}
		if turnEnds > 1 {
			t.Fatalf("durable turn has %d turn/end events", turnEnds)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("durable terminal not committed: status=%s phase=%s turn_ends=%d\n%s", status, phase, turnEnds, agent.logs())
}

func seedAgentProviderFromEnv(t *testing.T, as *agentServer) {
	t.Helper()
	apiKey := strings.TrimSpace(os.Getenv("AGENT_PROVIDER_API_KEY"))
	if apiKey == "" {
		t.Fatal("AGENT_PROVIDER_API_KEY is required for L2 agent evals")
	}
	model := strings.TrimSpace(os.Getenv("AGENT_PROVIDER_MODEL"))
	if model == "" {
		model = "gpt-4.1"
	}
	baseURL := strings.TrimSpace(os.Getenv("AGENT_PROVIDER_BASE_URL"))
	profileID := clockid.New()
	bindingID := clockid.New()
	var base any
	if baseURL != "" {
		base = baseURL
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO provider_profiles (
			id, name, provider_type, api_key, base_url, capabilities_json, default_models_json, config_json,
			enabled, created_at, updated_at
		) VALUES (
			$1, 'Agent eval live provider', 'openai_compatible', $2, $3,
			'["text_responses"]'::json, $4::json, '{}'::json,
			TRUE, NOW(), NOW()
		)
	`, profileID, apiKey, base, fmt.Sprintf(`{"agent_model":%q}`, model)); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `DELETE FROM provider_bindings WHERE purpose = 'agent'`); err != nil {
		t.Fatal(err)
	}
	configJSON := "{}"
	if effort := strings.TrimSpace(os.Getenv("AGENT_PROVIDER_REASONING_EFFORT")); effort != "" {
		configJSON = fmt.Sprintf(`{"reasoning_effort":%q}`, effort)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO provider_bindings (
			id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at
		) VALUES (
			$1, 'agent', 'openai', $2, $3::json, $4::json, NOW(), NOW()
		)
	`, bindingID, profileID, fmt.Sprintf(`{"model":%q}`, model), configJSON); err != nil {
		t.Fatal(err)
	}
}

func seedFakeAgentProvider(t *testing.T, as *agentServer) {
	t.Helper()
	profileID := clockid.New()
	bindingID := clockid.New()
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO provider_profiles (
			id, name, provider_type, api_key, capabilities_json, default_models_json, config_json,
			enabled, created_at, updated_at
		) VALUES (
			$1, 'Agent OpenAI fixture', 'openai_compatible', 'sk-test',
			'["text_responses"]'::json, '{"agent_model":"fake-model"}'::json, '{}'::json,
			TRUE, NOW(), NOW()
		)
	`, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `DELETE FROM provider_bindings WHERE purpose = 'agent'`); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO provider_bindings (
			id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at
		) VALUES (
			$1, 'agent', 'openai', $2, '{"model":"fake-model"}'::json, '{}'::json, NOW(), NOW()
		)
	`, bindingID, profileID); err != nil {
		t.Fatal(err)
	}
}

func agentServiceDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "agent-service"))
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
