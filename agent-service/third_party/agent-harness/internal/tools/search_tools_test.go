package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredSearchToolsReturnPagedWorkspaceResults(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	workspace := t.TempDir()
	for _, directory := range []string{filepath.Join(workspace, "src"), filepath.Join(workspace, ".git")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"src/a.go":    "package demo\n// needle\n",
		"src/b.txt":   "needle hidden by glob\n",
		".env":        "needle secret\n",
		".git/config": "needle metadata\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	builtins := BuiltinTools(workspace)
	t.Cleanup(builtins.Close)
	registry := NewRegistry(builtins.Tools...)

	list := mustTool(t, registry, "list_dir")
	firstRaw := callTool(t, list, `{"path":".","depth":2,"limit":2}`)
	var first listDirResult
	if err := json.Unmarshal([]byte(firstRaw), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || first.Entries[0].Path != "src" || first.Entries[1].Path != "src/a.go" || first.NextOffset != 2 {
		t.Fatalf("first page = %#v", first)
	}
	secondRaw := callTool(t, list, `{"path":".","depth":2,"offset":2,"limit":2}`)
	if strings.Contains(secondRaw, ".env") || strings.Contains(secondRaw, ".git") || !strings.Contains(secondRaw, "src/b.txt") {
		t.Fatalf("second page = %s", secondRaw)
	}

	find := mustTool(t, registry, "find_files")
	found := callTool(t, find, `{"glob":"*.go"}`)
	if !strings.Contains(found, "src/a.go") || strings.Contains(found, "b.txt") {
		t.Fatalf("find_files = %s", found)
	}

	search := mustTool(t, registry, "search_text")
	matchesRaw := callTool(t, search, `{"pattern":"needle","globs":["*.go"]}`)
	var matches searchTextResult
	if err := json.Unmarshal([]byte(matchesRaw), &matches); err != nil {
		t.Fatal(err)
	}
	if len(matches.Matches) != 1 || matches.Matches[0].Path != "src/a.go" || matches.Matches[0].Line != 2 || matches.Matches[0].Column != 4 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestStructuredSearchToolsOnlyAutoApproveSafeWorkspacePaths(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	builtins := BuiltinTools(workspace)
	t.Cleanup(builtins.Close)
	registry := NewRegistry(builtins.Tools...)
	for _, name := range []string{"list_dir", "find_files", "search_text"} {
		tool := mustTool(t, registry, name)
		if !tool.CanAutoApprove(json.RawMessage(`{"path":"."}`)) {
			t.Fatalf("%s should approve workspace root", name)
		}
		if tool.CanAutoApprove(json.RawMessage(`{"path":".git"}`)) {
			t.Fatalf("%s approved .git", name)
		}
		raw, err := json.Marshal(map[string]string{"path": outside})
		if err != nil {
			t.Fatal(err)
		}
		if tool.CanAutoApprove(raw) {
			t.Fatalf("%s approved outside path", name)
		}
	}
}

func mustTool(t *testing.T, registry *Registry, name string) Tool {
	t.Helper()
	tool, ok := registry.Lookup(name)
	if !ok {
		t.Fatalf("tool %q not found", name)
	}
	return tool
}

func callTool(t *testing.T, tool Tool, raw string) string {
	t.Helper()
	output, err := tool.Handler(context.Background(), json.RawMessage(raw))
	if err != nil {
		t.Fatalf("%s: %v", tool.Name, err)
	}
	return output
}
