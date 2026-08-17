package agenttask_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/turn"
)

type serviceWireRequest struct {
	Input []json.RawMessage `json:"input"`
	Tools []struct {
		Name   string `json:"name"`
		Strict bool   `json:"strict"`
	} `json:"tools"`
}

func TestServiceMultimodalArtifactIdempotencyAndReplay(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var requests []serviceWireRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var captured serviceWireRequest
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, captured)
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			writeServiceStream(t, writer, `{"id":"artifact_1","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"draft_1","name":"propose_workflow_draft","arguments":"{\"nodes\":[\"hero\"]}"}]}`)
		case 2:
			writeServiceStream(t, writer, `{"id":"artifact_2","status":"completed","output":[{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"draft ready"}]}]}`)
		default:
			t.Errorf("unexpected provider call %d", calls.Load())
			http.Error(writer, "too many calls", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	database := filepath.Join(t.TempDir(), "turns.db")
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{
		ToolProjector: func(job durable.Job) []turn.ToolStep {
			return []turn.ToolStep{{StepID: job.Steps[0].ID, Kind: "propose_draft", Summary: "Propose workflow draft", Status: "running"}}
		},
		Runner: agenttask.Config{
			Database: database, Workspace: workspace, SkillUserHome: workspace,
			Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
			Policy:   testPolicy(),
			RequiredArtifact: &agenttask.RequiredArtifact{
				Name: agenttask.WorkflowDraftToolName,
				Schema: map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{"nodes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
					"required":   []string{"nodes"},
				},
			},
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })

	png := servicePNG(t)
	input := agenttask.TurnInput{
		SchemaVersion: agenttask.TurnInputSchemaVersion,
		Content: []agenttask.InputContent{
			{Type: agenttask.ContentInputText, Text: "create a workflow draft from this image"},
			{Type: agenttask.ContentInputImage, Image: &agenttask.InputImage{
				Data: png, MediaType: "image/png", SizeBytes: int64(len(png)), CheckpointMode: agenttask.ImageCheckpointEmbed,
			}},
		},
	}
	request := agenttask.StartTurnRequest{RunID: "run-artifact", Input: input, IdempotencyKey: "request-1"}
	const starters = 8
	ids := make(chan string, starters)
	errs := make(chan error, starters)
	for range starters {
		go func() {
			state, startErr := service.StartTurn(t.Context(), request)
			ids <- state.TurnID
			errs <- startErr
		}()
	}
	var turnID string
	for range starters {
		if startErr := <-errs; startErr != nil {
			t.Fatalf("StartTurn: %v", startErr)
		}
		if id := <-ids; turnID == "" {
			turnID = id
		} else if id != turnID {
			t.Fatalf("idempotent IDs diverged: %q and %q", turnID, id)
		}
	}
	state := awaitTurn(t, service, "run-artifact", turnID, turn.StatusAwaitingConfirmation)
	if state.Artifact == nil || state.Artifact.Name != agenttask.WorkflowDraftToolName ||
		string(state.Artifact.Value) != `{"nodes":["hero"]}` || state.Output != "draft ready" {
		t.Fatalf("state = %#v", state)
	}
	mu.Lock()
	captured := append([]serviceWireRequest(nil), requests...)
	mu.Unlock()
	if calls.Load() != 2 || len(captured) != 2 {
		t.Fatalf("provider calls = %d, requests = %d", calls.Load(), len(captured))
	}
	var user struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			ImageURL string `json:"image_url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(captured[0].Input[1], &user); err != nil {
		t.Fatal(err)
	}
	if len(user.Content) != 2 || !strings.HasPrefix(user.Content[1].ImageURL, "data:image/png;base64,") {
		t.Fatalf("multimodal request = %#v", user)
	}
	strict := false
	for _, tool := range captured[0].Tools {
		if tool.Name == agenttask.WorkflowDraftToolName {
			strict = tool.Strict
		}
	}
	if !strict {
		t.Fatal("required artifact tool was not sent with strict=true")
	}
	events, err := service.Events(t.Context(), state.RunID, state.TurnID, 0)
	if err != nil || len(events) < 5 {
		t.Fatalf("events = %#v, err = %v", events, err)
	}
	for index, event := range events {
		if event.RunID != state.RunID || event.TurnID != state.TurnID || event.SchemaVersion != turn.EventSchemaVersion ||
			event.Sequence != int64(index+1) || event.CreatedAt.IsZero() {
			t.Fatalf("event %d = %#v", index, event)
		}
	}
	var streamed strings.Builder
	sawAttemptStarted := false
	for _, event := range events {
		if event.Kind == "tool.step" {
			sawAttemptStarted = true
		}
		if event.Kind != agenttask.EventTextDelta {
			continue
		}
		if !sawAttemptStarted {
			t.Fatal("text.delta was sequenced before projected tool.step")
		}
		var payload struct {
			Delta     string `json:"delta"`
			StepID    string `json:"step_id"`
			AttemptID string `json:"attempt_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.StepID == "" || payload.AttemptID == "" {
			t.Fatalf("text.delta payload = %s, err = %v", event.Payload, err)
		}
		streamed.WriteString(payload.Delta)
	}
	if streamed.String() != state.Output {
		t.Fatalf("replayed text.delta = %q, final output = %q", streamed.String(), state.Output)
	}
	cursor := events[len(events)/2].Sequence
	replayed, err := service.Events(t.Context(), state.RunID, state.TurnID, cursor)
	if err != nil || len(replayed) == 0 || replayed[0].Sequence != cursor+1 {
		t.Fatalf("replayed = %#v, err = %v", replayed, err)
	}

	different := request
	different.Input = agenttask.TextInput("different input")
	if _, err := service.StartTurn(t.Context(), different); !errors.Is(err, turn.ErrIdempotencyConflict) {
		t.Fatalf("different idempotent request error = %v", err)
	}
}

