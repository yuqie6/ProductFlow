package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *sqliteStore) claimJob(ctx context.Context, jobID, owner string, leaseTTL time.Duration, now time.Time) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	expires := now.Add(leaseTTL)
	updated, err := tx.ExecContext(ctx, `UPDATE jobs SET
		status = ?, error = '', lease_owner = ?, lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)
		AND (lease_owner = '' OR lease_expires_at IS NULL OR lease_expires_at <= ?)`,
		JobRunning, owner, expires.UnixNano(), now.UnixNano(), jobID,
		JobPending, JobRunning, now.UnixNano())
	if err != nil {
		return Job{}, err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return Job{}, err
	}
	if changed != 1 {
		var status JobStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Job{}, ErrNotFound
			}
			return Job{}, err
		}
		if status != JobPending && status != JobRunning {
			return Job{}, fmt.Errorf("%w: job %s 状态为 %s", ErrInvalidState, jobID, status)
		}
		return Job{}, ErrLeaseHeld
	}
	payload, _ := json.Marshal(map[string]any{"owner": owner, "lease_expires_at": expires})
	if err := appendEvent(ctx, tx, jobID, "", "", "job.claimed", payload, now); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return s.getJob(ctx, jobID)
}

func (s *sqliteStore) heartbeatJob(ctx context.Context, jobID, owner string, leaseTTL time.Duration, now time.Time) error {
	updated, err := s.db.ExecContext(ctx, `UPDATE jobs SET lease_expires_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?`,
		now.Add(leaseTTL).UnixNano(), jobID, JobRunning, owner)
	if err != nil {
		return err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrLeaseHeld
	}
	return nil
}

func (s *sqliteStore) runnableJobIDs(ctx context.Context, supportedTools []string, limit int, now time.Time) ([]string, error) {
	if len(supportedTools) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(supportedTools))
	args := []any{JobKindToolSequence, JobPending, JobRunning, now.UnixNano(), StepSucceeded}
	for index, name := range supportedTools {
		placeholders[index] = "?"
		args = append(args, name)
	}
	args = append(args, limit)
	query := `SELECT j.id FROM jobs j
		WHERE j.kind = ? AND (j.status = ? OR (
			j.status = ? AND (j.lease_owner = '' OR j.lease_expires_at IS NULL OR j.lease_expires_at <= ?)
		)) AND NOT EXISTS (
			SELECT 1 FROM steps s
			WHERE s.job_id = j.id AND s.status != ? AND s.tool NOT IN (` + strings.Join(placeholders, ",") + `)
		)
		ORDER BY j.created_at, j.id LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
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
	return ids, rows.Err()
}

func (s *sqliteStore) listJobs(ctx context.Context, options ListOptions) ([]Job, error) {
	query := "SELECT id FROM jobs"
	args := make([]any, 0, len(options.Statuses)+len(options.Kinds)+1)
	predicates := make([]string, 0, 2)
	if len(options.Statuses) > 0 {
		placeholders := make([]string, len(options.Statuses))
		for index, status := range options.Statuses {
			placeholders[index] = "?"
			args = append(args, status)
		}
		predicates = append(predicates, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(options.Kinds) > 0 {
		placeholders := make([]string, len(options.Kinds))
		for index, kind := range options.Kinds {
			placeholders[index] = "?"
			args = append(args, kind)
		}
		predicates = append(predicates, "kind IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(predicates) > 0 {
		query += " WHERE " + strings.Join(predicates, " AND ")
	}
	if options.OldestFirst {
		query += " ORDER BY updated_at, id LIMIT ?"
	} else {
		query += " ORDER BY updated_at DESC, id LIMIT ?"
	}
	args = append(args, options.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(ids))
	for _, id := range ids {
		job, err := s.getJob(ctx, id)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}
