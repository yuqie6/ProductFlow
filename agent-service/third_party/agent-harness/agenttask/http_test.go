package agenttask_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/turn"
)

func TestHTTPHandlerControlsQuestionTurnAndReplaysSSE(t *testing.T) {
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		switch providerCalls.Add(1) {
		case 1:
			writeServiceStream(t, writer, `{"id":"http_question","status":"completed","output":[{"id":"question_call","type":"function_call","call_id":"ask","name":"ask_user","arguments":"{\"header\":\"Scope\",\"question\":\"Which scope?\",\"options\":[{\"label\":\"Default\"}]}"}]}`)
		case 2:
			writeServiceStream(t, writer, `{"id":"http_complete","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"HTTP turn complete"}]}]}`)
		default:
			t.Errorf("unexpected provider call %d", providerCalls.Load())
		}
	}))
	t.Cleanup(provider.Close)

	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "http.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: provider.URL, Model: "model", HTTPClient: provider.Client()},
		Policy:   testPolicy(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	handler, err := agenttask.NewHTTPHandler(service, agenttask.HTTPOptions{
		EventPollInterval: 2 * time.Millisecond, HeartbeatInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)

	startBody := agenttask.HTTPStartTurnRequest{
		Input: agenttask.TextInput("ask through HTTP"), IdempotencyKey: "http-request",
	}
	response := httpJSON(t, api.Client(), http.MethodPost, api.URL+"/v1alpha1/runs/http-run/turns", startBody)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, body = %s", response.StatusCode, response.body)
	}
	var state agenttask.Turn
	decodeHTTPBody(t, response.body, &state)
	if state.RunID != "http-run" || state.TurnID == "" || response.header.Get("Location") == "" {
		t.Fatalf("started state = %#v, location = %q", state, response.header.Get("Location"))
	}
	duplicate := httpJSON(t, api.Client(), http.MethodPost, api.URL+"/v1alpha1/runs/http-run/turns", startBody)
	var duplicateState agenttask.Turn
	decodeHTTPBody(t, duplicate.body, &duplicateState)
	if duplicate.StatusCode != http.StatusAccepted || duplicateState.TurnID != state.TurnID {
		t.Fatalf("duplicate start status=%d state=%#v", duplicate.StatusCode, duplicateState)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusRequiresInput)

	get := httpJSON(t, api.Client(), http.MethodGet, api.URL+"/v1alpha1/runs/http-run/turns/"+state.TurnID, nil)
	var got agenttask.Turn
	decodeHTTPBody(t, get.body, &got)
	if get.StatusCode != http.StatusOK || got.Question == nil || got.Question.ID != state.Question.ID {
		t.Fatalf("get status=%d state=%#v", get.StatusCode, got)
	}

	followerResponse := httpJSON(t, api.Client(), http.MethodPost, api.URL+"/v1alpha1/runs/http-run/turns", agenttask.HTTPStartTurnRequest{
		Input: agenttask.TextInput("cancel me"), IdempotencyKey: "follower",
	})
	var follower agenttask.Turn
	decodeHTTPBody(t, followerResponse.body, &follower)
	canceled := httpJSON(t, api.Client(), http.MethodPost, api.URL+"/v1alpha1/runs/http-run/turns/"+follower.TurnID+"/cancel", nil)
	decodeHTTPBody(t, canceled.body, &follower)
	if canceled.StatusCode != http.StatusOK || follower.Status != turn.StatusCanceled {
		t.Fatalf("cancel status=%d state=%#v", canceled.StatusCode, follower)
	}

	answerURL := api.URL + "/v1alpha1/runs/http-run/turns/" + state.TurnID + "/questions/" + state.Question.ID + "/answer"
	answered := httpJSON(t, api.Client(), http.MethodPost, answerURL, agenttask.HTTPAnswerQuestionRequest{
		Answer: agenttask.TextAnswer("custom scope"),
	})
	decodeHTTPBody(t, answered.body, &state)
	if answered.StatusCode != http.StatusOK || state.Status != turn.StatusQueued {
		t.Fatalf("answer status=%d state=%#v", answered.StatusCode, state)
	}
	conflictingAnswer := httpJSON(t, api.Client(), http.MethodPost, answerURL, agenttask.HTTPAnswerQuestionRequest{
		Answer: agenttask.TextAnswer("different scope"),
	})
	if conflictingAnswer.StatusCode != http.StatusConflict || !strings.Contains(conflictingAnswer.body, "question_already_answered") {
		t.Fatalf("conflicting answer status=%d body=%s", conflictingAnswer.StatusCode, conflictingAnswer.body)
	}
	resumed := httpJSON(t, api.Client(), http.MethodPost, api.URL+"/v1alpha1/runs/http-run/turns/"+state.TurnID+"/resume", nil)
	if resumed.StatusCode != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", resumed.StatusCode, resumed.body)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusSucceeded)
	if state.Output != "HTTP turn complete" || providerCalls.Load() != 2 {
		t.Fatalf("terminal state=%#v calls=%d", state, providerCalls.Load())
	}

	eventsURL := api.URL + "/v1alpha1/runs/http-run/turns/" + state.TurnID + "/events"
	eventsResponse, err := api.Client().Get(eventsURL)
	if err != nil {
		t.Fatal(err)
	}
	eventsBody, err := io.ReadAll(eventsResponse.Body)
	_ = eventsResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if eventsResponse.StatusCode != http.StatusOK || !strings.HasPrefix(eventsResponse.Header.Get("Content-Type"), "text/event-stream") ||
		!strings.Contains(string(eventsBody), "event: text.delta") || !strings.Contains(string(eventsBody), `"delta":"HTTP turn complete"`) {
		t.Fatalf("SSE status=%d content-type=%q body=%s", eventsResponse.StatusCode, eventsResponse.Header.Get("Content-Type"), eventsBody)
	}
	ids := sseEventIDs(t, string(eventsBody))
	if len(ids) < 3 {
		t.Fatalf("SSE ids = %v", ids)
	}
	cursor := ids[len(ids)/2]
	replayRequest, _ := http.NewRequest(http.MethodGet, eventsURL, nil)
	replayRequest.Header.Set("Last-Event-ID", strconv.FormatInt(cursor, 10))
	replayResponse, err := api.Client().Do(replayRequest)
	if err != nil {
		t.Fatal(err)
	}
	replayBody, _ := io.ReadAll(replayResponse.Body)
	_ = replayResponse.Body.Close()
	for _, id := range sseEventIDs(t, string(replayBody)) {
		if id <= cursor {
			t.Fatalf("replayed id %d is not after cursor %d: %s", id, cursor, replayBody)
		}
	}

	conflictingCursor, _ := http.NewRequest(http.MethodGet, eventsURL+"?after=1", nil)
	conflictingCursor.Header.Set("Last-Event-ID", "2")
	cursorResponse, err := api.Client().Do(conflictingCursor)
	if err != nil {
		t.Fatal(err)
	}
	_ = cursorResponse.Body.Close()
	if cursorResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("conflicting cursor status = %d", cursorResponse.StatusCode)
	}

	badStart := httpJSONRaw(t, api.Client(), http.MethodPost, api.URL+"/v1alpha1/runs/http-run/turns", `{"input":{"schema_version":1,"content":[{"type":"input_text","text":"bad"}]},"idempotency_key":"bad","unknown":true}`)
	if badStart.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", badStart.StatusCode, badStart.body)
	}
	badID := httpJSON(t, api.Client(), http.MethodGet, api.URL+"/v1alpha1/runs/http-run/turns/%20", nil)
	if badID.StatusCode != http.StatusBadRequest || !strings.Contains(badID.body, "invalid turn_id") {
		t.Fatalf("invalid ID status=%d body=%s", badID.StatusCode, badID.body)
	}
	idempotencyConflict := httpJSON(t, api.Client(), http.MethodPost, api.URL+"/v1alpha1/runs/http-run/turns", agenttask.HTTPStartTurnRequest{
		Input: agenttask.TextInput("different"), IdempotencyKey: "http-request",
	})
	if idempotencyConflict.StatusCode != http.StatusConflict || !strings.Contains(idempotencyConflict.body, "idempotency_conflict") {
		t.Fatalf("idempotency status=%d body=%s", idempotencyConflict.StatusCode, idempotencyConflict.body)
	}
}