func TestOpenServiceRejectsStoredBackgroundWithoutReplayableTokenStream(t *testing.T) {
	_, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Provider: agenttask.ProviderConfig{ResponseMode: agenttask.ResponseModeStoredBackground},
	}})
	if err == nil || !strings.Contains(err.Error(), "token-level text.delta") {
		t.Fatalf("stored background service error = %v", err)
	}
}

func TestServiceQuestionAnswersSurviveRestartWithConflicts(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var requests []serviceWireRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var captured serviceWireRequest
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		requests = append(requests, captured)
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			writeServiceStream(t, writer, `{"id":"q_1","status":"completed","output":[{"id":"fc_q1","type":"function_call","call_id":"ask_1","name":"ask_user","arguments":"{\"header\":\"Scope\",\"question\":\"Describe the custom scope\",\"options\":[{\"label\":\"Default\"}]}"}]}`)
		case 2:
			writeServiceStream(t, writer, `{"id":"q_2","status":"completed","output":[{"id":"fc_q2","type":"function_call","call_id":"ask_2","name":"ask_user","arguments":"{\"header\":\"Mode\",\"question\":\"Choose a mode\",\"options\":[{\"label\":\"Fast\"},{\"label\":\"Safe\"}]}"}]}`)
		case 3:
			writeServiceStream(t, writer, `{"id":"q_3","status":"completed","output":[{"id":"msg_q3","type":"message","role":"assistant","content":[{"type":"output_text","text":"questions complete"}]}]}`)
		default:
			t.Errorf("unexpected call %d", calls.Load())
		}
	}))
	t.Cleanup(server.Close)

	database := filepath.Join(t.TempDir(), "questions.db")
	workspace := t.TempDir()
	config := agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: database, Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
	}}
	service, err := agenttask.OpenService(config)
	if err != nil {
		t.Fatal(err)
	}
	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-question", Input: agenttask.TextInput("ask both questions"), IdempotencyKey: "question-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusRequiresInput)
	firstQuestion := *state.Question
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	service, err = agenttask.OpenService(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })

	if _, err := service.AnswerQuestion(t.Context(), state.RunID, state.TurnID, "stale-question", turn.OptionAnswer(99)); !errors.Is(err, turn.ErrQuestionExpired) {
		t.Fatalf("stale answer error = %v", err)
	}
	type answerResult struct {
		state agenttask.Turn
		err   error
	}
	const answerers = 8
	answers := make(chan answerResult, answerers)
	runID, turnID := state.RunID, state.TurnID
	for range answerers {
		go func() {
			answered, answerErr := service.AnswerQuestion(
				t.Context(), runID, turnID, firstQuestion.ID, turn.TextAnswer("custom scope"),
			)
			answers <- answerResult{state: answered, err: answerErr}
		}()
	}
	for range answerers {
		result := <-answers
		if result.err != nil || result.state.Status != turn.StatusQueued {
			t.Fatalf("concurrent free-text answer state = %#v, err = %v", result.state, result.err)
		}
		state = result.state
	}
	later, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: state.RunID, Input: agenttask.TextInput("must wait behind the answered turn"), IdempotencyKey: "later-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	state, err = service.GetTurn(t.Context(), state.RunID, state.TurnID)
	if err != nil || state.Status != turn.StatusQueued || calls.Load() != 1 {
		t.Fatalf("answered turn resumed without ResumeTurn: state=%#v calls=%d err=%v", state, calls.Load(), err)
	}
	if later, err = service.CancelTurn(t.Context(), later.RunID, later.TurnID); err != nil || later.Status != turn.StatusCanceled {
		t.Fatalf("cancel later turn = %#v, err = %v", later, err)
	}
	if duplicate, err := service.AnswerQuestion(t.Context(), state.RunID, state.TurnID, firstQuestion.ID, turn.TextAnswer("custom scope")); err != nil || duplicate.TurnID != state.TurnID {
		t.Fatalf("duplicate answer = %#v, err = %v", duplicate, err)
	}
	if _, err := service.AnswerQuestion(t.Context(), state.RunID, state.TurnID, firstQuestion.ID, turn.TextAnswer("different")); !errors.Is(err, turn.ErrQuestionAlreadyAnswered) {
		t.Fatalf("conflicting duplicate error = %v", err)
	}
	if _, err := service.ResumeTurn(t.Context(), state.RunID, state.TurnID); err != nil {
		t.Fatal(err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusRequiresInput)
	secondQuestion := *state.Question
	if duplicate, err := service.AnswerQuestion(t.Context(), state.RunID, state.TurnID, firstQuestion.ID, turn.TextAnswer("custom scope")); err != nil || duplicate.Question == nil || duplicate.Question.ID != secondQuestion.ID {
		t.Fatalf("old duplicate answer = %#v, err = %v", duplicate, err)
	}
	state, err = service.AnswerQuestion(t.Context(), state.RunID, state.TurnID, secondQuestion.ID, turn.OptionAnswer(1))
	if err != nil || state.Status != turn.StatusQueued {
		t.Fatalf("option answer state = %#v, err = %v", state, err)
	}
	if duplicate, err := service.AnswerQuestion(t.Context(), state.RunID, state.TurnID, secondQuestion.ID, turn.OptionAnswer(1)); err != nil || duplicate.Status != turn.StatusQueued {
		t.Fatalf("duplicate option answer = %#v, err = %v", duplicate, err)
	}
	if _, err := service.ResumeTurn(t.Context(), state.RunID, state.TurnID); err != nil {
		t.Fatal(err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusSucceeded)
	if state.Output != "questions complete" || calls.Load() != 3 {
		t.Fatalf("state = %#v, calls = %d", state, calls.Load())
	}
	mu.Lock()
	captured := append([]serviceWireRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 3 || !wireHasText(captured[1], "custom scope") || !wireHasText(captured[2], "Safe") {
		t.Fatalf("question continuation requests = %#v", captured)
	}
}

func TestServiceRejectsProseOnlyCompletionWhenArtifactIsRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writeServiceStream(t, writer, `{"id":"prose","status":"completed","output":[{"id":"msg","type":"message","role":"assistant","content":[{"type":"output_text","text":"I described a draft but did not submit it"}]}]}`)
	}))
	t.Cleanup(server.Close)
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "artifact-gate.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
		RequiredArtifact: &agenttask.RequiredArtifact{Schema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"nodes": map[string]any{"type": "array"}}, "required": []string{"nodes"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-prose", Input: agenttask.TextInput("create a draft"), IdempotencyKey: "prose-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusFailed)
	if state.Artifact != nil || !strings.Contains(state.Error, "propose_workflow_draft") {
		t.Fatalf("prose-only state = %#v", state)
	}
}

