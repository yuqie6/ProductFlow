package imagesession

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestImageSessionSSESnapshotLoad(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_IMAGE_SESSION_SSE_LOAD") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_IMAGE_SESSION_SSE_LOAD=1 to run the SSE snapshot gate")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("SSE snapshot gate requires DATABASE_URL")
	}
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_isse_%d", time.Now().UnixNano()))
	ss := newSessionServerWithDatabase(t, pool, gdb)
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	session := schema.ImageSessions{ID: "sse-load-session", Title: "snapshot load", CreatedAt: now, UpdatedAt: now}
	if err := gdb.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	var tasks []schema.ImageSessionGenerationTasks
	for i := 0; i < 26; i++ {
		metadata := `{"note":"` + strings.Repeat("m", 512) + `"}`
		tasks = append(tasks, schema.ImageSessionGenerationTasks{
			ID: fmt.Sprintf("sse-task-%02d", i), SessionID: session.ID, Status: "queued",
			Prompt: strings.Repeat(httpLoadPrompt, 40), Size: "1024x1024", GenerationCount: 1,
			ProgressMetadata: &metadata, CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	if err := gdb.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int64
	callback := "test:sse_snapshot_reads"
	if err := gdb.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if _, ok := db.Statement.Dest.(*schema.ImageSessionRounds); ok {
			reads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gdb.Callback().Query().Remove(callback) })
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	type event struct {
		status StatusResponse
		at     time.Time
	}
	type stream struct {
		events chan event
		done   chan error
		frames atomic.Int64
		bytes  atomic.Int64
		beats  atomic.Int64
	}
	open := func() *stream {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ss.srv.URL+"/api/image-sessions/"+session.ID+"/events", nil)
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
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("SSE status=%d", resp.StatusCode)
		}
		s := &stream{events: make(chan event, 128), done: make(chan error, 1)}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer resp.Body.Close()
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				s.bytes.Add(int64(len(line) + 1))
				if line == ": keep-alive" {
					s.beats.Add(1)
				}
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				var status StatusResponse
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &status); err != nil {
					s.done <- err
					return
				}
				s.frames.Add(1)
				select {
				case s.events <- event{status, time.Now()}:
				case <-ctx.Done():
					s.done <- ctx.Err()
					return
				}
			}
			s.done <- scanner.Err()
		}()
		return s
	}
	await := func(s *stream, match func(StatusResponse) bool) event {
		t.Helper()
		for {
			select {
			case e := <-s.events:
				if match(e.status) {
					return e
				}
			case <-ctx.Done():
				t.Fatal("timed out waiting for snapshot")
			}
		}
	}
	var streams []*stream
	for i := 0; i < 4; i++ {
		s := open()
		streams = append(streams, s)
		e := await(s, func(StatusResponse) bool { return true })
		if !e.status.HasActiveGenerationTask || len(e.status.GenerationTasks) != 26 {
			t.Fatal("initial snapshot truncated active tasks")
		}
	}
	// Allow seven normal 2s fallback reads and the independent 15s heartbeat.
	idleStart := time.Now()
	select {
	case <-time.After(15250 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var idleFrames, idleBytes, beats int64
	for _, s := range streams {
		idleFrames += s.frames.Load()
		idleBytes += s.bytes.Load()
		beats += s.beats.Load()
	}
	t.Logf("SSE_IDLE clients=4 active_tasks=26 effects=0 prompt_bytes=%d note_bytes=512 window=%s frames_including_initial=%d wire_body_bytes=%d heartbeats=%d status_reads=%d",
		len(tasks[0].Prompt), time.Since(idleStart), idleFrames, idleBytes, beats, reads.Load())
	if reads.Load() < 32 || beats < 4 {
		t.Fatal("fixture did not exercise periodic PG recovery reads and heartbeats")
	}
	// No NOTIFY and no session.updated_at change: fallback must still discover task progress.
	changedAt := time.Now()
	if err := gdb.Model(&schema.ImageSessionGenerationTasks{}).Where("id = ?", tasks[0].ID).
		Update("progress_phase", "fixture-progress").Error; err != nil {
		t.Fatal(err)
	}
	var maxProgress time.Duration
	for _, s := range streams {
		e := await(s, func(status StatusResponse) bool {
			for _, task := range status.GenerationTasks {
				if task.ID == tasks[0].ID && task.ProgressPhase != nil && *task.ProgressPhase == "fixture-progress" {
					return true
				}
			}
			return false
		})
		maxProgress = max(maxProgress, e.at.Sub(changedAt))
	}
	terminalAt := time.Now()
	if err := gdb.Model(&schema.ImageSessionGenerationTasks{}).Where("session_id = ?", session.ID).
		Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	var maxTerminal time.Duration
	for _, s := range streams {
		e := await(s, func(status StatusResponse) bool { return !status.HasActiveGenerationTask })
		maxTerminal = max(maxTerminal, e.at.Sub(terminalAt))
		if len(e.status.GenerationTasks) != 0 {
			t.Fatal("terminal snapshot retained active tasks")
		}
		if err := <-s.done; err != nil {
			t.Fatalf("terminal stream: %v", err)
		}
	}
	reconnected := open()
	await(reconnected, func(status StatusResponse) bool { return !status.HasActiveGenerationTask })
	if err := <-reconnected.done; err != nil && err != io.EOF {
		t.Fatal(err)
	}
	t.Logf("SSE_CHANGE clients=4 progress_max=%s terminal_max=%s reconnect_frames=%d", maxProgress, maxTerminal, reconnected.frames.Load())
	if maxProgress > 3*time.Second || maxTerminal > 3*time.Second || reconnected.frames.Load() != 1 {
		t.Error("fallback delivery exceeded local 3s budget or reconnect lost its snapshot")
	}
	if idleFrames != 4 {
		t.Errorf("unchanged state emitted %d snapshots, want one initial snapshot per client (4)", idleFrames)
	}
}
