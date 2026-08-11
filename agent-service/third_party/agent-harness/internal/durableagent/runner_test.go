package durableagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/internal/skills"
)

type scriptedClient struct {
	mu           sync.Mutex
	responses    []llm.Message
	err          error
	calls        [][]llm.Message
	catalogs     [][]llm.Tool
	firstStarted chan struct{}
	releaseFirst <-chan struct{}
}

type storedScriptedClient struct {
	mu            sync.Mutex
	response      llm.Message
	createCalls   int
	retrieveCalls int
	ambiguous     bool
}

func (c *storedScriptedClient) Chat(context.Context, []llm.Message, []llm.Tool) (llm.Message, string, error) {
	return llm.Message{}, "", errors.New("unexpected non-stored model call")
}

func (c *storedScriptedClient) CreateStored(context.Context, []llm.Message, []llm.Tool, string) (llm.StoredResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.createCalls++
	if c.ambiguous {
		return llm.StoredResponse{}, &llm.RequestError{Operation: "create", Ambiguous: true, Err: errors.New("connection lost")}
	}
	return llm.StoredResponse{ID: "resp_stored", Status: "queued"}, nil
}

func (c *storedScriptedClient) RetrieveStored(context.Context, string) (llm.StoredResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.retrieveCalls++
	return llm.StoredResponse{ID: "resp_stored", Status: "completed", Message: c.response, Complete: true}, nil
}

func (c *storedScriptedClient) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.createCalls, c.retrieveCalls
}

func (c *scriptedClient) Chat(ctx context.Context, messages []llm.Message, catalog []llm.Tool) (llm.Message, string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, append([]llm.Message(nil), messages...))
	c.catalogs = append(c.catalogs, append([]llm.Tool(nil), catalog...))
	callNumber := len(c.calls)
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return llm.Message{}, "", err
	}
	if len(c.responses) == 0 {
		c.mu.Unlock()
		return llm.Message{}, "", errors.New("unexpected model call")
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	started, release := c.firstStarted, c.releaseFirst
	c.mu.Unlock()
	if callNumber == 1 && started != nil {
		close(started)
	}
	if callNumber == 1 && release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return llm.Message{}, "", ctx.Err()
		}
	}
	return response, "completed", nil
}

func (c *scriptedClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func TestRunnerJournalsModelReadAndFinalAnswer(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("durable hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-read", "read_file", `{"path":"note.txt"}`),
		textMessage("读取完成"),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{})
	result, err := runner.Start(t.Context(), "读取 note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Kind != durable.JobKindAgentTurn || result.Job.Status != durable.JobSucceeded ||
		result.Output != "读取完成" || result.ModelCalls != 2 || result.ToolCalls != 1 {
		t.Fatalf("result = %#v", result)
	}
	wantTools := []string{modelToolName, "read_file", modelToolName}
	for index, want := range wantTools {
		if result.Job.Steps[index].Tool != want {
			t.Fatalf("step %d tool = %s, want %s", index, result.Job.Steps[index].Tool, want)
		}
	}
	if client.callCount() != 2 || len(client.catalogs[0]) != 6 ||
		!catalogHas(client.catalogs[0], "read_file") || !catalogHas(client.catalogs[0], "ask_user") ||
		!catalogHas(client.catalogs[0], "list_dir") || !catalogHas(client.catalogs[0], "find_files") ||
		!catalogHas(client.catalogs[0], "search_text") || !catalogHas(client.catalogs[0], skills.LoadToolName) {
		t.Fatalf("model calls = %d, catalog = %#v", client.callCount(), client.catalogs)
	}
	if got := client.calls[1][len(client.calls[1])-1].String(); !strings.Contains(got, "durable hello") {
		t.Fatalf("tool result sent to model = %q", got)
	}
}

func TestRunnerJournalsStructuredSearchAndInjectsInstructions(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("durable needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-search", "search_text", `{"pattern":"needle"}`),
		textMessage("搜索完成"),
	}}
	config := testConfig(filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{})
	config.Instructions = []string{"Repository rule: verify evidence."}
	runner, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	result, err := runner.Start(t.Context(), "搜索 needle")
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != durable.JobSucceeded || result.Job.Steps[1].Tool != "search_text" {
		t.Fatalf("result = %#v", result)
	}
	if got := client.calls[0][0].String(); !strings.Contains(got, "Repository rule: verify evidence.") {
		t.Fatalf("system instruction = %q", got)
	}
	if got := client.calls[1][len(client.calls[1])-1].String(); !strings.Contains(got, "note.txt") || !strings.Contains(got, "durable needle") {
		t.Fatalf("search result = %q", got)
	}
}

