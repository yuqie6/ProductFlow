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

func TestLoadSMTPEnvironmentOverlayPreservesPasswordWhitespace(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://fake:fake@127.0.0.1:15432/fake")
	t.Setenv("SESSION_SECRET", "fake-session-secret-for-smtp")
	t.Setenv("AUTH_RATE_LIMIT_NAMESPACE", "productflow:test:smtp")
	t.Setenv("BACKEND_CORS_ORIGINS", "")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	t.Setenv("STORAGE_ROOT", t.TempDir())
	t.Setenv("LOG_DIR", t.TempDir())
	t.Setenv("SMTP_HOST", " smtp.test.invalid ")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_SECURITY", " tls ")
	t.Setenv("SMTP_USERNAME", " sender@test.invalid ")
	t.Setenv("SMTP_PASSWORD", "  fixed fake smtp secret  ")
	t.Setenv("SMTP_FROM_ADDRESS", " sender@test.invalid ")
	t.Setenv("SMTP_FROM_NAME", " ProductFlow ")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTPHost != "smtp.test.invalid" {
		t.Fatalf("SMTPHost=%q", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 465 {
		t.Fatalf("SMTPPort=%d", cfg.SMTPPort)
	}
	if cfg.SMTPSecurity != "tls" {
		t.Fatalf("SMTPSecurity=%q", cfg.SMTPSecurity)
	}
	if cfg.SMTPUsername != "sender@test.invalid" {
		t.Fatalf("SMTPUsername=%q", cfg.SMTPUsername)
	}
	if cfg.SMTPPassword != "  fixed fake smtp secret  " {
		t.Fatalf("SMTPPassword=%q", cfg.SMTPPassword)
	}
	if cfg.SMTPFromAddress != "sender@test.invalid" || cfg.SMTPFromName != "ProductFlow" {
		t.Fatalf("SMTP from fields=%q/%q", cfg.SMTPFromAddress, cfg.SMTPFromName)
	}
}
