package imagesession

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

const (
	activeStatusRawMarker     = "raw-request-must-not-leak"
	activeStatusNoteBytes     = 512
	activeStatusLogDir        = "/tmp/productflow-perf-imagesession-active-status-0905"
	activeStatusPayloadBudget = 1 << 20
)

func TestImageSessionStatusActiveSetHTTP(t *testing.T) {
	ss := newSessionServer(t)
	var queries, effectReads, taskReads atomic.Int64
	callback := "test:active_status_http_queries"
	if err := ss.db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.DryRun {
			return
		}
		queries.Add(1)
		switch db.Statement.Dest.(type) {
		case *[]schema.ImageSessionGenerationTasks:
			taskReads.Add(1)
		case *[]schema.ImageSessionProviderEffects:
			effectReads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.db.Callback().Query().Remove(callback) })

	prompt := strings.Repeat(httpLoadPrompt, 40)
	queryCounts := map[int]int{}
	for _, n := range []int{26, 100, 300} {
		sessionID := seedActiveStatusSession(t, ss.db, n, prompt)
		queries.Store(0)
		effectReads.Store(0)
		taskReads.Store(0)
		raw, elapsed := readImageSessionStatusHTTP(t, ss, sessionID)
		assertActiveStatusPayload(t, raw, n, prompt)
		queryCounts[n] = int(queries.Load())
		t.Logf("STATUS_ACTIVE_HTTP tasks=%d effects=%d payload_bytes=%d elapsed=%s queries=%d task_list_reads=%d effect_reads=%d prompt_bytes=%d note_bytes=%d",
			n, n, len(raw), elapsed, queries.Load(), taskReads.Load(), effectReads.Load(), len(prompt), activeStatusNoteBytes)
		if taskReads.Load() != 1 || effectReads.Load() != 1 {
			t.Fatalf("status query amplification: task_list_reads=%d effect_reads=%d", taskReads.Load(), effectReads.Load())
		}
	}
	if queryCounts[300] > queryCounts[26]+2 {
		t.Fatalf("status queries grew with active set: 26=%d 100=%d 300=%d", queryCounts[26], queryCounts[100], queryCounts[300])
	}
}

func TestImageSessionStatusActiveSetScale(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_IMAGE_SESSION_ACTIVE_STATUS") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_IMAGE_SESSION_ACTIVE_STATUS=1 to run the isolated active-status gate")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("active-status gate requires DATABASE_URL")
	}
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_iactive_%d", time.Now().UnixNano()))
	ss := newSessionServerWithDatabase(t, pool, gdb)
	ss.client.Timeout = 15 * time.Second
	prompt := strings.Repeat(httpLoadPrompt, 40)
	if err := os.MkdirAll(activeStatusLogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(activeStatusLogDir, "results.jsonl")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	var queries, effectReads, taskReads atomic.Int64
	callback := "test:active_status_scale_queries"
	if err := gdb.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.DryRun {
			return
		}
		queries.Add(1)
		switch db.Statement.Dest.(type) {
		case *[]schema.ImageSessionGenerationTasks:
			taskReads.Add(1)
		case *[]schema.ImageSessionProviderEffects:
			effectReads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gdb.Callback().Query().Remove(callback) })

	writeLog := func(row map[string]any) {
		t.Helper()
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := logFile.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}

	queryCounts := map[int]int{}
	for _, n := range []int{26, 100, 300} {
		sessionID := seedActiveStatusSession(t, gdb, n, prompt)
		warmup, measured := 0, 1
		if n == 300 {
			warmup, measured = 10, 100
		}
		durations := make([]time.Duration, 0, measured)
		maxBytes := 0
		var lastRaw []byte
		for i := 0; i < warmup+measured; i++ {
			if i == warmup {
				queries.Store(0)
				effectReads.Store(0)
				taskReads.Store(0)
			}
			raw, elapsed := readImageSessionStatusHTTP(t, ss, sessionID)
			assertActiveStatusPayload(t, raw, n, prompt)
			if i >= warmup {
				durations = append(durations, elapsed)
				maxBytes = max(maxBytes, len(raw))
				lastRaw = raw
			}
		}
		slices.Sort(durations)
		queries.Store(0)
		effectReads.Store(0)
		taskReads.Store(0)
		probe, _ := readImageSessionStatusHTTP(t, ss, sessionID)
		assertActiveStatusPayload(t, probe, n, prompt)
		queryCounts[n] = int(queries.Load())
		p50, p95 := "", ""
		if len(durations) == 100 {
			p50, p95 = durations[49].String(), durations[94].String()
		}
		t.Logf("STATUS_ACTIVE_SCALE tasks=%d effects=%d samples=%d warmup=%d p50=%s p95=%s max_payload_bytes=%d queries_per_request=%d task_list_reads=%d effect_reads=%d prompt_bytes=%d note_bytes=%d",
			n, n, len(durations), warmup, p50, p95, maxBytes, queries.Load(), taskReads.Load(), effectReads.Load(), len(prompt), activeStatusNoteBytes)
		writeLog(map[string]any{
			"route": "status", "tasks": n, "effects": n, "samples": len(durations),
			"p50": p50, "p95": p95, "max_payload_bytes": maxBytes,
			"queries_per_request": queries.Load(), "task_list_reads": taskReads.Load(), "effect_reads": effectReads.Load(),
			"prompt_bytes": len(prompt), "note_bytes": activeStatusNoteBytes,
			"response_has_active": true, "transport": "loopback_http",
		})
		if lastRaw == nil {
			t.Fatal("missing measured status payload")
		}
		if n == 300 {
			sseBytes, sseTasks := readImageSessionStatusSSESnapshot(t, ss, sessionID)
			if sseTasks != n {
				t.Fatalf("SSE snapshot truncated active tasks: %d", sseTasks)
			}
			t.Logf("STATUS_ACTIVE_SSE tasks=%d snapshot_data_bytes=%d", n, sseBytes)
			writeLog(map[string]any{
				"route": "sse_snapshot", "tasks": n, "effects": n, "snapshot_data_bytes": sseBytes,
				"prompt_bytes": len(prompt), "note_bytes": activeStatusNoteBytes,
			})
		}
	}
	if queryCounts[300] > queryCounts[26]+2 {
		t.Fatalf("status queries grew with active set: 26=%d 100=%d 300=%d", queryCounts[26], queryCounts[100], queryCounts[300])
	}
}