func TestRunnerResumeReusesCommittedModelDecision(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("once\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-read", "read_file", `{"path":"note.txt"}`),
		textMessage("done"),
	}}
	first, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "read once")
	if err != nil {
		t.Fatal(err)
	}
	job, err = first.engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != durable.JobAwaitingSteps || client.callCount() != 1 {
		t.Fatalf("boundary job = %#v, calls = %d, err = %v", job, client.callCount(), err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	result, err := resumed.Resume(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "done" || result.Job.Status != durable.JobSucceeded || client.callCount() != 2 {
		t.Fatalf("resumed result = %#v, calls = %d", result, client.callCount())
	}
}

func TestRunnerRefusesLegacyModelDecisionWithoutToolContract(t *testing.T) {
	workspace := t.TempDir()
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-read", "read_file", `{"path":"note.txt"}`),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{})
	content := "read the note"
	step, err := runner.modelStep("legacy-tool-contract", 1, []llm.Message{{Role: "user", Content: &content}})
	if err != nil {
		t.Fatal(err)
	}
	input, err := decodeModelInput(step.Input)
	if err != nil {
		t.Fatal(err)
	}
	input.ToolContractSHA256 = ""
	step.Input, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	job, err := runner.engine.Submit(t.Context(), durable.JobSpec{
		ID: "legacy-tool-contract", Kind: durable.JobKindAgentTurn, Name: "legacy contract", Steps: []durable.StepSpec{step},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = runner.engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != durable.JobAwaitingSteps || len(job.Steps) != 1 {
		t.Fatalf("legacy decision = %#v, err = %v", job, err)
	}
	result, err := runner.AdvanceOne(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrConflict) || result.Job.Status != durable.JobAwaitingSteps || len(result.Job.Steps) != 1 {
		t.Fatalf("legacy advance = %#v, err = %v", result, err)
	}
}

func TestRunnerRefusesContractDriftBeforePendingToolRuns(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-read", "read_file", `{"path":"missing.txt"}`),
	}}
	runner, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	job, err := runner.Submit(t.Context(), "read the missing file")
	if err != nil {
		t.Fatal(err)
	}
	modelBoundary, err := runner.AdvanceOne(t.Context(), job.ID)
	if err != nil || modelBoundary.Job.Status != durable.JobAwaitingSteps || len(modelBoundary.Job.Steps) != 1 {
		t.Fatalf("model boundary = %#v, err = %v", modelBoundary, err)
	}
	toolBoundary, err := runner.AdvanceOne(t.Context(), job.ID)
	if err != nil || toolBoundary.Job.Status != durable.JobPending || len(toolBoundary.Job.Steps) != 2 {
		t.Fatalf("tool boundary = %#v, err = %v", toolBoundary, err)
	}

	runner.model.toolContractSHA256 = strings.Repeat("f", sha256.Size*2)
	result, err := runner.AdvanceOne(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrConflict) || result.Progressed || result.Job.Status != durable.JobPending {
		t.Fatalf("drifted result = %#v, err = %v", result, err)
	}
	if len(result.Job.Steps) != 2 || result.Job.Steps[1].Status != durable.StepPending || client.callCount() != 1 {
		t.Fatalf("drifted job = %#v, model calls = %d", result.Job, client.callCount())
	}
}

func TestRunnerUsesReconcilableEditOnlyWhenEnabled(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "value.txt")
	before := []byte("before\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(before)
	readArgs := `{"path":"value.txt","include_metadata":true}`
	editArgs, _ := json.Marshal(map[string]string{
		"path": "value.txt", "expected_sha256": hex.EncodeToString(digest[:]),
		"old_text": "before", "new_text": "after",
	})
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-read", "read_file", readArgs),
		toolCallMessage("call-edit", "edit_file", string(editArgs)),
		textMessage("edited"),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, true, Policy{})
	result, err := runner.Start(t.Context(), "修改 value.txt")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "after\n" {
		t.Fatalf("content = %q, err = %v", content, err)
	}
	if result.Job.Status != durable.JobSucceeded || result.ModelCalls != 3 || result.ToolCalls != 2 {
		t.Fatalf("result = %#v", result)
	}
	if len(client.catalogs[0]) != 7 || !catalogHas(client.catalogs[0], "edit_file") {
		t.Fatalf("catalog = %#v", client.catalogs[0])
	}
}

func TestRunnerLoadsDiscoveredSkillAsJournaledPureStep(t *testing.T) {
	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, ".agents", "skills", "review-code")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: review-code\ndescription: Review code changes safely.\n---\n# Review\n\nCheck the diff first.\n"
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	arguments, _ := json.Marshal(map[string]string{"name": "review-code"})
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-skill", skills.LoadToolName, string(arguments)),
		textMessage("reviewed"),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{SkillCatalogMaxBytes: 8000})
	result, err := runner.Start(t.Context(), "$review-code inspect changes")
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != durable.JobSucceeded || result.Job.Steps[1].Tool != skills.LoadToolName {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(client.calls[0][0].String(), `"name":"review-code"`) {
		t.Fatalf("skill metadata missing from initial model call: %#v", client.calls[0])
	}
	if !strings.Contains(client.calls[1][len(client.calls[1])-1].String(), "Check the diff first") {
		t.Fatalf("skill content missing from second model call: %#v", client.calls[1])
	}
}

