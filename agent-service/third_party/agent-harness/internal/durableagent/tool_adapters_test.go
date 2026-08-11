package durableagent

import (
	"encoding/json"
	"testing"

	"github.com/yuqie6/agent-harness/durable"
)

func TestContentToolResultTurnsExecutionErrorIntoNativeTextPart(t *testing.T) {
	encoded, err := json.Marshal(toolExecutionResult{Error: "invalid gallery result"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := toolResultValue(durable.Step{Tool: "inspect_gallery_images", Result: encoded}, toolResultContentEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != nil || len(result.ContentParts) != 1 || result.ContentParts[0].Type != "input_text" ||
		result.ContentParts[0].Text != "工具执行失败: invalid gallery result" {
		t.Fatalf("result = %#v", result)
	}
}
