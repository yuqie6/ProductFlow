package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
)

func TestAgentEvalStateL2Live(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_AGENT_EVALS_L2") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_AGENT_EVALS_L2=1 to run L2 Go+PG+Pi evals")
	}
	if strings.TrimSpace(os.Getenv("AGENT_PROVIDER_API_KEY")) == "" {
		t.Fatal("AGENT_PROVIDER_API_KEY is required for L2 agent evals")
	}
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l2")
	if err != nil {
		t.Fatal(err)
	}
	tasks = FilterEvalTasks(tasks, os.Getenv("PRODUCTFLOW_AGENT_EVAL_FILTER"))
	if len(tasks) == 0 {
		t.Fatal("no L2 tasks matched PRODUCTFLOW_AGENT_EVAL_FILTER")
	}
	k := evalTrialCountFromEnv()
	gw := &liveGateway{}
	as := newAgentServer(t, gw, gopgPiInternalToken)
	seedAgentProviderFromEnv(t, as)
	dataRoot := t.TempDir()
	pi := spawnPiAgentWithEnv(t, dataRoot, as.srv.URL, "", gopgPiInternalToken, map[string]string{
		"AGENT_MAX_ITERATIONS":           "12",
		"AGENT_PROVIDER_REQUEST_TIMEOUT": "180s",
		"PRODUCTFLOW_REQUEST_TIMEOUT":    "180s",
		"AGENT_QUESTION_TIMEOUT":         "8s",
		"AGENT_MAX_CONCURRENT_TURNS":     "1",
		"AGENT_PROVIDER_API_KEY":         os.Getenv("AGENT_PROVIDER_API_KEY"),
		"AGENT_PROVIDER_MODEL":           os.Getenv("AGENT_PROVIDER_MODEL"),
		"AGENT_PROVIDER_BASE_URL":        os.Getenv("AGENT_PROVIDER_BASE_URL"),
	})
	gw.set(HTTPGateway{BaseURL: pi.baseURL, Token: gopgPiInternalToken, ReadTimeout: 3 * time.Minute})

	runID := newEvalRunID()
	storageRoot, err := config.ResolveStorageRoot(os.Getenv("STORAGE_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(storageRoot, "agent-evals", runID)
	transcriptDir := filepath.Join(runDir, "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeEvalRunJSON(runDir, runID, k, tasks, pi.baseURL); err != nil {
		t.Fatal(err)
	}
	trialsPath := filepath.Join(runDir, "trials.jsonl")
	trialsFile, err := os.OpenFile(trialsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer trialsFile.Close()

	var failed int
	for _, task := range tasks {
		world := worlds[task.World]
		for trial := 1; trial <= k; trial++ {
			record := runL2Trial(t, as, pi, task, world, runID, trial, transcriptDir)
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := trialsFile.Write(append(raw, '\n')); err != nil {
				t.Fatal(err)
			}
			if !record["passed"].(bool) {
				failed++
				t.Errorf("%s trial %d: %v", task.ID, trial, record["errors"])
			}
		}
	}
	t.Logf("L2 run_id=%s dir=%s tasks=%d k=%d failed_trials=%d", runID, runDir, len(tasks), k, failed)
}

func runL2Trial(t *testing.T, as *agentServer, pi piAgentProc, task EvalTask, world EvalWorld, runID string, trial int, transcriptDir string) map[string]any {
	t.Helper()
	started := time.Now().UTC()
	utterance := task.Utterances[(trial-1)%len(task.Utterances)]
	errors := []string{}
	seeded := seededEvalWorld{}
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				errors = append(errors, fmt.Sprintf("panic: %v", recovered))
			}
		}()
		seeded = seedEvalWorld(t, as, task, world)
	}()
	status := "failed"
	var toolNames []string
	var toolCalls []map[string]any
	var turnID string
	if len(errors) == 0 {
		revision := 0
		if seeded.ProductID != "" && seeded.GraphID != "" {
			live, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
			if err != nil {
				errors = append(errors, err.Error())
			} else {
				revision = live.Revision
			}
		}
		if len(errors) == 0 {
			body := map[string]any{
				"input_text":      utterance,
				"idempotency_key": clockid.New(),
				"page_context":    evalPageContext(task, seeded, revision),
			}
			if len(selectedAssetIDs(task)) > 0 {
				body["asset_ids"] = remapIDs(selectedAssetIDs(task), seeded.AssetIDs)
			}
			resp := as.doJSON(t, http.MethodPost, evalTurnCollectionPath(seeded), body)
			if resp.StatusCode != http.StatusAccepted {
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				errors = append(errors, fmt.Sprintf("start turn HTTP %d %s", resp.StatusCode, raw))
			} else {
				var submitted SubmitTurnResponse
				as.decode(t, resp, &submitted)
				turnID = submitted.Turn.ID
				if submitted.Turn.HarnessTurnID == nil {
					errors = append(errors, "missing harness turn id")
				} else {
					agentState, waitErr := waitAgentTurnAnyTerminal(t, pi.baseURL, seeded.ConvID, *submitted.Turn.HarnessTurnID, 3*time.Minute, pi)
					if waitErr != nil {
						errors = append(errors, waitErr.Error())
					} else {
						status, _ = agentState["status"].(string)
						synced := evalSyncTurn(t, as, seeded, submitted.Turn.ID)
						status = synced.Status
						toolNames, toolCalls = toolCallsFromSteps(synced.ToolSteps)
					}
				}
			}
		}
	}
	if len(errors) == 0 {
		errors = append(errors, gradeEvalTools(toolNames, task.Expect.Tools)...)
		errors = append(errors, gradeEvalState(t, as, seeded, task.Expect.State)...)
		if !containsString(task.Expect.Terminal, status) {
			errors = append(errors, fmt.Sprintf("terminal %s not in %v", status, task.Expect.Terminal))
		}
	}
	durationMS := time.Since(started).Seconds() * 1000
	relative := filepath.Join("transcripts", fmt.Sprintf("%s-%d.json", task.ID, trial))
	transcript := map[string]any{
		"schema_version":  1,
		"run_id":          runID,
		"task_id":         task.ID,
		"trial":           trial,
		"utterance":       utterance,
		"terminal_status": status,
		"tool_calls":      toolCalls,
		"errors":          errors,
		"product_id":      seeded.ProductID,
		"graph_id":        seeded.GraphID,
		"conversation_id": seeded.ConvID,
		"turn_id":         turnID,
	}
	raw, _ := json.MarshalIndent(transcript, "", "  ")
	_ = os.WriteFile(filepath.Join(filepath.Dir(transcriptDir), relative), append(raw, '\n'), 0o644)
	var terminal any
	if status != "" {
		terminal = status
	}
	return map[string]any{
		"schema_version":  1,
		"run_id":          runID,
		"layer":           "l2",
		"task_id":         task.ID,
		"skill":           task.Skill,
		"suite":           task.Suite,
		"trial":           trial,
		"utterance":       utterance,
		"started_at":      started.Format(time.RFC3339Nano),
		"duration_ms":     durationMS,
		"status":          status,
		"passed":          len(errors) == 0,
		"errors":          errors,
		"terminal":        terminal,
		"tool_calls":      toolCalls,
		"token_count":     nil,
		"transcript_path": relative,
	}
}

