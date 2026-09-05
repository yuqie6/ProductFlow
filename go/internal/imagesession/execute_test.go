package imagesession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"os"
	"testing"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/product"
)

type countingProvider struct {
	MockChatProvider
	calls int
	reqs  []ChatRequest
}

func (c *countingProvider) Generate(ctx context.Context, req ChatRequest) (ChatResult, error) {
	c.calls++
	copied := req
	if req.BaseBytes != nil {
		copied.BaseBytes = append([]byte(nil), req.BaseBytes...)
	}
	if len(req.ReferenceBytes) > 0 {
		copied.ReferenceBytes = make([][]byte, len(req.ReferenceBytes))
		for i, b := range req.ReferenceBytes {
			copied.ReferenceBytes[i] = append([]byte(nil), b...)
		}
	}
	if req.PreviousResponseID != nil {
		v := *req.PreviousResponseID
		copied.PreviousResponseID = &v
	}
	c.reqs = append(c.reqs, copied)
	return c.MockChatProvider.Generate(ctx, req)
}

func createQueuedGeneration(t *testing.T, ss *sessionServer, body map[string]any) (DetailResponse, string) {
	t.Helper()
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", body)
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	if len(session.GenerationTasks) == 0 {
		t.Fatal("missing generation task")
	}
	taskID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, taskID)
	return session, taskID
}

func loadSessionDetail(t *testing.T, ss *sessionServer, sessionID string) DetailResponse {
	t.Helper()
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+sessionID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	var session DetailResponse
	ss.decode(t, got, &session)
	return session
}

func uploadSessionRef(t *testing.T, ss *sessionServer, sessionID string, png []byte) string {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("reference_images", "ref.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := ss.do(t, http.MethodPost, "/api/image-sessions/"+sessionID+"/reference-images", &buf, w.FormDataContentType())
	ss.mustStatus(t, resp, http.StatusOK)
	var detail DetailResponse
	ss.decode(t, resp, &detail)
	for _, asset := range detail.Assets {
		if asset.Kind == kindReference {
			return asset.ID
		}
	}
	t.Fatal("no reference asset")
	return ""
}

func insertAppliedEffect(t *testing.T, ss *sessionServer, taskID string, start, count int) {
	t.Helper()
	opKey := "image-session-task:" + taskID + ":candidates:" + itoa(start) + "-" + itoa(count)
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err := ss.pool.Exec(context.Background(), `
		INSERT INTO image_session_provider_effects (
			id, generation_task_id, candidate_start_index, candidate_count, operation_key, effect_kind,
			request_hash, provider_name, attempt_id, effect_result, reconciliation_state, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'mock', $8, 'applied', 'applied', NOW(), NOW())
	`, clockid.New(), taskID, start, count, opKey, effectKind, hash, clockid.New())
	if err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestExecuteResumesFromCompletedCandidates(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "续跑候选", "size": "1024x1024", "generation_count": 2,
	})
	if _, err := ss.pool.Exec(context.Background(), `
		UPDATE image_session_generation_tasks SET completed_candidates = 1 WHERE id = $1
	`, taskID); err != nil {
		t.Fatal(err)
	}
	prov := &countingProvider{MockChatProvider: MockChatProvider{PromptVersion: "resume-v1"}}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "succeeded" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) != 1 || got.Rounds[0].CandidateIndex != 2 {
		t.Fatalf("rounds %+v", got.Rounds)
	}
}

