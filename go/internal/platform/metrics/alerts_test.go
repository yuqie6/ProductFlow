package metrics

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentAlertRulesCoverRequiredNames(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "ops", "prometheus", "agent-alerts.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	required := []string{
		"ProductFlowAgentUnknownTurns",
		"ProductFlowAgentProjectionMismatch",
		"ProductFlowAgentExpiredLeases",
		"ProductFlowAgentEffectUnknown",
		"ProductFlowAgentSequenceConflicts",
		"ProductFlowAgentUsageMissing",
		"ProductFlowQueueBacklog",
		"ProductFlowAgentProviderHTTP5xx",
		"ProductFlowAgentActiveTimeout",
	}
	for _, name := range required {
		if !strings.Contains(body, "alert: "+name) {
			t.Fatalf("missing alert %s", name)
		}
	}
	forbidden := []string{"conversation_id", "turn_id", "user_text", "error_detail"}
	for _, label := range forbidden {
		if strings.Contains(body, label) {
			t.Fatalf("alert rules must not use unbounded label %s", label)
		}
	}
}
