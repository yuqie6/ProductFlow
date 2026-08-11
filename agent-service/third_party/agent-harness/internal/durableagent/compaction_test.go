package durableagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/contextmgr"
	"github.com/yuqie6/agent-harness/internal/llm"
)

type recordedModelCall struct {
	messages []llm.Message
	tools    []llm.Tool
}

type loopingClient struct {
	mu              sync.Mutex
	normalRounds    int
	normalCalls     int
	compactionCalls int
	calls           []recordedModelCall
}

func (c *loopingClient) Chat(_ context.Context, messages []llm.Message, catalog []llm.Tool) (llm.Message, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, recordedModelCall{
		messages: append([]llm.Message(nil), messages...),
		tools:    append([]llm.Tool(nil), catalog...),
	})
	if len(catalog) == 0 {
		c.compactionCalls++
		return textMessage("cumulative-summary-" + strconv.Itoa(c.compactionCalls)), "completed", nil
	}
	c.normalCalls++
	if c.normalCalls <= c.normalRounds {
		id := "call-read-" + strconv.Itoa(c.normalCalls)
		return toolCallMessage(id, "read_file", `{"path":"note.txt"}`), "completed", nil
	}
	return textMessage("finished after compaction"), "completed", nil
}

func (c *loopingClient) snapshot() ([]recordedModelCall, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]recordedModelCall(nil), c.calls...), c.normalCalls, c.compactionCalls
}

func TestRunnerCompactsLongTaskAtDurableBoundaries(t *testing.T) {
	workspace := compactionWorkspace(t)
	client := &loopingClient{normalRounds: 6}
	policy := Policy{MaxIterations: 10, ModelContextWindow: 3200, AutoCompactTokenLimit: 2400}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, policy)

	result, err := runner.Start(t.Context(), "repeatedly inspect note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != durable.JobSucceeded || result.Output != "finished after compaction" {
		t.Fatalf("result = %#v", result)
	}
	if result.ModelCalls != 7 || result.ToolCalls != 6 || result.CompactionCalls < 2 {
		t.Fatalf("counts = model:%d compact:%d tools:%d", result.ModelCalls, result.CompactionCalls, result.ToolCalls)
	}

	calls, normalCalls, compactCalls := client.snapshot()
	if normalCalls != result.ModelCalls || compactCalls != result.CompactionCalls {
		t.Fatalf("client calls = normal:%d compact:%d, result = %#v", normalCalls, compactCalls, result)
	}
	maxNormalMessages := 0
	highestSummaryVersion := 0
	compactionPayloads := make([]string, 0, compactCalls)
	for _, call := range calls {
		if len(call.tools) == 0 {
			compactionPayloads = append(compactionPayloads, call.messages[len(call.messages)-1].String())
			continue
		}
		if tokens := contextmgr.TotalTokens(call.messages) + contextmgr.EstimateToolsTokens(call.tools); tokens > policy.ModelContextWindow {
			t.Fatalf("normal request used %d tokens", tokens)
		}
		maxNormalMessages = max(maxNormalMessages, len(call.messages))
		summaries := 0
		for _, message := range call.messages {
			_, version, _, found, parseErr := parseDurableSummaryMessage(message)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if found {
				summaries++
				highestSummaryVersion = max(highestSummaryVersion, version)
			}
		}
		if summaries > 1 {
			t.Fatalf("normal request contains %d summaries", summaries)
		}
	}
	if maxNormalMessages > 7 {
		t.Fatalf("model window kept growing: %d messages", maxNormalMessages)
	}
	if highestSummaryVersion < 2 {
		t.Fatalf("highest summary version = %d", highestSummaryVersion)
	}
	if len(compactionPayloads) < 2 || !strings.Contains(compactionPayloads[1], `"previous_summary":"cumulative-summary-1"`) {
		t.Fatalf("second compaction did not accumulate first summary: %q", compactionPayloads)
	}

	for _, step := range result.Job.Steps {
		if step.Tool != modelToolName {
			continue
		}
		input, decodeErr := decodeModelInput(step.Input)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if tokens := contextmgr.TotalTokens(input.Messages) + contextmgr.EstimateToolsTokens(input.Tools); tokens > policy.ModelContextWindow {
			t.Fatalf("journaled model input used %d tokens", tokens)
		}
	}
}

