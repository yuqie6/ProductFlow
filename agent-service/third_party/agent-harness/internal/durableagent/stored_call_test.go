package durableagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
)

type unavailableStoredClient struct {
	creates   atomic.Int64
	retrieves atomic.Int64
}

func (*unavailableStoredClient) Chat(context.Context, []llm.Message, []llm.Tool) (llm.Message, string, error) {
	return llm.Message{}, "", errors.New("unexpected Chat")
}

func (c *unavailableStoredClient) CreateStored(context.Context, []llm.Message, []llm.Tool, string) (llm.StoredResponse, error) {
	c.creates.Add(1)
	return llm.StoredResponse{ID: "resp_missing", Status: "queued"}, nil
}

func (c *unavailableStoredClient) RetrieveStored(context.Context, string) (llm.StoredResponse, error) {
	c.retrieves.Add(1)
	return llm.StoredResponse{}, fmt.Errorf("provider returned 404")
}

func TestWaitForStoredChatStopsAtConfiguredTimeout(t *testing.T) {
	client := &unavailableStoredClient{}
	start := time.Now()
	_, _, err := waitForStoredChat(t.Context(), client, "resp_missing", 25*time.Millisecond)
	if !errors.Is(err, errStoredWaitTimeout) {
		t.Fatalf("wait error = %v, want errStoredWaitTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("configured stored timeout took %s", elapsed)
	}
	if client.retrieves.Load() == 0 {
		t.Fatal("stored response was never retrieved")
	}
}

func TestStoredReconcileTurnsWaitTimeoutIntoUnknown(t *testing.T) {
	client := &unavailableStoredClient{}
	checkpoint, _ := json.Marshal(storedResponseCheckpoint{ResponseID: "resp_missing"})
	_, _, state, err := reconcileStoredChat(t.Context(), client, durable.Invocation{Checkpoint: checkpoint}, 25*time.Millisecond)
	if err != nil || state != durable.ReconcileUnknown {
		t.Fatalf("state = %s, err = %v", state, err)
	}
}

func TestStoredTaskWaitTimeoutStopsInUnknown(t *testing.T) {
	client := &unavailableStoredClient{}
	workspace := t.TempDir()
	config := testConfig(filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{})
	config.StoredResponseTimeout = 25 * time.Millisecond
	runner, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	result, err := runner.Start(t.Context(), "finish eventually")
	if !errors.Is(err, durable.ErrUnknown) || result.Job.Status != durable.JobUnknown {
		t.Fatalf("job = %#v, err = %v", result.Job, err)
	}
	if client.creates.Load() != 1 || client.retrieves.Load() == 0 {
		t.Fatalf("provider calls = create:%d retrieve:%d", client.creates.Load(), client.retrieves.Load())
	}
}