func TestRunnerTurnsUnexposedEditIntoToolError(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "value.txt")
	if err := os.WriteFile(path, []byte("unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-edit", "edit_file", `{"path":"value.txt","expected_sha256":"absent","old_text":"","new_text":"changed"}`),
		textMessage("edit unavailable"),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{})
	result, err := runner.Start(t.Context(), "try edit")
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "unchanged\n" {
		t.Fatalf("content = %q, err = %v", content, readErr)
	}
	if result.Job.Steps[1].Tool != rejectionToolName || !strings.Contains(client.calls[1][len(client.calls[1])-1].String(), "不可用") {
		t.Fatalf("steps = %#v, second request = %#v", result.Job.Steps, client.calls[1])
	}
}

func TestRunnerStopsBeforeExecutingToolRoundBeyondLimit(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("limit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-1", "read_file", `{"path":"note.txt"}`),
		toolCallMessage("call-2", "read_file", `{"path":"note.txt"}`),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{MaxIterations: 1})
	result, err := runner.Start(t.Context(), "keep reading")
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("error = %v", err)
	}
	if result.Job.Status != durable.JobFailed || result.ToolCalls != 1 || client.callCount() != 2 {
		t.Fatalf("result = %#v, calls = %d", result, client.callCount())
	}
	if !strings.Contains(result.Job.Error, "最大工具迭代次数 1") {
		t.Fatalf("job error = %q", result.Job.Error)
	}
}

func TestRunnerDoesNotReplayModelRequestAfterLostResult(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &scriptedClient{responses: []llm.Message{textMessage("possibly billed")}}
	config := testConfig(database, workspace, client, false, Policy{})
	config.EngineOptions = durable.Options{
		Owner: "crashed-model-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(point durable.FaultPoint, _ durable.FaultContext) {
			if point == durable.FaultAfterEffect {
				panic("lost model result")
			}
		},
	}
	crashed, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	job, err := crashed.Submit(t.Context(), "one model call")
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("fault injector did not interrupt model step")
			}
		}()
		_, _ = crashed.Resume(t.Context(), job.ID)
	}()
	if err := crashed.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(160 * time.Millisecond)

	recovery, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	result, err := recovery.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrUnknown) || result.Job.Status != durable.JobUnknown {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if client.callCount() != 1 {
		t.Fatalf("opaque model request executed %d times", client.callCount())
	}
}

