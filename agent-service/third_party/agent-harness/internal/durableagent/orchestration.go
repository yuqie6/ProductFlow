package durableagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuqie6/agent-harness/durable"
)

func (r *Runner) continueJob(ctx context.Context, jobID string) (Result, error) {
	for {
		advanced, err := r.AdvanceOne(ctx, jobID)
		if err != nil || advanced.Terminal {
			return advanced.Result, err
		}
		if advanced.Progressed {
			continue
		}
		if err := waitForContention(ctx); err != nil {
			return advanced.Result, err
		}
	}
}

// AdvanceOne advances a task across at most one durable orchestration
// boundary. It is safe for multiple controllers to call concurrently; stale
// tail updates are observed as contention instead of task failure.
func (r *Runner) AdvanceOne(ctx context.Context, jobID string) (AdvanceResult, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return AdvanceResult{}, errors.New("durable agent advance 缺少 task ID")
	}
	job, err := r.engine.Get(ctx, jobID)
	if err != nil {
		return AdvanceResult{}, err
	}
	switch job.Status {
	case durable.JobPending, durable.JobRunning:
		if err := r.validatePending(job); err != nil {
			return advanceFor(job, "", false, false), err
		}
		run, runErr := r.engine.Run(ctx, job.ID)
		if isContention(runErr) {
			return r.observeLatest(ctx, job.ID)
		}
		if runErr != nil {
			return advanceFor(run, "", true, isTerminal(run.Status)), runErr
		}
		return r.observe(run, true)
	case durable.JobAwaitingSteps:
		return r.advanceAwaiting(ctx, job)
	default:
		return r.observe(job, false)
	}
}

func (r *Runner) advanceAwaiting(ctx context.Context, job durable.Job) (AdvanceResult, error) {
	phase, err := r.inspect(job)
	if err != nil {
		return advanceFor(job, "", false, false), err
	}
	if len(phase.result.Message.ToolCalls) == 0 {
		if gateErr := r.verificationGateError(job); gateErr != nil {
			result, failErr := r.failBoundary(ctx, job, gateErr)
			if isContention(failErr) {
				return r.observeLatest(ctx, job.ID)
			}
			return advanceFromResult(result, true, true), failErr
		}
		if gateErr := requiredArtifactGateError(job, r.requiredArtifact); gateErr != nil {
			result, failErr := r.failBoundary(ctx, job, gateErr)
			if isContention(failErr) {
				return r.observeLatest(ctx, job.ID)
			}
			return advanceFromResult(result, true, true), failErr
		}
		completed, completeErr := r.engine.Complete(ctx, job.ID, phase.tail.ID)
		if isContention(completeErr) {
			return r.observeLatest(ctx, job.ID)
		}
		if completeErr != nil {
			return advanceFor(job, "", false, false), completeErr
		}
		return r.observe(completed, true)
	}
	if len(phase.toolSteps) == 0 {
		if phase.input.ToolContractSHA256 == "" {
			return advanceFor(job, "", false, false), fmt.Errorf(
				"%w: durable agent 模型步骤缺少工具执行契约摘要,拒绝追加工具步骤",
				durable.ErrConflict,
			)
		}
		if phase.toolRoundsBefore >= r.policy.MaxIterations {
			result, failErr := r.failBoundary(ctx, job, fmt.Errorf("%w %d", ErrMaxIterations, r.policy.MaxIterations))
			if isContention(failErr) {
				return r.observeLatest(ctx, job.ID)
			}
			return advanceFromResult(result, true, true), failErr
		}
		steps := r.toolSteps(job.ID, phase.modelOrdinal, phase.result.Message.ToolCalls)
		appended, appendErr := r.engine.AppendSteps(ctx, job.ID, phase.tail.ID, steps)
		if isContention(appendErr) {
			return r.observeLatest(ctx, job.ID)
		}
		if appendErr != nil {
			return advanceFor(job, "", false, false), appendErr
		}
		return advanceFor(appended, "", true, false), nil
	}

	messages, err := phase.nextMessages(r.resultCodecs)
	if err != nil {
		return advanceFor(job, "", false, false), err
	}
	if phase.compaction != nil {
		messages, err = phase.compactedMessages(r.compact, r.resultCodecs)
		if err != nil {
			return advanceFor(job, "", false, false), err
		}
	} else {
		compact, planned, planErr := r.compactionStep(job.ID, phase.modelOrdinal, messages)
		if planErr != nil {
			if errors.Is(planErr, ErrContextBudget) {
				result, failErr := r.failBoundary(ctx, job, planErr)
				if isContention(failErr) {
					return r.observeLatest(ctx, job.ID)
				}
				return advanceFromResult(result, true, true), failErr
			}
			return advanceFor(job, "", false, false), planErr
		}
		if planned {
			appended, appendErr := r.engine.AppendSteps(ctx, job.ID, phase.tail.ID, []durable.StepSpec{compact})
			if isContention(appendErr) {
				return r.observeLatest(ctx, job.ID)
			}
			if appendErr != nil {
				return advanceFor(job, "", false, false), appendErr
			}
			return advanceFor(appended, "", true, false), nil
		}
	}
	next, err := r.modelStep(job.ID, phase.modelOrdinal+1, messages)
	if err != nil {
		if errors.Is(err, ErrContextBudget) {
			result, failErr := r.failBoundary(ctx, job, err)
			if isContention(failErr) {
				return r.observeLatest(ctx, job.ID)
			}
			return advanceFromResult(result, true, true), failErr
		}
		return advanceFor(job, "", false, false), err
	}
	appended, appendErr := r.engine.AppendSteps(ctx, job.ID, phase.tail.ID, []durable.StepSpec{next})
	if isContention(appendErr) {
		return r.observeLatest(ctx, job.ID)
	}
	if appendErr != nil {
		return advanceFor(job, "", false, false), appendErr
	}
	return advanceFor(appended, "", true, false), nil
}

