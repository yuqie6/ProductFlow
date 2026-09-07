package providers

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
)

func TestMalformedPromptResponseRetainsDecodeCause(t *testing.T) {
	_, _, _, err := parseResponsesStructured([]byte(`{"output":`), "fixture", nil)
	var syntax *json.SyntaxError
	if !errors.Is(err, graph.ErrProviderUnknown()) || !errors.As(err, &syntax) {
		t.Fatalf("lost decode cause: %v", err)
	}
}
