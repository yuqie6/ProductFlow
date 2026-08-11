package durableagent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/tools"
)

func TestVerificationGateRequiresPassAfterLatestEdit(t *testing.T) {
	runner := &Runner{requiredChecks: []string{"unit", "smoke"}}
	pass := func(name string) durable.Step {
		input, _ := json.Marshal(tools.CheckSnapshot{Name: name, ConfigSHA256: "digest"})
		result, _ := json.Marshal(toolExecutionResult{Output: `{"name":"` + name + `","result":{"exit_code":0,"timed_out":false}}`})
		return durable.Step{Tool: tools.RunCheckToolName, Input: input, Result: result, Status: durable.StepSucceeded}
	}
	fail := func(name string) durable.Step {
		input, _ := json.Marshal(tools.CheckSnapshot{Name: name, ConfigSHA256: "digest"})
		result, _ := json.Marshal(toolExecutionResult{Error: "exit 1"})
		return durable.Step{Tool: tools.RunCheckToolName, Input: input, Result: result, Status: durable.StepSucceeded}
	}
	edit := durable.Step{Tool: "edit_file", Status: durable.StepSucceeded}

	tests := []struct {
		name    string
		steps   []durable.Step
		wantErr bool
	}{
		{name: "both pass", steps: []durable.Step{edit, pass("unit"), pass("smoke")}},
		{name: "missing gate", steps: []durable.Step{edit, pass("unit")}, wantErr: true},
		{name: "pass before edit is stale", steps: []durable.Step{pass("unit"), pass("smoke"), edit}, wantErr: true},
		{name: "latest failure wins", steps: []durable.Step{edit, pass("unit"), pass("smoke"), fail("unit")}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := runner.verificationGateError(durable.Job{Steps: test.steps})
			if test.wantErr != errors.Is(err, ErrVerificationGate) {
				t.Fatalf("error = %v, want verification error = %v", err, test.wantErr)
			}
		})
	}
}
