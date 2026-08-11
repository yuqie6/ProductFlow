package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ResolveRequiredAction applies an audited operator decision to the current
// requires_action step. Retry prepares a new attempt but never executes it.
func (e *Engine) ResolveRequiredAction(ctx context.Context, jobID string, resolution RequiredActionResolution) (Job, error) {
	jobID = strings.TrimSpace(jobID)
	resolution.StepID = strings.TrimSpace(resolution.StepID)
	resolution.AttemptID = strings.TrimSpace(resolution.AttemptID)
	resolution.Actor = strings.TrimSpace(resolution.Actor)
	resolution.Reason = strings.TrimSpace(resolution.Reason)
	if resolution.StepID == "" || len(resolution.StepID) > 300 {
		return Job{}, errors.New("required action step_id 不能为空且不能超过 300 字符")
	}
	if resolution.AttemptID == "" || len(resolution.AttemptID) > 300 {
		return Job{}, errors.New("required action attempt_id 不能为空且不能超过 300 字符")
	}
	if resolution.Actor == "" || len(resolution.Actor) > 200 {
		return Job{}, errors.New("required action actor 不能为空且不能超过 200 字符")
	}
	if resolution.Reason == "" || len(resolution.Reason) > 2000 {
		return Job{}, errors.New("required action reason 不能为空且不能超过 2000 字符")
	}
	var prepared json.RawMessage
	switch resolution.Outcome {
	case ResolutionApplied:
		result, err := normalizeJSON(resolution.Result)
		if err != nil {
			return Job{}, fmt.Errorf("required action result: %w", err)
		}
		resolution.Result = result
	case ResolutionFailed:
		if len(resolution.Result) != 0 {
			return Job{}, errors.New("failed resolution 不能携带 result")
		}
	case ResolutionRetry:
		if len(resolution.Result) != 0 {
			return Job{}, errors.New("retry resolution 不能携带 result")
		}
		job, err := e.store.getJob(ctx, jobID)
		if err != nil {
			return Job{}, err
		}
		if job.Status != JobRequiresAction {
			return Job{}, fmt.Errorf("%w: job %s 状态为 %s,不是 requires_action", ErrInvalidState, jobID, job.Status)
		}
		step, ok := requiredActionStep(job)
		if !ok {
			return Job{}, fmt.Errorf("%w: job %s 没有待处理步骤", ErrInvalidState, jobID)
		}
		if step.ID != resolution.StepID {
			return Job{}, fmt.Errorf("%w: job %s 当前待处理 step 为 %s,不是 %s", ErrInvalidState, jobID, step.ID, resolution.StepID)
		}
		target, err := e.PendingResolution(ctx, jobID)
		if err != nil {
			return Job{}, err
		}
		if target.AttemptID != resolution.AttemptID {
			return Job{}, fmt.Errorf("%w: job %s 当前待处理 attempt 为 %s,不是 %s", ErrInvalidState, jobID, target.AttemptID, resolution.AttemptID)
		}
		tool := e.tools[step.Tool]
		if tool == nil {
			return Job{}, fmt.Errorf("required action retry 需要未注册工具 %q", step.Tool)
		}
		if tool.Effect() != step.Effect {
			return Job{}, fmt.Errorf("工具 %s 的副作用契约从 %s 变为 %s,拒绝 retry", step.Tool, step.Effect, tool.Effect())
		}
		prepared, err = tool.Prepare(ctx, cloneJSON(step.Input))
		if err != nil {
			return Job{}, fmt.Errorf("required action retry prepare: %w", err)
		}
		prepared, err = normalizeJSON(prepared)
		if err != nil {
			return Job{}, fmt.Errorf("required action retry prepared payload: %w", err)
		}
	default:
		return Job{}, fmt.Errorf("required action outcome 无效: %q", resolution.Outcome)
	}
	if err := e.store.resolveRequiredAction(ctx, jobID, resolution, prepared, time.Now().UTC()); err != nil {
		return Job{}, err
	}
	return e.store.getJob(ctx, jobID)
}

func requiredActionStep(job Job) (Step, bool) {
	for _, step := range job.Steps {
		if step.Status == StepRequiresAction {
			return step, true
		}
	}
	return Step{}, false
}
