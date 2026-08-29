package settings

import (
	"context"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
)

type GenerationQueueOverview struct {
	ActiveCount        int `json:"active_count"`
	RunningCount       int `json:"running_count"`
	QueuedCount        int `json:"queued_count"`
	MaxConcurrentTasks int `json:"max_concurrent_tasks"`
}

func (s *Store) GenerationQueue(ctx context.Context) (GenerationQueueOverview, error) {
	type nodeRow struct {
		runID  string
		status string
	}
	runStatus := map[string]string{}
	rows, err := pfdb.Query(ctx, s.db, `SELECT id, status FROM workflow_graph_runs WHERE status = 'running'`)
	if err != nil {
		return GenerationQueueOverview{}, err
	}
	ids := []string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return GenerationQueueOverview{}, err
		}
		runStatus[id] = status
		ids = append(ids, id)
	}
	rows.Close()
	nodes := []nodeRow{}
	if len(ids) > 0 {
		nRows, err := pfdb.Query(ctx, s.db, `SELECT graph_run_id, status FROM workflow_graph_node_runs WHERE graph_run_id = ANY($1)`, ids)
		if err != nil {
			return GenerationQueueOverview{}, err
		}
		for nRows.Next() {
			var row nodeRow
			if err := nRows.Scan(&row.runID, &row.status); err != nil {
				nRows.Close()
				return GenerationQueueOverview{}, err
			}
			nodes = append(nodes, row)
		}
		nRows.Close()
	}
	byRun := map[string][]string{}
	for _, node := range nodes {
		byRun[node.runID] = append(byRun[node.runID], node.status)
	}
	graphRunning, graphQueued := 0, 0
	for id := range runStatus {
		switch classifyGraphDelivery(runStatus[id], byRun[id]) {
		case "running":
			graphRunning++
		case "queued":
			graphQueued++
		}
	}
	var sessionRunning, sessionQueued int
	if err := pfdb.QueryRow(ctx, s.db, `SELECT COUNT(*) FROM image_session_generation_tasks WHERE status = 'running'`).Scan(&sessionRunning); err != nil {
		return GenerationQueueOverview{}, err
	}
	if err := pfdb.QueryRow(ctx, s.db, `SELECT COUNT(*) FROM image_session_generation_tasks WHERE status = 'queued'`).Scan(&sessionQueued); err != nil {
		return GenerationQueueOverview{}, err
	}
	maxConcurrent := 3
	if raw, err := s.overrides(ctx); err == nil {
		if n, ok := parseOverrideInt(raw, "generation_max_concurrent_tasks"); ok && n > 0 {
			maxConcurrent = n
		}
	}
	running := graphRunning + sessionRunning
	queued := graphQueued + sessionQueued
	return GenerationQueueOverview{
		ActiveCount: running + queued, RunningCount: running, QueuedCount: queued,
		MaxConcurrentTasks: maxConcurrent,
	}, nil
}

func classifyGraphDelivery(runStatus string, nodeStatuses []string) string {
	if runStatus != "running" {
		return "none"
	}
	hasRunning, hasQueued, allTerminal := false, false, len(nodeStatuses) > 0
	for _, status := range nodeStatuses {
		switch status {
		case "running":
			hasRunning = true
			allTerminal = false
		case "queued":
			hasQueued = true
			allTerminal = false
		case "succeeded", "failed", "unknown":
		default:
			allTerminal = false
		}
	}
	if hasRunning {
		return "running"
	}
	if hasQueued || allTerminal {
		return "queued"
	}
	return "none"
}
