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
	"sort"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/canonjson"
)

type l2AgentIdentity struct {
	HarnessHash string `json:"harness_hash"`
	SkillHash   string `json:"skill_catalog_hash"`
}

func l2Digest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && value == strings.ToLower(value)
}

func readL2AgentIdentity(agentURL string) (l2AgentIdentity, error) {
	var identity l2AgentIdentity
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(agentURL + "/healthz")
	if err != nil {
		return identity, fmt.Errorf("read eval Agent identity: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return identity, fmt.Errorf("read eval Agent identity: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		return identity, err
	}
	if !l2Digest(identity.HarnessHash) || !l2Digest(identity.SkillHash) {
		return identity, fmt.Errorf("eval Agent returned invalid harness or Skill catalog identity")
	}
	return identity, nil
}

func l2InputSnapshot(tasks []EvalTask, worlds map[string]EvalWorld) (map[string]any, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("L2 provenance requires selected tasks")
	}
	selected := map[string]EvalWorld{}
	ids := map[string]bool{}
	for _, task := range tasks {
		if task.ID == "" || ids[task.ID] {
			return nil, fmt.Errorf("L2 provenance has missing or duplicate task ID")
		}
		ids[task.ID] = true
		world, ok := worlds[task.World]
		if !ok {
			return nil, fmt.Errorf("L2 provenance has missing world for task %s", task.ID)
		}
		selected[task.World] = world
	}
	return map[string]any{"tasks": tasks, "worlds": selected}, nil
}

func l2GitProvenance(directory string) (map[string]any, error) {
	git := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = directory
		return cmd.Output()
	}
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("read L2 checkout root: %w", err)
	}
	directory = strings.TrimSpace(string(root))
	commit, err := git("rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read L2 commit: %w", err)
	}
	status, err := git("status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	names, err := git("ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	paths := strings.Split(strings.TrimSuffix(string(names), "\x00"), "\x00")
	sort.Strings(paths)
	files := map[string]string{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		full := filepath.Join(strings.TrimSpace(string(root)), path)
		stat, err := os.Lstat(full)
		if os.IsNotExist(err) {
			files[path] = "deleted"
			continue
		}
		if err != nil {
			return nil, err
		}
		sum := sha256.New()
		if stat.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil {
				return nil, err
			}
			sum.Write([]byte(target))
		} else if stat.Mode().IsRegular() {
			file, err := os.Open(full)
			if err != nil {
				return nil, err
			}
			_, readErr := io.Copy(sum, file)
			closeErr := file.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
		} else {
			return nil, fmt.Errorf("unsupported L2 checkout entry %s", path)
		}
		files[path] = fmt.Sprintf("%s:%x", stat.Mode().String(), sum.Sum(nil))
	}
	sha := strings.TrimSpace(string(commit))
	hash, err := canonjson.SHA256Hex(map[string]any{"commit": sha, "status": string(status), "files": files})
	if err != nil {
		return nil, err
	}
	return map[string]any{"commit": sha, "worktree_dirty": len(status) > 0, "worktree_hash": hash}, nil
}

func startL2Run(runDir, runID string, k int, tasks []EvalTask, worlds map[string]EvalWorld, agentURL string) (map[string]any, error) {
	if k < 1 {
		return nil, fmt.Errorf("L2 trial count must be positive")
	}
	identity, err := readL2AgentIdentity(agentURL)
	if err != nil {
		return nil, err
	}
	inputs, err := l2InputSnapshot(tasks, worlds)
	if err != nil {
		return nil, err
	}
	hash, err := canonjson.SHA256Hex(inputs)
	if err != nil {
		return nil, err
	}
	meta, err := l2GitProvenance("")
	if err != nil {
		return nil, err
	}
	for key, value := range map[string]any{
		"schema_version": 1, "provenance_version": "l2-content-v1", "run_id": runID,
		"created_at": time.Now().UTC().Format(time.RFC3339), "run_status": "incomplete",
		"harness_hash": identity.HarnessHash, "skill_catalog_hash": identity.SkillHash,
		"task_set_hash": hash, "input_snapshot": "inputs.json", "model": "unobserved",
		"provider_kind": "unobserved", "reasoning_effort": nil, "trials": k,
		"task_count": len(tasks), "layers": []string{"l2"},
	} {
		meta[key] = value
	}
	if err := writeL2JSON(filepath.Join(runDir, "run.json"), meta, true); err != nil {
		return nil, err
	}
	if err := writeL2JSON(filepath.Join(runDir, "inputs.json"), inputs, true); err != nil {
		return nil, err
	}
	return meta, nil
}

