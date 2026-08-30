package graph

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"gorm.io/gorm"
)

type graphRunSubmission struct {
	Run     graphRunRow
	Created bool
}

func submitGraphRun(ctx context.Context, tx *gorm.DB, productID, graphID, scope string, targetNodeID *string) (graphRunSubmission, error) {
	row, err := loadGraphForUpdate(ctx, tx, productID, graphID)
	if err != nil {
		return graphRunSubmission{}, err
	}
	if !row.Active {
		return graphRunSubmission{}, apperr.Conflict("只能运行 active schema-v3 工作流")
	}
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return graphRunSubmission{}, err
	}
	sources, _, _, err := loadGraphSources(ctx, tx, row, applied)
	if err != nil {
		return graphRunSubmission{}, err
	}
	var sourceArg map[string]SourceRecord
	if scope == RunScopeNode {
		sourceArg = sources
	}
	selected, err := SelectRunNodeIDs(applied, scope, ptrStr(targetNodeID), sourceArg)
	if err != nil {
		return graphRunSubmission{}, err
	}
	active, err := loadActiveRun(ctx, tx, row.ID)
	if err != nil {
		return graphRunSubmission{}, err
	}
	if active != nil {
		if active.RunScope == scope && ptrEqual(active.RequestedNodeID, targetNodeID) && active.GraphRevision == row.Revision {
			if _, err := queue.StageForActor(ctx, tx, queue.ActorGraphRun, active.ID, 0); err != nil {
				return graphRunSubmission{}, err
			}
			full, err := loadGraphRun(ctx, tx, productID, graphID, active.ID)
			if err != nil {
				return graphRunSubmission{}, err
			}
			return graphRunSubmission{Run: full, Created: false}, nil
		}
		return graphRunSubmission{}, apperr.Conflict("工作流已有正在进行的运行")
	}
	snapshot := SnapshotGraph(applied, sources)
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return graphRunSubmission{}, err
	}
	// Python submit_graph_run 不在提交时编译节点。预编译会把「上游提示词尚未生成」的 NODE 跑图直接标失败，祖先 prompt 永远进不了队。
	runStatus := RunStatusRunning
	nodeStatus := NodeRunQueued
	var failure *string
	var finishedAt *time.Time
	now := time.Now().UTC()
	meta, _ := json.Marshal(map[string]any{"run_scope": scope, "requested_node_id": targetNodeID})
	runID := clockid.New()
	metaStr := string(meta)
	err = tx.WithContext(ctx).Create(&schema.WorkflowGraphRuns{
		ID:               runID,
		GraphID:          row.ID,
		Status:           runStatus,
		RunScope:         scope,
		RequestedNodeID:  targetNodeID,
		GraphRevision:    row.Revision,
		SnapshotJSON:     string(snapshotJSON),
		FailureReason:    failure,
		IsRetryable:      true,
		ProgressMetadata: &metaStr,
		StartedAt:        now,
		FinishedAt:       finishedAt,
	}).Error
	if uniqueViolation(err) {
		return graphRunSubmission{}, apperr.Conflict("工作流已有正在进行的运行")
	}
	if err != nil {
		return graphRunSubmission{}, err
	}
	for index, nodeID := range selected {
		title := snapshotNodeTitle(snapshot, nodeID)
		trace := snapshotInputTrace(snapshot, nodeID)
		compiled, err := json.Marshal(map[string]any{"node_title": title, "input_trace": trace})
		if err != nil {
			return graphRunSubmission{}, err
		}
		nodeRunID := clockid.New()
		nid := nodeID
		compiledStr := string(compiled)
		if err := tx.WithContext(ctx).Create(&schema.WorkflowGraphNodeRuns{
			ID:                  nodeRunID,
			GraphRunID:          runID,
			NodeID:              &nid,
			Status:              nodeStatus,
			SortOrder:           index,
			CompiledContextJSON: &compiledStr,
			FailureReason:       failure,
			StartedAt:           now,
			FinishedAt:          finishedAt,
		}).Error; err != nil {
			return graphRunSubmission{}, err
		}
	}
	if _, err := queue.StageForActor(ctx, tx, queue.ActorGraphRun, runID, 0); err != nil {
		return graphRunSubmission{}, err
	}
	full, err := loadGraphRun(ctx, tx, productID, graphID, runID)
	if err != nil {
		return graphRunSubmission{}, err
	}
	return graphRunSubmission{Run: full, Created: true}, nil
}

