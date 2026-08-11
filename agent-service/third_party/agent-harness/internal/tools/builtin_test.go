package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadToolStopsAtConfiguredLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	content := strings.Repeat("x", maxReadBytes+128)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}

	got, err := readTool().Handler(context.Background(), args)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if len(got) != maxReadBytes+len(readTruncatedNote) {
		t.Fatalf("result length = %d, want %d", len(got), maxReadBytes+len(readTruncatedNote))
	}
	if !strings.HasSuffix(got, readTruncatedNote) {
		t.Fatalf("result does not contain truncation note")
	}
}

func TestCappedWriterDiscardsBytesPastLimit(t *testing.T) {
	writer := newCappedWriter(5)
	for _, chunk := range []string{"abc", "def", "ghi"} {
		n, err := writer.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = (%d, %v)", chunk, n, err)
		}
	}

	if got := writer.String(); got != "abcde"+bashTruncatedNote {
		t.Fatalf("String() = %q", got)
	}
	if writer.buffer.Len() != 5 {
		t.Fatalf("captured bytes = %d, want 5", writer.buffer.Len())
	}
}

func TestExecCommandMarksTruncatedOutput(t *testing.T) {
	args := json.RawMessage(`{"command":"head -c 205000 /dev/zero | tr '\\0' x"}`)
	got, err := execCommandTool(hostBashExecutor{}, func() time.Duration { return defaultExecCommandTimeout }).Handler(context.Background(), args)
	if err != nil {
		t.Fatalf("exec_command: %v", err)
	}
	var result commandResult
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatal(err)
	}
	if !result.StdoutTruncated || !strings.HasSuffix(result.Stdout, bashTruncatedNote) {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Stdout) != maxBashOutputBytes+len(bashTruncatedNote) {
		t.Fatalf("result length = %d, want %d", len(result.Stdout), maxBashOutputBytes+len(bashTruncatedNote))
	}
}