func waitAgentTurnAnyTerminal(t *testing.T, agentURL, convID, harnessTurnID string, timeout time.Duration, agent piAgentProc) (map[string]any, error) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, agentURL+"/internal/v1/conversations/"+convID+"/turns/"+harnessTurnID, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+gopgPiInternalToken)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			last = map[string]any{}
			if json.Unmarshal(raw, &last) == nil {
				status, _ := last["status"].(string)
				switch status {
				case "succeeded", "failed", "canceled", "unknown", "requires_input", "awaiting_confirmation":
					return last, nil
				}
			}
		} else if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	return last, fmt.Errorf("Agent turn %s did not reach a terminal status last=%v\n%s", harnessTurnID, last, agent.logs())
}

func toolCallsFromSteps(steps []map[string]any) ([]string, []map[string]any) {
	var names []string
	var calls []map[string]any
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, step := range steps {
		name, _ := step["tool_name"].(string)
		if name == "" || name == "productflow_context_injection" {
			continue
		}
		names = append(names, name)
		calls = append(calls, map[string]any{"name": name, "params": map[string]any{}, "ts": now})
	}
	return names, calls
}

func evalTrialCountFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("PRODUCTFLOW_AGENT_EVAL_TRIALS"))
	if raw == "" {
		return 3
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 3
	}
	return n
}

func newEvalRunID() string {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	return stamp + "-" + clockid.New()[:8]
}

func writeEvalRunJSON(runDir, runID string, k int, tasks []EvalTask, agentURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(agentURL + "/healthz")
	if err != nil {
		return fmt.Errorf("read eval Agent identity: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("read eval Agent identity: HTTP %d", resp.StatusCode)
	}
	var identity struct {
		HarnessHash string `json:"harness_hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		return err
	}
	digest, err := hex.DecodeString(identity.HarnessHash)
	if err != nil || len(digest) != 32 || identity.HarnessHash != strings.ToLower(identity.HarnessHash) {
		return fmt.Errorf("eval Agent returned invalid harness_hash")
	}
	commit := "unknown"
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}
	sum := sha256.New()
	for _, task := range tasks {
		sum.Write([]byte(task.ID))
		sum.Write([]byte{0})
	}
	meta := map[string]any{
		"schema_version": 1,
		"run_id":         runID,
		"created_at":     time.Now().UTC().Format(time.RFC3339),
		"commit":         commit,
		"harness_hash":   identity.HarnessHash,
		"task_set_hash":  hex.EncodeToString(sum.Sum(nil)),
		"model":          os.Getenv("AGENT_PROVIDER_MODEL"),
		"trials":         k,
		"layers":         []string{"l2"},
		"task_count":     len(tasks),
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, "run.json"), append(raw, '\n'), 0o644)
}