func seedActiveStatusSession(t *testing.T, gdb *gorm.DB, n int, prompt string) string {
	t.Helper()
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	merchantID := auth.MustDevMerchantID(t, gdb)
	session := schema.ImageSessions{ID: clockid.New(), MerchantID: merchantID, Title: fmt.Sprintf("active status %d", n), CreatedAt: now, UpdatedAt: now}
	if err := gdb.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	metadata := `{"note":"` + strings.Repeat("m", activeStatusNoteBytes) + `"}`
	raw := `{"` + activeStatusRawMarker + `":"` + strings.Repeat("x", 64*1024) + `"}`
	responseID, providerStatus := "fixture-response", "in_progress"
	tasks := make([]schema.ImageSessionGenerationTasks, 0, n)
	effects := make([]schema.ImageSessionProviderEffects, 0, n)
	for i := 0; i < n; i++ {
		task := schema.ImageSessionGenerationTasks{
			ID: clockid.New(), SessionID: session.ID, Status: "queued", Prompt: prompt,
			Size: "1024x1024", GenerationCount: 1, ProgressMetadata: &metadata,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		if i == 0 {
			task.Status = "running"
			attempt := clockid.New()
			task.ActiveAttemptID, task.StartedAt = &attempt, &now
		}
		tasks = append(tasks, task)
		effects = append(effects, schema.ImageSessionProviderEffects{
			ID: clockid.New(), GenerationTaskID: task.ID, CandidateStartIndex: 1, CandidateCount: 1,
			OperationKey: clockid.New(), EffectKind: "image_session_generation", RequestHash: strings.Repeat("a", 64),
			ProviderName: "fixture", AttemptID: clockid.New(), EffectResult: "unknown", ReconciliationState: "not_requested",
			ProviderResponseID: &responseID, ProviderStatus: &providerStatus,
			RequestJSON: &raw, ResultJSON: &raw, Detail: &task.ID, CreatedAt: now, UpdatedAt: now,
		})
	}
	t.Cleanup(func() {
		if err := gdb.Where("generation_task_id IN (?)", gdb.Model(&schema.ImageSessionGenerationTasks{}).Select("id").Where("session_id = ?", session.ID)).
			Delete(&schema.ImageSessionProviderEffects{}).Error; err != nil {
			t.Error(err)
		}
		if err := gdb.Where("session_id = ?", session.ID).Delete(&schema.ImageSessionGenerationTasks{}).Error; err != nil {
			t.Error(err)
		}
		if err := gdb.Where("id = ?", session.ID).Delete(&schema.ImageSessions{}).Error; err != nil {
			t.Error(err)
		}
	})
	if err := gdb.CreateInBatches(tasks, 50).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.CreateInBatches(effects, 50).Error; err != nil {
		t.Fatal(err)
	}
	return session.ID
}

func readImageSessionStatusHTTP(t *testing.T, ss *sessionServer, sessionID string) ([]byte, time.Duration) {
	t.Helper()
	start := time.Now()
	resp := ss.do(t, http.MethodGet, "/api/image-sessions/"+sessionID+"/status", nil, "")
	raw, err := io.ReadAll(io.LimitReader(resp.Body, activeStatusPayloadBudget+1))
	resp.Body.Close()
	elapsed := time.Since(start)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status status=%d read=%v body=%.200s", resp.StatusCode, err, raw)
	}
	return raw, elapsed
}