func TestRunnerDoesNotReplayLostCompactionResponse(t *testing.T) {
	workspace := compactionWorkspace(t)
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &loopingClient{normalRounds: 5}
	policy := Policy{MaxIterations: 10, ModelContextWindow: 3200, AutoCompactTokenLimit: 2400}
	config := testConfig(database, workspace, client, false, policy)
	config.EngineOptions = durable.Options{
		Owner: "crashed-compactor", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(point durable.FaultPoint, fault durable.FaultContext) {
			if point == durable.FaultAfterEffect && strings.Contains(fault.StepID, "-compact-") {
				panic("lost compaction response")
			}
		},
	}
	crashed, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	job, err := crashed.Submit(t.Context(), "read until context compacts")
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("fault injector did not interrupt compaction")
			}
		}()
		_, _ = crashed.Resume(t.Context(), job.ID)
	}()
	if err := crashed.Close(); err != nil {
		t.Fatal(err)
	}
	_, callsBeforeRecovery, compactionsBeforeRecovery := client.snapshot()
	if compactionsBeforeRecovery != 1 {
		t.Fatalf("compaction calls before recovery = %d", compactionsBeforeRecovery)
	}
	time.Sleep(160 * time.Millisecond)

	recovery, err := Open(testConfig(database, workspace, client, false, policy))
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	result, err := recovery.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrUnknown) || result.Job.Status != durable.JobUnknown {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	_, callsAfterRecovery, compactionsAfterRecovery := client.snapshot()
	if callsAfterRecovery != callsBeforeRecovery || compactionsAfterRecovery != compactionsBeforeRecovery {
		t.Fatalf("opaque compaction replayed: before normal=%d compact=%d, after normal=%d compact=%d",
			callsBeforeRecovery, compactionsBeforeRecovery, callsAfterRecovery, compactionsAfterRecovery)
	}
}

