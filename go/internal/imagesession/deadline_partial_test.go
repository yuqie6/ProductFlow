package imagesession

import (
	"context"
	"errors"
	"testing"
	"time"
)

type partialDeadlineProvider struct {
	MockChatProvider
	calls    int
	onSecond func()
}

func (p *partialDeadlineProvider) Generate(ctx context.Context, req ChatRequest) (ChatResult, error) {
	p.calls++
	if p.calls == 1 {
		return p.MockChatProvider.Generate(ctx, req)
	}
	if p.calls > 2 {
		return ChatResult{}, errors.New("unexpected provider replay")
	}
	p.onSecond()
	<-ctx.Done()
	return ChatResult{}, ctx.Err()
}

func TestExecuteDeadlinePreservesCompletedCandidate(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "deadline after first candidate", "size": "1024x1024", "generation_count": 2,
	})
	provider := &partialDeadlineProvider{}
	var assetID string
	provider.onSecond = func() {
		detail := loadSessionDetail(t, ss, session.ID)
		task := generationTaskByID(t, detail, taskID)
		if task.Status != "running" || task.CompletedCandidates != 1 || task.ActiveCandidateIndex == nil || *task.ActiveCandidateIndex != 2 {
			t.Fatalf("before deadline: %+v", task)
		}
		if task.ProgressUpdatedAt == nil || time.Since(*task.ProgressUpdatedAt) > time.Minute {
			t.Fatalf("expected recent progress heartbeat: %v", task.ProgressUpdatedAt)
		}
		if len(detail.Rounds) != 1 {
			t.Fatalf("completed rounds=%d", len(detail.Rounds))
		}
		assetID = detail.Rounds[0].GeneratedAsset.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	executor := Executor{DB: ss.db, Media: ss.media, Provider: provider}
	if err := executor.Execute(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("context error=%v", ctx.Err())
	}
	detail := loadSessionDetail(t, ss, session.ID)
	task := generationTaskByID(t, detail, taskID)
	if task.Status != "unknown" || task.IsRetryable || task.CompletedCandidates != 1 {
		t.Fatalf("deadline result: %+v", task)
	}
	if len(detail.Rounds) != 1 || detail.Rounds[0].GeneratedAsset.ID != assetID {
		t.Fatal("deadline lost or replaced the completed candidate")
	}
	if len(task.ProviderEffects) != 2 {
		t.Fatalf("effects=%+v", task.ProviderEffects)
	}
	for _, effect := range task.ProviderEffects {
		want := "applied"
		if effect.CandidateStartIndex == 2 {
			want = "unknown"
		}
		if effect.EffectResult != want {
			t.Fatalf("effect=%+v want %s", effect, want)
		}
	}
	if err := executor.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("terminal task re-executed provider: %d calls", provider.calls)
	}
	t.Logf("deadline=5s completed=1/2 preserved_asset=true effects=applied,unknown provider_calls=%d retryable=false", provider.calls)
}
