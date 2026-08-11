package durableagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
)

type PendingEditReview struct {
	StepID         string `json:"step_id"`
	Path           string `json:"path"`
	ExpectedSHA256 string `json:"expected_sha256"`
	OldText        string `json:"old_text"`
	NewText        string `json:"new_text"`
	Detail         string `json:"detail,omitempty"`
}

func EditReviewFromJob(job durable.Job) (PendingEditReview, bool, error) {
	for index, step := range job.Steps {
		if step.Status != durable.StepRequiresAction || step.Tool != editReviewToolName {
			continue
		}
		if index+1 >= len(job.Steps) || job.Steps[index+1].Tool != durable.FileEditToolName ||
			job.Steps[index+1].Status != durable.StepPending || !bytes.Equal(step.Input, job.Steps[index+1].Input) {
			return PendingEditReview{}, false, fmt.Errorf("%w: edit review 与待执行 edit_file 不匹配", durable.ErrConflict)
		}
		input, err := decodeEditReviewInput(step.Input)
		if err != nil {
			return PendingEditReview{}, false, err
		}
		return PendingEditReview{
			StepID: step.ID, Path: input.Path, ExpectedSHA256: input.ExpectedSHA256,
			OldText: input.OldText, NewText: input.NewText, Detail: step.Error,
		}, true, nil
	}
	return PendingEditReview{}, false, nil
}

type editReviewTool struct {
	source *durable.FileEditTool
}

func newEditReviewTool(source *durable.FileEditTool) *editReviewTool {
	return &editReviewTool{source: source}
}

func (t *editReviewTool) Name() string                { return editReviewToolName }
func (t *editReviewTool) Effect() durable.EffectClass { return durable.EffectPure }

func (t *editReviewTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if _, err := t.source.Prepare(ctx, raw); err != nil {
		return nil, err
	}
	input, err := decodeEditReviewInput(raw)
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: edit_file 等待操作者批准: %s", durable.ErrConflict, input.Path)
}

func (*editReviewTool) Execute(context.Context, durable.Invocation) (json.RawMessage, error) {
	return nil, errors.New("agent_edit_review 必须由操作者 resolution 完成")
}

func (*editReviewTool) Reconcile(context.Context, durable.Invocation) (durable.ReconcileResult, error) {
	return durable.ReconcileResult{}, errors.New("agent_edit_review 不执行外部副作用")
}

func decodeEditReviewInput(raw json.RawMessage) (durable.FileEditInput, error) {
	var input durable.FileEditInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, fmt.Errorf("解析 edit review: %w", err)
	}
	if strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.ExpectedSHA256) == "" {
		return input, errors.New("edit review 缺少 path 或 expected_sha256")
	}
	return input, nil
}
