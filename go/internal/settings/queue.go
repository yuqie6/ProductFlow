package settings

import (
	"context"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
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
	var runs []schema.WorkflowGraphRuns
	if err := s.db.WithContext(ctx).Where("status = ?", "running").Find(&runs).Error; err != nil {
		return GenerationQueueOverview{}, err
	}
	ids := []string{}
	for _, run := range runs {
		runStatus[run.ID] = run.Status
		ids = append(ids, run.ID)
	}
	nodes := []nodeRow{}
	if len(ids) > 0 {
		var nodeRuns []schema.WorkflowGraphNodeRuns
		if err := s.db.WithContext(ctx).Where("graph_run_id IN ?", ids).Find(&nodeRuns).Error; err != nil {
			return GenerationQueueOverview{}, err
		}
		for _, row := range nodeRuns {
			nodes = append(nodes, nodeRow{runID: row.GraphRunID, status: row.Status})
		}
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
	var sessionRunning, sessionQueued int64
	if err := s.db.WithContext(ctx).Model(&schema.ImageSessionGenerationTasks{}).Where("status = ?", "running").Count(&sessionRunning).Error; err != nil {
		return GenerationQueueOverview{}, err
	}
	if err := s.db.WithContext(ctx).Model(&schema.ImageSessionGenerationTasks{}).Where("status = ?", "queued").Count(&sessionQueued).Error; err != nil {
		return GenerationQueueOverview{}, err
	}
	maxConcurrent := 3
	if raw, err := s.overrides(ctx); err == nil {
		if n, ok := parseOverrideInt(raw, "generation_max_concurrent_tasks"); ok && n > 0 {
			maxConcurrent = n
		}
	}
	running := graphRunning + int(sessionRunning)
	queued := graphQueued + int(sessionQueued)
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
