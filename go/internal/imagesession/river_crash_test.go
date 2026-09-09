package imagesession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	imageSessionCrashBeforeCall  = "before_call"
	imageSessionCrashAfterEffect = "after_effect"
	imageSessionCrashAfterResult = "after_result"
)

// crashHTTPProvider makes the provider boundary observable without calling a real model.
// The control endpoint is only a parent/child gate; /generate is the recorded external effect.
type crashHTTPProvider struct {
	baseURL string
	mode    string
	client  *http.Client
}

func (p crashHTTPProvider) Name() string { return "crash-http" }

func (p crashHTTPProvider) Generate(ctx context.Context, req ChatRequest) (ChatResult, error) {
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	if p.mode == imageSessionCrashBeforeCall {
		response, err := p.post(ctx, client, "/control/before-call", map[string]any{
			"prompt": req.Prompt,
		})
		if err != nil {
			return ChatResult{}, err
		}
		if err := closeCrashResponse(response); err != nil {
			return ChatResult{}, err
		}
	}
	response, err := p.post(ctx, client, "/generate", map[string]any{
		"prompt": req.Prompt,
		"size":   req.Size,
		"count":  req.Count,
	})
	if err != nil {
		return ChatResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return ChatResult{}, fmt.Errorf("crash provider status %d: %s", response.StatusCode, bytes.TrimSpace(body))
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return ChatResult{}, err
	}
	if len(data) == 0 {
		return ChatResult{}, errors.New("crash provider returned empty image")
	}
	count := req.Count
	if count < 1 {
		count = 1
	}
	images := make([][]byte, count)
	for i := range images {
		images[i] = data
	}
	return ChatResult{
		Bytes:          data,
		Images:         images,
		MIME:           "image/png",
		Model:          "crash-http-model",
		PromptVersion:  "crash-http-v1",
		ResponseID:     "crash-http-response",
		ProviderStatus: "completed",
		OutputJSON:     map[string]any{"status": "completed"},
	}, nil
}

func (p crashHTTPProvider) post(ctx context.Context, client *http.Client, path string, payload map[string]any) (*http.Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	return client.Do(request)
}

func closeCrashResponse(response *http.Response) error {
	defer response.Body.Close()
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	return fmt.Errorf("crash provider control status %d: %s", response.StatusCode, bytes.TrimSpace(body))
}

type crashHTTPGate struct {
	mode          string
	entered       chan struct{}
	release       chan struct{}
	enteredOne    sync.Once
	releaseOne    sync.Once
	beforeCall    atomic.Int32
	providerCalls atomic.Int32
	server        *httptest.Server
}