func TestExecuteSkipsGenerateWhenEffectAlreadyApplied(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "跳过已 applied", "size": "1024x1024", "generation_count": 2,
	})
	insertAppliedEffect(t, ss, taskID, 1, 1)
	prov := &countingProvider{}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1 (candidate 2 only)", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "succeeded" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) != 1 || got.Rounds[0].CandidateIndex != 2 {
		t.Fatalf("rounds %+v", got.Rounds)
	}
	var effect string
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT effect_result FROM image_session_provider_effects
		WHERE generation_task_id = $1 AND candidate_start_index = 1
	`, taskID).Scan(&effect); err != nil {
		t.Fatal(err)
	}
	if effect != "applied" {
		t.Fatalf("candidate 1 effect %s", effect)
	}
}

func TestExecuteFillsChatRequestContextFromSession(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	first := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "一只杯子", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, first, http.StatusAccepted)
	ss.decode(t, first, &session)
	firstID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, firstID)
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{ResponseID: "resp-base", PromptVersion: "live-image-v2", Model: "gpt-image-1"}}).Execute(context.Background(), firstID); err != nil {
		t.Fatal(err)
	}
	session = loadSessionDetail(t, ss, session.ID)
	if session.Rounds[0].PromptVersion != "live-image-v2" {
		t.Fatalf("prompt_version %s", session.Rounds[0].PromptVersion)
	}
	if session.Rounds[0].ModelName != "gpt-image-1" {
		t.Fatalf("model %s", session.Rounds[0].ModelName)
	}
	baseID := session.Rounds[0].GeneratedAsset.ID
	refPNG := pngBytes(t, 8, 6)
	refID := uploadSessionRef(t, ss, session.ID, refPNG)

	branch := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "换成蓝色", "size": "1024x1024", "generation_count": 1,
		"base_asset_id": baseID, "selected_reference_asset_ids": []string{refID},
	})
	ss.mustStatus(t, branch, http.StatusAccepted)
	ss.decode(t, branch, &session)
	var branchID string
	for _, task := range session.GenerationTasks {
		if task.Status == "queued" {
			branchID = task.ID
			break
		}
	}
	if branchID == "" {
		t.Fatal("missing branch task")
	}
	ss.dropDispatch(t, branchID)
	prov := &countingProvider{MockChatProvider: MockChatProvider{ResponseID: "resp-branch"}}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), branchID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("calls %d", prov.calls)
	}
	req := prov.reqs[0]
	wantBase := grayPNG(1024, 1024)
	if !bytes.Equal(req.BaseBytes, wantBase) {
		t.Fatalf("base bytes len %d want %d", len(req.BaseBytes), len(wantBase))
	}
	if len(req.ReferenceBytes) != 1 || !bytes.Equal(req.ReferenceBytes[0], refPNG) {
		t.Fatalf("reference bytes %+v", len(req.ReferenceBytes))
	}
	if req.PreviousResponseID != nil {
		t.Fatalf("previous_response_id %+v", req.PreviousResponseID)
	}
	if req.HistoryBlock != "" {
		t.Fatalf("history %q", req.HistoryBlock)
	}
	session = loadSessionDetail(t, ss, session.ID)
	last := session.Rounds[0]
	if last.PreviousResponseID != nil {
		t.Fatalf("persisted previous_response_id %+v", last.PreviousResponseID)
	}
}

func TestExecuteFailsWhenReferenceMediaCorrupt(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	first := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "一只杯子", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, first, http.StatusAccepted)
	ss.decode(t, first, &session)
	firstID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, firstID)
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{ResponseID: "resp-base"}}).Execute(context.Background(), firstID); err != nil {
		t.Fatal(err)
	}
	session = loadSessionDetail(t, ss, session.ID)
	baseID := session.Rounds[0].GeneratedAsset.ID
	refID := uploadSessionRef(t, ss, session.ID, pngBytes(t, 8, 6))
	corruptSessionMedia(t, ss, refID)

	branch := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "换成蓝色", "size": "1024x1024", "generation_count": 1,
		"base_asset_id": baseID, "selected_reference_asset_ids": []string{refID},
	})
	ss.mustStatus(t, branch, http.StatusAccepted)
	ss.decode(t, branch, &session)
	var branchID string
	for _, task := range session.GenerationTasks {
		if task.Status == "queued" {
			branchID = task.ID
			break
		}
	}
	if branchID == "" {
		t.Fatal("missing branch task")
	}
	ss.dropDispatch(t, branchID)
	prov := &countingProvider{MockChatProvider: MockChatProvider{ResponseID: "resp-branch"}}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), branchID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 0 {
		t.Fatalf("provider must not receive corrupt bytes, calls %d", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	var task TaskResponse
	for _, item := range got.GenerationTasks {
		if item.ID == branchID {
			task = item
			break
		}
	}
	if task.Status != "failed" || task.IsRetryable {
		t.Fatalf("status %+v", task)
	}
	if task.FailureReason == nil || *task.FailureReason != "会话图片文件不可用" {
		t.Fatalf("failure_reason %v", task.FailureReason)
	}
}

func corruptSessionMedia(t *testing.T, ss *sessionServer, assetID string) {
	t.Helper()
	var path string
	var size int
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT mo.storage_path, mo.byte_size
		FROM image_session_assets a
		JOIN media_objects mo ON mo.id = a.media_object_id
		WHERE a.id = $1
	`, assetID).Scan(&path, &size); err != nil {
		t.Fatal(err)
	}
	abs, err := ss.media.Files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteValidationDoesNotAutoRetry(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "拒绝", "size": "1024x1024", "generation_count": 1,
	})
	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: apperr.Validation("图片供应商拒绝了本次请求，请调整提示词、参考图或参数后重试")}}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := loadSessionDetail(t, ss, session.ID)
	task := got.GenerationTasks[0]
	if task.Status != "failed" {
		t.Fatalf("status %s", task.Status)
	}
	if task.IsRetryable {
		t.Fatal("HTTP 400 must not auto-retry")
	}
	if task.Attempts != 1 {
		t.Fatalf("attempts %d", task.Attempts)
	}
	var pending int
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM async_dispatches WHERE aggregate_id = $1 AND status = 'pending'
	`, taskID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending dispatches %d", pending)
	}
}

func TestExecuteTerminalTaskDoesNotBusyRetry(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "终态不再 busy", "size": "1024x1024", "generation_count": 1,
	})
	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "succeeded" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatalf("terminal task must consume, not busy-retry: %v", err)
	}
}

func TestExecuteRateLimitIsFailedRetryable(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "限流", "size": "1024x1024", "generation_count": 1,
	})
	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: ErrRateLimit}}
	for i := 0; i < maxAttempts; i++ {
		if err := exec.Execute(context.Background(), taskID); err != nil {
			t.Fatal(err)
		}
	}
	got := loadSessionDetail(t, ss, session.ID)
	task := got.GenerationTasks[0]
	if task.Status != "failed" {
		t.Fatalf("status %s", task.Status)
	}
	if !task.IsRetryable {
		t.Fatal("429 must stay retryable")
	}
	if task.Attempts != maxAttempts {
		t.Fatalf("attempts %d", task.Attempts)
	}
	if len(task.ProviderEffects) == 0 || task.ProviderEffects[0].EffectResult != "failed" {
		t.Fatalf("effects %+v", task.ProviderEffects)
	}
}

func TestExecuteProvider5xxIsUnknown(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "供应商 5xx", "size": "1024x1024", "generation_count": 1,
	})
	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: ErrProvider5xx}}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := loadSessionDetail(t, ss, session.ID)
	task := got.GenerationTasks[0]
	if task.Status != "unknown" {
		t.Fatalf("status %s", task.Status)
	}
	if task.IsRetryable {
		t.Fatal("5xx must stay unknown without auto-retry")
	}
	if task.Attempts != 1 {
		t.Fatalf("attempts %d", task.Attempts)
	}
	if len(task.ProviderEffects) == 0 || task.ProviderEffects[0].EffectResult != "unknown" {
		t.Fatalf("effects %+v", task.ProviderEffects)
	}
}

func TestExecuteTimeoutIsUnknown(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "供应商超时", "size": "1024x1024", "generation_count": 1,
	})
	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: ErrTimeout}}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := loadSessionDetail(t, ss, session.ID)
	task := got.GenerationTasks[0]
	if task.Status != "unknown" || task.IsRetryable {
		t.Fatalf("timeout %+v", task)
	}
}

type contextCancelProvider struct {
	cancel context.CancelFunc
}

func (p contextCancelProvider) Name() string { return "mock" }

func (p contextCancelProvider) Generate(ctx context.Context, req ChatRequest) (ChatResult, error) {
	p.cancel()
	<-ctx.Done()
	return ChatResult{}, ctx.Err()
}

func TestExecuteCanceledContextMarksUnknown(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "asynq 取消", "size": "1024x1024", "generation_count": 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	exec := Executor{DB: ss.db, Media: ss.media, Provider: contextCancelProvider{cancel: cancel}}
	if err := exec.Execute(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	got := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
	if got.Status != "unknown" || got.IsRetryable {
		t.Fatalf("canceled execute %+v", got)
	}
	if len(got.ProviderEffects) == 0 || got.ProviderEffects[0].EffectResult != "unknown" {
		t.Fatalf("effects %+v", got.ProviderEffects)
	}
}

func TestIsUncertainProviderFailureIncludesCanceledContext(t *testing.T) {
	if !IsUncertainProviderFailure(context.Canceled) || !IsUncertainProviderFailure(context.DeadlineExceeded) {
		t.Fatal("canceled/deadline must be unknown")
	}
	if IsRetryableProviderFailure(context.Canceled) {
		t.Fatal("canceled context must not auto-retry as failed")
	}
}

func TestExecutePersistsImagesBatchCandidateCount(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "批量候选", "size": "1024x1024", "generation_count": 3,
	})
	prov := &countingProvider{MockChatProvider: MockChatProvider{ProviderName: "openai-images"}}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1", prov.calls)
	}
	if len(prov.reqs) != 1 || prov.reqs[0].Count != 3 {
		t.Fatalf("request count %+v", prov.reqs)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "succeeded" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) != 3 {
		t.Fatalf("rounds %d", len(got.Rounds))
	}
	var effectCount int
	var requestJSON []byte
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT candidate_count, request_json FROM image_session_provider_effects
		WHERE generation_task_id = $1
	`, taskID).Scan(&effectCount, &requestJSON); err != nil {
		t.Fatal(err)
	}
	if effectCount != 3 {
		t.Fatalf("effect candidate_count %d", effectCount)
	}
	var effectResult, recon string
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT effect_result, reconciliation_state FROM image_session_provider_effects
		WHERE generation_task_id = $1 AND candidate_start_index = 1
	`, taskID).Scan(&effectResult, &recon); err != nil {
		t.Fatal(err)
	}
	if effectResult != "applied" || recon != "applied" {
		t.Fatalf("effect %s recon %s", effectResult, recon)
	}
	var req map[string]any
	if err := json.Unmarshal(requestJSON, &req); err != nil {
		t.Fatal(err)
	}
	if req["candidate_count"] != float64(3) {
		t.Fatalf("request_json %+v", req)
	}
	if req["provider"] != "openai-images" {
		t.Fatalf("provider %+v", req["provider"])
	}
}

func TestIsNonRetryableGenerationError(t *testing.T) {
	if !isNonRetryableGenerationError(apperr.Validation("bad")) {
		t.Fatal("validation")
	}
	if !isNonRetryableGenerationError(apperr.Error{Status: 400, Detail: "http 400"}) {
		t.Fatal("http 400")
	}
	if !isNonRetryableGenerationError(ErrTextOutput) {
		t.Fatal("text output")
	}
	if isNonRetryableGenerationError(ErrMissingOutput) {
		t.Fatal("missing output must stay unknown, not confirmed failure")
	}
	if isNonRetryableGenerationError(errors.New("provider crashed")) {
		t.Fatal("unknown crash should retry/unknown path, not this helper")
	}
	if isNonRetryableGenerationError(ErrRateLimit) {
		t.Fatal("rate limit")
	}
	if isNonRetryableGenerationError(ErrTimeout) {
		t.Fatal("timeout")
	}
	if isNonRetryableGenerationError(ErrConnection) {
		t.Fatal("connection")
	}
	if isNonRetryableGenerationError(ErrProvider5xx) {
		t.Fatal("provider 5xx")
	}
}

type scriptedImagesProvider struct {
	name   string
	images [][]byte
	calls  int
}

func (p *scriptedImagesProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "openai-images"
}

func (p *scriptedImagesProvider) Generate(ctx context.Context, req ChatRequest) (ChatResult, error) {
	p.calls++
	out := ChatResult{
		Images:         p.images,
		MIME:           "image/png",
		Model:          "mock-image",
		PromptVersion:  "mock-image-v1",
		ProviderStatus: "completed",
		OutputJSON:     map[string]any{"status": "completed"},
	}
	if len(p.images) > 0 {
		out.Bytes = p.images[0]
	}
	return out, nil
}

func loadEffectAt(t *testing.T, ss *sessionServer, taskID string, start int) (result string, count int, recon string) {
	t.Helper()
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT effect_result, candidate_count, reconciliation_state
		FROM image_session_provider_effects
		WHERE generation_task_id = $1 AND candidate_start_index = $2
	`, taskID, start).Scan(&result, &count, &recon); err != nil {
		t.Fatal(err)
	}
	return result, count, recon
}

