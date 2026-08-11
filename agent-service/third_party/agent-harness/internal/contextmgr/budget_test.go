package contextmgr

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/internal/llm"
)

func TestTrimToBudgetDropsToolPairWithoutMutatingSession(t *testing.T) {
	system := "system"
	toolOutput := "tool output that exceeds the tiny budget"
	latest := "latest"
	messages := []llm.Message{
		{Role: "system", Content: &system},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "read_file",
					Arguments: json.RawMessage(`"{\"path\":\"a.txt\"}"`),
				},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: &toolOutput},
		{Role: "user", Content: &latest},
	}
	original := append([]llm.Message(nil), messages...)

	trimmed := TrimToBudget(messages, 1)
	if len(trimmed) != 2 || trimmed[0].Role != "system" || trimmed[1].String() != "latest" {
		t.Fatalf("trimmed = %#v", trimmed)
	}
	if !reflect.DeepEqual(messages, original) {
		t.Fatalf("TrimToBudget mutated the source session")
	}
}

func TestTrimToBudgetDropsWholeTurnInsteadOfOnlyOldUserMessage(t *testing.T) {
	system := "system"
	oldUser := "old question"
	oldAnswer := strings.Repeat("old answer ", 30)
	latestUser := "latest question"
	latestAnswer := "latest answer"
	messages := []llm.Message{
		{Role: "system", Content: &system},
		{Role: "user", Content: &oldUser},
		{Role: "assistant", Content: &oldAnswer},
		{Role: "user", Content: &latestUser},
		{Role: "assistant", Content: &latestAnswer},
	}

	trimmed := TrimToBudget(messages, 30)
	if len(trimmed) != 3 || trimmed[1].String() != latestUser || trimmed[2].String() != latestAnswer {
		t.Fatalf("trimmed = %#v", trimmed)
	}
	for _, message := range trimmed {
		if message.String() == oldAnswer {
			t.Fatal("old assistant message survived without its user turn")
		}
	}
}

func TestBuildWindowReportsUnsummarizedMessagesInsteadOfHidingLoss(t *testing.T) {
	system := "system"
	oldUser := "old requirement"
	oldAnswer := strings.Repeat("old evidence ", 40)
	latestUser := "latest request"
	messages := []llm.Message{
		{Role: "system", Content: &system},
		{Role: "user", Content: &oldUser},
		{Role: "assistant", Content: &oldAnswer},
		{Role: "user", Content: &latestUser},
	}

	window := BuildWindowResult(messages, nil, 30)
	if !window.OmitsUnsummarizedMessages() || window.OmittedStart != 1 || window.OmittedThrough != 3 {
		t.Fatalf("window = %#v", window)
	}
	if got := window.Messages[len(window.Messages)-1].String(); got != latestUser {
		t.Fatalf("latest message = %q", got)
	}
}

func TestCompactionPlanProducesTraceableSummaryAndKeepsRecentTurns(t *testing.T) {
	system := "system"
	messages := []llm.Message{{Role: "system", Content: &system}}
	for i := 1; i <= 5; i++ {
		user := fmt.Sprintf("question %d %s", i, strings.Repeat("x", 45))
		answer := fmt.Sprintf("answer %d %s", i, strings.Repeat("y", 45))
		messages = append(messages,
			llm.Message{Role: "user", Content: &user},
			llm.Message{Role: "assistant", Content: &answer},
		)
	}

	plan, ok := PlanCompaction(messages, nil, 160)
	if !ok {
		t.Fatal("expected compaction plan")
	}
	if plan.ThroughMessageCount <= 1 || plan.ThroughMessageCount >= len(messages)-3 || plan.SourceDigest == "" {
		t.Fatalf("plan = %#v", plan)
	}
	summary, ok := CompleteCompaction(plan, "保留较早会话的关键决定", time.Now(), 1000)
	if !ok {
		t.Fatal("summary was rejected")
	}
	window := BuildWindowResult(messages, &summary, 160).Messages
	if len(window) < 4 || window[0].Role != "system" || window[1].Role != "system" || !strings.Contains(window[1].String(), "关键决定") {
		t.Fatalf("window = %#v", window)
	}
	if got := window[len(window)-2].String(); !strings.Contains(got, "question 5") {
		t.Fatalf("latest turn missing: %#v", window)
	}
	if !strings.Contains(CompactionInput(plan), "question 1") {
		t.Fatal("compaction input omitted retired messages")
	}
	if count := VerbatimUserMessageCount(messages, &summary); count == 0 {
		t.Fatal("summary did not expose any verbatim user messages")
	}
	joined := ""
	for _, message := range window {
		joined += message.String()
	}
	if !strings.Contains(joined, messages[1].String()) || !strings.Contains(joined, "source_sha256="+summary.SourceDigest) {
		t.Fatalf("window omitted pinned source context: %#v", window)
	}

	tampered := append([]llm.Message(nil), messages...)
	changed := "changed source"
	tampered[1].Content = &changed
	if SummaryMatchesMessages(&summary, tampered) {
		t.Fatal("summary digest accepted tampered source history")
	}
}

