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

func TestReadFilePaginatesWithWholeFileDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lines.txt")
	content := "one\ntwo\nthree\nfour\nfive\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(readFileArgs{Path: path, StartLine: 2, LineCount: 2, IncludeMetadata: true})
	output, err := readTool().Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var result readFileResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != "two\nthree" || result.StartLine != 2 || result.EndLine != 3 ||
		result.TotalLines != 5 || result.NextStartLine != 4 || result.SHA256 != sha256Hex([]byte(content)) {
		t.Fatalf("read result = %#v", result)
	}

	raw, _ = json.Marshal(readFileArgs{Path: path, StartLine: result.NextStartLine, LineCount: 2, IncludeMetadata: true})
	output, err = readTool().Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	result = readFileResult{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != "four\nfive" || result.NextStartLine != 0 || result.EndLine != 5 {
		t.Fatalf("second page = %#v", result)
	}
}

func TestReadFilePageOmitsWholeFileScanUnlessMetadataRequested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lines.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(readFileArgs{Path: path, StartLine: 1, LineCount: 1})
	output, err := readTool().Handler(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var result readFileResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != "one" || result.NextStartLine != 2 || result.SHA256 != "" || result.TotalLines != 0 {
		t.Fatalf("read result = %#v", result)
	}
}

func TestReadFileStopsBeforeIOWhenContextCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := readTool().Handler(ctx, json.RawMessage(`{"path":"`+path+`","start_line":1,"line_count":1}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read error = %v, want context canceled", err)
	}
}

func TestEditFileUsesDigestPreconditionAndAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.go")
	before := []byte("package sample\n\nconst value = 1\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	args := editFileArgs{
		Path: path, ExpectedSHA256: sha256Hex(before),
		OldText: "const value = 1", NewText: "const value = 2",
	}
	raw, _ := json.Marshal(args)
	output, err := editTool(filepath.Dir(path)).Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var result editFileResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.BeforeSHA256 != sha256Hex(before) ||
		result.AfterSHA256 != sha256Hex(current) || !strings.Contains(string(current), "value = 2") {
		t.Fatalf("edit result = %#v, content = %q", result, current)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestEditFileRejectsConcurrentModification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	original := []byte("original\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(editFileArgs{
		Path: path, ExpectedSHA256: sha256Hex(original), OldText: "original", NewText: "agent",
	})
	output, err := editTool(filepath.Dir(path)).Handler(context.Background(), raw)
	if err == nil {
		t.Fatal("expected digest conflict")
	}
	var result editFileResult
	if json.Unmarshal([]byte(output), &result) != nil || result.Status != "conflict" {
		t.Fatalf("conflict result = %q", output)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "external change\n" {
		t.Fatalf("conflict overwrote file: %q", content)
	}
}

func TestEditFileCreationCanReconcileRepeatedCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "created.txt")
	raw, _ := json.Marshal(editFileArgs{Path: path, ExpectedSHA256: "absent", NewText: "created\n"})
	for attempt := 0; attempt < 2; attempt++ {
		output, err := editTool(filepath.Dir(path)).Handler(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var result editFileResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "succeeded" || result.AlreadyApplied != (attempt == 1) {
			t.Fatalf("attempt %d result = %#v", attempt, result)
		}
	}
}

func TestExecCommandReturnsStructuredFailure(t *testing.T) {
	tool := execCommandTool(hostBashExecutor{workingDir: t.TempDir()}, func() time.Duration { return defaultExecCommandTimeout })
	output, err := tool.Handler(context.Background(), json.RawMessage(`{"command":"printf out; printf err >&2; exit 7"}`))
	if err == nil {
		t.Fatal("expected non-zero command error")
	}
	var result commandResult
	if decodeErr := json.Unmarshal([]byte(output), &result); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if result.ExitCode != 7 || result.Stdout != "out" || result.Stderr != "err" || result.TimedOut {
		t.Fatalf("command result = %#v", result)
	}
	if tool.Effect != EffectOpaque {
		t.Fatalf("effect = %q", tool.Effect)
	}
}

func TestBuiltinExecTimeoutCanBeUpdated(t *testing.T) {
	set := BuiltinTools(t.TempDir())
	t.Cleanup(set.Close)
	if err := set.SetExecTimeout(20 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(set.Tools...)
	tool, ok := registry.Lookup("exec_command")
	if !ok {
		t.Fatal("exec_command missing")
	}
	output, err := tool.Handler(context.Background(), json.RawMessage(`{"command":"sleep 1"}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, output = %s", err, output)
	}
	var result commandResult
	if json.Unmarshal([]byte(output), &result) != nil || !result.TimedOut {
		t.Fatalf("result = %s", output)
	}
}

func TestEditAndReadEffectClasses(t *testing.T) {
	edit := editTool(t.TempDir())
	if readTool().Effect != EffectPure || edit.Effect != EffectReconcilable {
		t.Fatalf("effects = read:%q edit:%q", readTool().Effect, edit.Effect)
	}
}

func TestEditFileReportsFilesystemFailure(t *testing.T) {
	path := t.TempDir()
	raw, _ := json.Marshal(editFileArgs{Path: path, ExpectedSHA256: "absent", NewText: "x"})
	_, err := editTool(filepath.Dir(path)).Handler(context.Background(), raw)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directory edit error = %v", err)
	}
}
