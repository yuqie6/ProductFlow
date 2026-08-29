package graph

import (
	"context"
	"errors"
	"strings"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const generationCapacityLockKey = 42630001

func generationMaxConcurrent(ctx context.Context, q *gorm.DB) int {
	var raw *string
	_ = pfdb.QueryRow(ctx, q, `SELECT value FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`).Scan(&raw)
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

func runningGenerationCount(ctx context.Context, tx *gorm.DB) (int, error) {
	var graphCount, sessionCount int
	err := pfdb.QueryRow(ctx, tx, `
		SELECT COUNT(*)
		FROM workflow_graph_node_runs n
		JOIN workflow_graph_runs r ON r.id = n.graph_run_id
		WHERE r.status = 'running' AND n.status = 'running'
	`).Scan(&graphCount)
	if err != nil {
		return 0, err
	}
	_ = pfdb.QueryRow(ctx, tx, `
		SELECT COUNT(*) FROM image_session_generation_tasks WHERE status = 'running'
	`).Scan(&sessionCount)
	return graphCount + sessionCount, nil
}

// GenerationCapacityAvailable 与连续生图共用同一把容量锁。
func GenerationCapacityAvailable(ctx context.Context, tx *gorm.DB) (bool, error) {
	return generationCapacityAvailable(ctx, tx)
}

func generationCapacityAvailable(ctx context.Context, tx *gorm.DB) (bool, error) {
	if _, err := pfdb.Exec(ctx, tx, `SELECT pg_advisory_xact_lock($1)`, generationCapacityLockKey); err != nil {
		return false, err
	}
	limit := generationMaxConcurrent(ctx, tx)
	count, err := runningGenerationCount(ctx, tx)
	if err != nil {
		return false, err
	}
	return count < limit, nil
}

var (
	errNotClaimed      = errors.New("not claimed")
	errWaitingCapacity = errors.New("waiting_for_capacity")
)

func claimQueuedNodeRun(ctx context.Context, gdb *gorm.DB, nodeRunID string) (bool, error) {
	claimed := false
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		ok, err := generationCapacityAvailable(ctx, dbTx)
		if err != nil {
			return err
		}
		if !ok {
			return errWaitingCapacity
		}
		now := time.Now().UTC()
		attemptID := clockid.New()
		n, err := pfdb.Exec(ctx, dbTx, `
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
			return err
		}
		if n != 1 {
			return errNotClaimed
		}
		claimed = true
		return nil
	})
	if errors.Is(err, errNotClaimed) {
		return false, nil
	}
	if errors.Is(err, errWaitingCapacity) {
		return false, errWaitingCapacity
	}
	return claimed, err
}

func loadNodeRunStatuses(ctx context.Context, tx *gorm.DB, runID string) ([]string, []string, error) {
	rows, err := pfdb.Query(ctx, tx, `SELECT status, failure_reason FROM workflow_graph_node_runs WHERE graph_run_id = $1`, runID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var statuses []string
	var reasons []string
	for rows.Next() {
		var st string
		var reason *string
		if err := rows.Scan(&st, &reason); err != nil {
			return nil, nil, err
		}
		statuses = append(statuses, st)
		if reason != nil {
			reasons = append(reasons, *reason)
		} else {
			reasons = append(reasons, "")
		}
	}
	return statuses, reasons, rows.Err()
}

func completeGraphRunIfNodesTerminal(ctx context.Context, tx *gorm.DB, runID string) (bool, error) {
	var status string
	err := pfdb.QueryRow(ctx, tx, `SELECT status FROM workflow_graph_runs WHERE id = $1`, runID).Scan(&status)
	if err != nil || status != RunStatusRunning {
		return false, err
	}
	statuses, reasons, err := loadNodeRunStatuses(ctx, tx, runID)
	if err != nil {
		return false, err
	}
	if len(statuses) == 0 {
		return false, nil
	}
	blocking := false
	for _, st := range statuses {
		if st == NodeRunFailed || st == NodeRunUnknown {
			blocking = true
			break
		}
	}
	if blocking {
		now := time.Now().UTC()
		if _, err := pfdb.Exec(ctx, tx, `
			UPDATE workflow_graph_node_runs SET
				status = 'failed',
				failure_reason = COALESCE(failure_reason, '上游节点已失败'),
				finished_at = COALESCE(finished_at, $2),
				active_attempt_id = NULL,
				progress_updated_at = $2
			WHERE graph_run_id = $1 AND status = 'queued'
		`, runID, now); err != nil {
			return false, err
		}
		statuses, reasons, err = loadNodeRunStatuses(ctx, tx, runID)
		if err != nil {
			return false, err
		}
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
	_, err = pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_runs
		SET status = $2, failure_reason = $3, is_retryable = $4, finished_at = $5
		WHERE id = $1 AND status = 'running'
	`, runID, runStatus, failure, retryable, now)
	return err == nil, err
}

func failGraphRunLocked(ctx context.Context, tx *gorm.DB, runID, reason string) error {
	now := time.Now().UTC()
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	if _, err := pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_node_runs SET
			status = 'failed', failure_reason = $2, finished_at = $3,
			active_attempt_id = NULL, progress_updated_at = $3
		WHERE graph_run_id = $1 AND status IN ('queued', 'running')
	`, runID, reason, now); err != nil {
		return err
	}
	_, err := pfdb.Exec(ctx, tx, `
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

func failClaimedNode(ctx context.Context, gdb *gorm.DB, runID, nodeRunID, reason string) error {
	return tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		var runStatus string
		err := pfdb.QueryRow(ctx, dbTx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus)
		if err != nil {
			return err
		}
		if isTerminalRun(runStatus) {
			return nil
		}
		var nodeStatus string
		var phase *string
		var attempt *string
		err = pfdb.QueryRow(ctx, dbTx, `
		SELECT status, progress_phase, active_attempt_id
		FROM workflow_graph_node_runs
		WHERE id = $1 AND graph_run_id = $2
		FOR UPDATE
	`, nodeRunID, runID).Scan(&nodeStatus, &phase, &attempt)
		if errors.Is(err, sqldb.ErrNoRows) {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		if err != nil {
			return err
		}
		if nodeStatus == NodeRunFailed || nodeStatus == NodeRunUnknown || nodeStatus == NodeRunSucceeded {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		if nodePastProviderBoundary(phase) && strings.TrimSpace(reason) == "" {
			if err := markNodeUnknown(ctx, dbTx, runID, nodeRunID, attempt, ProviderUnknownDetail); err != nil {
				return err
			}
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		if nodeStatus != NodeRunQueued && nodeStatus != NodeRunRunning {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		now := time.Now().UTC()
		if len(reason) > 1000 {
			reason = reason[:1000]
		}
		if _, err := pfdb.Exec(ctx, dbTx, `
		UPDATE workflow_graph_node_runs SET
			status = 'failed', failure_reason = $2, finished_at = $3,
			active_attempt_id = NULL, progress_updated_at = $3
		WHERE id = $1
	`, nodeRunID, reason, now); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
		return err
	})
}

func failGraphRun(ctx context.Context, gdb *gorm.DB, runID, reason string) error {
	return tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		var runStatus string
		err := pfdb.QueryRow(ctx, dbTx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus)
		if err != nil {
			return err
		}
		if isTerminalRun(runStatus) {
			return nil
		}
		rows, err := pfdb.Query(ctx, dbTx, `
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
			if err := markNodeUnknown(ctx, dbTx, runID, boundaryID, boundaryAttempt, ProviderUnknownDetail); err != nil {
				return err
			}
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		return failGraphRunLocked(ctx, dbTx, runID, reason)
	})
}