func TestServiceProjectsOptionalArtifactWithoutGatingReadOnlyCompletion(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			writeServiceStream(t, writer, `{"id":"optional-call","status":"completed","output":[{"id":"call","type":"function_call","call_id":"optional","name":"propose_optional_artifact","arguments":"{\"value\":\"organized\"}"}]}`)
		case 2:
			writeServiceStream(t, writer, `{"id":"optional-done","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"已准备整理建议。"}]}]}`)
		default:
			t.Errorf("unexpected provider call %d", calls.Load())
			http.Error(writer, "too many calls", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "optional-artifact.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
		OptionalArtifact: &agenttask.RequiredArtifact{
			Name: "propose_optional_artifact",
			Schema: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"value": map[string]any{"type": "string"}},
				"required":   []string{"value"},
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })

	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-optional-artifact", Input: agenttask.TextInput("整理最近生成的素材"), IdempotencyKey: "optional-artifact",
	})
	if err != nil {
		t.Fatal(err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusSucceeded)
	if state.Artifact == nil || state.Artifact.Name != "propose_optional_artifact" ||
		string(state.Artifact.Value) != `{"value":"organized"}` || state.Output != "已准备整理建议。" {
		t.Fatalf("optional artifact state = %#v", state)
	}
}

func TestServiceAllowsProseOnlyFollowUpAfterArtifact(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			writeServiceStream(t, writer, `{"id":"draft","status":"completed","output":[{"id":"call","type":"function_call","call_id":"draft","name":"propose_workflow_draft","arguments":"{\"nodes\":[]}"}]}`)
		case 2:
			writeServiceStream(t, writer, `{"id":"draft-ready","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"草案已提交。"}]}]}`)
		case 3:
			writeServiceStream(t, writer, `{"id":"follow-up","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"当前草案已经准备好，请使用确认操作继续。"}]}]}`)
		default:
			t.Errorf("unexpected provider call %d", calls.Load())
			http.Error(writer, "too many calls", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "follow-up-artifact.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
		RequiredArtifact: &agenttask.RequiredArtifact{
			Schema: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"nodes": map[string]any{"type": "array"}}, "required": []string{"nodes"},
			},
			AllowPriorTranscriptArtifact: true,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })

	first, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-follow-up-artifact", Input: agenttask.TextInput("create a draft"), IdempotencyKey: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	first = awaitTurn(t, service, first.RunID, first.TurnID, turn.StatusAwaitingConfirmation)
	if first.Artifact == nil {
		t.Fatalf("first turn artifact = %#v", first.Artifact)
	}

	second, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: first.RunID, Input: agenttask.TextInput("what happens next?"), IdempotencyKey: "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	second = awaitTurn(t, service, second.RunID, second.TurnID, turn.StatusSucceeded)
	if second.Output != "当前草案已经准备好，请使用确认操作继续。" || calls.Load() != 3 {
		t.Fatalf("follow-up turn = %#v, provider calls = %d", second, calls.Load())
	}
}