func newCrashHTTPGate(t *testing.T, mode string) *crashHTTPGate {
	t.Helper()
	gate := &crashHTTPGate{
		mode:    mode,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	gate.server = httptest.NewServer(http.HandlerFunc(gate.serveHTTP))
	t.Cleanup(gate.server.Close)
	return gate
}

func (g *crashHTTPGate) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	switch r.URL.Path {
	case "/control/before-call":
		n := g.beforeCall.Add(1)
		g.enteredOne.Do(func() { close(g.entered) })
		if n == 1 {
			select {
			case <-g.release:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case "/generate":
		n := g.providerCalls.Add(1)
		g.enteredOne.Do(func() { close(g.entered) })
		// In before_call the first Generate invocation never reaches this endpoint.
		// Any unexpected later invocation must finish so the recovery worker cannot hang.
		if n == 1 && g.mode != imageSessionCrashBeforeCall {
			select {
			case <-g.release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(grayPNG(32, 32))
	default:
		http.NotFound(w, r)
	}
}

func (g *crashHTTPGate) unblock() {
	g.releaseOne.Do(func() { close(g.release) })
}

// TestImageSessionRiverCrashChild is run in a separate process so the provider
// call cannot return a normal Go error before the SIGKILL boundary is observed.
func TestImageSessionRiverCrashChild(t *testing.T) {
	databaseURL := os.Getenv("PF_IMAGESESSION_CRASH_DB")
	if databaseURL == "" {
		return
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db, err := pfdb.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	provider := crashHTTPProvider{
		baseURL: os.Getenv("PF_IMAGESESSION_CRASH_PROVIDER"),
		mode:    os.Getenv("PF_IMAGESESSION_CRASH_MODE"),
	}
	executor := Executor{
		DB:       db,
		Media:    media.Store{Files: storage.Local{Root: os.Getenv("PF_IMAGESESSION_CRASH_ROOT")}},
		Provider: provider,
	}
	worker, err := queue.NewClient(pool, map[string]queue.ActorFunc{
		queue.ActorImageSession: executor.Execute,
	}, queue.WorkerConfig{
		GenerationWorkers: 1,
		JobTimeout:        5 * time.Second,
		RescueAfter:       6 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestImageSessionRiverCrashBoundaries(t *testing.T) {
	for _, mode := range []string{
		imageSessionCrashBeforeCall,
		imageSessionCrashAfterEffect,
		imageSessionCrashAfterResult,
	} {
		t.Run(mode, func(t *testing.T) {
			runImageSessionRiverCrashBoundary(t, mode)
		})
	}
}

func runImageSessionRiverCrashBoundary(t *testing.T, mode string) {
	t.Helper()
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_img_crash_%s_%d", mode, time.Now().UnixNano()))
	ss := newSessionServerWithDatabase(t, pool, db)
	gate := newCrashHTTPGate(t, mode)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "river crash boundary " + mode,
		"size":   "1024x1024",
	})
	job := stageImageSessionJob(t, db, taskID)

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "-test.run=^TestImageSessionRiverCrashChild$", "-test.timeout=5m")
	child.Env = append(os.Environ(),
		"PF_IMAGESESSION_CRASH_DB="+pool.Config().ConnString(),
		"PF_IMAGESESSION_CRASH_ROOT="+ss.root,
		"PF_IMAGESESSION_CRASH_PROVIDER="+gate.server.URL,
		"PF_IMAGESESSION_CRASH_MODE="+mode,
	)
	var childOutput bytes.Buffer
	child.Stdout = &childOutput
	child.Stderr = &childOutput
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if waited {
			return
		}
		_ = child.Process.Signal(syscall.SIGKILL)
		_ = child.Wait()
	})

	select {
	case <-gate.entered:
	case <-time.After(20 * time.Second):
		t.Fatalf("child did not reach crash boundary: %s", childOutput.String())
	}
	waitCrashCondition(t, "pending provider effect", 10*time.Second, func() (bool, error) {
		var count int64
		err := db.Model(&schema.ImageSessionProviderEffects{}).
			Where("generation_task_id = ? AND candidate_start_index = ? AND effect_result = ?", taskID, 1, "pending").
			Count(&count).Error
		return count == 1, err
	})

	var effectLock *gormDBTransaction
	if mode == imageSessionCrashAfterResult {
		effectLock = lockCrashEffect(t, db, taskID)
		gate.unblock()
		waitCrashCondition(t, "saved round", 20*time.Second, func() (bool, error) {
			var count int64
			err := db.Model(&schema.ImageSessionRounds{}).Where("session_id = ?", session.ID).Count(&count).Error
			return count == 1, err
		})
	}

	if err := child.Process.Signal(syscall.SIGKILL); err != nil {
		waited = true
		_ = child.Wait()
		t.Fatalf("send SIGKILL: %v output=%s", err, childOutput.String())
	}
	waited = true
	if err := child.Wait(); err == nil {
		t.Fatal("crash child exited normally")
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("child wait: %v output=%s", err, childOutput.String())
		}
		status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("child was not SIGKILLed: %v output=%s", err, childOutput.String())
		}
	}
	if effectLock != nil {
		effectLock.rollback(t)
	}
	callsAtKill := int(gate.providerCalls.Load())

	provider := crashHTTPProvider{baseURL: gate.server.URL, mode: mode}
	executor := Executor{DB: db, Media: ss.media, Provider: provider}
	consumer, err := queue.NewClient(pool, map[string]queue.ActorFunc{
		queue.ActorImageSession: executor.Execute,
	}, queue.WorkerConfig{
		GenerationWorkers: 1,
		JobTimeout:        5 * time.Second,
		RescueAfter:       6 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := consumer.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	}()

	// River's maintenance rescuer scans on its own cadence; RescueAfter is the
	// eligibility threshold, not the upper bound for the first scan.
	waitCrashCondition(t, "rescued River job", 50*time.Second, func() (bool, error) {
		var row struct {
			Attempt int
			State   string
		}
		err := db.Table("river_job").Select("attempt, state").Where("id = ?", job.ID).Take(&row).Error
		return row.Attempt >= 2, err
	})

	wantStatus := "unknown"
	if mode == imageSessionCrashAfterResult {
		wantStatus = "succeeded"
	}
	waitCrashCondition(t, "business recovery", 90*time.Second, func() (bool, error) {
		if _, err := RecoverUnfinished(context.Background(), pool, time.Minute); err != nil {
			return false, err
		}
		var task schema.ImageSessionGenerationTasks
		if err := db.Where("id = ?", taskID).Take(&task).Error; err != nil {
			return false, err
		}
		return task.Status == wantStatus, nil
	})

	waitCrashCondition(t, "completed River job", 15*time.Second, func() (bool, error) {
		var state string
		err := db.Table("river_job").Select("state").Where("id = ?", job.ID).Scan(&state).Error
		return state == "completed", err
	})

	assertImageSessionCrashOutcome(t, db, ss, session.ID, taskID, auth.MustDevMerchantID(t, db), mode, callsAtKill, gate)
}

// gormDBTransaction keeps the row lock alive until the child is dead.
type gormDBTransaction struct {
	db *gorm.DB
}

func lockCrashEffect(t *testing.T, db *gorm.DB, taskID string) *gormDBTransaction {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	var effect schema.ImageSessionProviderEffects
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("generation_task_id = ? AND candidate_start_index = ?", taskID, 1).Take(&effect).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatal(err)
	}
	return &gormDBTransaction{db: tx}
}

func (tx *gormDBTransaction) rollback(t *testing.T) {
	t.Helper()
	if tx == nil || tx.db == nil {
		return
	}
	if err := tx.db.Rollback().Error; err != nil {
		t.Error(err)
	}
	tx.db = nil
}

func assertImageSessionCrashOutcome(t *testing.T, db *gorm.DB, ss *sessionServer, sessionID, taskID, merchantID, mode string, callsAtKill int, gate *crashHTTPGate) {
	t.Helper()
	var task schema.ImageSessionGenerationTasks
	if err := db.Where("id = ?", taskID).Take(&task).Error; err != nil {
		t.Fatal(err)
	}
	var effect schema.ImageSessionProviderEffects
	if err := db.Where("generation_task_id = ? AND candidate_start_index = ?", taskID, 1).Take(&effect).Error; err != nil {
		t.Fatal(err)
	}
	var rounds, assets int64
	if err := db.Model(&schema.ImageSessionRounds{}).Where("session_id = ?", sessionID).Count(&rounds).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schema.ImageSessionAssets{}).Where("session_id = ? AND kind = ?", sessionID, kindGenerated).Count(&assets).Error; err != nil {
		t.Fatal(err)
	}
	merchantKey := generationQuotaKey(taskID, task.BillingSeq)
	hold := loadQuotaHold(t, db, merchantID, merchantKey)

	wantStatus, wantEffect, wantReconciliation := "unknown", "unknown", "unknown"
	wantRounds, wantAssets := int64(0), int64(0)
	wantHold, wantEvent := quota.StatusPendingReconciliation, quota.EventMarkUnknown
	if mode == imageSessionCrashAfterResult {
		wantStatus, wantEffect, wantReconciliation = "succeeded", "applied", "applied"
		wantRounds, wantAssets = 1, 1
		wantHold, wantEvent = quota.StatusSettled, quota.EventSettle
	}
	if task.Status != wantStatus || task.IsRetryable || task.ActiveAttemptID != nil {
		t.Fatalf("mode=%s task status=%s retryable=%t active_attempt=%v", mode, task.Status, task.IsRetryable, task.ActiveAttemptID)
	}
	if effect.EffectResult != wantEffect || effect.ReconciliationState != wantReconciliation {
		t.Fatalf("mode=%s effect=%s reconciliation=%s", mode, effect.EffectResult, effect.ReconciliationState)
	}
	if rounds != wantRounds || assets != wantAssets {
		t.Fatalf("mode=%s rounds=%d assets=%d", mode, rounds, assets)
	}
	if hold.Status != wantHold {
		t.Fatalf("mode=%s quota hold=%s want=%s", mode, hold.Status, wantHold)
	}
	if got := countQuotaEvents(t, db, merchantID, wantEvent, merchantKey); got != 1 {
		t.Fatalf("mode=%s quota event %s count=%d", mode, wantEvent, got)
	}
	if got := gate.providerCalls.Load(); int(got) != callsAtKill {
		t.Fatalf("mode=%s provider calls changed after recovery: kill=%d final=%d", mode, callsAtKill, got)
	}
	if mode == imageSessionCrashBeforeCall && callsAtKill != 0 {
		t.Fatalf("before provider boundary reached /generate calls=%d", callsAtKill)
	}
	if mode != imageSessionCrashBeforeCall && callsAtKill != 1 {
		t.Fatalf("mode=%s expected one external provider effect, got %d", mode, callsAtKill)
	}
	if mode == imageSessionCrashAfterResult {
		var asset schema.ImageSessionAssets
		if err := db.Where("session_id = ? AND kind = ?", sessionID, kindGenerated).Take(&asset).Error; err != nil {
			t.Fatal(err)
		}
		var object schema.MediaObjects
		if err := db.Where("id = ?", asset.MediaObjectID).Take(&object).Error; err != nil {
			t.Fatal(err)
		}
		path, err := ss.media.Files.Resolve(object.StoragePath)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			t.Fatalf("saved provider image path=%s bytes=%d err=%v", path, len(data), err)
		}
	}
}

func waitCrashCondition(t *testing.T, label string, timeout time.Duration, condition func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, err := condition()
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s condition not reached within %s", label, timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
