package imagesession

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

const httpLoadPrompt = "Preserve product identity, shape, material and label. "

func TestImageSessionHTTPTargetScale(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_IMAGE_SESSION_HTTP_LOAD") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_IMAGE_SESSION_HTTP_LOAD=1 to run the target-scale HTTP gate")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("HTTP load gate requires DATABASE_URL")
	}
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_ihttp_%d", time.Now().UnixNano()))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	seedTargetScaleImageSessions(t, ctx, pool)
	ss := newSessionServerWithDatabase(t, pool, gdb)
	ss.client.Timeout = 10 * time.Second
	seedImageSessionHTTPPayload(t, ss)
	for table, want := range map[string]int{
		"image_sessions": 25000, "image_session_rounds": 10000,
		"image_session_generation_tasks": 1000, "image_session_provider_effects": 990,
		"image_session_assets": 10006,
	} {
		var got int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil || got != want {
			t.Fatalf("fixture %s rows=%d want=%d err=%v", table, got, want, err)
		}
	}
	t.Logf("HTTP_LOAD_FIXTURE sessions=25000 rounds=10000 tasks=1000 effects=990 references=6 prompt_bytes=%d progress_note_bytes=512 provider_request_bytes=4096 transport=loopback_http concurrency=1", len(strings.Repeat(httpLoadPrompt, 40)))

	base := "/api/image-sessions/" + queryPlanHotSessionID
	unauthorized, err := ss.client.Get(ss.srv.URL + base)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated detail status=%d", unauthorized.StatusCode)
	}
	read := func(path string) ([]byte, time.Duration) {
		t.Helper()
		start := time.Now()
		resp := ss.do(t, http.MethodGet, path, nil, "")
		raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		resp.Body.Close()
		elapsed := time.Since(start)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d read=%v body=%.200s", path, resp.StatusCode, err, raw)
		}
		if len(raw) >= 1<<20 {
			t.Fatalf("GET %s payload=%d exceeds fixture budget <1MiB", path, len(raw))
		}
		if bytes.Contains(raw, []byte("raw-request-must-not-leak")) {
			t.Fatalf("GET %s leaked provider request JSON", path)
		}
		return raw, elapsed
	}
	decode := func(raw []byte, out any) {
		t.Helper()
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
	}
	validateRounds := func(rounds []RoundResponse, first, count int) {
		t.Helper()
		if len(rounds) != count {
			t.Fatalf("rounds=%d want=%d", len(rounds), count)
		}
		for i, round := range rounds {
			if round.ID != fmt.Sprintf("plan-iround-%05d", first+i) ||
				round.GeneratedAsset.ID != fmt.Sprintf("plan-iasset-%05d", first+i) ||
				round.Prompt != strings.Repeat(httpLoadPrompt, 40) || round.CandidateIndex != 1 ||
				!strings.HasPrefix(round.GeneratedAsset.DownloadURL, "/api/image-session-assets/") {
				t.Fatalf("round %d identity/content mismatch: id=%s asset=%s", i, round.ID, round.GeneratedAsset.ID)
			}
		}
	}
	validateList := func(first, count int) func([]byte) {
		return func(raw []byte) {
			var page ListResponse
			decode(raw, &page)
			if len(page.Items) != count || page.NextCursor == nil || *page.NextCursor == "" {
				t.Fatalf("list page count=%d cursor=%v", len(page.Items), page.NextCursor)
			}
			for i, item := range page.Items {
				if item.ID != fmt.Sprintf("plan-img-%05d", first+i) {
					t.Fatalf("list item %d id=%s", i, item.ID)
				}
				if first+i == 0 {
					if item.RoundsCount != 10000 || item.LatestGeneratedAsset == nil || item.LatestGeneratedAsset.ID != "plan-iasset-00000" {
						t.Fatal("hot session summary lost count/latest asset")
					}
				} else if item.RoundsCount != 0 || item.LatestGeneratedAsset != nil {
					t.Fatalf("session %s leaked hot-session data", item.ID)
				}
			}
		}
	}
	validateDetail := func(raw []byte) {
		var detail DetailResponse
		decode(raw, &detail)
		if detail.ID != queryPlanHotSessionID || detail.RoundsCount != 10000 || len(detail.Assets) != 6 ||
			len(detail.GenerationTasks) != 50 || detail.HistoryNextAfter == nil || *detail.HistoryNextAfter == "" {
			t.Fatalf("detail id=%s rounds=%d refs=%d tasks=%d cursor=%v", detail.ID, detail.RoundsCount, len(detail.Assets), len(detail.GenerationTasks), detail.HistoryNextAfter)
		}
		validateRounds(detail.Rounds, 0, 20)
		seen := map[string]bool{}
		for _, task := range detail.GenerationTasks {
			if seen[task.ID] || task.SessionID != queryPlanHotSessionID || task.Prompt != strings.Repeat(httpLoadPrompt, 40) || task.ProgressMetadata["note"] != strings.Repeat("m", 512) {
				t.Fatalf("task identity/content mismatch: %s", task.ID)
			}
			seen[task.ID] = true
			if task.Status == "queued" {
				if task.QueuePosition == nil || *task.QueuePosition < 1 || *task.QueuePosition > 10 || len(task.ProviderEffects) != 0 {
					t.Fatalf("queued task projection mismatch: %s", task.ID)
				}
			} else if task.Status != "succeeded" || len(task.ProviderEffects) != 1 || task.ProviderEffects[0].GenerationTaskID != task.ID || task.ProviderEffects[0].EffectResult != "applied" {
				t.Fatalf("terminal task effects mismatch: %s", task.ID)
			}
		}
		for _, bounds := range [][2]int{{0, 30}, {980, 1000}} {
			for i := bounds[0]; i < bounds[1]; i++ {
				if !seen[fmt.Sprintf("plan-itask-%04d", i)] {
					t.Fatalf("detail omitted expected task %d", i)
				}
			}
		}
	}
	validateHistory := func(first, count int) func([]byte) {
		return func(raw []byte) {
			var page HistoryResponse
			decode(raw, &page)
			validateRounds(page.Items, first, count)
			if page.NextAfter == nil || *page.NextAfter == "" {
				t.Fatal("history page lost next cursor")
			}
		}
	}

	// Derive keyset requests from real wire cursors, outside measured samples.
	raw, _ := read("/api/image-sessions?limit=20")
	validateList(0, 20)(raw)
	var list ListResponse
	decode(raw, &list)
	raw, _ = read(base)
	validateDetail(raw)
	var detail DetailResponse
	decode(raw, &detail)
	for _, tc := range []struct {
		name     string
		path     string
		validate func([]byte)
		listPage bool
	}{
		{"list_20", "/api/image-sessions?limit=20", validateList(0, 20), true},
		{"list_100", "/api/image-sessions?limit=100", validateList(0, 100), true},
		{"list_next_20", "/api/image-sessions?limit=20&after=" + url.QueryEscape(*list.NextCursor), validateList(20, 20), true},
		{"detail", base, validateDetail, false},
		{"history_20", base + "/history?limit=20", validateHistory(0, 20), true},
		{"history_100", base + "/history?limit=100", validateHistory(0, 100), true},
		{"history_next_20", base + "/history?limit=20&after=" + url.QueryEscape(*detail.HistoryNextAfter), validateHistory(20, 20), true},
	} {
		durations := make([]time.Duration, 0, 100)
		maxBytes := 0
		for i := 0; i < 110; i++ {
			raw, elapsed := read(tc.path)
			tc.validate(raw)
			if i >= 10 {
				durations = append(durations, elapsed)
				maxBytes = max(maxBytes, len(raw))
			}
		}
		slices.Sort(durations)
		// Nearest-rank percentiles for exactly 100 measured responses.
		t.Logf("IMAGE_SESSION_HTTP route=%s warmup=10 samples=100 p50=%s p95=%s max_payload_bytes=%d", tc.name, durations[49], durations[94], maxBytes)
		if tc.listPage && durations[94] >= 300*time.Millisecond {
			t.Errorf("%s p95=%s exceeds initial local budget <300ms", tc.name, durations[94])
		}
	}

	// Keep the original seven-route fixture unchanged; measure unrelated backlog separately.
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_session_generation_tasks
		(id, session_id, status, prompt, size, generation_count, created_at, attempts, is_retryable, completed_candidates)
		SELECT 'http-backlog-' || lpad(g::text, 5, '0'), 'plan-img-24999', 'queued'::jobstatus,
		'p', '1024x1024', 1, '2026-09-05T00:00:00Z'::timestamptz, 0, TRUE, 0
		FROM generate_series(1, 25000) AS g`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE image_session_generation_tasks`); err != nil {
		t.Fatal(err)
	}
	var queueReads atomic.Int64
	callback := "test:http_empty_queue_reads"
	if err := gdb.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if isImageSessionQueueOverviewQuery(db) {
			queueReads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gdb.Callback().Query().Remove(callback) })
	t.Log("HTTP_EMPTY_FIXTURE sessions=25000 tasks=26000 queued=25010 rounds=10000 target_tasks=0 target_rounds=0 concurrency=1")
	for _, suffix := range []string{"", "/status"} {
		durations := make([]time.Duration, 0, 100)
		maxBytes := 0
		var before int64
		for i := 0; i < 110; i++ {
			if i == 10 {
				before = queueReads.Load()
			}
			raw, elapsed := read("/api/image-sessions/plan-img-00001" + suffix)
			var projection struct {
				ID              string         `json:"id"`
				GenerationTasks []TaskResponse `json:"generation_tasks"`
				RoundsCount     int            `json:"rounds_count"`
			}
			decode(raw, &projection)
			if projection.ID != "plan-img-00001" || projection.GenerationTasks == nil || len(projection.GenerationTasks) != 0 || projection.RoundsCount != 0 {
				t.Fatal("empty projection leaked other-session data or returned null tasks")
			}
			if i >= 10 {
				durations = append(durations, elapsed)
				maxBytes = max(maxBytes, len(raw))
			}
		}
		slices.Sort(durations)
		t.Logf("HTTP_EMPTY route=detail%s warmup=10 samples=100 p50=%s p95=%s max_payload_bytes=%d queue_queries=%d",
			suffix, durations[49], durations[94], maxBytes, queueReads.Load()-before)
	}
}