func TestRunnerResumesFromCommittedCompaction(t *testing.T) {
	workspace := compactionWorkspace(t)
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &loopingClient{normalRounds: 3}
	policy := Policy{MaxIterations: 10, ModelContextWindow: 3200, AutoCompactTokenLimit: 2400}
	first, err := Open(testConfig(database, workspace, client, false, policy))
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "resume after compact commit")
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	for range 30 {
		advanced, advanceErr := first.AdvanceOne(t.Context(), job.ID)
		if advanceErr != nil {
			t.Fatal(advanceErr)
		}
		job = advanced.Job
		if tail := job.Steps[len(job.Steps)-1]; tail.Tool == compactionToolName && tail.Status == durable.StepSucceeded {
			committed = true
			break
		}
	}
	if !committed {
		t.Fatalf("compaction did not commit: %#v", job.Steps)
	}
	_, normalBefore, compactBefore := client.snapshot()
	if normalBefore != 3 || compactBefore != 1 {
		t.Fatalf("before restart normal=%d compact=%d", normalBefore, compactBefore)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := Open(testConfig(database, workspace, client, false, policy))
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	result, err := resumed.Resume(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, normalAfter, compactAfter := client.snapshot()
	if result.Job.Status != durable.JobSucceeded || result.Output != "finished after compaction" ||
		result.CompactionCalls != 1 || normalAfter != 4 || compactAfter != 1 {
		t.Fatalf("result = %#v, calls normal=%d compact=%d", result, normalAfter, compactAfter)
	}
}

func TestRunnerFailsWhenSingleRoundCannotFitOrCompact(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte(strings.Repeat("oversized evidence\n", 800)), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &loopingClient{normalRounds: 1}
	policy := Policy{MaxIterations: 3, ModelContextWindow: 3200, AutoCompactTokenLimit: 2400}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, policy)

	result, err := runner.Start(t.Context(), "read one oversized result")
	if !errors.Is(err, ErrContextBudget) || result.Job.Status != durable.JobFailed {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	_, normalCalls, compactCalls := client.snapshot()
	if normalCalls != 1 || compactCalls != 0 || result.CompactionCalls != 0 {
		t.Fatalf("calls normal=%d compact=%d, result=%#v", normalCalls, compactCalls, result)
	}
}

func TestCompactionPrepareRejectsChangedSource(t *testing.T) {
	workspace := t.TempDir()
	client := &loopingClient{}
	policy := Policy{MaxIterations: 10, ModelContextWindow: 3200, AutoCompactTokenLimit: 2400}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, policy)
	messages := syntheticToolRounds(4, strings.Repeat("source evidence ", 120))
	step, planned, err := runner.compactionStep("agent-test", 4, messages)
	if err != nil || !planned {
		t.Fatalf("planned = %v, err = %v", planned, err)
	}
	input, err := decodeCompactionInput(step.Input)
	if err != nil {
		t.Fatal(err)
	}
	changed := "changed after planning"
	input.Messages[len(input.Messages)-1].Content = &changed
	tampered, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.compact.Prepare(t.Context(), tampered); !errors.Is(err, durable.ErrConflict) {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestCompactionStepUsesConfiguredSummaryLimit(t *testing.T) {
	workspace := t.TempDir()
	policy := Policy{
		MaxIterations: 10, ModelContextWindow: 3200, AutoCompactTokenLimit: 2400,
		CompactionSummaryMaxChars: 37,
	}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, &loopingClient{}, false, policy)
	step, planned, err := runner.compactionStep("agent-test", 4, syntheticToolRounds(4, strings.Repeat("evidence ", 120)))
	if err != nil || !planned {
		t.Fatalf("planned = %v, err = %v", planned, err)
	}
	input, err := decodeCompactionInput(step.Input)
	if err != nil {
		t.Fatal(err)
	}
	if input.MaxSummaryRunes <= 0 || input.MaxSummaryRunes > policy.CompactionSummaryMaxChars {
		t.Fatalf("summary limit = %d, configured = %d", input.MaxSummaryRunes, policy.CompactionSummaryMaxChars)
	}
}

func TestCompactionRetiresPriorTurnsButKeepsLatestUserInput(t *testing.T) {
	workspace := t.TempDir()
	policy := Policy{
		MaxIterations: 10, ModelContextWindow: 4200, AutoCompactTokenLimit: 1800,
		CompactionSummaryMaxChars: 1200,
	}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, &loopingClient{}, false, policy)
	system := "Use tools."
	firstUser := strings.Repeat("first turn evidence ", 180)
	firstAnswer := "first turn final answer"
	latestUser := "continue using the first turn"
	messages := []llm.Message{
		{Role: "system", Content: &system},
		{Role: "user", Content: &firstUser},
		{Role: "assistant", Content: &firstAnswer},
		{Role: "user", Content: &latestUser},
	}
	for index := 1; index <= 3; index++ {
		id := "current-call-" + strconv.Itoa(index)
		messages = append(messages, toolCallMessage(id, "read_file", `{"path":"note.txt"}`))
		output := strings.Repeat("current evidence ", 40)
		messages = append(messages, llm.Message{Role: "tool", ToolCallID: id, Content: &output})
	}
	step, planned, err := runner.compactionStep("multi-turn", 4, messages)
	if err != nil || !planned {
		t.Fatalf("planned=%t err=%v", planned, err)
	}
	input, err := decodeCompactionInput(step.Input)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := parseDurableContext(input.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if input.RetireThrough < 4 || layout.task.String() != latestUser {
		t.Fatalf("retire_through=%d task=%q", input.RetireThrough, layout.task.String())
	}
	compacted := assembleCompactedMessages(input, layout, "prior turn summary")
	if len(compacted) < 3 || compacted[2].Role != "user" || compacted[2].String() != latestUser {
		t.Fatalf("compacted messages = %#v", compacted)
	}
	if strings.Contains(messagesText(compacted), firstUser) || !strings.Contains(messagesText(compacted), "prior turn summary") {
		t.Fatalf("compacted transcript = %#v", compacted)
	}
	if _, err := parseDurableContext(compacted); err != nil {
		t.Fatalf("compacted transcript cannot seed the next boundary: %v", err)
	}
}

func TestFitSummaryRejectsRatherThanTruncates(t *testing.T) {
	task := "preserve the complete summary"
	input := compactionInput{
		SummaryVersion: 1, SourceDigest: strings.Repeat("a", 64), MaxSummaryRunes: 5,
		Policy: Policy{ModelContextWindow: 1000, CompactionSummaryMaxChars: 5},
	}
	tool := &compactionTool{}
	if _, err := tool.fitSummary(input, durableContextLayout{task: llm.Message{Role: "user", Content: &task}}, "123456"); err == nil || !strings.Contains(err.Error(), "拒绝静默截断") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenRequiresSummaryLimitWhenCompactionIsEnabled(t *testing.T) {
	config := testConfig(filepath.Join(t.TempDir(), "jobs.db"), t.TempDir(), &loopingClient{}, false, Policy{})
	config.Policy.AutoCompactTokenLimit = 1000
	config.Policy.CompactionSummaryMaxChars = 0
	if _, err := Open(config); err == nil || !strings.Contains(err.Error(), "CompactionSummaryMaxChars") {
		t.Fatalf("error = %v", err)
	}
}

func compactionWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	content := strings.Repeat("0123456789abcdef\n", 120)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func syntheticToolRounds(count int, output string) []llm.Message {
	system, prompt := "Use tools.", "Inspect evidence."
	messages := []llm.Message{{Role: "system", Content: &system}, {Role: "user", Content: &prompt}}
	for index := 1; index <= count; index++ {
		id := "call-" + strconv.Itoa(index)
		messages = append(messages, toolCallMessage(id, "read_file", `{"path":"note.txt"}`))
		copy := output
		messages = append(messages, llm.Message{Role: "tool", ToolCallID: id, Content: &copy})
	}
	return messages
}

func messagesText(messages []llm.Message) string {
	var text strings.Builder
	for _, message := range messages {
		text.WriteString(message.String())
		text.WriteByte('\n')
	}
	return text.String()
}
