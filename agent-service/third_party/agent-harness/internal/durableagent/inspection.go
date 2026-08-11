package durableagent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
)

// WorkspaceFromJob returns the workspace snapshot embedded in an agent task.
// It lets control-plane clients scope a shared journal without reimplementing
// the persisted model-input protocol.
func WorkspaceFromJob(job durable.Job) (string, error) {
	if job.Kind != durable.JobKindAgentTurn {
		return "", fmt.Errorf("job %s 不是 durable agent task", job.ID)
	}
	for _, step := range job.Steps {
		if step.Tool != modelToolName {
			continue
		}
		input, err := decodeModelInput(step.Input)
		if err != nil {
			return "", err
		}
		if input.Workspace == "" {
			return "", errors.New("durable agent 模型输入缺少 workspace")
		}
		return input.Workspace, nil
	}
	return "", errors.New("durable agent job 缺少模型步骤")
}

// ToolNamesFromJob returns the unique model tool names embedded in an agent
// task. Control planes can use it to reject tasks whose operator-owned tools
// they cannot reconstruct before opening a runner.
func ToolNamesFromJob(job durable.Job) ([]string, error) {
	if job.Kind != durable.JobKindAgentTurn {
		return nil, fmt.Errorf("job %s 不是 durable agent task", job.ID)
	}
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, step := range job.Steps {
		if step.Tool != modelToolName {
			continue
		}
		input, err := decodeModelInput(step.Input)
		if err != nil {
			return nil, err
		}
		for _, tool := range input.Tools {
			name := strings.TrimSpace(tool.Function.Name)
			if name == "" {
				continue
			}
			if _, duplicate := seen[name]; duplicate {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		for _, step := range job.Steps {
			if step.Tool == modelToolName {
				return names, nil
			}
		}
		return nil, errors.New("durable agent job 缺少模型步骤")
	}
	return names, nil
}

// EditModeFromJob returns the persisted edit policy for an agent task. Jobs
// created before edit_mode was added are inferred from their immutable tool
// catalog so control planes can still select a compatible runner.
func EditModeFromJob(job durable.Job) (string, error) {
	if job.Kind != durable.JobKindAgentTurn {
		return "", fmt.Errorf("job %s 不是 durable agent task", job.ID)
	}
	for _, step := range job.Steps {
		if step.Tool != modelToolName {
			continue
		}
		input, err := decodeModelInput(step.Input)
		if err != nil {
			return "", err
		}
		switch input.EditMode {
		case editModeReadOnly, editModeReview, editModeAuto:
			return input.EditMode, nil
		case "":
		default:
			return "", fmt.Errorf("durable agent 模型输入包含未知编辑策略 %q", input.EditMode)
		}
		for _, tool := range input.Tools {
			if tool.Function.Name != durable.FileEditToolName {
				continue
			}
			if strings.Contains(tool.Function.Description, editReviewDescription) {
				return editModeReview, nil
			}
			return editModeAuto, nil
		}
		return editModeReadOnly, nil
	}
	return "", errors.New("durable agent job 缺少模型步骤")
}

func (r *Runner) validatePending(job durable.Job) error {
	for _, step := range job.Steps {
		switch step.Tool {
		case modelToolName:
			input, err := decodeModelInput(step.Input)
			if err != nil {
				return err
			}
			if err := r.model.validate(input); err != nil {
				return err
			}
		case compactionToolName:
			input, err := decodeCompactionInput(step.Input)
			if err != nil {
				return err
			}
			if _, err := r.compact.validate(input); err != nil {
				return err
			}
		}
	}
	return nil
}

// Transcript returns the exact terminal Responses conversation for seeding a
// later application Turn. It keeps provider output items and tool linkage, but
// excludes the runner-owned fixed system prompt so the next task can prepend
// the currently validated copy.
func (r *Runner) Transcript(ctx context.Context, jobID string) ([]llm.Message, error) {
	job, err := r.engine.Get(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	if job.Status != durable.JobSucceeded {
		return nil, fmt.Errorf("durable agent task %s 没有成功终态 transcript", job.ID)
	}
	current, err := r.inspect(job)
	if err != nil {
		return nil, err
	}
	if len(current.result.Message.ToolCalls) != 0 || len(current.toolSteps) != 0 || current.compaction != nil {
		return nil, errors.New("durable agent 终态 transcript 缺少最终模型回答")
	}
	messages := append(llm.CloneMessages(current.input.Messages), llm.CloneMessages([]llm.Message{current.result.Message})...)
	if r.system != "" {
		if len(messages) == 0 || messages[0].Role != "system" || messages[0].Content == nil || *messages[0].Content != r.system {
			return nil, fmt.Errorf("%w: durable agent system prompt 已改变", durable.ErrConflict)
		}
		messages = messages[1:]
	}
	return llm.CloneMessages(messages), nil
}

type phase struct {
	tail             durable.Step
	input            modelInput
	result           modelResult
	toolSteps        []durable.Step
	compaction       *durable.Step
	modelOrdinal     int
	toolRoundsBefore int
}

func (r *Runner) inspect(job durable.Job) (phase, error) {
	lastModel := -1
	modelOrdinal := 0
	toolRounds := 0
	for index, step := range job.Steps {
		if step.Tool == compactionToolName {
			input, err := decodeCompactionInput(step.Input)
			if err != nil {
				return phase{}, err
			}
			if _, err := r.compact.validate(input); err != nil {
				return phase{}, err
			}
			if step.Status == durable.StepSucceeded {
				if _, err := decodeCompactionResult(step.Result); err != nil {
					return phase{}, err
				}
			}
		}
		if step.Tool != modelToolName {
			continue
		}
		modelOrdinal++
		input, err := decodeModelInput(step.Input)
		if err != nil {
			return phase{}, err
		}
		if err := r.model.validate(input); err != nil {
			return phase{}, err
		}
		if step.Status != durable.StepSucceeded {
			return phase{}, fmt.Errorf("模型步骤 %s 尚未成功", step.ID)
		}
		result, err := decodeModelResult(step.Result)
		if err != nil {
			return phase{}, err
		}
		lastModel = index
		if len(result.Message.ToolCalls) > 0 {
			toolRounds++
		}
	}
	if lastModel < 0 {
		return phase{}, errors.New("durable agent job 缺少模型步骤")
	}
	modelStep := job.Steps[lastModel]
	input, _ := decodeModelInput(modelStep.Input)
	result, _ := decodeModelResult(modelStep.Result)
	following := append([]durable.Step(nil), job.Steps[lastModel+1:]...)
	expectedGroups := make([][]durableCallStep, len(result.Message.ToolCalls))
	expectedCount := 0
	for index, call := range result.Message.ToolCalls {
		expectedGroups[index] = r.normalizeDurableCall(call)
		expectedCount += len(expectedGroups[index])
	}
	if len(following) != 0 && len(following) != expectedCount && len(following) != expectedCount+1 {
		return phase{}, errors.New("durable agent 工具步骤数量与模型调用不一致")
	}
	var compaction *durable.Step
	if len(following) == expectedCount+1 {
		if expectedCount == 0 || following[expectedCount].Tool != compactionToolName {
			return phase{}, errors.New("durable agent 模型轮次包含无效压缩步骤")
		}
		copy := following[expectedCount]
		compaction = &copy
		following = following[:expectedCount]
	}
	toolSteps := make([]durable.Step, 0, len(expectedGroups))
	if len(following) > 0 {
		position := 0
		for _, group := range expectedGroups {
			for _, expected := range group {
				step := following[position]
				if step.Status != durable.StepSucceeded {
					return phase{}, fmt.Errorf("工具步骤 %s 尚未成功", step.ID)
				}
				if step.Tool != expected.tool {
					return phase{}, fmt.Errorf("工具步骤 %s 使用 %s,模型要求 %s", step.ID, step.Tool, expected.tool)
				}
				position++
			}
			toolSteps = append(toolSteps, following[position-1])
		}
	}
	roundsBefore := toolRounds
	if len(result.Message.ToolCalls) > 0 {
		roundsBefore--
	}
	return phase{
		tail: job.Steps[len(job.Steps)-1], input: input, result: result, toolSteps: toolSteps, compaction: compaction,
		modelOrdinal: modelOrdinal, toolRoundsBefore: roundsBefore,
	}, nil
}

func (p phase) compactedMessages(compact *compactionTool, resultCodecs map[string]toolResultCodec) ([]llm.Message, error) {
	if p.compaction == nil {
		return nil, errors.New("durable agent 当前轮次没有压缩结果")
	}
	source, err := p.nextMessages(resultCodecs)
	if err != nil {
		return nil, err
	}
	input, err := decodeCompactionInput(p.compaction.Input)
	if err != nil {
		return nil, err
	}
	digest, err := digestDurableMessages(source)
	if err != nil {
		return nil, err
	}
	if digest != input.SourceDigest {
		return nil, fmt.Errorf("%w: 压缩 Step 不是从当前模型与工具结果派生", durable.ErrConflict)
	}
	result, err := decodeCompactionResult(p.compaction.Result)
	if err != nil {
		return nil, err
	}
	return compact.compactedMessages(input, result)
}

func (p phase) nextMessages(resultCodecs map[string]toolResultCodec) ([]llm.Message, error) {
	messages := append([]llm.Message(nil), p.input.Messages...)
	messages = append(messages, p.result.Message)
	for index, step := range p.toolSteps {
		codec, ok := resultCodecs[step.Tool]
		if !ok {
			return nil, fmt.Errorf("工具 %s 缺少已验证的结果编码", step.Tool)
		}
		output, err := toolResultValue(step, codec)
		if err != nil {
			return nil, err
		}
		callID := p.result.Message.ToolCalls[index].ID
		messages = append(messages, output.Message(callID))
	}
	return messages, nil
}

func (r *Runner) failBoundary(ctx context.Context, job durable.Job, cause error) (Result, error) {
	failed, err := r.engine.Fail(ctx, job.ID, job.Steps[len(job.Steps)-1].ID, cause.Error())
	if err != nil {
		return resultFor(job, ""), errors.Join(cause, err)
	}
	return resultFor(failed, ""), cause
}

func resultFor(job durable.Job, output string) Result {
	result := Result{Job: job, Output: output}
	for _, step := range job.Steps {
		switch step.Tool {
		case modelToolName:
			result.ModelCalls++
		case compactionToolName:
			result.CompactionCalls++
		case editReviewToolName:
			result.ApprovalCalls++
		default:
			result.ToolCalls++
		}
	}
	return result
}