func assertActiveStatusPayload(t *testing.T, raw []byte, wantTasks int, prompt string) {
	t.Helper()
	if len(raw) >= activeStatusPayloadBudget {
		t.Fatalf("status payload=%d exceeds fixture budget <1MiB", len(raw))
	}
	if bytes.Contains(raw, []byte(activeStatusRawMarker)) {
		t.Fatal("status leaked provider request/result JSON")
	}
	if bytes.Contains(raw, []byte(prompt)) || bytes.Contains(raw, []byte(`"prompt":`)) {
		t.Fatal("status repeated full prompts on the wire")
	}
	var status StatusResponse
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	if !status.HasActiveGenerationTask || len(status.GenerationTasks) != wantTasks {
		t.Fatalf("status truncated active set: has_active=%t tasks=%d want=%d", status.HasActiveGenerationTask, len(status.GenerationTasks), wantTasks)
	}
	seen := map[string]bool{}
	queued, running := 0, 0
	for _, task := range status.GenerationTasks {
		if seen[task.ID] || task.SessionID == "" {
			t.Fatalf("duplicate or incomplete task: %+v", task)
		}
		seen[task.ID] = true
		if task.Size != "1024x1024" || task.GenerationCount != 1 {
			t.Fatalf("task lost generation fields: %s", task.ID)
		}
		if task.Prompt != "" {
			t.Fatalf("status repeated prompt for %s", task.ID)
		}
		if note, _ := task.ProgressMetadata["note"].(string); note != strings.Repeat("m", activeStatusNoteBytes) {
			t.Fatalf("task lost progress metadata: %s", task.ID)
		}
		if len(task.ProviderEffects) != 1 || task.ProviderEffects[0].GenerationTaskID != task.ID ||
			task.ProviderEffects[0].EffectResult != "unknown" || task.ProviderEffects[0].RequestHash != strings.Repeat("a", 64) ||
			task.ProviderEffects[0].Detail == nil || *task.ProviderEffects[0].Detail != task.ID {
			t.Fatalf("effect projection changed for %s: %+v", task.ID, task.ProviderEffects)
		}
		switch task.Status {
		case "queued":
			queued++
			if task.QueuePosition == nil || *task.QueuePosition < 1 || !task.IsCancelable {
				t.Fatalf("queued task lost queue fields: %s", task.ID)
			}
		case "running":
			running++
			if !task.IsCancelable {
				t.Fatalf("running task not cancelable: %s", task.ID)
			}
		default:
			t.Fatalf("status returned non-active task %s status=%s", task.ID, task.Status)
		}
	}
	if running != 1 || queued != wantTasks-1 {
		t.Fatalf("active mix running=%d queued=%d want 1/%d", running, queued, wantTasks-1)
	}
}

func readImageSessionStatusSSESnapshot(t *testing.T, ss *sessionServer, sessionID string) (int, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ss.srv.URL+"/api/image-sessions/"+sessionID+"/events", nil)
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
		t.Fatalf("SSE status=%d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := []byte(strings.TrimPrefix(line, "data: "))
		assertActiveStatusPayload(t, payload, 300, strings.Repeat(httpLoadPrompt, 40))
		var status StatusResponse
		if err := json.Unmarshal(payload, &status); err != nil {
			t.Fatal(err)
		}
		cancel()
		return len(payload), len(status.GenerationTasks)
	}
	if err := scanner.Err(); err != nil && err != context.Canceled {
		t.Fatal(err)
	}
	t.Fatal("SSE snapshot missing")
	return 0, 0
}
