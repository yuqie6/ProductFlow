package graph

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

const generationCapacityLockKey = 42630001

func generationMaxConcurrent(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) int {
	var raw *string
	_ = q.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`).Scan(&raw)
	n := 3
	if raw != nil && *raw != "" {
		parsed := 0
		for _, ch := range *raw {
			if ch < '0' || ch > '9' {
				parsed = 0
				break
			}
			parsed = parsed*10 + int(ch-'0')
		}
		if parsed > 0 {
			n = parsed
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 20 {
		n = 20
	}
	return n
}

func runningGenerationCount(ctx context.Context, tx pgx.Tx) (int, error) {
	var graphCount, sessionCount int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM workflow_graph_node_runs n
		JOIN workflow_graph_runs r ON r.id = n.graph_run_id
		WHERE r.status = 'running' AND n.status = 'running'
	`).Scan(&graphCount)
	if err != nil {
		return 0, err
	}
	_ = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM image_session_generation_tasks WHERE status = 'running'
	`).Scan(&sessionCount)
	return graphCount + sessionCount, nil
}

func generationCapacityAvailable(ctx context.Context, tx pgx.Tx) (bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, generationCapacityLockKey); err != nil {
		return false, err
	}
	limit := generationMaxConcurrent(ctx, tx)
	count, err := runningGenerationCount(ctx, tx)
	if err != nil {
		return false, err
	}
	return count < limit, nil
}

func claimQueuedNodeRun(ctx context.Context, pool *pgxpool.Pool, nodeRunID string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ok, err := generationCapacityAvailable(ctx, tx)
	if err != nil || !ok {
		return false, err
	}
	now := time.Now().UTC()
	attemptID := clockid.New()
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_graph_node_runs SET
			status = 'running',
			active_attempt_id = $2,
			progress_phase = 'claimed',
			progress_updated_at = $3,
			started_at = $3,
			failure_reason = NULL
		WHERE id = $1 AND status = 'queued'
	`, nodeRunID, attemptID, now)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func completeGraphRunIfNodesTerminal(ctx context.Context, tx pgx.Tx, runID string) (bool, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM workflow_graph_runs WHERE id = $1`, runID).Scan(&status)
	if err != nil || status != RunStatusRunning {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT status, failure_reason FROM workflow_graph_node_runs WHERE graph_run_id = $1`, runID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var statuses []string
	var reasons []string
	for rows.Next() {
		var st string
		var reason *string
		if err := rows.Scan(&st, &reason); err != nil {
			return false, err
		}
		statuses = append(statuses, st)
		if reason != nil {
			reasons = append(reasons, *reason)
		} else {
			reasons = append(reasons, "")
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(statuses) == 0 {
		return false, nil
	}
	for _, st := range statuses {
		if st == NodeRunQueued || st == NodeRunRunning {
			return false, nil
		}
	}
	now := time.Now().UTC()
	runStatus := RunStatusSucceeded
	var failure *string
	retryable := false
	for i, st := range statuses {
		if st == NodeRunUnknown {
			runStatus = RunStatusUnknown
			msg := reasons[i]
			if msg == "" {
				msg = ProviderUnknownDetail
			}
			if len(msg) > 1000 {
				msg = msg[:1000]
			}
			failure = &msg
			retryable = false
			break
		}
	}
	if runStatus == RunStatusSucceeded {
		for i, st := range statuses {
			if st == NodeRunFailed {
				runStatus = RunStatusFailed
				msg := reasons[i]
				if msg == "" {
					msg = "节点运行失败"
				}
				if len(msg) > 1000 {
					msg = msg[:1000]
				}
				failure = &msg
				retryable = true
				break
			}
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE workflow_graph_runs
		SET status = $2, failure_reason = $3, is_retryable = $4, finished_at = $5
		WHERE id = $1 AND status = 'running'
	`, runID, runStatus, failure, retryable, now)
	return err == nil, err
}

func failGraphRunLocked(ctx context.Context, tx pgx.Tx, runID, reason string) error {
	now := time.Now().UTC()
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_graph_node_runs SET
			status = 'failed', failure_reason = $2, finished_at = $3,
			active_attempt_id = NULL, progress_updated_at = $3
		WHERE graph_run_id = $1 AND status IN ('queued', 'running')
	`, runID, reason, now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE workflow_graph_runs SET
			status = 'failed', failure_reason = $2, finished_at = $3, is_retryable = TRUE
		WHERE id = $1 AND status = 'running'
	`, runID, reason, now)
	return err
}

func nodePastProviderBoundary(phase *string) bool {
	if phase == nil {
		return false
	}
	return *phase == "provider_call" || *phase == "provider_result_received"
}

func failClaimedNode(ctx context.Context, pool *pgxpool.Pool, runID, nodeRunID, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var runStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus)
	if err != nil {
		return err
	}
	if isTerminalRun(runStatus) {
		return tx.Commit(ctx)
	}
	var nodeStatus string
	var phase *string
	var attempt *string
	err = tx.QueryRow(ctx, `
		SELECT status, progress_phase, active_attempt_id
		FROM workflow_graph_node_runs
		WHERE id = $1 AND graph_run_id = $2
		FOR UPDATE
	`, nodeRunID, runID).Scan(&nodeStatus, &phase, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = completeGraphRunIfNodesTerminal(ctx, tx, runID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if nodePastProviderBoundary(phase) {
		if err := markNodeUnknown(ctx, tx, runID, nodeRunID, attempt, ProviderUnknownDetail); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, tx, runID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if nodeStatus != NodeRunQueued && nodeStatus != NodeRunRunning {
		_, err = completeGraphRunIfNodesTerminal(ctx, tx, runID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	now := time.Now().UTC()
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_graph_node_runs SET
			status = 'failed', failure_reason = $2, finished_at = $3,
			active_attempt_id = NULL, progress_updated_at = $3
		WHERE id = $1
	`, nodeRunID, reason, now); err != nil {
		return err
	}
	if _, err := completeGraphRunIfNodesTerminal(ctx, tx, runID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func failGraphRun(ctx context.Context, pool *pgxpool.Pool, runID, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var runStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus)
	if err != nil {
		return err
	}
	if isTerminalRun(runStatus) {
		return tx.Commit(ctx)
	}
	rows, err := tx.Query(ctx, `
		SELECT id, status, progress_phase, active_attempt_id
		FROM workflow_graph_node_runs WHERE graph_run_id = $1
	`, runID)
	if err != nil {
		return err
	}
	var boundaryID string
	var boundaryAttempt *string
	found := false
	for rows.Next() {
		var id, status string
		var phase *string
		var attempt *string
		if err := rows.Scan(&id, &status, &phase, &attempt); err != nil {
			rows.Close()
			return err
		}
		if status == NodeRunRunning && nodePastProviderBoundary(phase) {
			boundaryID = id
			boundaryAttempt = attempt
			found = true
			break
		}
	}
	rows.Close()
	if found {
		if err := markNodeUnknown(ctx, tx, runID, boundaryID, boundaryAttempt, ProviderUnknownDetail); err != nil {
			return err
		}
		if _, err := completeGraphRunIfNodesTerminal(ctx, tx, runID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err := failGraphRunLocked(ctx, tx, runID, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
