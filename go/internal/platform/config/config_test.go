package config

import (
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
