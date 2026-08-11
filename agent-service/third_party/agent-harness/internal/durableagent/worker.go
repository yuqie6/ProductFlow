package durableagent

import (
	"context"

	"github.com/yuqie6/agent-harness/durable"
)

// ActiveTasks returns agent turns that can execute or need one orchestration
// transition. Human-blocked and terminal turns are deliberately excluded.
func (r *Runner) ActiveTasks(ctx context.Context, limit int) ([]durable.Job, error) {
	return r.engine.List(ctx, durable.ListOptions{
		Kinds: []durable.JobKind{durable.JobKindAgentTurn},
		Statuses: []durable.JobStatus{
			durable.JobPending,
			durable.JobRunning,
			durable.JobAwaitingSteps,
		},
		Limit:       limit,
		OldestFirst: true,
	})
}
