package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxOffIsExplicitAndDisablesBashAutoApproval(t *testing.T) {
	tools, info, err := NewBuiltinTools(t.TempDir(), BuiltinOptions{Sandbox: "off"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tools.Close)
	if info.Enabled || info.Backend != "host" || info.Reason == "" {
		t.Fatalf("sandbox info = %#v", info)
	}
	command := lookupTool(t, tools.Tools, "exec_command")
	if command.CanAutoApprove(json.RawMessage(`{"command":"pwd"}`)) {
		t.Fatal("HOST fallback must not auto-approve commands")
	}
}

func TestBuiltinToolSetCloseTerminatesCommandSessions(t *testing.T) {
	toolSet, _, err := NewBuiltinTools(t.TempDir(), BuiltinOptions{Sandbox: "off"})
	if err != nil {
		t.Fatal(err)
	}
	start := lookupTool(t, toolSet.Tools, "start_command")
	raw, err := start.Handler(context.Background(), json.RawMessage(`{"command":"sleep 30"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started commandSessionResult
	if err := json.Unmarshal([]byte(raw), &started); err != nil {
		t.Fatal(err)
	}

	toolSet.Close()
	toolSet.Close()
	if _, err := toolSet.commands.get(started.SessionID); err == nil {
		t.Fatal("closed tool set retained its command session")
	}
	if _, err := toolSet.commands.start(ExecutionHost, startCommandArgs{Command: "true"}); err == nil {
		t.Fatal("closed tool set accepted a new command session")
	}
}

func TestBwrapSandboxEnforcesReadOnlyAndWorkspaceWriteBoundaries(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap is not installed")
	}
	workspace := sandboxTestWorkspace(t)
	toolSet, info, err := NewBuiltinTools(workspace, BuiltinOptions{Sandbox: "required"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(toolSet.Close)
	if !info.Enabled || info.Backend != "bwrap" {
		t.Fatalf("sandbox info = %#v", info)
	}
	command := lookupTool(t, toolSet.Tools, "exec_command")

	blocked := filepath.Join(workspace, "blocked.txt")
	output := runCommandTool(t, command, WithExecutionBoundary(context.Background(), ExecutionSandboxReadOnly), "printf blocked > "+shellQuote(blocked), true)
	if _, err := os.Stat(blocked); !os.IsNotExist(err) {
		t.Fatalf("read-only sandbox wrote %s; output = %q", blocked, output)
	}

	allowed := filepath.Join(workspace, "allowed.txt")
	runCommandTool(t, command, WithExecutionBoundary(context.Background(), ExecutionSandboxWorkspaceWrite), "printf allowed > "+shellQuote(allowed), false)
	content, err := os.ReadFile(allowed)
	if err != nil || string(content) != "allowed" {
		t.Fatalf("workspace write = %q, %v", content, err)
	}
}

func TestCommandSubprocessDoesNotInheritProviderKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	command := execCommandTool(hostBashExecutor{workingDir: t.TempDir()}, func() time.Duration { return defaultExecCommandTimeout })
	output := runCommandTool(t, command, context.Background(), `if [ -z "${OPENAI_API_KEY:-}" ]; then printf clean; else printf leaked; fi`, false)
	if output != "clean" {
		t.Fatalf("subprocess environment = %q", output)
	}
}

func TestSandboxPathDropsWindowsInteropEntries(t *testing.T) {
	got := sandboxPath("/usr/bin:/mnt/c/Windows/System32:/bin:/mnt/d/tools")
	if got != "/usr/bin:/bin" {
		t.Fatalf("sandbox PATH = %q", got)
	}
}

func TestBwrapSandboxBlocksWSLInterop(t *testing.T) {
	if !shouldMaskWSLInterop() {
		t.Skip("not running under WSL with /init")
	}
	const windowsCommand = "/mnt/c/Windows/System32/cmd.exe"
	if _, err := os.Stat(windowsCommand); err != nil {
		t.Skipf("Windows command is unavailable: %v", err)
	}
	toolSet, _, err := NewBuiltinTools(sandboxTestWorkspace(t), BuiltinOptions{Sandbox: "required"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(toolSet.Close)
	output := runCommandTool(t, lookupTool(t, toolSet.Tools, "exec_command"),
		WithExecutionBoundary(context.Background(), ExecutionSandboxWorkspaceWrite),
		windowsCommand+" /c echo interop-escaped", true)
	if strings.Contains(output, "interop-escaped\r") || strings.Contains(output, "interop-escaped\n") {
		t.Fatalf("WSL interop escaped bwrap: %q", output)
	}
}

func lookupTool(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return Tool{}
}

func runCommandTool(t *testing.T, tool Tool, ctx context.Context, command string, allowFailure bool) string {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := tool.Handler(ctx, arguments)
	if runErr != nil && !allowFailure {
		t.Fatal(runErr)
	}
	var result commandResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode command result: %v; output=%q", err, output)
	}
	return result.Stdout + result.Stderr
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func sandboxTestWorkspace(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".harness-sandbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove sandbox test workspace: %v", err)
		}
	})
	return root
}
