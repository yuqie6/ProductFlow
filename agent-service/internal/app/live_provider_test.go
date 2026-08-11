package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

const liveProviderSwitch = "PRODUCTFLOW_RUN_LIVE_AGENT"

func TestLiveProviderTwoTurnTranscript(t *testing.T) {
	if strings.TrimSpace(os.Getenv(liveProviderSwitch)) != "1" {
		t.Skip(liveProviderSwitch + " is not enabled")
	}
	apiKey := strings.TrimSpace(os.Getenv("AGENT_PROVIDER_API_KEY"))
	if apiKey == "" {
		t.Fatal("AGENT_PROVIDER_API_KEY is required when live provider validation is enabled")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AGENT_PROVIDER_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(os.Getenv("AGENT_PROVIDER_MODEL"))
	if model == "" {
		model = "gpt-5.4"
	}

	productFlowServer := newProductFlowFixture(t, nil, testProductID)
	t.Cleanup(productFlowServer.Close)
	productFlowClient, err := productflow.NewClient(
		productFlowServer.URL,
		testInternalToken,
		productFlowServer.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(ManagerConfig{
		DataRoot: t.TempDir(),
		Provider: agenttask.ProviderConfig{
			APIKey:           apiKey,
			BaseURL:          baseURL,
			Model:            model,
			ResponseMode:     agenttask.ResponseModeOpaque,
			ReasoningEffort:  strings.TrimSpace(os.Getenv("AGENT_PROVIDER_REASONING_EFFORT")),
			ReasoningSummary: strings.TrimSpace(os.Getenv("AGENT_PROVIDER_REASONING_SUMMARY")),
			TextVerbosity:    strings.TrimSpace(os.Getenv("AGENT_PROVIDER_TEXT_VERBOSITY")),
			ServiceTier:      strings.TrimSpace(os.Getenv("AGENT_PROVIDER_SERVICE_TIER")),
		},
		Policy: agenttask.Policy{
			MaxIterations:             12,
			ModelContextWindow:        128_000,
			AutoCompactTokenLimit:     96_000,
			CompactionSummaryMaxChars: 4_000,
		},
		ProductFlow: productFlowClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	entry, err := manager.Get(t.Context(), testConversationID)
	if err != nil {
		t.Fatal(err)
	}

	marker := fmt.Sprintf("PF-LIVE-%d", time.Now().UnixNano())
	first, err := entry.Service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: entry.Scope.RunID,
		Input: agenttask.TextInput(
			"This is a provider integration check. Do not call read or mutation tools. " +
				"Submit the required workflow draft with title exactly " + marker + ".",
		),
		IdempotencyKey: "live-provider-turn-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	first = awaitLiveTurn(t, entry.Service, first.RunID, first.TurnID)
	firstTitle := liveArtifactTitle(t, first)
	if firstTitle != marker {
		t.Fatalf("first artifact title = %q, want %q", firstTitle, marker)
	}

	second, err := entry.Service.StartTurn(t.Context(), agenttask.StartTurnRequest{
		RunID: entry.Scope.RunID,
		Input: agenttask.TextInput(
			"Use the previous turn's submitted title. Do not call read or mutation tools. " +
				"Submit a revised required workflow draft whose title is that exact previous title followed by ` / revised`.",
		),
		IdempotencyKey: "live-provider-turn-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second = awaitLiveTurn(t, entry.Service, second.RunID, second.TurnID)
	secondTitle := liveArtifactTitle(t, second)
	if secondTitle != firstTitle+" / revised" {
		t.Fatalf("second artifact title = %q, want transcript-derived %q", secondTitle, firstTitle+" / revised")
	}
}

func awaitLiveTurn(t *testing.T, service *agenttask.Service, runID, turnID string) agenttask.Turn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := service.GetTurn(ctx, runID, turnID)
		if err != nil {
			t.Fatal(err)
		}
		switch state.Status {
		case agenttask.TurnAwaitingConfirmation:
			return state
		case agenttask.TurnFailed, agenttask.TurnCanceled, agenttask.TurnUnknown, agenttask.TurnRequiresInput:
			t.Fatalf("live provider turn stopped in %s: %s", state.Status, state.Error)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("live provider turn timed out: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func liveArtifactTitle(t *testing.T, state agenttask.Turn) string {
	t.Helper()
	if state.Artifact == nil {
		t.Fatalf("turn %s has no required artifact", state.TurnID)
	}
	var value struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(state.Artifact.Value, &value); err != nil {
		t.Fatalf("decode live provider artifact: %v", err)
	}
	return value.Title
}