func listGraphRuns(ctx context.Context, tx *gorm.DB, productID, graphID string, limit int) ([]graphRunRow, error) {
	if _, err := loadGraph(ctx, tx, productID, graphID); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	var recs []schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Select("id").
		Where("graph_id = ?", graphID).
		Order("started_at DESC, id DESC").
		Limit(limit).
		Find(&recs).Error; err != nil {
		return nil, err
	}
	out := make([]graphRunRow, 0, len(recs))
	for _, rec := range recs {
		run, err := loadGraphRun(ctx, tx, productID, graphID, rec.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func loadGraphRun(ctx context.Context, tx *gorm.DB, productID, graphID, runID string) (graphRunRow, error) {
	if _, err := loadGraph(ctx, tx, productID, graphID); err != nil {
		return graphRunRow{}, err
	}
	var rec schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Where("id = ? AND graph_id = ?", runID, graphID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRunRow{}, apperr.NotFound("工作流运行不存在")
	}
	if err != nil {
		return graphRunRow{}, err
	}
	run := graphRunFromSchema(rec)
	nodes, err := loadNodeRuns(ctx, tx, run.ID)
	if err != nil {
		return graphRunRow{}, err
	}
	run.NodeRuns = nodes
	return run, nil
}

func loadGraphRunByIDLocked(ctx context.Context, tx *gorm.DB, runID string) (graphRunRow, error) {
	var rec schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRunRow{}, err
	}
	if err != nil {
		return graphRunRow{}, err
	}
	run := graphRunFromSchema(rec)
	nodes, err := loadNodeRuns(ctx, tx, run.ID)
	if err != nil {
		return graphRunRow{}, err
	}
	run.NodeRuns = nodes
	return run, nil
}

func loadGraphRunByID(ctx context.Context, tx *gorm.DB, runID string) (graphRunRow, error) {
	var rec schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Where("id = ?", runID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRunRow{}, apperr.NotFound("工作流运行不存在")
	}
	if err != nil {
		return graphRunRow{}, err
	}
	run := graphRunFromSchema(rec)
	nodes, err := loadNodeRuns(ctx, tx, run.ID)
	if err != nil {
		return graphRunRow{}, err
	}
	run.NodeRuns = nodes
	return run, nil
}

func graphRunFromSchema(rec schema.WorkflowGraphRuns) graphRunRow {
	run := graphRunRow{
		ID:              rec.ID,
		GraphID:         rec.GraphID,
		Status:          rec.Status,
		RunScope:        rec.RunScope,
		RequestedNodeID: rec.RequestedNodeID,
		GraphRevision:   rec.GraphRevision,
		FailureReason:   rec.FailureReason,
		IsRetryable:     rec.IsRetryable,
		StartedAt:       rec.StartedAt,
		FinishedAt:      rec.FinishedAt,
	}
	if rec.SnapshotJSON != "" {
		_ = json.Unmarshal([]byte(rec.SnapshotJSON), &run.Snapshot)
	}
	if run.Snapshot == nil {
		run.Snapshot = map[string]any{}
	}
	return run
}

func nodeRunFromSchema(rec schema.WorkflowGraphNodeRuns) graphNodeRunRow {
	return graphNodeRunRow{
		ID:              rec.ID,
		GraphRunID:      rec.GraphRunID,
		NodeID:          rec.NodeID,
		Status:          rec.Status,
		SortOrder:       rec.SortOrder,
		CompiledContext: jsonPtrBytes(rec.CompiledContextJSON),
		OutputJSON:      jsonPtrBytes(rec.OutputJSON),
		FailureReason:   rec.FailureReason,
		ActiveAttemptID: rec.ActiveAttemptID,
		ProgressPhase:   rec.ProgressPhase,
		ProgressUpdated: rec.ProgressUpdatedAt,
		StartedAt:       rec.StartedAt,
		FinishedAt:      rec.FinishedAt,
	}
}

func loadNodeRuns(ctx context.Context, tx *gorm.DB, runID string) ([]graphNodeRunRow, error) {
	var recs []schema.WorkflowGraphNodeRuns
	if err := tx.WithContext(ctx).Where("graph_run_id = ?", runID).Order("sort_order, id").Find(&recs).Error; err != nil {
		return nil, err
	}
	out := make([]graphNodeRunRow, 0, len(recs))
	for _, rec := range recs {
		out = append(out, nodeRunFromSchema(rec))
	}
	return out, nil
}

func loadActiveRun(ctx context.Context, tx *gorm.DB, graphID string) (*graphRunRow, error) {
	var rec schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Where("graph_id = ? AND status = ?", graphID, "running").Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run := graphRunFromSchema(rec)
	return &run, nil
}

func cancelGraphRun(ctx context.Context, tx *gorm.DB, productID, graphID, runID string) (graphRunRow, error) {
	if _, err := loadGraph(ctx, tx, productID, graphID); err != nil {
		return graphRunRow{}, err
	}
	var rec schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND graph_id = ?", runID, graphID).
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRunRow{}, apperr.NotFound("工作流运行不存在")
	}
	if err != nil {
		return graphRunRow{}, err
	}
	run := graphRunFromSchema(rec)
	if run.Status == RunStatusCancelled {
		nodes, err := loadNodeRuns(ctx, tx, run.ID)
		if err != nil {
			return graphRunRow{}, err
		}
		run.NodeRuns = nodes
		return run, nil
	}
	if isTerminalRun(run.Status) {
		return graphRunRow{}, apperr.Conflict("已结束的工作流运行不能取消")
	}
	now := time.Now().UTC()
	reason := GraphCancelledReason
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status":         "cancelled",
		"failure_reason": reason,
		"finished_at":    now,
	}).Error; err != nil {
		return graphRunRow{}, err
	}
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
		Where("graph_run_id = ? AND status IN ?", run.ID, []string{"queued", "running"}).
		Updates(map[string]any{
			"status":              "failed",
			"failure_reason":      reason,
			"finished_at":         now,
			"active_attempt_id":   nil,
			"progress_updated_at": now,
		}).Error; err != nil {
		return graphRunRow{}, err
	}
	return loadGraphRun(ctx, tx, productID, graphID, runID)
}

func retryGraphRun(ctx context.Context, tx *gorm.DB, productID, graphID, runID string) (graphRunSubmission, error) {
	source, err := loadGraphRun(ctx, tx, productID, graphID, runID)
	if err != nil {
		return graphRunSubmission{}, err
	}
	if source.Status != RunStatusFailed {
		return graphRunSubmission{}, apperr.Validation("只有失败的工作流运行可以重试")
	}
	if !source.IsRetryable {
		return graphRunSubmission{}, apperr.Validation("该工作流运行不可重试")
	}
	return submitGraphRun(ctx, tx, productID, graphID, source.RunScope, source.RequestedNodeID)
}

func isTerminalRun(status string) bool {
	switch status {
	case RunStatusSucceeded, RunStatusFailed, RunStatusCancelled, RunStatusUnknown:
		return true
	default:
		return false
	}
}

func ptrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
