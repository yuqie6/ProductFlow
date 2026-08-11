package durable

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFileEditContractReturnsFreshObjectSchema(t *testing.T) {
	first := FileEditParameters()
	if first["type"] != "object" || !reflect.DeepEqual(first["required"], []string{"path", "expected_sha256", "old_text", "new_text"}) {
		t.Fatalf("schema = %#v", first)
	}
	first["type"] = "array"
	if second := FileEditParameters(); second["type"] != "object" {
		t.Fatalf("schema mutation leaked: %#v", second)
	}
	tool, err := NewFileEditTool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name() != FileEditToolName || FileEditDescription == "" {
		t.Fatalf("tool contract = %q, %q", tool.Name(), FileEditDescription)
	}
}

func TestFileEditToolRejectsEscapeAndSensitivePaths(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	tool, err := NewFileEditTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, link, filepath.Join(workspace, ".env"), filepath.Join(workspace, ".git", "config")} {
		input, _ := json.Marshal(FileEditInput{Path: path, ExpectedSHA256: "absent", NewText: "x"})
		if _, err := tool.Prepare(t.Context(), input); err == nil {
			t.Fatalf("Prepare(%q) succeeded", path)
		}
	}
}

func TestFileEditToolPreparationDetectsDigestConflict(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "source.txt")
	if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, _ := NewFileEditTool(workspace)
	input, _ := json.Marshal(FileEditInput{
		Path: path, ExpectedSHA256: digestBytes([]byte("stale")), OldText: "current", NewText: "new",
	})
	if _, err := tool.Prepare(t.Context(), input); !errors.Is(err, ErrConflict) {
		t.Fatalf("Prepare error = %v", err)
	}
}

func TestFileEditToolExecuteRejectsParentSymlinkSwap(t *testing.T) {
	workspace := t.TempDir()
	originalParent := filepath.Join(workspace, "subdir")
	if err := os.Mkdir(originalParent, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	tool, err := NewFileEditTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(FileEditInput{
		Path: filepath.Join(originalParent, "new.txt"), ExpectedSHA256: "absent", NewText: "agent\n",
	})
	prepared, err := tool.Prepare(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	movedParent := filepath.Join(workspace, "moved-subdir")
	if err := os.Rename(originalParent, movedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, originalParent); err != nil {
		t.Fatal(err)
	}

	_, err = tool.Execute(t.Context(), Invocation{Prepared: prepared})
	if err == nil {
		t.Fatal("Execute succeeded after the prepared parent was replaced by an escaping symlink")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "new.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside file error = %v, want not exist", statErr)
	}
}

func TestFileEditToolDetectsChangeImmediatelyBeforeCommit(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "source.txt")
	before := []byte("base\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := NewFileEditTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(FileEditInput{
		Path: path, ExpectedSHA256: digestBytes(before), OldText: "base", NewText: strings.Repeat("agent", (maxFileEditBytes-1)/5),
	})
	prepared, err := tool.Prepare(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, executeErr := tool.Execute(t.Context(), Invocation{Prepared: prepared})
		done <- executeErr
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, readErr := os.ReadDir(workspace)
		if readErr != nil {
			t.Fatal(readErr)
		}
		seenTemp := false
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".harness-edit-") {
				seenTemp = true
				break
			}
		}
		if seenTemp {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("did not observe durable edit temporary file")
		}
		time.Sleep(100 * time.Microsecond)
	}
	if err := os.WriteFile(path, []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrConflict) {
		t.Fatalf("Execute error = %v, want ErrConflict", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "external\n" {
		t.Fatalf("concurrent writer was overwritten: %q", content)
	}
}
