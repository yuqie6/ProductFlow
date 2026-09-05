package imagesession

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"gorm.io/gorm"
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
	var dispatch queue.Dispatch
	if err := ss.db.Transaction(func(db *gorm.DB) error {
		var err error
		dispatch, err = queue.StageForActor(context.Background(), db, queue.ActorImageSession, taskID, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	provider := &pacedBatchProvider{MockChatProvider: MockChatProvider{PNG: grayPNG(1024, 1024)}}
	executor := Executor{DB: ss.db, Media: ss.media, Provider: provider}
	if _, claimed, err := queue.ClaimForConsumption(context.Background(), ss.pool, dispatch.ID, taskID, queue.DefaultConsumerLeaseSeconds); err != nil || claimed {
		t.Fatalf("pending envelope initialization: claimed=%v err=%v", claimed, err)
	}
	started := time.Now()
	var firstStarted time.Time
	for batch := 1; batch <= 3; batch++ {
		// Model dispatcher delivery without starting a broker or touching other envelopes.
		if err := ss.db.Model(&schema.AsyncDispatches{}).Where("id = ?", dispatch.ID).
			Updates(map[string]any{"status": queue.StatusSent, "sent_at": time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := queue.Consume(ctx, ss.pool, dispatch.ID, taskID, map[string]queue.ActorFunc{queue.ActorImageSession: executor.Execute})
		cancel()
		if err != nil {
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
		var envelope schema.AsyncDispatches
		if err := ss.db.Where("id = ?", dispatch.ID).Take(&envelope).Error; err != nil {
			t.Fatal(err)
		}
		wantTask, wantDispatch := "queued", queue.StatusPending
		if batch == 3 {
			wantTask, wantDispatch = "succeeded", queue.StatusConsumed
		}
		if task.Status != wantTask || envelope.Status != wantDispatch || envelope.LeaseToken != nil {
			t.Fatalf("batch=%d task=%s dispatch=%s lease=%v", batch, task.Status, envelope.Status, envelope.LeaseToken)
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
	if err := executor.Execute(context.Background(), taskID); err != queue.ErrLater {
		t.Fatalf("first batch: %v", err)
	}
	executor.Provider = MockChatProvider{Err: ErrRateLimit}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := executor.Execute(context.Background(), taskID); err != nil {
			t.Fatal(err)
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