func TestServiceLocallyRejectsArtifactOutsideRequiredSchema(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			writeServiceStream(t, writer, `{"id":"invalid_artifact","status":"completed","output":[{"id":"call","type":"function_call","call_id":"draft","name":"propose_workflow_draft","arguments":"{\"nodes\":\"not-an-array\"}"}]}`)
			return
		}
		writeServiceStream(t, writer, `{"id":"final","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"draft submitted"}]}]}`)
	}))
	t.Cleanup(server.Close)
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "invalid-artifact.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
		RequiredArtifact: &agenttask.RequiredArtifact{Schema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"nodes": map[string]any{"type": "array"}}, "required": []string{"nodes"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-invalid-artifact", Input: agenttask.TextInput("create a draft"), IdempotencyKey: "invalid-artifact",
	})
	if err != nil {
		t.Fatal(err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusFailed)
	if state.Artifact != nil || calls.Load() != 2 || !strings.Contains(state.Error, "does not match schema") {
		t.Fatalf("invalid artifact state = %#v", state)
	}
}

func TestServiceRetriesRequiredArtifactRejectedByApplication(t *testing.T) {
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch providerCalls.Add(1) {
		case 1:
			writeServiceStream(t, writer, `{"id":"invalid_artifact","status":"completed","output":[{"id":"call_1","type":"function_call","call_id":"draft_1","name":"propose_workflow_draft","arguments":"{\"nodes\":[\"invalid\"]}"}]}`)
		case 2:
			writeServiceStream(t, writer, `{"id":"valid_artifact","status":"completed","output":[{"id":"call_2","type":"function_call","call_id":"draft_2","name":"propose_workflow_draft","arguments":"{\"nodes\":[\"accepted\"]}"}]}`)
		case 3:
			writeServiceStream(t, writer, `{"id":"final","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"draft submitted"}]}]}`)
		default:
			http.Error(writer, "unexpected provider call", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	var validationCalls atomic.Int32
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "application-artifact-validation.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
		RequiredArtifact: &agenttask.RequiredArtifact{
			Schema: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"nodes": map[string]any{"type": "array"}}, "required": []string{"nodes"},
			},
			Validate: func(_ context.Context, raw json.RawMessage) error {
				validationCalls.Add(1)
				if strings.Contains(string(raw), "invalid") {
					return errors.New("node plan violates application invariants")
				}
				return nil
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-application-artifact-validation", Input: agenttask.TextInput("create a draft"),
		IdempotencyKey: "application-artifact-validation",
	})
	if err != nil {
		t.Fatal(err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusAwaitingConfirmation)
	if state.Artifact == nil || string(state.Artifact.Value) != `{"nodes":["accepted"]}` ||
		providerCalls.Load() != 3 || validationCalls.Load() != 2 {
		t.Fatalf(
			"application-validated artifact state=%#v provider_calls=%d validation_calls=%d",
			state,
			providerCalls.Load(),
			validationCalls.Load(),
		)
	}
}

func TestServiceSerializesSessionAndCancelsQueuedTurn(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
			select {
			case <-release:
			case <-request.Context().Done():
				return
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		writeServiceStream(t, writer, `{"id":"serial","status":"completed","output":[{"id":"msg","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`)
	}))
	t.Cleanup(server.Close)
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "serial.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	first, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{RunID: "run-serial", Input: agenttask.TextInput("first"), IdempotencyKey: "first"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first turn did not start")
	}
	second, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{RunID: "run-serial", Input: agenttask.TextInput("second"), IdempotencyKey: "second"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("same-session turns ran concurrently: calls=%d", calls.Load())
	}
	second, err = service.CancelTurn(t.Context(), second.RunID, second.TurnID)
	if err != nil || second.Status != turn.StatusCanceled {
		t.Fatalf("canceled second = %#v, err = %v", second, err)
	}
	if repeated, err := service.CancelTurn(t.Context(), second.RunID, second.TurnID); err != nil || repeated.Status != turn.StatusCanceled {
		t.Fatalf("repeated queued cancellation = %#v, err = %v", repeated, err)
	}
	close(release)
	first = awaitTurn(t, service, first.RunID, first.TurnID, turn.StatusSucceeded)
	if first.Output != "done" || calls.Load() != 1 {
		t.Fatalf("first = %#v, calls = %d", first, calls.Load())
	}
}

