package app

import (
	"reflect"
	"testing"
)

func TestProductContextToolUsesExplicitStrictEmptyObjectSchema(t *testing.T) {
	tools := scopedReadTools(nil, Scope{})
	for _, tool := range tools {
		if tool.Name != productContextToolName {
			continue
		}
		want := map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		}
		if !tool.Strict || !reflect.DeepEqual(tool.Parameters, want) {
			t.Fatalf("product context tool strict schema = %#v, want %#v", tool.Parameters, want)
		}
		return
	}
	t.Fatalf("tool %q is not registered", productContextToolName)
}
