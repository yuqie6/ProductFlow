package settings

import (
	"context"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// GenerationQueueOverview 汇总图运行与连续生图的 queued/running 计数。
type GenerationQueueOverview struct {
	ActiveCount        int `json:"active_count"`         // running + queued
	RunningCount       int `json:"running_count"`        // 图运行占用 + 连续生图 running
	QueuedCount        int `json:"queued_count"`         // 图运行排队 + 连续生图 queued
	MaxConcurrentTasks int `json:"max_concurrent_tasks"` // app_settings generation_max_concurrent_tasks
}

// GenerationQueue 读取当前生成队列占用（图运行 + 连续生图任务）。
// 调用时机：GET /api/generation-queue。无写入。不含交付/局部编辑任务。
// 读库失败或 ctx 取消时返回 error。
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

// classifyGraphDelivery 把一次 Graph Run 收成队列占用：run 不是 running 算 none；
// 有节点 running 算 running；只剩 queued 或节点都终态但 run 还 running 算 queued（等收尾）。
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
