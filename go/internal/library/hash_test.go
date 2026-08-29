package library

import (
	"testing"
	"time"
)

func TestNormalizeKeyCasefold(t *testing.T) {
	got, err := normalizeKey("Straße", kindFolder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "strasse" {
		t.Fatalf("got %q", got)
	}
	tag, err := normalizeKey("  Featured  ", kindTag)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "featured" {
		t.Fatalf("got %q", tag)
	}
}

func TestProvenanceHashMatchesPydanticDump(t *testing.T) {
	captured, err := time.Parse(time.RFC3339Nano, "2026-08-16T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	p := Provenance{
		SchemaVersion:    1,
		SourceType:       SourceProduct,
		SourceID:         "asset-1",
		SHA256:           "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		MIMEType:         "image/png",
		ByteSize:         100,
		Width:            10,
		Height:           10,
		OriginalFilename: "1.png",
		CapturedAt:       captured,
	}
	first, err := provenanceHash(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provenanceHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("%s", first)
	}
	parsed, err := parseProvenance(storeProvenance(p))
	if err != nil {
		t.Fatal(err)
	}
	rehash, err := provenanceHash(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if rehash != first {
		t.Fatalf("store/parse drift %s vs %s", first, rehash)
	}
}

func TestParseProvenanceRejectsExtraField(t *testing.T) {
	_, err := parseProvenance(map[string]any{
		"schema_version":    1,
		"source_type":       SourceProduct,
		"source_id":         "asset-1",
		"sha256":            "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"mime_type":         "image/png",
		"byte_size":         100,
		"width":             10,
		"height":            10,
		"original_filename": "1.png",
		"captured_at":       "2026-08-16T00:00:00+00:00",
		"storage_path":      "media/secret.png",
	})
	if err == nil {
		t.Fatal("expected extra field error")
	}
}
