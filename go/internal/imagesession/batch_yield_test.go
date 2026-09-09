package imagesession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type pacedBatchProvider struct {
	MockChatProvider
	calls int
}

func (p *pacedBatchProvider) Generate(ctx context.Context, req ChatRequest) (ChatResult, error) {
	p.calls++
	select {
	case <-time.After(time.Second):
		return p.MockChatProvider.Generate(ctx, req)
	case <-ctx.Done():
		return ChatResult{}, ctx.Err()
	}
}

func TestExecuteBatchesYieldThroughConsumer(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "three independent execution windows", "size": "1024x1024", "generation_count": 3,
	})
	job := stageImageSessionJob(t, ss.db, taskID)
	provider := &pacedBatchProvider{MockChatProvider: MockChatProvider{PNG: grayPNG(1024, 1024)}}
	executor := Executor{DB: ss.db, Media: ss.media, Provider: provider}
	started := time.Now()
	var firstStarted time.Time
	for batch := 1; batch <= 3; batch++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := executeImageSessionJob(ctx, job, executor)
		cancel()
		var snooze *river.JobSnoozeError
		if err != nil && !errors.As(err, &snooze) {
			t.Fatal(err)
		}
		detail := loadSessionDetail(t, ss, session.ID)
		task := generationTaskByID(t, detail, taskID)
		if task.CompletedCandidates != batch || task.Attempts != 1 || provider.calls != batch || len(detail.Rounds) != batch {
			t.Fatalf("batch=%d completed=%d attempts=%d calls=%d rounds=%d", batch, task.CompletedCandidates, task.Attempts, provider.calls, len(detail.Rounds))
		}
		if task.StartedAt == nil {
			t.Fatal("missing initial start")
		}
		if batch == 1 {
			firstStarted = *task.StartedAt
		}
		if !task.StartedAt.Equal(firstStarted) {
			t.Fatal("normal continuation reset start time")
		}
		wantTask := "queued"
		if batch == 3 {
			wantTask = "succeeded"
		}
		if task.Status != wantTask {
			t.Fatalf("batch=%d task=%s", batch, task.Status)
		}
		for _, effect := range task.ProviderEffects {
			if effect.EffectResult != "applied" {
				t.Fatalf("effect=%+v", effect)
			}
		}
	}
	if time.Since(started) <= 2*time.Second {
		t.Fatal("fixture did not exceed one handler window")
	}
	t.Logf("3 candidates across 3 consumer windows, each deadline=2s; elapsed=%s attempts=1 provider_calls=3", time.Since(started))
}

func TestCompletedBatchDoesNotBypassFailureRetryLimit(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "retry after completed batch", "size": "1024x1024", "generation_count": 2,
	})
	executor := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}
	job := stageImageSessionJob(t, ss.db, taskID)
	if err := executeImageSessionJob(context.Background(), job, executor); err == nil {
		t.Fatal("first batch must yield")
	} else {
		var snooze *river.JobSnoozeError
		if !errors.As(err, &snooze) {
			t.Fatalf("first batch: %v", err)
		}
	}
	executor.Provider = MockChatProvider{Err: ErrRateLimit}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := executeImageSessionJob(context.Background(), job, executor); err != nil {
			var snooze *river.JobSnoozeError
			if !errors.As(err, &snooze) {
				t.Fatal(err)
			}
		}
		task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
		if task.Attempts != attempt {
			t.Fatalf("attempt=%d got=%d", attempt, task.Attempts)
		}
		want := "queued"
		if attempt == maxAttempts {
			want = "failed"
		}
		if task.Status != want || task.CompletedCandidates != 1 {
			t.Fatalf("task=%+v", task)
		}
	}
}