func TestBuiltinToolsAutoApproveOnlyKnownWorkspaceReads(t *testing.T) {
	workingDir := t.TempDir()
	sourcePath := filepath.Join(workingDir, "source.go")
	if err := os.WriteFile(sourcePath, []byte("package example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(workingDir, ".env")
	if err := os.WriteFile(envPath, []byte("SECRET=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(workingDir, "outside-link")
	if err := os.Symlink(outsidePath, symlinkPath); err != nil {
		t.Fatal(err)
	}

	builtins := BuiltinTools(workingDir)
	t.Cleanup(builtins.Close)
	registry := NewRegistry(builtins.Tools...)
	tests := []struct {
		name     string
		toolName string
		args     map[string]string
		want     bool
	}{
		{name: "workspace file", toolName: "read_file", args: map[string]string{"path": sourcePath}, want: true},
		{name: "secret env", toolName: "read_file", args: map[string]string{"path": envPath}, want: false},
		{name: "outside file", toolName: "read_file", args: map[string]string{"path": outsidePath}, want: false},
		{name: "outside symlink", toolName: "read_file", args: map[string]string{"path": symlinkPath}, want: false},
		{name: "pwd", toolName: "exec_command", args: map[string]string{"command": "pwd"}, want: true},
		{name: "ripgrep files", toolName: "exec_command", args: map[string]string{"command": "rg --files"}, want: true},
		{name: "workspace absolute path", toolName: "exec_command", args: map[string]string{"command": "head " + sourcePath}, want: true},
		{name: "outside absolute path", toolName: "exec_command", args: map[string]string{"command": "head " + outsidePath}, want: false},
		{name: "outside symlink command", toolName: "exec_command", args: map[string]string{"command": "head " + symlinkPath}, want: false},
		{name: "git status", toolName: "exec_command", args: map[string]string{"command": "git status --short --branch"}, want: true},
		{name: "git current branch", toolName: "exec_command", args: map[string]string{"command": "git branch --show-current"}, want: true},
		{name: "git object secret", toolName: "exec_command", args: map[string]string{"command": "git show HEAD:.env"}, want: false},
		{name: "pipeline", toolName: "exec_command", args: map[string]string{"command": "rg TODO | head"}, want: false},
		{name: "shell redirect", toolName: "exec_command", args: map[string]string{"command": "ls > files.txt"}, want: false},
		{name: "ripgrep preprocessor", toolName: "exec_command", args: map[string]string{"command": "rg --pre=cat TODO ."}, want: false},
		{name: "hidden search", toolName: "exec_command", args: map[string]string{"command": "rg --hidden SECRET ."}, want: false},
		{name: "git output file", toolName: "exec_command", args: map[string]string{"command": "git diff --output=patch.txt"}, want: false},
		{name: "git mutation", toolName: "exec_command", args: map[string]string{"command": "git commit -m test"}, want: false},
		{name: "unknown command", toolName: "exec_command", args: map[string]string{"command": "go test ./..."}, want: false},
		{name: "file write", toolName: "edit_file", args: map[string]string{"path": sourcePath}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, ok := registry.Lookup(tt.toolName)
			if !ok {
				t.Fatalf("tool %q not found", tt.toolName)
			}
			raw, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if got := tool.CanAutoApprove(raw); got != tt.want {
				t.Fatalf("CanAutoApprove(%s) = %v, want %v", raw, got, tt.want)
			}
		})
	}
}

func TestBuiltinToolsAutoEditOnlyWorkspaceWrites(t *testing.T) {
	workingDir := t.TempDir()
	existing := filepath.Join(workingDir, "existing.go")
	if err := os.WriteFile(existing, []byte("package example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.go")
	symlinkDir := filepath.Join(workingDir, "outside-link")
	if err := os.Symlink(outsideDir, symlinkDir); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(workingDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitLink := filepath.Join(workingDir, "git-link")
	if err := os.Symlink(gitDir, gitLink); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(workingDir, ".env")
	if err := os.WriteFile(envFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	envLink := filepath.Join(workingDir, "config-link")
	if err := os.Symlink(envFile, envLink); err != nil {
		t.Fatal(err)
	}

	builtins := BuiltinTools(workingDir)
	t.Cleanup(builtins.Close)
	registry := NewRegistry(builtins.Tools...)
	write, ok := registry.Lookup("edit_file")
	if !ok {
		t.Fatal("edit_file not found")
	}
	command, ok := registry.Lookup("exec_command")
	if !ok {
		t.Fatal("exec_command not found")
	}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "existing workspace file", path: existing, want: true},
		{name: "new workspace file", path: filepath.Join(workingDir, "new.go"), want: true},
		{name: "nested new workspace file", path: filepath.Join(workingDir, "new", "nested.go"), want: true},
		{name: "outside file", path: outside, want: false},
		{name: "outside symlink child", path: filepath.Join(symlinkDir, "child.go"), want: false},
		{name: "git symlink", path: filepath.Join(gitLink, "config"), want: false},
		{name: "environment symlink", path: envLink, want: false},
		{name: "environment file", path: filepath.Join(workingDir, ".env"), want: false},
		{name: "git metadata", path: filepath.Join(workingDir, ".git", "config"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"path": tt.path, "content": "updated"})
			if err != nil {
				t.Fatal(err)
			}
			if got := write.CanAutoApproveEdit(raw); got != tt.want {
				t.Fatalf("CanAutoApproveEdit(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
	if command.CanAutoApproveEdit(json.RawMessage(`{"command":"go test ./..."}`)) {
		t.Fatal("Auto Edit must not approve commands")
	}
}

func TestAutoEditDoesNotFollowParentSwappedAfterApproval(t *testing.T) {
	workspace := t.TempDir()
	parent := filepath.Join(workspace, "subdir")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	builtins := BuiltinTools(workspace)
	t.Cleanup(builtins.Close)
	registry := NewRegistry(builtins.Tools...)
	edit, ok := registry.Lookup("edit_file")
	if !ok {
		t.Fatal("edit_file not found")
	}
	raw, _ := json.Marshal(editFileArgs{
		Path: filepath.Join(parent, "new.txt"), ExpectedSHA256: "absent", NewText: "agent\n",
	})
	if !edit.CanAutoApproveEdit(raw) {
		t.Fatal("initial workspace edit was not auto-approved")
	}
	if err := os.Rename(parent, filepath.Join(workspace, "moved-subdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}

	if _, err := edit.Handler(t.Context(), raw); err == nil {
		t.Fatal("edit succeeded after approved parent was replaced by an escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file error = %v, want not exist", err)
	}
}

func TestBuiltinCatalogHidesLegacyOverwriteAndShellAliases(t *testing.T) {
	builtins := BuiltinTools(t.TempDir())
	t.Cleanup(builtins.Close)
	registry := NewRegistry(builtins.Tools...)
	want := []string{
		"edit_file", "exec_command", "find_files", "list_dir", "poll_command",
		"read_file", "search_text", "start_command", "terminate_command", "write_stdin",
	}
	if got := registry.Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("builtin tools = %v, want %v", got, want)
	}
	for _, name := range []string{"bash", "write_file"} {
		if _, ok := registry.Lookup(name); ok {
			t.Fatalf("legacy tool %q is still model-visible", name)
		}
	}
	for _, name := range []string{"edit_file", "exec_command"} {
		if _, ok := registry.Lookup(name); !ok {
			t.Fatalf("replacement tool %q is missing", name)
		}
	}
}
