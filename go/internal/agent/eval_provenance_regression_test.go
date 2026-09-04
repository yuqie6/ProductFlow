package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/canonjson"
)

func TestL2InputIdentityHashesTaskAndWorldContent(t *testing.T) {
	task := EvalTask{ID: "task", World: "world", Utterances: []string{"inspect"}, Expect: EvalExpect{Terminal: []string{"succeeded"}}}
	world := EvalWorld{Name: "world", Intake: json.RawMessage(`{"b":2,"a":1}`)}
	hash := func(task EvalTask, world EvalWorld) string {
		t.Helper()
		snapshot, err := l2InputSnapshot([]EvalTask{task}, map[string]EvalWorld{"world": world, "unused": {Name: "unused"}})
		if err != nil {
			t.Fatal(err)
		}
		digest, err := canonjson.SHA256Hex(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}
	want := hash(task, world)
	equivalent := world
	equivalent.Intake = json.RawMessage(`{ "a": 1, "b": 2 }`)
	if hash(task, equivalent) != want {
		t.Fatal("equivalent JSON has unstable identity")
	}
	for _, change := range []string{"utterance", "expect", "world", "id"} {
		changedTask, changedWorld := task, world
		switch change {
		case "utterance":
			changedTask.Utterances = []string{"change"}
		case "expect":
			changedTask.Expect.Terminal = []string{"failed"}
		case "world":
			changedWorld.Intake = json.RawMessage(`{"a":3,"b":2}`)
		case "id":
			changedTask.ID = "other"
		}
		if hash(changedTask, changedWorld) == want {
			t.Fatalf("%s did not alter identity", change)
		}
	}
	if _, err := l2InputSnapshot([]EvalTask{task}, nil); err == nil {
		t.Fatal("accepted missing world")
	}
	if _, err := l2InputSnapshot([]EvalTask{task, task}, map[string]EvalWorld{"world": world}); err == nil {
		t.Fatal("accepted duplicate task")
	}
}

func l2TestRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", dir}, {"-C", dir, "-c", "user.name=eval-test", "-c", "user.email=eval@test.invalid", "commit", "--allow-empty", "-qm", "fixture"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s %v", out, err)
		}
	}
	t.Chdir(dir)
	return dir
}

