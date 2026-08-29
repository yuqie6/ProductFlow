package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewWritesJSONFileAndProcessField(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(Options{
		Level:         "INFO",
		Dir:           dir,
		Process:       ProcessAPI,
		MaxBytes:      10 * 1024 * 1024,
		BackupCount:   5,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("probe", zap.String("request_id", "req-1"))
	_ = logger.Sync()

	body, err := os.ReadFile(FilePath(dir, ProcessAPI))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"process":"api"`) {
		t.Fatalf("missing process field: %s", text)
	}
	if !strings.Contains(text, `"msg":"probe"`) {
		t.Fatalf("missing probe line: %s", text)
	}
	if !strings.Contains(text, `"request_id":"req-1"`) {
		t.Fatalf("missing request_id: %s", text)
	}
}

func TestNewRejectsUnsafeProcessName(t *testing.T) {
	_, err := New(Options{Dir: t.TempDir(), Process: "../etc"})
	if err == nil {
		t.Fatal("expected invalid process name")
	}
}

func TestCleanupOldLogsDeletesExpiredRotations(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "productflow-api.log.1")
	freshPath := filepath.Join(dir, "productflow-api.log")
	otherPath := filepath.Join(dir, "unrelated.log")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshPath, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-20 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	deleted, err := CleanupOldLogs(dir, 14)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted %d, want 1", deleted)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old rotation still present: %v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatal(err)
	}
}
