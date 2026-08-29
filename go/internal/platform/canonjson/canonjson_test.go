package canonjson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestCompactDoesNotHTMLEscape(t *testing.T) {
	raw, err := Compact(map[string]any{
		"b": "a&b<c>",
		"a": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u003e`) || strings.Contains(got, `\u0026`) {
		t.Fatalf("HTML-escaped compact JSON: %s", got)
	}
	want := `{"a":1,"b":"a&b<c>"}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSHA256HexMatchesUnescapedCompact(t *testing.T) {
	value := map[string]any{
		"text": "x&y<z>",
		"ok":   true,
	}
	raw, err := Compact(value)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`\u003c`)) || bytes.Contains(raw, []byte(`\u003e`)) || bytes.Contains(raw, []byte(`\u0026`)) {
		t.Fatalf("HTML-escaped compact JSON: %s", raw)
	}
	got, err := SHA256Hex(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestCompactSortsNestedKeys(t *testing.T) {
	raw, err := Compact(map[string]any{
		"z": map[string]any{"b": 2, "a": 1},
		"y": []any{"p&q", map[string]any{"d": "<", "c": ">"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"y":["p&q",{"c":">","d":"<"}],"z":{"a":1,"b":2}}`
	if string(raw) != want {
		t.Fatalf("got %s want %s", raw, want)
	}
}