func TestServiceDrainsSerializedRunWithoutStrandingQueuedTurns(t *testing.T) {
	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maxActive.Load()
			if current <= observed || maxActive.CompareAndSwap(observed, current) {
				break
			}
		}
		calls.Add(1)
		time.Sleep(5 * time.Millisecond)
		writer.Header().Set("Content-Type", "application/json")
		writeServiceStream(t, writer, `{"id":"drain","status":"completed","output":[{"id":"msg","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`)
	}))
	t.Cleanup(server.Close)
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "drain.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	const count = 20
	turns := make([]agenttask.Turn, count)
	for index := range turns {
		turns[index], err = service.StartTurn(t.Context(), agenttask.StartTurnRequest{
			RunID: "run-drain", Input: agenttask.TextInput(fmt.Sprintf("turn %d", index)),
			IdempotencyKey: fmt.Sprintf("request-%d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range turns {
		awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusSucceeded)
	}
	if calls.Load() != count || maxActive.Load() != 1 {
		t.Fatalf("provider calls=%d max concurrent=%d", calls.Load(), maxActive.Load())
	}
}

func TestServiceCompilesQueuedTurnFromPriorDurableTranscriptAtClaimTime(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var requests []serviceWireRequest
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var captured serviceWireRequest
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		requests = append(requests, captured)
		mu.Unlock()
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-request.Context().Done():
				return
			}
			writeServiceStream(t, writer, `{"id":"history_1","status":"completed","output":[{"id":"reasoning_1","type":"reasoning","encrypted_content":"opaque-history"},{"id":"message_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"remember cobalt"}]}]}`)
		case 2:
			writeServiceStream(t, writer, `{"id":"history_2","status":"completed","output":[{"id":"message_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"cobalt"}]}]}`)
		default:
			t.Errorf("unexpected provider call %d", calls.Load())
		}
	}))
	t.Cleanup(server.Close)

	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "history-claim.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	firstInput := agenttask.TurnInput{SchemaVersion: agenttask.TurnInputSchemaVersion, Content: []agenttask.InputContent{
		{Type: agenttask.ContentInputText, Text: "remember the code word"},
		{Type: agenttask.ContentInputImage, Image: &agenttask.InputImage{
			URL: "https://cdn.example/history.webp", MediaType: "image/webp", SizeBytes: 123,
			CheckpointMode: agenttask.ImageCheckpointReference,
		}},
	}}
	first, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-history-claim", Input: firstInput, IdempotencyKey: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first provider request did not start")
	}
	second, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: first.RunID, Input: agenttask.TextInput("what is the code word?"), IdempotencyKey: "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("queued turn reached provider early: calls=%d", calls.Load())
	}
	close(releaseFirst)
	first = awaitTurn(t, service, first.RunID, first.TurnID, turn.StatusSucceeded)
	second = awaitTurn(t, service, second.RunID, second.TurnID, turn.StatusSucceeded)
	if first.Output != "remember cobalt" || second.Output != "cobalt" {
		t.Fatalf("history outputs: first=%#v second=%#v", first, second)
	}
	mu.Lock()
	captured := append([]serviceWireRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 2 || !wireHasText(captured[1], "remember the code word") ||
		!wireHasText(captured[1], "remember cobalt") || !wireHasText(captured[1], "what is the code word?") ||
		!wireHasText(captured[1], "opaque-history") || !wireHasText(captured[1], "https://cdn.example/history.webp") {
		t.Fatalf("second Turn did not inherit exact prior transcript: %#v", captured)
	}
}