func TestCompleteCompactionRejectsOversizedSummaryInsteadOfTruncatingIt(t *testing.T) {
	plan := CompactionPlan{
		Version: 1, ThroughMessageCount: 1, SourceDigest: strings.Repeat("a", 64),
	}
	if _, ok := CompleteCompaction(plan, "123456", time.Now(), 5); ok {
		t.Fatal("oversized summary was silently truncated")
	}
}

func TestCompactionPlanRetiresCompleteToolRoundsInsideCurrentTurn(t *testing.T) {
	system := "system"
	user := "audit this repository"
	messages := []llm.Message{
		{Role: "system", Content: &system},
		{Role: "user", Content: &user},
	}
	for round := 1; round <= 4; round++ {
		callID := fmt.Sprintf("call-%d", round)
		output := fmt.Sprintf("evidence-%d %s", round, strings.Repeat("x", 600))
		messages = append(messages,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
				ID: callID, Type: "function",
				Function: llm.ToolCallFunction{Name: "read_file", Arguments: json.RawMessage(`{"path":"file.go"}`)},
			}}},
			llm.Message{Role: "tool", ToolCallID: callID, Content: &output},
		)
	}

	plan, ok := PlanCompaction(messages, nil, 550)
	if !ok {
		t.Fatal("single long tool turn did not produce a compaction plan")
	}
	if plan.ThroughMessageCount <= 2 || plan.ThroughMessageCount >= len(messages)-3 {
		t.Fatalf("compaction boundary = %d, messages = %d", plan.ThroughMessageCount, len(messages))
	}
	summary, ok := CompleteCompaction(plan, "user requested an audit; earlier evidence was collected", time.Now(), 1000)
	if !ok {
		t.Fatal("summary was rejected")
	}
	window := BuildWindowResult(messages, &summary, 550).Messages
	if len(window) < 4 || window[0].Role != "system" || window[1].Role != "system" {
		t.Fatalf("window = %#v", window)
	}
	assertCompleteToolRounds(t, window[2:])
}

func assertCompleteToolRounds(t *testing.T, messages []llm.Message) {
	t.Helper()
	pending := make(map[string]bool)
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			pending[call.ID] = true
		}
		if message.Role == "tool" {
			if !pending[message.ToolCallID] {
				t.Fatalf("orphaned tool output %q in %#v", message.ToolCallID, messages)
			}
			delete(pending, message.ToolCallID)
		}
	}
	if len(pending) != 0 {
		t.Fatalf("tool calls without outputs: %v", pending)
	}
}

func TestEstimateToolsTokensIncludesSchemaPayload(t *testing.T) {
	tools := []llm.Tool{{
		Type: "function",
		Function: llm.ToolFunction{
			Name: "read_file", Description: strings.Repeat("description ", 20),
			Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		},
	}}
	if got := EstimateToolsTokens(tools); got < 50 {
		t.Fatalf("tool token estimate = %d", got)
	}
}

func TestConservativeUTF8RuneLimitFitsWorstCaseEncoding(t *testing.T) {
	for tokens := 1; tokens <= 20; tokens++ {
		limit := ConservativeUTF8RuneLimit(tokens)
		if limit == 0 {
			continue
		}
		content := strings.Repeat("\U0010ffff", limit)
		emptyContent := ""
		empty := llm.Message{Role: "assistant", Content: &emptyContent}
		message := llm.Message{Role: "assistant", Content: &content}
		if extra := EstimateTokens(message) - EstimateTokens(empty); extra > tokens {
			t.Fatalf("%d token budget produced %d-rune limit using %d tokens", tokens, limit, extra)
		}
	}
}