func requiredArtifactGateError(job durable.Job, name string) error {
	if name == "" {
		return nil
	}
	var lastFailure error
	for _, step := range job.Steps {
		if step.Tool != name || step.Status != durable.StepSucceeded {
			continue
		}
		var result toolExecutionResult
		if err := json.Unmarshal(step.Result, &result); err != nil {
			lastFailure = fmt.Errorf("artifact step %s has invalid execution result: %w", step.ID, err)
			continue
		}
		if result.Error == "" {
			return nil
		}
		lastFailure = errors.New(result.Error)
	}
	if lastFailure != nil {
		return fmt.Errorf("%w: %s did not validate: %v", ErrRequiredArtifactMissing, name, lastFailure)
	}
	return fmt.Errorf("%w: model completed without calling %s", ErrRequiredArtifactMissing, name)
}

func (r *Runner) observeLatest(ctx context.Context, jobID string) (AdvanceResult, error) {
	job, err := r.engine.Get(ctx, jobID)
	if err != nil {
		return AdvanceResult{}, err
	}
	return r.observe(job, false)
}

func (r *Runner) observe(job durable.Job, progressed bool) (AdvanceResult, error) {
	switch job.Status {
	case durable.JobSucceeded:
		phase, err := r.inspect(job)
		if err != nil {
			return advanceFor(job, "", progressed, true), err
		}
		if len(phase.result.Message.ToolCalls) != 0 || len(phase.toolSteps) != 0 || phase.compaction != nil {
			return advanceFor(job, "", progressed, true), errors.New("durable agent 已完成 job 缺少最终模型回答")
		}
		return advanceFor(job, phase.result.Message.String(), progressed, true), nil
	case durable.JobFailed:
		return advanceFor(job, "", progressed, true), fmt.Errorf("%w: %s", durable.ErrJobFailed, job.Error)
	case durable.JobUnknown:
		return advanceFor(job, "", progressed, true), fmt.Errorf("%w: %s", durable.ErrUnknown, job.Error)
	case durable.JobRequiresAction:
		return advanceFor(job, "", progressed, true), fmt.Errorf("%w: %s", durable.ErrRequiresAction, job.Error)
	case durable.JobPending, durable.JobRunning, durable.JobAwaitingSteps:
		return advanceFor(job, "", progressed, false), nil
	default:
		return advanceFor(job, "", progressed, false), fmt.Errorf("durable agent 未知 job 状态 %q", job.Status)
	}
}

func isContention(err error) bool {
	return errors.Is(err, durable.ErrLeaseHeld) || errors.Is(err, durable.ErrInvalidState)
}

func isTerminal(status durable.JobStatus) bool {
	switch status {
	case durable.JobSucceeded, durable.JobFailed, durable.JobUnknown, durable.JobRequiresAction:
		return true
	default:
		return false
	}
}

func advanceFor(job durable.Job, output string, progressed, terminal bool) AdvanceResult {
	return advanceFromResult(resultFor(job, output), progressed, terminal)
}

func advanceFromResult(result Result, progressed, terminal bool) AdvanceResult {
	return AdvanceResult{Result: result, Progressed: progressed, Terminal: terminal}
}

func waitForContention(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
