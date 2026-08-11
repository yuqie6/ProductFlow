package durableagent

import (
	"context"

	"github.com/yuqie6/agent-harness/durable"
)

func (r *Runner) Task(ctx context.Context, jobID string) (durable.Job, error) {
	return r.engine.Get(ctx, jobID)
}

func (r *Runner) Tasks(ctx context.Context, options durable.ListOptions) ([]durable.Job, error) {
	return r.engine.List(ctx, options)
}

func (r *Runner) Attempts(ctx context.Context, jobID string) ([]durable.Attempt, error) {
	return r.engine.Attempts(ctx, jobID)
}

func (r *Runner) Events(ctx context.Context, jobID string, afterSequence int64) ([]durable.Event, error) {
	return r.engine.Events(ctx, jobID, afterSequence)
}

func (r *Runner) PendingResolution(ctx context.Context, jobID string) (durable.ResolutionTarget, error) {
	return r.engine.PendingResolution(ctx, jobID)
}

func (r *Runner) ResolveRequiredAction(
	ctx context.Context,
	jobID string,
	resolution durable.RequiredActionResolution,
) (durable.Job, error) {
	return r.engine.ResolveRequiredAction(ctx, jobID, resolution)
}

func (r *Runner) ResolveUnknown(
	ctx context.Context,
	jobID string,
	resolution durable.UnknownResolution,
) (durable.Job, error) {
	return r.engine.ResolveUnknown(ctx, jobID, resolution)
}