func TestRunnerMarksAmbiguousOpaqueCreateUnknownWithoutRetry(t *testing.T) {
	client := &scriptedClient{err: &llm.RequestError{
		Operation: "create", Ambiguous: true, Err: errors.New("connection lost"),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), t.TempDir(), client, false, Policy{})
	result, err := runner.Start(t.Context(), "ambiguous opaque create")
	if !errors.Is(err, durable.ErrUnknown) || result.Job.Status != durable.JobUnknown {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if client.callCount() != 1 {
		t.Fatalf("opaque model request executed %d times", client.callCount())
	}
}

func TestRunnerRecoversStoredModelResponseFromAttemptCheckpoint(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &storedScriptedClient{response: textMessage("recovered stored response")}
	config := testConfig(database, workspace, client, false, Policy{})
	config.EngineOptions = durable.Options{
		Owner: "crashed-model-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(point durable.FaultPoint, _ durable.FaultContext) {
			if point == durable.FaultAfterEffect {
				panic("lost local model result")
			}
		},
	}
	crashed, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	job, err := crashed.Submit(t.Context(), "one stored model call")
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = crashed.Resume(t.Context(), job.ID)
	}()
	if err := crashed.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(160 * time.Millisecond)

	recovery, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	result, err := recovery.Resume(t.Context(), job.ID)
	if err != nil || result.Job.Status != durable.JobSucceeded || result.Output != "recovered stored response" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	creates, retrieves := client.counts()
	if creates != 1 || retrieves != 2 {
		t.Fatalf("stored calls = create:%d retrieve:%d", creates, retrieves)
	}
}

func TestRunnerMarksAmbiguousStoredCreateUnknownWithoutRetry(t *testing.T) {
	client := &storedScriptedClient{ambiguous: true}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), t.TempDir(), client, false, Policy{})
	result, err := runner.Start(t.Context(), "ambiguous create")
	if !errors.Is(err, durable.ErrUnknown) || result.Job.Status != durable.JobUnknown {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	creates, retrieves := client.counts()
	if creates != 1 || retrieves != 0 {
		t.Fatalf("stored calls = create:%d retrieve:%d", creates, retrieves)
	}
}