func TestServiceInheritsPriorTurnTranscriptAfterRestart(t *testing.T) {
	var calls atomic.Int32
	secondRequest := make(chan serviceWireRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var captured serviceWireRequest
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if calls.Add(1) == 1 {
			writeServiceStream(t, writer, `{"id":"restart_history_1","status":"completed","output":[{"id":"message_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"persisted answer"}]}]}`)
			return
		}
		secondRequest <- captured
		writeServiceStream(t, writer, `{"id":"restart_history_2","status":"completed","output":[{"id":"message_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"continued"}]}]}`)
	}))
	t.Cleanup(server.Close)
	database := filepath.Join(t.TempDir(), "history-restart.db")
	workspace := t.TempDir()
	config := agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: database, Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
	}}
	service, err := agenttask.OpenService(config)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-history-restart", Input: agenttask.TextInput("first persisted question"), IdempotencyKey: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, service, first.RunID, first.TurnID, turn.StatusSucceeded)
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	service, err = agenttask.OpenService(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	second, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: first.RunID, Input: agenttask.TextInput("continue after restart"), IdempotencyKey: "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, service, second.RunID, second.TurnID, turn.StatusSucceeded)
	select {
	case captured := <-secondRequest:
		if !wireHasText(captured, "first persisted question") || !wireHasText(captured, "persisted answer") ||
			!wireHasText(captured, "continue after restart") {
			t.Fatalf("restart transcript = %#v", captured)
		}
	case <-time.After(time.Second):
		t.Fatal("second provider request was not captured")
	}
}