func seedImageSessionHTTPPayload(t *testing.T, ss *sessionServer) {
	t.Helper()
	ctx := context.Background()
	for _, query := range []string{
		`UPDATE image_session_rounds SET prompt = repeat('Preserve product identity, shape, material and label. ', 40), candidate_index = 1,
		 provider_request_json = json_build_object('raw-request-must-not-leak', repeat('r', 4096))`,
		`UPDATE image_session_generation_tasks SET prompt = repeat('Preserve product identity, shape, material and label. ', 40),
		 completed_candidates = CASE WHEN status = 'queued' THEN 0 ELSE 1 END,
		 progress_metadata = json_build_object('note', repeat('m', 512))`,
		`INSERT INTO image_session_provider_effects (id, generation_task_id, candidate_start_index, candidate_count,
		 operation_key, effect_kind, request_hash, provider_name, attempt_id, effect_result, reconciliation_state,
		 request_json, created_at, updated_at)
		 SELECT 'http-effect-' || right(id, 4), id, 1, 1, 'http-op-' || id, 'image_session_generation',
		 repeat('a', 64), 'fixture-provider', 'http-attempt-' || right(id, 4), 'applied', 'not_requested',
		 json_build_object('raw-request-must-not-leak', repeat('r', 4096)), created_at, created_at
		 FROM image_session_generation_tasks WHERE status = 'succeeded'`,
		`INSERT INTO image_session_assets (id, session_id, kind, original_filename, mime_type, storage_path, created_at, media_object_id)
		 SELECT 'http-ref-' || right(id, 5), session_id, 'reference_upload', original_filename, mime_type, storage_path, created_at, media_object_id
		 FROM image_session_assets WHERE id < 'plan-iasset-00006'`,
		`ANALYZE image_session_rounds; ANALYZE image_session_generation_tasks; ANALYZE image_session_provider_effects; ANALYZE image_session_assets`,
	} {
		if _, err := ss.pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
}
