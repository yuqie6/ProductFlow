package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuqie6/agent-harness/internal/artifact"
)

func TestArtifactToolReadsOnlyKnownContentAddressedOutput(t *testing.T) {
	store, err := artifact.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, reference, err := store.Pack(strings.Repeat("result", 100), 10)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := NewArtifactTool(store, 32)
	if err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"id":"` + reference.ID + `","limit":12}`)
	if !tool.CanAutoApprove(args) {
		t.Fatal("valid artifact page should auto-approve")
	}
	output, err := tool.Handler(context.Background(), args)
	if err != nil || !strings.Contains(output, `"next_offset":12`) || !strings.Contains(output, `"content":"resultresult"`) {
		t.Fatalf("output = %s, err = %v", output, err)
	}
	if tool.CanAutoApprove(json.RawMessage(`{"id":"../../auth.json"}`)) {
		t.Fatal("invalid artifact path auto-approved")
	}
	if tool.CanAutoApprove(json.RawMessage(`{"id":"` + reference.ID + `","limit":33}`)) {
		t.Fatal("configured page limit was bypassed")
	}
}