func TestHTTPHandlerDrainsEventsCommittedBetweenPollAndTerminalObservation(t *testing.T) {
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(providerStarted)
		select {
		case <-releaseProvider:
		case <-request.Context().Done():
			return
		}
		writeServiceStream(t, writer, `{"id":"terminal_race","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"terminal batch"}]}]}`)
	}))
	t.Cleanup(provider.Close)

	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "terminal-race.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: provider.URL, Model: "model", HTTPClient: provider.Client()},
		Policy:   testPolicy(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "terminal-race-run", Input: agenttask.TextInput("finish in the race window"), IdempotencyKey: "terminal-race",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providerStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	handler, err := agenttask.NewHTTPHandler(service, agenttask.HTTPOptions{
		EventPollInterval: time.Second, HeartbeatInterval: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := &flushCallbackRecorder{ResponseRecorder: httptest.NewRecorder()}
	recorder.onSecondFlush = func() {
		if strings.Contains(recorder.Body.String(), "event: turn.succeeded") {
			t.Fatal("terminal event was already present in the first event batch")
		}
		close(releaseProvider)
		state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusSucceeded)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1alpha1/runs/"+state.RunID+"/turns/"+state.TurnID+"/events",
		nil,
	)
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.flushes < 3 || !strings.Contains(body, "event: text.delta") ||
		!strings.Contains(body, "event: turn.succeeded") || !strings.Contains(body, `"output":"terminal batch"`) {
		t.Fatalf("final event batch was not drained: flushes=%d body=%s", recorder.flushes, body)
	}
}

type flushCallbackRecorder struct {
	*httptest.ResponseRecorder
	flushes       int
	onSecondFlush func()
}

func (recorder *flushCallbackRecorder) Flush() {
	recorder.ResponseRecorder.Flush()
	recorder.flushes++
	if recorder.flushes == 2 && recorder.onSecondFlush != nil {
		recorder.onSecondFlush()
	}
}

type httpResult struct {
	StatusCode int
	header     http.Header
	body       string
}

func httpJSON(t *testing.T, client *http.Client, method, url string, value any) httpResult {
	t.Helper()
	if value == nil {
		request, err := http.NewRequest(method, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		return doHTTP(t, client, request)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return httpJSONRaw(t, client, method, url, string(encoded))
}

func httpJSONRaw(t *testing.T, client *http.Client, method, url, body string) httpResult {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	return doHTTP(t, client, request)
}

func doHTTP(t *testing.T, client *http.Client, request *http.Request) httpResult {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResult{StatusCode: response.StatusCode, header: response.Header.Clone(), body: string(body)}
}

func decodeHTTPBody(t *testing.T, body string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("decode HTTP body %q: %v", body, err)
	}
}

func sseEventIDs(t *testing.T, body string) []int64 {
	t.Helper()
	var ids []int64
	for _, line := range strings.Split(body, "\n") {
		value, found := strings.CutPrefix(line, "id: ")
		if !found {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatalf("invalid SSE id %q: %v", value, err)
		}
		ids = append(ids, id)
	}
	return ids
}