// Only allowlisted SDK configuration is exported; raw checkpoints may contain unrelated data.
type l2ModelConfiguration struct {
	SchemaVersion    int     `json:"schema_version"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	API              string  `json:"api"`
	BaseURLHash      string  `json:"base_url_hash"`
	ThinkingLevel    string  `json:"thinking_level"`
	ReasoningSummary *string `json:"reasoning_summary"`
	TextVerbosity    *string `json:"text_verbosity"`
	ServiceTier      *string `json:"service_tier"`
	ContextWindow    int     `json:"context_window"`
	MaxTokens        int     `json:"max_tokens"`
}

type l2RequestIdentity struct {
	RequestID     string               `json:"model_request_id"`
	HarnessHash   string               `json:"harness_hash"`
	SkillHash     string               `json:"skill_catalog_hash"`
	Configuration l2ModelConfiguration `json:"model_configuration"`
}

func readL2RequestIdentities(as *agentServer, projectionID string, meta map[string]any) ([]l2RequestIdentity, error) {
	rows, err := as.pool.Query(context.Background(), `SELECT c.payload_json::text, COALESCE(i.provider, ''), COALESCE(i.model, '')
		FROM agent_turn_checkpoints c LEFT JOIN agent_model_invocations i
		ON i.turn_projection_id = c.turn_projection_id AND i.model_request_id = c.payload_json::jsonb->>'model_request_id'
		WHERE c.turn_projection_id = $1 AND c.kind = 'before_model_request' ORDER BY c.created_at, c.id`, projectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := []l2RequestIdentity{}
	for rows.Next() {
		var raw, provider, model string
		if err := rows.Scan(&raw, &provider, &model); err != nil {
			return nil, err
		}
		var request l2RequestIdentity
		if err := json.Unmarshal([]byte(raw), &request); err != nil {
			return nil, err
		}
		if err := validateL2RequestIdentity(request, provider, model, meta); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("L2 provenance has no observed model request")
	}
	return requests, nil
}

func validateL2RequestIdentity(request l2RequestIdentity, provider, model string, meta map[string]any) error {
	c := request.Configuration
	if request.RequestID == "" || request.HarnessHash != meta["harness_hash"] || request.SkillHash != meta["skill_catalog_hash"] {
		return fmt.Errorf("L2 model request has missing or mismatched runtime identity")
	}
	if c.SchemaVersion != 1 || c.Provider == "" || c.Model == "" || c.Provider != provider || c.Model != model || c.API == "" || !l2Digest(c.BaseURLHash) || c.ContextWindow < 1 || c.MaxTokens < 1 {
		return fmt.Errorf("L2 model request has missing or mismatched SDK configuration")
	}
	if !containsString([]string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}, c.ThinkingLevel) {
		return fmt.Errorf("L2 model request has invalid SDK thinking level")
	}
	return nil
}

func attachL2TrialProvenance(as *agentServer, record map[string]any, runDir string, meta map[string]any) error {
	details := record["details"].(map[string]any)
	projectionID := details["turn_projection_id"].(string)
	var requests []l2RequestIdentity
	var identityErr error
	if projectionID == "" {
		identityErr = fmt.Errorf("L2 provenance has no Turn projection")
	} else {
		requests, identityErr = readL2RequestIdentities(as, projectionID, meta)
	}
	provenance := map[string]any{"valid": identityErr == nil, "model_requests": requests}
	details["provenance"] = provenance
	if identityErr != nil {
		record["passed"] = false
		record["errors"] = append(record["errors"].([]string), identityErr.Error())
		provenance["error"] = identityErr.Error()
	}
	path := filepath.Join(runDir, record["transcript_path"].(string))
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var transcript map[string]any
	if err := json.Unmarshal(raw, &transcript); err != nil {
		return err
	}
	transcript["provenance"] = provenance
	transcript["errors"] = record["errors"]
	return writeL2JSON(path, transcript, false)
}

func finishL2Run(runDir string, meta map[string]any, records []map[string]any, agentURL string) error {
	problems := []string{}
	inputBytes, inputErr := os.ReadFile(filepath.Join(runDir, "inputs.json"))
	var inputs map[string]json.RawMessage
	if inputErr == nil {
		inputErr = json.Unmarshal(inputBytes, &inputs)
	}
	inputHash, hashErr := canonjson.SHA256Hex(inputs)
	if inputErr != nil || hashErr != nil || inputHash != meta["task_set_hash"] {
		problems = append(problems, "L2 input snapshot changed or became unavailable")
	}
	var tasks []EvalTask
	if err := json.Unmarshal(inputs["tasks"], &tasks); err != nil {
		problems = append(problems, "L2 input snapshot has no task selection")
	}
	expected := map[string]bool{}
	for _, task := range tasks {
		for trial := 1; trial <= meta["trials"].(int); trial++ {
			expected[fmt.Sprintf("%s/%d", task.ID, trial)] = true
		}
	}
	identity, err := readL2AgentIdentity(agentURL)
	if err != nil || identity.HarnessHash != meta["harness_hash"] || identity.SkillHash != meta["skill_catalog_hash"] {
		problems = append(problems, "L2 runtime identity changed or became unavailable")
	}
	git, err := l2GitProvenance("")
	if err != nil || git["commit"] != meta["commit"] || git["worktree_hash"] != meta["worktree_hash"] {
		problems = append(problems, "L2 checkout changed or became unavailable")
	}
	if len(records) != meta["task_count"].(int)*meta["trials"].(int) {
		problems = append(problems, "L2 trial count is incomplete")
	}
	var configuration *l2ModelConfiguration
	requestCount := 0
	for _, record := range records {
		key := fmt.Sprintf("%s/%v", record["task_id"], record["trial"])
		if !expected[key] || record["run_id"] != meta["run_id"] {
			problems = append(problems, "L2 has duplicate, unknown or foreign trial identity")
		}
		delete(expected, key)
		provenance := record["details"].(map[string]any)["provenance"].(map[string]any)
		if record["terminal"] == nil || provenance["valid"] != true {
			problems = append(problems, fmt.Sprintf("L2 task %s trial %v lacks valid terminal or request identity", record["task_id"], record["trial"]))
			continue
		}
		for _, request := range provenance["model_requests"].([]l2RequestIdentity) {
			requestCount++
			if configuration == nil {
				current := request.Configuration
				configuration = &current
			}
			want, _ := canonjson.SHA256Hex(configuration)
			got, _ := canonjson.SHA256Hex(request.Configuration)
			if got != want {
				problems = append(problems, "L2 effective model configuration changed between requests")
			}
		}
	}
	if len(expected) != 0 {
		problems = append(problems, "L2 has missing trial identities")
	}
	if configuration == nil {
		problems = append(problems, "L2 has no observed SDK model configuration")
	} else {
		meta["model"] = configuration.Model
		meta["provider_kind"] = configuration.Provider
		meta["reasoning_effort"] = configuration.ThinkingLevel
		meta["model_configuration"] = configuration
	}
	meta["run_status"] = "complete"
	if len(problems) > 0 {
		meta["run_status"] = "invalid"
	}
	meta["observed_trials"] = len(records)
	meta["observed_model_requests"] = requestCount
	meta["provenance_errors"] = problems
	if err := writeL2JSON(filepath.Join(runDir, "run.json"), meta, false); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("L2 provenance invalid: %s", strings.Join(problems, "; "))
	}
	return nil
}

func writeL2JSON(path string, value any, exclusive bool) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if exclusive {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, err = file.Write(append(raw, '\n'))
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".l2-metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(append(raw, '\n'))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
