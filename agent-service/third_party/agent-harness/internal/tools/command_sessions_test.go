package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandSessionStreamsOutputAndExitState(t *testing.T) {
	manager := newCommandManager(hostBashExecutor{workingDir: t.TempDir()})
	t.Cleanup(manager.close)
	started, err := manager.start(ExecutionHost, startCommandArgs{
		Command: "printf first; sleep 0.05; printf second; printf problem >&2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.SessionID == "" || started.PID == 0 {
		t.Fatalf("started = %#v", started)
	}
	finished, err := manager.poll(t.Context(), pollCommandArgs{SessionID: started.SessionID, WaitMillis: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != commandSucceeded || finished.ExitCode == nil || *finished.ExitCode != 0 ||
		finished.Stdout != "firstsecond" || finished.Stderr != "problem" || finished.FinishedAt == nil {
		t.Fatalf("finished = %#v", finished)
	}

	consumed, err := manager.poll(t.Context(), pollCommandArgs{
		SessionID: started.SessionID, StdoutOffset: finished.NextStdout, StderrOffset: finished.NextStderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Stdout != "" || consumed.Stderr != "" || consumed.NextStdout != finished.NextStdout {
		t.Fatalf("offset poll = %#v", consumed)
	}
}

func TestBwrapCommandSessionKeepsWorkspaceBoundary(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap is not installed")
	}
	workspace := sandboxTestWorkspace(t)
	bwrapPath, err := probeBwrap(workspace, shouldMaskWSLInterop())
	if err != nil {
		t.Fatal(err)
	}
	manager := newCommandManager(bwrapBashExecutor{
		path: bwrapPath, workingDir: workspace, maskWSLInterop: shouldMaskWSLInterop(),
	})
	t.Cleanup(manager.close)
	target := filepath.Join(workspace, "background.txt")
	started, err := manager.start(ExecutionSandboxWorkspaceWrite, startCommandArgs{
		Command: "printf contained > " + shellQuote(target) + "; printf streamed",
	})
	if err != nil {
		t.Fatal(err)
	}
	finished, err := manager.poll(t.Context(), pollCommandArgs{SessionID: started.SessionID, WaitMillis: 2000})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != commandSucceeded || finished.Stdout != "streamed" || string(content) != "contained" {
		t.Fatalf("finished = %#v, content = %q", finished, content)
	}
}

func TestCommandSessionAcceptsStdin(t *testing.T) {
	manager := newCommandManager(hostBashExecutor{workingDir: t.TempDir()})
	t.Cleanup(manager.close)
	started, err := manager.start(ExecutionHost, startCommandArgs{Command: `IFS= read -r line; printf 'got:%s' "$line"`})
	if err != nil {
		t.Fatal(err)
	}
	writeResult, err := manager.writeStdin(t.Context(), started.SessionID, "answer", true)
	if err != nil {
		t.Fatal(err)
	}
	if writeResult["bytes_written"] != 7 {
		t.Fatalf("write result = %#v", writeResult)
	}
	finished, err := manager.poll(t.Context(), pollCommandArgs{SessionID: started.SessionID, WaitMillis: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != commandSucceeded || finished.Stdout != "got:answer" {
		t.Fatalf("finished = %#v", finished)
	}
}

func TestPollCommandStopsWhenTurnIsCanceled(t *testing.T) {
	manager := newCommandManager(hostBashExecutor{workingDir: t.TempDir()})
	t.Cleanup(manager.close)
	started, err := manager.start(ExecutionHost, startCommandArgs{Command: "sleep 1", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, poll, _, _ := manager.tools()
	raw, _ := json.Marshal(pollCommandArgs{SessionID: started.SessionID, WaitMillis: 1000})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = poll.Handler(ctx, raw)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("poll error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("poll ignored cancellation for %s", elapsed)
	}
}

func TestWriteStdinStopsWhenTurnIsCanceled(t *testing.T) {
	manager := newCommandManager(hostBashExecutor{workingDir: t.TempDir()})
	t.Cleanup(manager.close)
	started, err := manager.start(ExecutionHost, startCommandArgs{Command: "sleep 1", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, _, write, _ := manager.tools()
	raw, _ := json.Marshal(map[string]any{
		"session_id": started.SessionID, "input": strings.Repeat("x", 1<<20), "append_newline": false,
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = write.Handler(ctx, raw)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write_stdin error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("write_stdin ignored cancellation for %s", elapsed)
	}
}

func TestCommandSessionReportsFailureAndTermination(t *testing.T) {
	manager := newCommandManager(hostBashExecutor{workingDir: t.TempDir()})
	t.Cleanup(manager.close)

	failed, err := manager.start(ExecutionHost, startCommandArgs{Command: "printf bad >&2; exit 9"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err = manager.poll(t.Context(), pollCommandArgs{SessionID: failed.SessionID, WaitMillis: 2000})
	if err == nil || failed.Status != commandFailed || failed.ExitCode == nil || *failed.ExitCode != 9 || failed.Stderr != "bad" {
		t.Fatalf("failed = %#v, err = %v", failed, err)
	}

	running, err := manager.start(ExecutionHost, startCommandArgs{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	terminated, err := manager.terminate(running.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if terminated.Status != commandTerminated || terminated.FinishedAt == nil {
		t.Fatalf("terminated = %#v", terminated)
	}
}

func TestCommandSessionToolsExposeStructuredLifecycle(t *testing.T) {
	manager := newCommandManager(hostBashExecutor{workingDir: t.TempDir()})
	t.Cleanup(manager.close)
	start, poll, write, terminate := manager.tools()
	if start.Name != "start_command" || poll.Name != "poll_command" || write.Name != "write_stdin" || terminate.Name != "terminate_command" {
		t.Fatalf("tool names = %q %q %q %q", start.Name, poll.Name, write.Name, terminate.Name)
	}
	if start.Effect != EffectOpaque || poll.Effect != EffectPure || terminate.Effect != EffectIdempotent {
		t.Fatalf("effects = %q %q %q", start.Effect, poll.Effect, terminate.Effect)
	}
	if !poll.CanAutoApprove(json.RawMessage(`{"session_id":"cmd-test"}`)) ||
		!write.CanAutoApprove(json.RawMessage(`{"session_id":"cmd-test","input":"x"}`)) ||
		!terminate.CanAutoApprove(json.RawMessage(`{"session_id":"cmd-test"}`)) {
		t.Fatal("management calls for an approved command session should be auto-approved")
	}
	if strings.TrimSpace(start.Description) == "" {
		t.Fatal("start command description is empty")
	}
}

func TestCommandManagerReclaimsCompletedSessionsAtCapacity(t *testing.T) {
	manager := newCommandManager(hostBashExecutor{workingDir: t.TempDir()})
	t.Cleanup(manager.close)
	var firstID string
	for index := 0; index < maxCommandSessions; index++ {
		started, err := manager.start(ExecutionHost, startCommandArgs{Command: "true"})
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
		if index == 0 {
			firstID = started.SessionID
		}
		if _, err := manager.poll(t.Context(), pollCommandArgs{SessionID: started.SessionID, WaitMillis: 2000}); err != nil {
			t.Fatalf("poll %d: %v", index, err)
		}
	}

	started, err := manager.start(ExecutionHost, startCommandArgs{Command: "true"})
	if err != nil {
		t.Fatalf("start after completed sessions: %v", err)
	}
	if _, err := manager.get(firstID); err == nil {
		t.Fatal("old completed session was not reclaimed")
	}
	if _, err := manager.poll(t.Context(), pollCommandArgs{SessionID: started.SessionID, WaitMillis: 2000}); err != nil {
		t.Fatal(err)
	}
}