func TestModelToolAcceptsLegacyStoredProviderSnapshotWithoutResponseMode(t *testing.T) {
	legacy := ProviderSnapshot{WireAPI: "responses", BaseURL: "https://provider.example/v1", Model: "model"}
	policy := Policy{MaxIterations: 50, ModelContextWindow: 24000}
	tool, err := newModelTool(&scriptedClient{}, "/workspace", normalizeProvider(legacy), policy, nil, "contract", true, editModeReadOnly, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	user := "resume legacy task"
	if err := tool.validate(modelInput{
		Version: protocolVersion, Workspace: "/workspace", Provider: legacy, Policy: policy,
		Messages: []llm.Message{{Role: "user", Content: &user}},
	}); err != nil {
		t.Fatalf("legacy provider snapshot was rejected: %v", err)
	}
}

func TestModelToolRejectsLegacyInputWhenCurrentCatalogAddsExternalEffect(t *testing.T) {
	provider := normalizeProvider(ProviderSnapshot{
		WireAPI: "responses", BaseURL: "https://provider.example/v1", Model: "model",
	})
	policy := Policy{MaxIterations: 50, ModelContextWindow: 24000}
	catalog := contractTestCatalog("set_business_state")
	legacyCompatible, err := legacyToolEffectsCompatible(
		catalog, contractTestTools("set_business_state", durable.EffectOpaque),
	)
	if err != nil {
		t.Fatal(err)
	}
	if legacyCompatible {
		t.Fatal("external effect was classified as legacy-compatible")
	}
	tool, err := newModelTool(
		&scriptedClient{}, "/workspace", provider, policy, catalog, "contract", legacyCompatible, editModeReadOnly, 0, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	user := "resume legacy task"
	err = tool.validate(modelInput{
		Version: protocolVersion, Workspace: "/workspace", Provider: provider, Policy: policy,
		Messages: []llm.Message{{Role: "user", Content: &user}}, Tools: catalog,
	})
	if !errors.Is(err, durable.ErrConflict) {
		t.Fatalf("legacy external-effect input error = %v", err)
	}
}

func TestRunnerRefusesProviderDriftBeforeContinuing(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &scriptedClient{responses: []llm.Message{textMessage("original")}}
	first, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "provider drift")
	if err != nil {
		t.Fatal(err)
	}
	job, err = first.engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != durable.JobAwaitingSteps {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	driftedConfig := testConfig(database, workspace, client, false, Policy{})
	driftedConfig.Provider.Model = "different-model"
	drifted, err := Open(driftedConfig)
	if err != nil {
		t.Fatal(err)
	}
	result, err := drifted.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrConflict) || result.Job.Status != durable.JobAwaitingSteps {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if client.callCount() != 1 {
		t.Fatalf("model called after provider drift: %d", client.callCount())
	}
	if err := drifted.Close(); err != nil {
		t.Fatal(err)
	}
	correct, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	defer correct.Close()
	result, err = correct.Resume(t.Context(), job.ID)
	if err != nil || result.Output != "original" || result.Job.Status != durable.JobSucceeded {
		t.Fatalf("correct resume result = %#v, err = %v", result, err)
	}
}

func TestRunnerRefusesProviderDriftBeforePendingModelCall(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &scriptedClient{responses: []llm.Message{textMessage("correct")}}
	first, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "pending provider drift")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	driftedConfig := testConfig(database, workspace, client, false, Policy{})
	driftedConfig.Provider.Model = "different-model"
	drifted, err := Open(driftedConfig)
	if err != nil {
		t.Fatal(err)
	}
	result, err := drifted.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrConflict) || result.Job.Status != durable.JobPending || client.callCount() != 0 {
		t.Fatalf("result = %#v, calls = %d, err = %v", result, client.callCount(), err)
	}
	if err := drifted.Close(); err != nil {
		t.Fatal(err)
	}

	correct, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	defer correct.Close()
	result, err = correct.Resume(t.Context(), job.ID)
	if err != nil || result.Output != "correct" || result.Job.Status != durable.JobSucceeded || client.callCount() != 1 {
		t.Fatalf("correct resume result = %#v, calls = %d, err = %v", result, client.callCount(), err)
	}
}

func openRunner(
	t *testing.T,
	database string,
	workspace string,
	client ChatClient,
	allowEdit bool,
	policy Policy,
) *Runner {
	t.Helper()
	runner, err := Open(testConfig(database, workspace, client, allowEdit, policy))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	return runner
}

func testConfig(database, workspace string, client ChatClient, allowEdit bool, policy Policy) Config {
	if policy.MaxIterations == 0 {
		policy.MaxIterations = 50
	}
	if policy.ModelContextWindow == 0 {
		policy.ModelContextWindow = 24000
	}
	if policy.AutoCompactTokenLimit > 0 && policy.CompactionSummaryMaxChars == 0 {
		policy.CompactionSummaryMaxChars = legacySummaryMaxChars
	}
	return Config{
		Database: database, Workspace: workspace, Client: client, AllowEdit: allowEdit, Policy: policy,
		StoredResponseTimeout: time.Second,
		Provider:              ProviderSnapshot{WireAPI: "responses", BaseURL: "https://provider.example/v1", Model: "test-model"},
		System:                "Use the available tools.", SkillUserHome: workspace,
	}
}

func toolCallMessage(id, name, arguments string) llm.Message {
	encodedArguments, _ := json.Marshal(arguments)
	return llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
		ID: id, Type: "function", Function: llm.ToolCallFunction{Name: name, Arguments: encodedArguments},
	}}}
}

func textMessage(content string) llm.Message {
	return llm.Message{Role: "assistant", Content: &content}
}

func countKind(events []durable.Event, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func catalogHas(catalog []llm.Tool, name string) bool {
	for _, tool := range catalog {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}