func TestExecuteEmptyImagesBatchIsUnknown(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "空批次", "size": "1024x1024", "generation_count": 3,
	})
	prov := &scriptedImagesProvider{name: "openai-images"}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "unknown" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) != 0 {
		t.Fatalf("rounds %d", len(got.Rounds))
	}
	result, _, _ := loadEffectAt(t, ss, taskID, 1)
	if result != "unknown" {
		t.Fatalf("effect %s", result)
	}
}

func TestExecuteFewerImagesThanBatchIsUnknown(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "少返回", "size": "1024x1024", "generation_count": 3,
	})
	png := grayPNG(32, 32)
	prov := &scriptedImagesProvider{name: "openai-images", images: [][]byte{png}}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "unknown" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) != 0 {
		t.Fatalf("rounds %d want 0", len(got.Rounds))
	}
	result, _, _ := loadEffectAt(t, ss, taskID, 1)
	if result != "unknown" {
		t.Fatalf("effect %s", result)
	}
}

func TestExecuteMoreImagesThanBatchIsUnknown(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "多返回", "size": "1024x1024", "generation_count": 3,
	})
	png := grayPNG(32, 32)
	prov := &scriptedImagesProvider{name: "openai-images", images: [][]byte{png, png, png, png}}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "unknown" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) != 0 {
		t.Fatalf("rounds %d want 0", len(got.Rounds))
	}
	result, _, _ := loadEffectAt(t, ss, taskID, 1)
	if result != "unknown" {
		t.Fatalf("effect %s", result)
	}
}

