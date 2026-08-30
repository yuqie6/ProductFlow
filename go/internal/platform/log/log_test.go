package log

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNewWritesJSONFileAndProcessField(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	logger, err := New(Options{
		Level:         "INFO",
		Format:        FormatConsole,
		Dir:           dir,
		Process:       ProcessAPI,
		MaxBytes:      10 * 1024 * 1024,
		BackupCount:   5,
		RetentionDays: 14,
		Stderr:        &stderr,
		DisableColor:  true,
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
	if !strings.Contains(text, `"caller":`) {
		t.Fatalf("file log missing caller: %s", text)
	}
}

func TestNewConsoleOmitsJSONAndCaller(t *testing.T) {
	var stderr bytes.Buffer
	logger, err := New(Options{
		Level:        "INFO",
		Format:       FormatConsole,
		Process:      ProcessAPI,
		Stderr:       &stderr,
		DisableColor: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("probe", zap.String("request_id", "req-1"))
	_ = logger.Sync()

	got := stderr.String()
	if strings.Count(got, "\n") < 1 {
		t.Fatalf("expected a console line, got %q", got)
	}
	if !strings.Contains(got, "INFO") || !strings.Contains(got, "api") || !strings.Contains(got, "probe") {
		t.Fatalf("console line missing columns: %q", got)
	}
	if !strings.Contains(got, "request_id=req-1") {
		t.Fatalf("console line missing key=value field: %q", got)
	}
	if strings.Contains(got, `"request_id"`) || strings.Contains(got, `"msg"`) {
		t.Fatalf("console line still looks like JSON: %q", got)
	}
	if strings.Contains(got, "log.go") || strings.Contains(got, "log_test.go") {
		t.Fatalf("console line includes caller: %q", got)
	}
}

func TestNewFileKeepsDebugWhenConsoleIsInfo(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	logger, err := New(Options{
		Level:        "INFO",
		Format:       FormatConsole,
		Dir:          dir,
		Process:      ProcessWorker,
		Stderr:       &stderr,
		DisableColor: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("heartbeat")
	logger.Info("listen")
	_ = logger.Sync()

	console := stderr.String()
	if strings.Contains(console, "heartbeat") {
		t.Fatalf("console showed debug line: %q", console)
	}
	if !strings.Contains(console, "listen") {
		t.Fatalf("console missing info line: %q", console)
	}

	body, err := os.ReadFile(FilePath(dir, ProcessWorker))
	if err != nil {
		t.Fatal(err)
	}
	file := string(body)
	if !strings.Contains(file, `"msg":"heartbeat"`) {
		t.Fatalf("file missing debug line: %s", file)
	}
	if !strings.Contains(file, `"msg":"listen"`) {
		t.Fatalf("file missing info line: %s", file)
	}
}

func TestNewJSONFormatWritesJSONToStderr(t *testing.T) {
	var stderr bytes.Buffer
	logger, err := New(Options{
		Level:        "INFO",
		Format:       FormatJSON,
		Process:      ProcessAPI,
		Stderr:       &stderr,
		DisableColor: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("probe", zap.String("request_id", "req-1"))
	_ = logger.Sync()

	got := stderr.String()
	if !strings.Contains(got, `"msg":"probe"`) || !strings.Contains(got, `"request_id":"req-1"`) {
		t.Fatalf("expected JSON stderr: %q", got)
	}
}

func TestPrettyEncoderRendersNestedObjectAsKeyValue(t *testing.T) {
	var buf bytes.Buffer
	core := zapcore.NewCore(newPrettyEncoder(false), zapcore.AddSync(&buf), zapcore.InfoLevel)
	logger := zap.New(core).With(zap.String("process", "dispatcher"))
	logger.Info("dispatcher cycle", zap.Int("pending", 0), zap.Int("sent", 2))
	_ = logger.Sync()

	got := buf.String()
	if !strings.Contains(got, "dispatcher cycle") {
		t.Fatalf("missing message: %q", got)
	}
	if !strings.Contains(got, "pending=0") || !strings.Contains(got, "sent=2") {
		t.Fatalf("missing flattened fields: %q", got)
	}
	if strings.Contains(got, "process=") {
		t.Fatalf("process duplicated as field: %q", got)
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
