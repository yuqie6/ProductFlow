package durableagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yuqie6/agent-harness/durable"
	questionpkg "github.com/yuqie6/agent-harness/internal/question"
	"github.com/yuqie6/agent-harness/internal/tools"
)

type PendingQuestion struct {
	StepID   string               `json:"step_id"`
	Header   string               `json:"header"`
	Question string               `json:"question"`
	Options  []questionpkg.Option `json:"options"`
}

func QuestionFromJob(job durable.Job) (PendingQuestion, bool, error) {
	for _, step := range job.Steps {
		if step.Status != durable.StepRequiresAction || step.Tool != questionpkg.ToolName {
			continue
		}
		prompt, err := decodeQuestionPrompt(step.Input)
		if err != nil {
			return PendingQuestion{}, false, err
		}
		return PendingQuestion{
			StepID: step.ID, Header: prompt.Header, Question: prompt.Question, Options: prompt.Options,
		}, true, nil
	}
	return PendingQuestion{}, false, nil
}

func (q PendingQuestion) EncodeAnswer(option int) (json.RawMessage, error) {
	prompt := questionpkg.Prompt{Header: q.Header, Question: q.Question, Options: q.Options}
	return questionpkg.EncodeAnswer(prompt, option)
}

func (q PendingQuestion) EncodeFreeTextAnswer(text string) (json.RawMessage, error) {
	answer, err := questionpkg.NewFreeTextAnswer(text)
	if err != nil {
		return nil, err
	}
	return json.Marshal(answer)
}

func questionSchema() tools.Tool {
	return tools.Tool{
		Name:        questionpkg.ToolName,
		Description: "当任务目标、边界或执行选择存在疑问时暂停 durable task,向操作者提供 1 到 8 个建议选项。",
		Parameters:  questionpkg.Parameters(),
	}
}

type durableQuestionTool struct{}

func (durableQuestionTool) Name() string                { return questionpkg.ToolName }
func (durableQuestionTool) Effect() durable.EffectClass { return durable.EffectPure }

func (durableQuestionTool) Prepare(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	prompt, err := decodeQuestionPrompt(raw)
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: 等待操作者回答问题 %q", durable.ErrConflict, prompt.Question)
}

func (durableQuestionTool) Execute(context.Context, durable.Invocation) (json.RawMessage, error) {
	return nil, errors.New("ask_user 必须由操作者 resolution 完成")
}

func (durableQuestionTool) Reconcile(context.Context, durable.Invocation) (durable.ReconcileResult, error) {
	return durable.ReconcileResult{}, errors.New("ask_user 不执行外部副作用")
}

func normalizeQuestionInput(raw json.RawMessage) (json.RawMessage, error) {
	prompt, err := decodeQuestionPrompt(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prompt)
}

func decodeQuestionPrompt(raw json.RawMessage) (questionpkg.Prompt, error) {
	var args struct {
		Header   string               `json:"header"`
		Question string               `json:"question"`
		Options  []questionpkg.Option `json:"options"`
	}
	if err := tools.ParseArguments(raw, &args); err != nil {
		return questionpkg.Prompt{}, fmt.Errorf("解析 ask_user 参数: %w", err)
	}
	return questionpkg.Normalize(args.Header, args.Question, args.Options)
}

func questionResultText(step durable.Step) (string, error) {
	prompt, err := decodeQuestionPrompt(step.Input)
	if err != nil {
		return "", err
	}
	answer, err := questionpkg.DecodeAnswer(step.Result, prompt)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