func TestExecuteSecondBatchMaterializeFailureIsUnknown(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "第二张落库失败", "size": "1024x1024", "generation_count": 2,
	})
	png := grayPNG(32, 32)
	prov := &scriptedImagesProvider{
		name:   "openai-images",
		images: [][]byte{png, []byte("not-an-image")},
	}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "unknown" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) > 1 {
		t.Fatalf("rounds %d want at most 1", len(got.Rounds))
	}
	result, _, recon := loadEffectAt(t, ss, taskID, 1)
	if result == "applied" || recon == "applied" {
		t.Fatalf("effect %s recon %s must not be applied", result, recon)
	}
	if result != "unknown" {
		t.Fatalf("effect %s", result)
	}
}

func TestExecuteSkipsAppliedImagesBatchByCandidateCount(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "跳过整批 applied", "size": "1024x1024", "generation_count": 3,
	})
	insertAppliedEffect(t, ss, taskID, 1, 3)
	prov := &countingProvider{MockChatProvider: MockChatProvider{ProviderName: "openai-images"}}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 0 {
		t.Fatalf("generate calls %d want 0", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "succeeded" {
		t.Fatalf("status %s", got.GenerationTasks[0].Status)
	}
	if len(got.Rounds) != 0 {
		t.Fatalf("rounds %d", len(got.Rounds))
	}
}

func oversizedPNG(t *testing.T, extra int) []byte {
	t.Helper()
	base := grayPNG(8, 8)
	out := make([]byte, len(base)+extra)
	copy(out, base)
	return out
}

func TestExecuteRejectsOversizedInputBeforeGenerate(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	padded := oversizedPNG(t, media.GenerationMaxBatchBytes+1-len(grayPNG(8, 8)))
	detail, err := ss.svc.AddReferences(context.Background(), session.ID, []product.Upload{{
		Content: padded, Filename: "big.png", MIMEType: "image/png",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var refID string
	for _, asset := range detail.Assets {
		if asset.Kind == kindReference {
			refID = asset.ID
			break
		}
	}
	if refID == "" {
		t.Fatal("missing reference")
	}

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "超限输入", "size": "1024x1024", "generation_count": 1,
		"selected_reference_asset_ids": []string{refID},
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	taskID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, taskID)

	prov := &countingProvider{}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 0 {
		t.Fatalf("generate calls %d want 0", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "failed" {
		t.Fatalf("status %s reason %+v", got.GenerationTasks[0].Status, got.GenerationTasks[0].FailureReason)
	}
	if len(got.Rounds) != 0 {
		t.Fatalf("rounds %d want 0", len(got.Rounds))
	}
}

func TestExecuteRejectsOversizedBatchBeforeSaveCandidate(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "第二张超限", "size": "1024x1024", "generation_count": 2,
	})
	png := grayPNG(32, 32)
	prov := &scriptedImagesProvider{
		name:   "openai-images",
		images: [][]byte{png, make([]byte, media.GenerationMaxImageBytes+1)},
	}
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: prov}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("generate calls %d want 1", prov.calls)
	}
	got := loadSessionDetail(t, ss, session.ID)
	if got.GenerationTasks[0].Status != "failed" {
		t.Fatalf("status %s reason %+v", got.GenerationTasks[0].Status, got.GenerationTasks[0].FailureReason)
	}
	if len(got.Rounds) != 0 {
		t.Fatalf("rounds %d want 0", len(got.Rounds))
	}
	result, _, recon := loadEffectAt(t, ss, taskID, 1)
	if result != "failed" || recon == "applied" {
		t.Fatalf("effect %s recon %s", result, recon)
	}
}
