package imagesession

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

// TestReplicaFieldTwoWorkersRespectGenerationCapacity 模拟两个 worker 同时 claim：
// 上限 1 时只有一个任务变成 running，另一个保持 queued 并标 waiting_for_capacity。
func TestReplicaFieldTwoWorkersRespectGenerationCapacity(t *testing.T) {
	name := fmt.Sprintf("pf_replc_%d", time.Now().UnixNano()%1_000_000_000)
	_, gdb := testdb.IsolatedMigrated(t, name)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now().UTC()

	if err := gdb.Exec(`
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES (?, '1', ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
	`, generation.MaxConcurrentSettingKey, now, now).Error; err != nil {
		t.Fatal(err)
	}

	sessionID := clockid.New()
	if err := gdb.Exec(`
		INSERT INTO image_sessions (id, title, created_at, updated_at) VALUES (?, 'replica-capacity', ?, ?)
	`, sessionID, now, now).Error; err != nil {
		t.Fatal(err)
	}

	taskA, taskB := clockid.New(), clockid.New()
	for _, id := range []string{taskA, taskB} {
		if err := gdb.Exec(`
			INSERT INTO image_session_generation_tasks (
				id, session_id, status, prompt, size, generation_count, created_at,
				attempts, is_retryable, completed_candidates
			) VALUES (?, ?, 'queued', 'p', '1024x1024', 1, ?, 0, TRUE, 0)
		`, id, sessionID, now).Error; err != nil {
			t.Fatal(err)
		}
	}

	ex := Executor{DB: gdb}
	type result struct {
		id      string
		claimed bool
		err     error
	}
	out := make(chan result, 2)
	var wg sync.WaitGroup
	for _, id := range []string{taskA, taskB} {
		wg.Add(1)
		go func(taskID string) {
			defer wg.Done()
			claimed, _, _, err := ex.claim(ctx, taskID)
			out <- result{id: taskID, claimed: claimed, err: err}
		}(id)
	}
	wg.Wait()
	close(out)

	claimed := 0
	waiting := 0
	for r := range out {
		if r.err != nil && !errors.Is(r.err, errWaitingCapacity) {
			t.Fatalf("claim %s: %v", r.id, r.err)
		}
		if r.claimed {
			claimed++
			continue
		}
		if errors.Is(r.err, errWaitingCapacity) {
			waiting++
		}
	}
	if claimed != 1 || waiting != 1 {
		t.Fatalf("claimed=%d waiting=%d want 1 and 1", claimed, waiting)
	}

	var running, queued int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM image_session_generation_tasks WHERE session_id = ? AND status = 'running'`, sessionID).Scan(&running).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Raw(`SELECT COUNT(*) FROM image_session_generation_tasks WHERE session_id = ? AND status = 'queued'`, sessionID).Scan(&queued).Error; err != nil {
		t.Fatal(err)
	}
	if running != 1 || queued != 1 {
		t.Fatalf("rows running=%d queued=%d", running, queued)
	}
}

func TestGenerateWhenCapacityFullStillQueuesWithoutDenied(t *testing.T) {
	name := fmt.Sprintf("pf_enqcap_%d", time.Now().UnixNano()%1_000_000_000)
	_, gdb := testdb.IsolatedMigrated(t, name)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now().UTC()

	if err := gdb.Exec(`
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES (?, '1', ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
	`, generation.MaxConcurrentSettingKey, now, now).Error; err != nil {
		t.Fatal(err)
	}

	holderID := clockid.New()
	if err := gdb.Exec(`
		INSERT INTO image_sessions (id, title, created_at, updated_at) VALUES (?, 'hold-slot', ?, ?)
	`, holderID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		INSERT INTO image_session_generation_tasks (
			id, session_id, status, prompt, size, generation_count, created_at,
			attempts, is_retryable, completed_candidates, active_attempt_id, started_at
		) VALUES (?, ?, 'running', 'hold-slot', '1024x1024', 1, ?, 0, TRUE, 0, ?, ?)
	`, clockid.New(), holderID, now, clockid.New(), now).Error; err != nil {
		t.Fatal(err)
	}

	sessionID := clockid.New()
	if err := gdb.Exec(`
		INSERT INTO image_sessions (id, title, created_at, updated_at) VALUES (?, 'enqueue-admission', ?, ?)
	`, sessionID, now, now).Error; err != nil {
		t.Fatal(err)
	}

	before := metrics.GenerationAdmissionDeniedCount("imagesession")
	out, err := (Service{DB: gdb}).Generate(ctx, sessionID, GenerateRequest{Prompt: "next", Size: "1024x1024"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.GenerationTasks) != 1 || out.GenerationTasks[0].Status != "queued" {
		t.Fatalf("enqueue must remain queued, got %+v", out.GenerationTasks)
	}
	queuedID := out.GenerationTasks[0].ID
	if got := metrics.GenerationAdmissionDeniedCount("imagesession"); got != before {
		t.Fatalf("enqueue counted denied: before=%d after=%d", before, got)
	}

	claimed, _, _, claimErr := (Executor{DB: gdb}).claim(ctx, queuedID)
	if claimed {
		t.Fatal("claim at full capacity must not start the queued task")
	}
	if !errors.Is(claimErr, errWaitingCapacity) {
		t.Fatalf("claim err %v", claimErr)
	}
	if got := metrics.GenerationAdmissionDeniedCount("imagesession"); got != before+1 {
		t.Fatalf("claim denied: before=%d after=%d", before, got)
	}
}
