package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolvedToolContractVersionIncludesDraftSchema(t *testing.T) {
	empty := resolvedToolContractVersion(map[string]any{})
	typed := resolvedToolContractVersion(map[string]any{"type": "object"})
	var schemaDoc map[string]any
	if err := json.Unmarshal(globalDraftSchemaJSON, &schemaDoc); err != nil {
		t.Fatal(err)
	}
	global := resolvedToolContractVersion(schemaDoc)
	if len(empty) != 64 || empty == ToolManifestVersion || empty == typed || global == empty || global == typed {
		t.Fatalf("hashes empty=%s typed=%s global=%s static=%s", empty, typed, global, ToolManifestVersion)
	}
}

func TestDraftSchemaCanonicalJSONSortsKeysAndDoesNotEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{"z": 1, "a": map[string]any{"b": 2, "<": "x&y"}}); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSuffix(buf.String(), "\n")
	want := `{"a":{"<":"x&y","b":2},"z":1}`
	if got != want {
		t.Fatalf("canonical JSON drifted from TypeScript encodeCanonical: got %s want %s", got, want)
	}
}
