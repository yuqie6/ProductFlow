package durable

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostCommandPersistsStructuredFailureResult(t *testing.T) {
	workspace := t.TempDir()
	tool, err := NewHostCommandTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if tool.Effect() != EffectOpaque {
		t.Fatalf("Effect() = %q", tool.Effect())
	}
	engine := openTestEngine(t, filepath.Join(t.TempDir(), "jobs.db"), Options{Owner: "command-worker"}, tool)
	input, _ := json.Marshal(HostCommandInput{Command: "printf stdout; printf stderr >&2; exit 7"})
	job, err := engine.Submit(t.Context(), JobSpec{
		Name: "failing command", Steps: []StepSpec{{Tool: tool.Name(), Input: input}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrJobFailed) || job.Status != JobFailed {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	if len(job.Steps) != 1 || len(job.Steps[0].Result) == 0 {
		t.Fatalf("step = %#v", job.Steps)
	}
	var result HostCommandResult
	if err := json.Unmarshal(job.Steps[0].Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 || result.Stdout != "stdout" || result.Stderr != "stderr" || result.TimedOut {
		t.Fatalf("result = %#v", result)
	}
	attempts, err := engine.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != AttemptFailed || string(attempts[0].Result) != string(job.Steps[0].Result) {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestHostCommandUsesAllowlistedEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-reach-command")
	t.Setenv("ANTHROPIC_API_KEY", "must-not-reach-command")
	tool, err := NewHostCommandTool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := tool.Prepare(t.Context(), json.RawMessage(`{
		"command":"if [ -n \"${OPENAI_API_KEY+x}${ANTHROPIC_API_KEY+x}\" ]; then exit 23; fi; printf clean"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	resultJSON, err := tool.Execute(t.Context(), Invocation{Prepared: prepared})
	if err != nil {
		t.Fatal(err)
	}
	var result HostCommandResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "clean" {
		t.Fatalf("result = %#v", result)
	}
}

func TestHostCommandRejectsInvalidInput(t *testing.T) {
	tool, err := NewHostCommandTool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []json.RawMessage{
		json.RawMessage(`{"command":""}`),
		json.RawMessage(`{"command":"true","timeout_seconds":3601}`),
		json.RawMessage(`{"command":"true","extra":true}`),
	} {
		if _, err := tool.Prepare(t.Context(), input); err == nil {
			t.Fatalf("Prepare(%s) succeeded", input)
		}
	}
}
