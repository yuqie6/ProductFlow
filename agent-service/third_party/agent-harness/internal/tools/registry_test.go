package tools

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseArgumentsAcceptsStringAndObject(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "string", raw: json.RawMessage(`"{\"path\":\"a.txt\"}"`)},
		{name: "object", raw: json.RawMessage(`{"path":"a.txt"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Path string `json:"path"`
			}
			if err := ParseArguments(tt.raw, &got); err != nil {
				t.Fatalf("ParseArguments: %v", err)
			}
			if got.Path != "a.txt" {
				t.Fatalf("path = %q", got.Path)
			}
		})
	}
}

func TestRegistryReturnsToolsInStableNameOrder(t *testing.T) {
	r := NewRegistry(
		Tool{Name: "write_file"},
		Tool{Name: "bash"},
		Tool{Name: "read_file"},
	)

	want := []string{"bash", "read_file", "write_file"}
	if got := r.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}

	schemas := r.Schemas()
	got := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		got = append(got, schema.Function.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Schemas() names = %v, want %v", got, want)
	}
}

func TestRegistryRemoveUpdatesFutureCatalogs(t *testing.T) {
	r := NewRegistry(Tool{Name: "read_file"}, Tool{Name: "spawn_agent"})
	if !r.Remove("spawn_agent") || r.Remove("spawn_agent") {
		t.Fatal("Remove did not report registration state")
	}
	if got := r.Names(); !reflect.DeepEqual(got, []string{"read_file"}) {
		t.Fatalf("Names() after Remove = %v", got)
	}
}
