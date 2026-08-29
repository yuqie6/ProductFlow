package config

import "testing"

func TestNormalizePostgresURL(t *testing.T) {
	got := NormalizePostgresURL("postgresql+psycopg://productflow:secret@127.0.0.1:15432/productflow")
	want := "postgres://productflow:secret@127.0.0.1:15432/productflow"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