func TestServiceCancellationIsIdempotentAndDoesNotHideAmbiguousAttempt(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	workspace := t.TempDir()
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "cancel.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-cancel", Input: agenttask.TextInput("wait for cancellation"), IdempotencyKey: "cancel-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request did not start")
	}
	first, err := service.CancelTurn(t.Context(), state.RunID, state.TurnID)
	if err != nil || first.Status != turn.StatusCancelRequested {
		t.Fatalf("first cancellation = %#v, err = %v", first, err)
	}
	duplicate, err := service.CancelTurn(t.Context(), state.RunID, state.TurnID)
	if err != nil || duplicate.TurnID != state.TurnID {
		t.Fatalf("duplicate cancellation = %#v, err = %v", duplicate, err)
	}
	state = awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusUnknown)
	if !strings.Contains(state.Error, "interrupted a running durable attempt") {
		t.Fatalf("canceled running attempt = %#v", state)
	}
	if repeated, err := service.CancelTurn(t.Context(), state.RunID, state.TurnID); err != nil || repeated.Status != turn.StatusUnknown {
		t.Fatalf("terminal duplicate cancellation = %#v, err = %v", repeated, err)
	}
}

func TestServiceCloseLeavesRunningTurnResumable(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	workspace := t.TempDir()
	config := agenttask.ServiceConfig{Runner: agenttask.Config{
		Database: filepath.Join(t.TempDir(), "restart.db"), Workspace: workspace, SkillUserHome: workspace,
		Provider: agenttask.ProviderConfig{APIKey: "secret", BaseURL: server.URL, Model: "model", HTTPClient: server.Client()},
		Policy:   testPolicy(),
		EngineOptions: durable.Options{
			LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		},
	}}
	service, err := agenttask.OpenService(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if service != nil {
			_ = service.Close()
		}
	})
	state, err := service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-restart", Input: agenttask.TextInput("survive service restart"), IdempotencyKey: "restart-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request did not start")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	service = nil
	service, err = agenttask.OpenService(config)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := service.GetTurn(t.Context(), state.RunID, state.TurnID)
	if err != nil || persisted.Status != turn.StatusRunning {
		t.Fatalf("persisted interrupted turn = %#v, err = %v", persisted, err)
	}
	resumed, err := service.ResumeTurn(t.Context(), state.RunID, state.TurnID)
	if err != nil || resumed.Status != turn.StatusQueued {
		t.Fatalf("resumed interrupted turn = %#v, err = %v", resumed, err)
	}
	awaitTurn(t, service, state.RunID, state.TurnID, turn.StatusUnknown)
}

func awaitTurn(t *testing.T, service *agenttask.Service, runID, turnID string, status turn.Status) agenttask.Turn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.GetTurn(context.Background(), runID, turnID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == status {
			return state
		}
		if state.Status == turn.StatusFailed || state.Status == turn.StatusUnknown || state.Status == turn.StatusCanceled {
			t.Fatalf("turn reached %s while waiting for %s: %#v", state.Status, status, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, _ := service.GetTurn(context.Background(), runID, turnID)
	t.Fatalf("timeout waiting for %s: %#v", status, state)
	return agenttask.Turn{}
}

func wireHasText(request serviceWireRequest, text string) bool {
	for _, raw := range request.Input {
		if strings.Contains(string(raw), text) {
			return true
		}
	}
	return false
}

func writeServiceStream(t *testing.T, writer http.ResponseWriter, response string) {
	t.Helper()
	var envelope struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal([]byte(response), &envelope); err != nil {
		t.Errorf("decode service response fixture: %v", err)
		http.Error(writer, "invalid fixture", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	for _, output := range envelope.Output {
		for _, content := range output.Content {
			if content.Type != "output_text" || content.Text == "" {
				continue
			}
			delta, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": content.Text})
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", delta)
		}
	}
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
}

func servicePNG(t *testing.T) []byte {
	t.Helper()
	value, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return value
}
