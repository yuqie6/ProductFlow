package generation

import (
	"context"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// Snapshot 是生成容量的只读视图：不持 advisory lock，不 claim，不改状态。
// AdmissionRunning 按 running 节点计数（admission 闸门）；Overview* 按「会话任务 + 图 run」计数（队列总览）。
type Snapshot struct {
	Max              int // app_settings generation_max_concurrent_tasks
	AdmissionRunning int // running node_runs JOIN running runs + running image-session tasks
	OverviewRunning  int // session running tasks + graph runs that have a running node
	OverviewQueued   int // session queued tasks + running graph runs with queued nodes and no running nodes
}

// OverviewActive 是队列总览的 running+queued，与 ImageSession queue_active_count 一致。
func (s Snapshot) OverviewActive() int {
	return s.OverviewRunning + s.OverviewQueued
}

// LoadQueueOverview 只读队列总览（上限 + run/task 计数），不跑 admission 节点计数。
// ImageSession 详情/status 走这里；worker claim 仍用 CountAdmissionRunning。
func LoadQueueOverview(ctx context.Context, db *gorm.DB) (Snapshot, error) {
	max, err := LoadMaxConcurrent(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}
	overviewRunning, overviewQueued, err := countOverview(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Max:             max,
		OverviewRunning: overviewRunning,
		OverviewQueued:  overviewQueued,
	}, nil
}

// LoadSnapshot 读上限、admission 节点计数和队列总览。查询失败原样返回。调用方不要把它当 claim。
func LoadSnapshot(ctx context.Context, db *gorm.DB) (Snapshot, error) {
	snap, err := LoadQueueOverview(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}
	admission, err := CountAdmissionRunning(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}
	snap.AdmissionRunning = admission
	return snap, nil
}

// CountAdmissionRunning 统计占用 admission 槽位的 running 工作：
// 图侧是 running run 上的 running 节点；会话侧是 running 任务。同一 run 上多个 running 节点各占一槽。
func CountAdmissionRunning(ctx context.Context, db *gorm.DB) (int, error) {
	var graphCount int64
	err := db.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
		Joins("JOIN workflow_graph_runs r ON r.id = workflow_graph_node_runs.graph_run_id").
		Where("r.status = ? AND workflow_graph_node_runs.status = ?", "running", "running").
		Count(&graphCount).Error
	if err != nil {
		return 0, err
	}
	var sessionCount int64
	if err := db.WithContext(ctx).Model(&schema.ImageSessionGenerationTasks{}).
		Where("status = ?", "running").
		Count(&sessionCount).Error; err != nil {
		return 0, err
	}
	return int(graphCount) + int(sessionCount), nil
}

func countOverview(ctx context.Context, db *gorm.DB) (int, int, error) {
	sessionCounts := db.Model(&schema.ImageSessionGenerationTasks{}).
		Select("COUNT(*) FILTER (WHERE status = ?) AS running, COUNT(*) FILTER (WHERE status = ?) AS queued", "running", "queued").
		Where("status IN ?", []string{"running", "queued"})
	graphActivity := db.Model(&schema.WorkflowGraphRuns{}).
		Select(`EXISTS (SELECT 1 FROM workflow_graph_node_runs n WHERE n.graph_run_id = workflow_graph_runs.id AND n.status = ?) AS has_running,
			EXISTS (SELECT 1 FROM workflow_graph_node_runs n WHERE n.graph_run_id = workflow_graph_runs.id AND n.status = ?) AS has_queued`, "running", "queued").
		Where("status = ?", "running")
	graphCounts := db.Table("(?) AS active_runs", graphActivity).
		Select("COUNT(*) FILTER (WHERE has_running) AS running, COUNT(*) FILTER (WHERE NOT has_running AND has_queued) AS queued")
	var counts struct {
		Running int64
		Queued  int64
	}
	// One statement keeps running/queued totals on one PG snapshot during state transitions.
	err := db.WithContext(ctx).Table("(?) AS session_counts CROSS JOIN (?) AS graph_counts", sessionCounts, graphCounts).
		Select("session_counts.running + graph_counts.running AS running, session_counts.queued + graph_counts.queued AS queued").
		Find(&counts).Error
	if err != nil {
		return 0, 0, err
	}
	return int(counts.Running), int(counts.Queued), nil
}
