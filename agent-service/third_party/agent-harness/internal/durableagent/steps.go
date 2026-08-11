package durableagent

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/contextmgr"
	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/internal/tools"
)

func (r *Runner) modelStep(jobID string, ordinal int, messages []llm.Message) (durable.StepSpec, error) {
	tokens := contextmgr.TotalTokens(messages) + contextmgr.EstimateToolsTokens(r.catalog)
	if tokens > r.policy.ModelContextWindow {
		return durable.StepSpec{}, fmt.Errorf("%w: 估算 %d tokens,窗口 %d", ErrContextBudget, tokens, r.policy.ModelContextWindow)
	}
	input, err := json.Marshal(modelInput{
		Version: protocolVersion, Workspace: r.workspace, Provider: r.provider, Policy: r.policy,
		EditMode: r.editMode, ToolContractSHA256: r.toolContractSHA256, Messages: messages, Tools: r.catalog,
	})
	if err != nil {
		return durable.StepSpec{}, err
	}
	return durable.StepSpec{
		ID: fmt.Sprintf("%s-model-%04d", jobID, ordinal), Tool: modelToolName,
		Input: input, AwaitContinuation: true,
	}, nil
}

func (r *Runner) toolSteps(jobID string, modelOrdinal int, calls []llm.ToolCall) []durable.StepSpec {
	steps := make([]durable.StepSpec, 0, len(calls))
	for index, call := range calls {
		for _, normalized := range r.normalizeDurableCall(call) {
			kind := "tool"
			if normalized.tool == editReviewToolName {
				kind = "review"
			}
			steps = append(steps, durable.StepSpec{
				ID:   fmt.Sprintf("%s-%s-%04d-%03d", jobID, kind, modelOrdinal, index+1),
				Tool: normalized.tool, Input: normalized.input,
			})
		}
	}
	if len(steps) > 0 {
		steps[len(steps)-1].AwaitContinuation = true
	}
	return steps
}

type durableCallStep struct {
	tool  string
	input json.RawMessage
}

func (r *Runner) normalizeDurableCall(call llm.ToolCall) []durableCallStep {
	name := r.durableToolName(call.Function.Name)
	input, err := normalizeToolArguments(call.Function.Arguments)
	if err == nil && name == (durableQuestionTool{}).Name() {
		input, err = normalizeQuestionInput(input)
	}
	if err == nil && name == tools.RunCheckToolName && r.checks != nil {
		input, err = r.checks.Snapshot(input)
	}
	if err == nil && name != rejectionToolName {
		steps := []durableCallStep{{tool: name, input: input}}
		if name == durable.FileEditToolName && r.reviewEdit {
			steps = append([]durableCallStep{{tool: editReviewToolName, input: input}}, steps...)
		}
		return steps
	}
	message := fmt.Sprintf("工具 %q 不可用", call.Function.Name)
	if err != nil {
		message = "工具参数无效: " + err.Error()
	}
	input, _ = json.Marshal(toolExecutionResult{Error: message})
	return []durableCallStep{{tool: rejectionToolName, input: input}}
}

func (r *Runner) durableToolName(requested string) string {
	if r.allowed[requested] {
		return requested
	}
	return rejectionToolName
}

func normalizeToolArguments(raw json.RawMessage) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, errors.New("arguments 不是 JSON")
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		decoded := json.RawMessage(encoded)
		if !json.Valid(decoded) {
			return nil, errors.New("arguments 字符串内容不是 JSON")
		}
		return decoded, nil
	}
	return append(json.RawMessage(nil), raw...), nil
}
