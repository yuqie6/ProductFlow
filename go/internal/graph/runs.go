package graph

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
)

type graphRunSubmission struct {
	Run     graphRunRow
	Created bool
}

func submitGraphRun(ctx context.Context, tx pgx.Tx, productID, graphID, scope string, targetNodeID *string) (graphRunSubmission, error) {
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
	meta, _ := json.Marshal(map[string]any{"run_scope": scope, "requested_node_id": targetNodeID})
	runID := clockid.New()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, requested_node_id, graph_revision,
			snapshot_json, is_retryable, progress_metadata, started_at
		) VALUES ($1, $2, 'running', $3, $4, $5, $6, TRUE, $7, $8)
	`, runID, row.ID, scope, targetNodeID, row.Revision, snapshotJSON, meta, now)
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_graph_node_runs (
				id, graph_run_id, node_id, status, sort_order, compiled_context_json, started_at
			) VALUES ($1, $2, $3, 'queued', $4, $5, $6)
		`, nodeRunID, runID, nodeID, index, compiled, now); err != nil {
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

func listGraphRuns(ctx context.Context, tx pgx.Tx, productID, graphID string, limit int) ([]graphRunRow, error) {
	if _, err := loadGraph(ctx, tx, productID, graphID); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := tx.Query(ctx, `
		SELECT id FROM workflow_graph_runs
		WHERE graph_id = $1
		ORDER BY started_at DESC, id DESC
		LIMIT $2
	`, graphID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]graphRunRow, 0, len(ids))
	for _, id := range ids {
		run, err := loadGraphRun(ctx, tx, productID, graphID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func loadGraphRun(ctx context.Context, tx pgx.Tx, productID, graphID, runID string) (graphRunRow, error) {
	if _, err := loadGraph(ctx, tx, productID, graphID); err != nil {
		return graphRunRow{}, err
	}
	run, err := scanGraphRun(ctx, tx, `
		SELECT id, graph_id, status, run_scope, requested_node_id, graph_revision,
		       snapshot_json, failure_reason, is_retryable, started_at, finished_at
		FROM workflow_graph_runs
		WHERE id = $1 AND graph_id = $2
	`, runID, graphID)
	if errors.Is(err, pgx.ErrNoRows) {
		return graphRunRow{}, apperr.NotFound("工作流运行不存在")
	}
	if err != nil {
		return graphRunRow{}, err
	}
	nodes, err := loadNodeRuns(ctx, tx, run.ID)
	if err != nil {
		return graphRunRow{}, err
	}
	run.NodeRuns = nodes
	return run, nil
}

func loadGraphRunByID(ctx context.Context, tx pgx.Tx, runID string) (graphRunRow, error) {
	run, err := scanGraphRun(ctx, tx, `
		SELECT id, graph_id, status, run_scope, requested_node_id, graph_revision,
		       snapshot_json, failure_reason, is_retryable, started_at, finished_at
		FROM workflow_graph_runs
		WHERE id = $1
	`, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return graphRunRow{}, apperr.NotFound("工作流运行不存在")
	}
	if err != nil {
		return graphRunRow{}, err
	}
	nodes, err := loadNodeRuns(ctx, tx, run.ID)
	if err != nil {
		return graphRunRow{}, err
	}
	run.NodeRuns = nodes
	return run, nil
}

func scanGraphRun(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, sql string, args ...any) (graphRunRow, error) {
	var run graphRunRow
	var snapshot []byte
	err := q.QueryRow(ctx, sql, args...).Scan(
		&run.ID, &run.GraphID, &run.Status, &run.RunScope, &run.RequestedNodeID, &run.GraphRevision,
		&snapshot, &run.FailureReason, &run.IsRetryable, &run.StartedAt, &run.FinishedAt,
	)
	if err != nil {
		return graphRunRow{}, err
	}
	if len(snapshot) > 0 {
		_ = json.Unmarshal(snapshot, &run.Snapshot)
	}
	if run.Snapshot == nil {
		run.Snapshot = map[string]any{}
	}
	return run, nil
}

func loadNodeRuns(ctx context.Context, tx pgx.Tx, runID string) ([]graphNodeRunRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, graph_run_id, node_id, status, sort_order, compiled_context_json, output_json,
		       failure_reason, active_attempt_id, progress_phase, progress_updated_at, started_at, finished_at
		FROM workflow_graph_node_runs
		WHERE graph_run_id = $1
		ORDER BY sort_order, id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []graphNodeRunRow
	for rows.Next() {
		var item graphNodeRunRow
		if err := rows.Scan(
			&item.ID, &item.GraphRunID, &item.NodeID, &item.Status, &item.SortOrder,
			&item.CompiledContext, &item.OutputJSON, &item.FailureReason, &item.ActiveAttemptID,
			&item.ProgressPhase, &item.ProgressUpdated, &item.StartedAt, &item.FinishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func loadActiveRun(ctx context.Context, tx pgx.Tx, graphID string) (*graphRunRow, error) {
	run, err := scanGraphRun(ctx, tx, `
		SELECT id, graph_id, status, run_scope, requested_node_id, graph_revision,
		       snapshot_json, failure_reason, is_retryable, started_at, finished_at
		FROM workflow_graph_runs
		WHERE graph_id = $1 AND status = 'running'
	`, graphID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func cancelGraphRun(ctx context.Context, tx pgx.Tx, productID, graphID, runID string) (graphRunRow, error) {
	if _, err := loadGraph(ctx, tx, productID, graphID); err != nil {
		return graphRunRow{}, err
	}
	run, err := scanGraphRun(ctx, tx, `
		SELECT id, graph_id, status, run_scope, requested_node_id, graph_revision,
		       snapshot_json, failure_reason, is_retryable, started_at, finished_at
		FROM workflow_graph_runs
		WHERE id = $1 AND graph_id = $2
		FOR UPDATE
	`, runID, graphID)
	if errors.Is(err, pgx.ErrNoRows) {
		return graphRunRow{}, apperr.NotFound("工作流运行不存在")
	}
	if err != nil {
		return graphRunRow{}, err
	}
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
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_graph_runs
		SET status = 'cancelled', failure_reason = $2, finished_at = $3
		WHERE id = $1
	`, run.ID, reason, now); err != nil {
		return graphRunRow{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_graph_node_runs
		SET status = 'failed', failure_reason = $2, finished_at = $3,
		    active_attempt_id = NULL, progress_updated_at = $3
		WHERE graph_run_id = $1 AND status IN ('queued', 'running')
	`, run.ID, reason, now); err != nil {
		return graphRunRow{}, err
	}
	return loadGraphRun(ctx, tx, productID, graphID, runID)
}

func retryGraphRun(ctx context.Context, tx pgx.Tx, productID, graphID, runID string) (graphRunSubmission, error) {
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
