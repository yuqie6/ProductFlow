package durable

import (
	"context"
	"fmt"
	"strings"
)

// PendingResolution returns the exact Step/Attempt pair currently waiting for
// an operator decision. ResolveUnknown and ResolveRequiredAction require this
// pair so a stale decision cannot apply to later work in the same job.
func (e *Engine) PendingResolution(ctx context.Context, jobID string) (ResolutionTarget, error) {
	jobID = strings.TrimSpace(jobID)
	job, err := e.store.getJob(ctx, jobID)
	if err != nil {
		return ResolutionTarget{}, err
	}
	var stepStatus StepStatus
	var attemptStatus AttemptStatus
	switch job.Status {
	case JobUnknown:
		stepStatus, attemptStatus = StepUnknown, AttemptUnknown
	case JobRequiresAction:
		stepStatus, attemptStatus = StepRequiresAction, AttemptRequiresAction
	default:
		return ResolutionTarget{}, fmt.Errorf("%w: job %s 状态为 %s,没有待处理 resolution", ErrInvalidState, job.ID, job.Status)
	}
	return e.store.pendingResolution(ctx, job.ID, stepStatus, attemptStatus)
}
