package queue

import (
	"context"
	"errors"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

// ErrSuperseded ends an obsolete invocation without changing the newer business execution.
var ErrSuperseded = errors.New("queue: execution no longer current")

// AssertExecution checks the immutable job identity under the business row lock.
// Call from the existing claim transaction, before changing state or claiming an
// external effect. Joins read ownership; only the task row is locked.
func AssertExecution(ctx context.Context, tx *gorm.DB, actor, id string) error {
	args, queued := TaskArgsFromContext(ctx)
	if !queued {
		return nil
	}
	if args.Actor != actor || args.AggregateID != id {
		return ErrSuperseded
	}
	table, err := taskTable(actor)
	if err != nil {
		return err
	}
	q := tx.WithContext(ctx).Table(table).Where(table+".id = ?", id)
	owner := "p.merchant_id"
	switch actor {
	case ActorGraphRun:
		q = q.Joins("JOIN workflow_graphs g ON g.id = workflow_graph_runs.graph_id").Joins("JOIN products p ON p.id = g.product_id")
	case ActorImageSession:
		q = q.Joins("JOIN image_sessions s ON s.id = image_session_generation_tasks.session_id")
		owner = "s.merchant_id"
	case ActorDelivery:
		q = q.Joins("JOIN products p ON p.id = delivery_rendition_jobs.product_id")
	case ActorLocalEdit:
		q = q.Joins("JOIN products p ON p.id = local_image_edit_tasks.product_id")
	case ActorAgentTurnSync:
		q = q.Joins("JOIN agent_conversations c ON c.id = agent_turn_projections.conversation_id")
		owner = "c.merchant_id"
	}
	var row struct {
		QueueExecutionID string
		MerchantID       string
	}
	err = q.Select(table + ".queue_execution_id, " + owner + " AS merchant_id").Clauses(pfdb.ForUpdateOf(table)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSuperseded
	}
	if err != nil {
		return err
	}
	if args.ExecutionID != row.QueueExecutionID || args.MerchantID != row.MerchantID {
		return ErrSuperseded
	}
	return nil
}
