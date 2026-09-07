package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizePostgresURL(t *testing.T) {
	got := NormalizePostgresURL("postgresql+psycopg://productflow:secret@127.0.0.1:15432/productflow")
	want := "postgres://productflow:secret@127.0.0.1:15432/productflow"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveStorageRootAbsolute(t *testing.T) {
	got, err := ResolveStorageRoot("/app/storage")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/app/storage" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveStorageRootRelativeUsesRepoRoot(t *testing.T) {
	got, err := ResolveStorageRoot("./storage-dev")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, string(filepath.Separator)+"storage-dev") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, string(filepath.Separator)+"go"+string(filepath.Separator)+"storage-dev") {
		t.Fatalf("resolved under go module dir: %q", got)
	}
}

func TestResolveLogDirDefaultsUnderStorage(t *testing.T) {
	got, err := ResolveLogDir("", "/app/storage")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/app/storage", "logs")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveLogDirAbsolute(t *testing.T) {
	got, err := ResolveLogDir("/var/log/productflow", "/app/storage")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/var/log/productflow" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveLogDirRelativeUsesRepoRoot(t *testing.T) {
	got, err := ResolveLogDir("./storage-dev/logs", "/unused")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, filepath.Join("storage-dev", "logs")) {
		t.Fatalf("got %q", got)
	}
}

func TestLoadWorkerMetricsAddr(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Setenv("DATABASE_URL", "postgres://productflow:secret@127.0.0.1:15432/productflow")
	}
	if os.Getenv("SESSION_SECRET") == "" {
		t.Setenv("SESSION_SECRET", "test-session-secret-for-worker-metrics")
	}
	t.Setenv("WORKER_METRICS_ADDR", "  0.0.0.0:29286  ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkerMetricsAddr != "0.0.0.0:29286" {
		t.Fatalf("WorkerMetricsAddr=%q", cfg.WorkerMetricsAddr)
	}
}

func TestLoadQuotaTrialUnits(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Setenv("DATABASE_URL", "postgres://productflow:secret@127.0.0.1:15432/productflow")
	}
	if os.Getenv("SESSION_SECRET") == "" {
		t.Setenv("SESSION_SECRET", "test-session-secret-for-quota-trial")
	}
	t.Setenv("QUOTA_TRIAL_UNITS", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuotaTrialUnits != 0 {
		t.Fatalf("QuotaTrialUnits=%d", cfg.QuotaTrialUnits)
	}
	t.Setenv("QUOTA_TRIAL_UNITS", "-1")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuotaTrialUnits != 100 {
		t.Fatalf("negative clamp QuotaTrialUnits=%d", cfg.QuotaTrialUnits)
	}
}

func TestValidConfiguredOrigin(t *testing.T) {
	for _, raw := range []string{"http://localhost:29283", "https://draw.example"} {
		if !validConfiguredOrigin(raw) {
			t.Fatalf("validConfiguredOrigin(%q)=false", raw)
		}
	}
	for _, raw := range []string{"null", "*", "http://user@example.test", "http://example.test/path", "http://example.test:bad"} {
		if validConfiguredOrigin(raw) {
			t.Fatalf("validConfiguredOrigin(%q)=true", raw)
		}
	}
}
