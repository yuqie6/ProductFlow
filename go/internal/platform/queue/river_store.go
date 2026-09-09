package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/yuqie6/productflow/internal/auth"
	"gorm.io/gorm"
)

// taskTable maps the five durable invocation targets to their business identity.
// Queue state never determines whether a new business execution is authorized.
func taskTable(actor string) (string, error) {
	switch actor {
	case ActorGraphRun:
		return "workflow_graph_runs", nil
	case ActorImageSession:
		return "image_session_generation_tasks", nil
	case ActorDelivery:
		return "delivery_rendition_jobs", nil
	case ActorLocalEdit:
		return "local_image_edit_tasks", nil
	case ActorAgentTurnSync:
		return "agent_turn_projections", nil
	default:
		return "", fmt.Errorf("queue: unsupported actor %q", actor)
	}
}

func executionID(ctx context.Context, tx *gorm.DB, actor, id string) (string, error) {
	table, err := taskTable(actor)
	if err != nil {
		return "", err
	}
	var row struct{ QueueExecutionID string }
	if err := tx.WithContext(ctx).Table(table).Select("queue_execution_id").Where("id = ?", id).Take(&row).Error; err != nil {
		return "", err
	}
	if row.QueueExecutionID == "" {
		return "", errors.New("queue: business execution identity missing")
	}
	return row.QueueExecutionID, nil
}

// StageTask enqueues the current business execution in the caller's transaction.
// Explicit retry changes the business identity before this call; continuation and
// recovery preserve it. Uniqueness includes stopped jobs, so scans cannot revive them.
func StageTask(ctx context.Context, tx *gorm.DB, actor, aggregateID string, payload any, availableAt *time.Time) (RiverJob, error) {
	if _, err := riverTx(tx); err != nil {
		return RiverJob{}, err
	}
	identity, err := executionID(ctx, tx, actor, aggregateID)
	if err != nil {
		return RiverJob{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return RiverJob{}, err
	}
	args := TaskArgs{Actor: actor, AggregateID: aggregateID, MerchantID: auth.ResolveMerchantID(ctx), ExecutionID: identity, Payload: raw}
	if args.MerchantID == "" {
		return RiverJob{}, errors.New("queue: merchant identity missing")
	}
	if err := AssertExecution(withTaskArgs(ctx, args), tx, actor, aggregateID); err != nil {
		return RiverJob{}, err
	}
	return insertRiverJob(ctx, tx, args, availableAt)
}

func StageTaskForActor(ctx context.Context, tx *gorm.DB, actor, id string, delay time.Duration) (RiverJob, error) {
	var at *time.Time
	if delay > 0 {
		value := time.Now().UTC().Add(delay)
		at = &value
	}
	return StageTask(ctx, tx, actor, id, nil, at)
}

func RestageTaskIfIdle(ctx context.Context, tx *gorm.DB, actor, id string, payload any) (bool, error) {
	identity, err := executionID(ctx, tx, actor, id)
	if err != nil {
		return false, err
	}
	var count int64
	if err := tx.WithContext(ctx).Table("river_job").Where("kind = ? AND args ->> 'actor' = ? AND args ->> 'aggregate_id' = ? AND args ->> 'execution_id' = ?", RiverTaskKind, actor, id, identity).Where("state <> ?", rivertype.JobStateCompleted).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	_, err = StageTask(ctx, tx, actor, id, payload, nil)
	return err == nil, err
}

var retainedJobStates = []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateScheduled,
	rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateCompleted,
	rivertype.JobStateCancelled, rivertype.JobStateDiscarded,
}
