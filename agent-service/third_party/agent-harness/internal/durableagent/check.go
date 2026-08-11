package durableagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/tools"
)

func checkToolSnapshot(checks *tools.NamedCheckSet) pureToolSnapshot {
	return func(raw json.RawMessage) (preparedTool, error) {
		snapshot, err := checks.Snapshot(raw)
		if errors.Is(err, tools.ErrCheckConfigChanged) {
			return preparedTool{}, fmt.Errorf("%w: %v", durable.ErrConflict, err)
		}
		return preparedTool{Arguments: snapshot}, err
	}
}

func (r *Runner) verificationGateError(job durable.Job) error {
	if len(r.requiredChecks) == 0 {
		return nil
	}
	passed := make(map[string]bool, len(r.requiredChecks))
	required := make(map[string]bool, len(r.requiredChecks))
	for _, name := range r.requiredChecks {
		required[name] = true
	}
	for _, step := range job.Steps {
		if step.Tool == durable.FileEditToolName && step.Status == durable.StepSucceeded {
			clear(passed)
			continue
		}
		if step.Tool != tools.RunCheckToolName || step.Status != durable.StepSucceeded {
			continue
		}
		var snapshot tools.CheckSnapshot
		if err := json.Unmarshal(step.Input, &snapshot); err != nil || !required[snapshot.Name] {
			continue
		}
		var result toolExecutionResult
		if err := json.Unmarshal(step.Result, &result); err != nil {
			passed[snapshot.Name] = false
			continue
		}
		passed[snapshot.Name] = strings.TrimSpace(result.Error) == "" && validCheckOutput(result.Output, snapshot.Name)
	}
	var missing []string
	for _, name := range r.requiredChecks {
		if !passed[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: 最后一次编辑后缺少成功检查 %s", ErrVerificationGate, strings.Join(missing, ", "))
}

func validCheckOutput(output, wantName string) bool {
	var result struct {
		Name   string `json:"name"`
		Result struct {
			ExitCode int  `json:"exit_code"`
			TimedOut bool `json:"timed_out"`
		} `json:"result"`
	}
	return json.Unmarshal([]byte(output), &result) == nil && result.Name == wantName &&
		result.Result.ExitCode == 0 && !result.Result.TimedOut
}
