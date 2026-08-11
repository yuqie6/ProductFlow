package agenttask_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuqie6/agent-harness/agenttask"
)

func TestPublicReadToolValidationRejectsUnsafeDefinitions(t *testing.T) {
	base := agenttask.Config{
		Database: t.TempDir() + "/jobs.db", Workspace: t.TempDir(),
		Provider: agenttask.ProviderConfig{APIKey: "key", BaseURL: "https://provider.invalid/v1", Model: "model"},
		Policy:   testPolicy(),
	}
	tests := []agenttask.Tool{
		{Name: "bad name", Description: "bad", Handler: func(context.Context, json.RawMessage) (string, error) { return "", nil }},
		{Name: "valid", Description: "missing handler"},
		{Name: "valid", Description: "bad schema", Parameters: map[string]any{"type": "array"}, Handler: func(context.Context, json.RawMessage) (string, error) { return "", nil }},
	}
	for _, tool := range tests {
		config := base
		config.Tools = []agenttask.Tool{tool}
		if _, err := agenttask.Open(config); err == nil || !strings.Contains(err.Error(), "agenttask tool") {
			t.Fatalf("tool = %#v, error = %v", tool, err)
		}
	}
}
