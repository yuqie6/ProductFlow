package agenttask_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/turn"
)

func TestServiceAdmissionLimitsConcurrentServices(t *testing.T) {
	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maxActive.Load()
			if current <= observed || maxActive.CompareAndSwap(observed, current) {
				break
			}
		}
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		writer.Header().Set("Content-Type", "application/json")
		writeServiceStream(t, writer, `{"id":"admission","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`)
	}))
	t.Cleanup(provider.Close)

	admission := agenttask.NewSemaphore(1)
	openService := func(database string) *agenttask.Service {
		service, err := agenttask.OpenService(agenttask.ServiceConfig{
			Admission: admission,
			Runner: agenttask.Config{
				Database: database, Workspace: t.TempDir(), SkillUserHome: t.TempDir(),
				Provider: agenttask.ProviderConfig{
					APIKey: "secret", BaseURL: provider.URL, Model: "model", HTTPClient: provider.Client(),
				},
				Policy: testPolicy(),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = service.Close() })
		return service
	}
	firstService := openService(filepath.Join(t.TempDir(), "first.db"))
	secondService := openService(filepath.Join(t.TempDir(), "second.db"))

	first, err := firstService.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-first", Input: agenttask.TextInput("first"), IdempotencyKey: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondService.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: "run-second", Input: agenttask.TextInput("second"), IdempotencyKey: "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first provider request did not start")
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 1 || maxActive.Load() != 1 {
		t.Fatalf("admission did not hold the second service: calls=%d max_active=%d", calls.Load(), maxActive.Load())
	}
	close(release)

	first = awaitTurn(t, firstService, first.RunID, first.TurnID, turn.StatusSucceeded)
	second = awaitTurn(t, secondService, second.RunID, second.TurnID, turn.StatusSucceeded)
	if first.Output != "done" || second.Output != "done" || calls.Load() != 2 || maxActive.Load() != 1 {
		t.Fatalf(
			"turns=%#v/%#v calls=%d max_active=%d",
			first,
			second,
			calls.Load(),
			maxActive.Load(),
		)
	}
}