func TestL2GitIdentityIncludesDirtyContentWithoutExportingIt(t *testing.T) {
	dir := l2TestRepository(t)
	before, err := l2GitProvenance(dir)
	if err != nil || before["worktree_dirty"] != false {
		t.Fatalf("clean provenance=%v err=%v", before, err)
	}
	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "add", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("stage: %s %v", out, err)
	}
	a, err := l2GitProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("secret-fixture-one"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := l2GitProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("secret-fixture-two"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := l2GitProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a["worktree_hash"] == b["worktree_hash"] || b["worktree_hash"] == c["worktree_hash"] || c["worktree_dirty"] != true {
		t.Fatal("tracked dirty bytes are not identified")
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := l2GitProvenance(dir)
	if err != nil || c["worktree_hash"] == d["worktree_hash"] {
		t.Fatal("untracked bytes are not identified")
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "secret-fixture") || strings.Contains(string(raw), "tracked.txt") {
		t.Fatalf("exported checkout contents: %s", raw)
	}
}

func l2TestHealth(t *testing.T, skillHash string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(l2AgentIdentity{HarnessHash: testHarnessHash, SkillHash: skillHash})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func l2TestConfiguration() l2ModelConfiguration {
	return l2ModelConfiguration{SchemaVersion: 1, Provider: "openai", Model: "actually-loaded", API: "openai-responses", BaseURLHash: testHarnessHash, ThinkingLevel: "high", ContextWindow: 128000, MaxTokens: 32000}
}

func l2TestRecord(configuration l2ModelConfiguration) map[string]any {
	return map[string]any{"run_id": "test-run", "task_id": "task", "trial": 1, "terminal": "failed", "passed": false,
		"details": map[string]any{"provenance": map[string]any{"valid": true, "model_requests": []l2RequestIdentity{{RequestID: "request", HarnessHash: testHarnessHash, SkillHash: testHarnessHash, Configuration: configuration}}}}}
}

func TestL2ProvenanceFinalizesOnlyCompleteConsistentRuns(t *testing.T) {
	l2TestRepository(t)
	t.Setenv("AGENT_PROVIDER_MODEL", "wrong-env-model")
	srv := l2TestHealth(t, testHarnessHash)
	for _, scenario := range []string{"complete-fail", "missing", "duplicate", "mixed-model", "missing-identity", "nonterminal", "modified-inputs", "changed-checkout"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			meta, err := startL2Run(dir, "test-run", 1, []EvalTask{{ID: "task", World: "world"}}, map[string]EvalWorld{"world": {}}, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if meta["run_status"] != "incomplete" || meta["model"] != "unobserved" {
				t.Fatalf("claimed effective config before observation: %v", meta)
			}
			records := []map[string]any{l2TestRecord(l2TestConfiguration())}
			switch scenario {
			case "missing":
				records = nil
			case "duplicate":
				records = append(records, records[0])
			case "mixed-model":
				p := records[0]["details"].(map[string]any)["provenance"].(map[string]any)
				requests := p["model_requests"].([]l2RequestIdentity)
				other := requests[0]
				other.RequestID = "other"
				other.Configuration.Model = "different"
				p["model_requests"] = append(requests, other)
			case "missing-identity":
				records[0]["details"].(map[string]any)["provenance"].(map[string]any)["valid"] = false
			case "nonterminal":
				records[0]["terminal"] = nil
			case "modified-inputs":
				if err := os.WriteFile(filepath.Join(dir, "inputs.json"), []byte(`{}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "changed-checkout":
				if err := os.WriteFile("changed.txt", []byte("change"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err = finishL2Run(dir, meta, records, srv.URL)
			if scenario == "complete-fail" {
				if err != nil || meta["run_status"] != "complete" || meta["model"] != "actually-loaded" || meta["reasoning_effort"] != "high" {
					t.Fatalf("valid FAIL not finalized with actual config: %v err=%v", meta, err)
				}
			} else if err == nil || meta["run_status"] != "invalid" {
				t.Fatalf("accepted %s: %v err=%v", scenario, meta, err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "run.json"))
			if err != nil || strings.Contains(string(raw), "wrong-env-model") {
				t.Fatalf("metadata=%s err=%v", raw, err)
			}
		})
	}
}

func TestL2ProvenanceRejectsMissingSkillAndExistingArtifacts(t *testing.T) {
	l2TestRepository(t)
	tasks, worlds := []EvalTask{{ID: "task", World: "world"}}, map[string]EvalWorld{"world": {}}
	for _, hash := range []string{"", strings.ToUpper(testHarnessHash), "short"} {
		dir := t.TempDir()
		if _, err := startL2Run(dir, "test", 1, tasks, worlds, l2TestHealth(t, hash).URL); err == nil {
			t.Fatal("accepted invalid loaded Skill identity")
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatal("wrote artifacts before identity validation")
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte("historical"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := startL2Run(dir, "test", 1, tasks, worlds, l2TestHealth(t, testHarnessHash).URL); err == nil {
		t.Fatal("overwrote historical run")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("modified existing run directory")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "run.json"))
	if string(raw) != "historical" {
		t.Fatal("historical metadata changed")
	}
	if err := writeL2JSON(filepath.Join(dir, "missing", "output.json"), map[string]any{}, false); err == nil {
		t.Fatal("ignored metadata write error")
	}
}

func TestL2RequestIdentityRejectsInvalidConfiguration(t *testing.T) {
	meta := map[string]any{"harness_hash": testHarnessHash, "skill_catalog_hash": testHarnessHash}
	valid := l2RequestIdentity{RequestID: "request", HarnessHash: testHarnessHash, SkillHash: testHarnessHash, Configuration: l2TestConfiguration()}
	if err := validateL2RequestIdentity(valid, "openai", "actually-loaded", meta); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"skill", "schema", "model", "thinking", "endpoint", "request"} {
		bad := valid
		switch field {
		case "skill":
			bad.SkillHash = ""
		case "schema":
			bad.Configuration.SchemaVersion = 0
		case "model":
			bad.Configuration.Model = "requested-not-effective"
		case "thinking":
			bad.Configuration.ThinkingLevel = "unsupported"
		case "endpoint":
			bad.Configuration.BaseURLHash = ""
		case "request":
			bad.RequestID = ""
		}
		if err := validateL2RequestIdentity(bad, "openai", "actually-loaded", meta); err == nil {
			t.Fatalf("accepted invalid %s", field)
		}
	}
}
