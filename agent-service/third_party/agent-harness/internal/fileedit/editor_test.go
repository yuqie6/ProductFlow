package fileedit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRechecksExpectedStateImmediatelyBeforeCommit(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "source.txt")
	before := []byte("base\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	editor, err := New(workspace)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := editor.Prepare(t.Context(), Input{
		Path: path, ExpectedSHA256: Digest(before), OldText: "base", NewText: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	editor.beforeCommit = func() {
		if writeErr := os.WriteFile(path, []byte("external\n"), 0o600); writeErr != nil {
			t.Error(writeErr)
		}
	}

	if _, err := editor.Apply(t.Context(), prepared); !errors.Is(err, ErrConflict) {
		t.Fatalf("Apply error = %v, want ErrConflict", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "external\n" {
		t.Fatalf("external edit was overwritten: %q", content)
	}
}

func TestApplyUsesNoReplaceWhenCreatingFile(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "new.txt")
	editor, err := New(workspace)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := editor.Prepare(t.Context(), Input{Path: path, ExpectedSHA256: "absent", NewText: "agent\n"})
	if err != nil {
		t.Fatal(err)
	}
	editor.beforeCommit = func() {
		if writeErr := os.WriteFile(path, []byte("external\n"), 0o600); writeErr != nil {
			t.Error(writeErr)
		}
	}

	if _, err := editor.Apply(t.Context(), prepared); !errors.Is(err, ErrConflict) {
		t.Fatalf("Apply error = %v, want ErrConflict", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "external\n" {
		t.Fatalf("concurrent creation was overwritten: %q", content)
	}
}

func TestEditorRejectsSymlinkedParent(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	editor, err := New(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Prepare(t.Context(), Input{
		Path: "link/new.txt", ExpectedSHA256: "absent", NewText: "agent\n",
	}); err == nil {
		t.Fatal("Prepare followed a symlinked parent")
	}
}
