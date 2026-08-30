package graph

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
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

func submitGraphRun(ctx context.Context, tx *gorm.DB, productID, graphID string, req GraphRunRequest) (graphRunSubmission, error) {
	if err := validateGraphRunRequest(req); err != nil {
		return graphRunSubmission{}, err
	}
	scope := req.Scope
	if scope == "" {
		scope = RunScopeGraph
	}
	targetNodeID := req.NodeID
	nodeIDs := normalizeRunNodeIDs(req.NodeIDs)
	force := req.Force
	mode := validRegenerateMode(req.RegenerateMode)
	row, err := loadGraphForUpdate(ctx, tx, productID, graphID)
	if err != nil {
		return graphRunSubmission{}, err
	}
	if !row.Active {
		return graphRunSubmission{}, apperr.Conflict("只能运行 active schema-v3 工作流")
	}
	active, err := loadActiveRun(ctx, tx, row.ID)
	if err != nil {
		return graphRunSubmission{}, err
	}
	if active != nil {
		if sameInFlightRun(*active, scope, targetNodeID, nodeIDs, force, mode, row.Revision) {
			if _, err := queue.StageForActor(ctx, tx, queue.ActorGraphRun, active.ID, 0); err != nil {
				return graphRunSubmission{}, err
			}
			full, err := loadGraphRun(ctx, tx, productID, graphID, active.ID)
			if err != nil {
				return graphRunSubmission{}, err
			}
			return graphRunSubmission{Run: full, Created: false}, nil
		}
		queued, err := findDuplicateQueuedRun(ctx, tx, row.ID, scope, targetNodeID, nodeIDs, force, mode)
		if err != nil {
			return graphRunSubmission{}, err
		}
		if queued != nil {
			full, err := loadGraphRun(ctx, tx, productID, graphID, queued.ID)
			if err != nil {
				return graphRunSubmission{}, err
			}
			return graphRunSubmission{Run: full, Created: false}, nil
		}
		queuedRun, err := insertQueuedGraphRun(ctx, tx, row, scope, targetNodeID, nodeIDs, force, mode)
		if err != nil {
			return graphRunSubmission{}, err
		}
		full, err := loadGraphRun(ctx, tx, productID, graphID, queuedRun.ID)
		if err != nil {
			return graphRunSubmission{}, err
		}
		return graphRunSubmission{Run: full, Created: true}, nil
	}
	// A stale queued row can outlive the running row after a crash or a manual
	// recovery. Preserve FIFO rather than starting a newer request ahead of it.
	queued, err := findDuplicateQueuedRun(ctx, tx, row.ID, scope, targetNodeID, nodeIDs, force, mode)
	if err != nil {
		return graphRunSubmission{}, err
	}
	var queuedCount int64
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
		Where("graph_id = ? AND status = ?", row.ID, RunStatusQueued).
		Count(&queuedCount).Error; err != nil {
		return graphRunSubmission{}, err
	}
	if queued != nil || queuedCount > 0 {
		if queued != nil {
			full, err := loadGraphRun(ctx, tx, productID, graphID, queued.ID)
			if err != nil {
				return graphRunSubmission{}, err
			}
			return graphRunSubmission{Run: full, Created: false}, nil
		}
		queuedRun, err := insertQueuedGraphRun(ctx, tx, row, scope, targetNodeID, nodeIDs, force, mode)
		if err != nil {
			return graphRunSubmission{}, err
		}
		full, err := loadGraphRun(ctx, tx, productID, graphID, queuedRun.ID)
		if err != nil {
			return graphRunSubmission{}, err
		}
		return graphRunSubmission{Run: full, Created: true}, nil
	}
	return startGraphRun(ctx, tx, productID, graphID, row, req)
}

func startGraphRun(ctx context.Context, tx *gorm.DB, productID, graphID string, row graphRow, req GraphRunRequest) (graphRunSubmission, error) {
	scope := req.Scope
	if scope == "" {
		scope = RunScopeGraph
	}
	targetNodeID := req.NodeID
	nodeIDs := normalizeRunNodeIDs(req.NodeIDs)
	force := req.Force
	mode := validRegenerateMode(req.RegenerateMode)
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return graphRunSubmission{}, err
	}
	sources, _, _, err := loadGraphSources(ctx, tx, row, applied)
	if err != nil {
		return graphRunSubmission{}, err
	}
	selected, err := SelectRunNodeIDsWithMode(applied, scope, ptrStr(targetNodeID), nodeIDs, sources, force, mode)
	if err != nil {
		return graphRunSubmission{}, err
	}
	preview, _ := PlanRun(applied, scope, ptrStr(targetNodeID), nodeIDs, sources, force, mode)
	actionByNode := map[string]string{}
	for _, item := range preview {
		actionByNode[item.NodeID] = item.Action
	}
	snapshot := SnapshotGraph(applied, sources)
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return graphRunSubmission{}, err
	}
	now := time.Now().UTC()
	meta, _ := json.Marshal(runProgressMeta(scope, targetNodeID, nodeIDs, force, mode))
	runID := clockid.New()
	metaStr := string(meta)
	err = tx.WithContext(ctx).Create(&schema.WorkflowGraphRuns{
		ID:               runID,
		GraphID:          row.ID,
		Status:           RunStatusRunning,
		RunScope:         scope,
		RequestedNodeID:  targetNodeID,
		GraphRevision:    row.Revision,
		SnapshotJSON:     string(snapshotJSON),
		IsRetryable:      true,
		ProgressMetadata: &metaStr,
		StartedAt:        now,
	}).Error
	if uniqueViolation(err) {
		return graphRunSubmission{}, apperr.Conflict("工作流已有正在进行的运行")
	}
	if err != nil {
		return graphRunSubmission{}, err
	}
	if err := insertNodeRunsForSelection(ctx, tx, runID, selected, snapshot, actionByNode, now); err != nil {
		return graphRunSubmission{}, err
	}
	if err := appendGraphRunEvent(ctx, tx, runID, "run.started", nil, map[string]any{
		"status": "running", "run_scope": scope, "graph_revision": row.Revision,
	}); err != nil {
		return graphRunSubmission{}, err
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

func insertQueuedGraphRun(ctx context.Context, tx *gorm.DB, row graphRow, scope string, targetNodeID *string, nodeIDs []string, force bool, mode string) (schema.WorkflowGraphRuns, error) {
	now := time.Now().UTC()
	meta, _ := json.Marshal(runProgressMeta(scope, targetNodeID, nodeIDs, force, mode))
	metaStr := string(meta)
	placeholder, _ := json.Marshal(map[string]any{"schema_version": GraphSnapshotSchemaVersion})
	rec := schema.WorkflowGraphRuns{
		ID:               clockid.New(),
		GraphID:          row.ID,
		Status:           RunStatusQueued,
		RunScope:         scope,
		RequestedNodeID:  targetNodeID,
		GraphRevision:    row.Revision,
		SnapshotJSON:     string(placeholder),
		IsRetryable:      true,
		ProgressMetadata: &metaStr,
		StartedAt:        now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return schema.WorkflowGraphRuns{}, err
	}
	if err := appendGraphRunEvent(ctx, tx, rec.ID, "run.queued", nil, map[string]any{
		"status": "queued", "run_scope": scope, "graph_revision": row.Revision,
	}); err != nil {
		return schema.WorkflowGraphRuns{}, err
	}
	return rec, nil
}

func insertNodeRunsForSelection(ctx context.Context, tx *gorm.DB, runID string, selected []string, snapshot map[string]any, actionByNode map[string]string, now time.Time) error {
	for index, nodeID := range selected {
		title := snapshotNodeTitle(snapshot, nodeID)
		trace := snapshotInputTrace(snapshot, nodeID)
		compiled, err := json.Marshal(map[string]any{"node_title": title, "input_trace": trace})
		if err != nil {
			return err
		}
		nid := nodeID
		compiledStr := string(compiled)
		var planned *string
		if action := actionByNode[nodeID]; action != "" {
			planned = &action
		} else {
			generate := PlannedGenerate
			planned = &generate
		}
		if err := tx.WithContext(ctx).Create(&schema.WorkflowGraphNodeRuns{
			ID:                  clockid.New(),
			GraphRunID:          runID,
			NodeID:              &nid,
			Status:              NodeRunQueued,
			SortOrder:           index,
			CompiledContextJSON: &compiledStr,
			StartedAt:           now,
			PlannedAction:       planned,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func runProgressMeta(scope string, targetNodeID *string, nodeIDs []string, force bool, mode string) map[string]any {
	return map[string]any{
		"run_scope":          scope,
		"requested_node_id":  targetNodeID,
		"requested_node_ids": nodeIDs,
		"force":              force,
		"regenerate_mode":    mode,
	}
}

func normalizeRunNodeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sameInFlightRun(active graphRunRow, scope string, targetNodeID *string, nodeIDs []string, force bool, mode string, revision int) bool {
	return active.RunScope == scope &&
		ptrEqual(active.RequestedNodeID, targetNodeID) &&
		sameStringSlice(active.RequestedNodeIDs, nodeIDs) &&
		active.Force == force &&
		validRegenerateMode(active.RegenerateMode) == mode &&
		active.GraphRevision == revision
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func findDuplicateQueuedRun(ctx context.Context, tx *gorm.DB, graphID, scope string, targetNodeID *string, nodeIDs []string, force bool, mode string) (*graphRunRow, error) {
	var recs []schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Where("graph_id = ? AND status = ?", graphID, RunStatusQueued).Order("started_at, id").Find(&recs).Error; err != nil {
		return nil, err
	}
	for _, rec := range recs {
		run := graphRunFromSchema(rec)
		if sameInFlightRun(run, scope, targetNodeID, nodeIDs, force, mode, run.GraphRevision) {
			return &run, nil
		}
	}
	return nil, nil
}

func validateGraphRunRequest(req GraphRunRequest) error {
	scope := req.Scope
	if scope == "" {
		scope = RunScopeGraph
	}
	if req.Force && scope == RunScopeGraph {
		return apperr.Validation("全图运行不能携带 force")
	}
	if req.Force && scope != RunScopeNode && scope != RunScopeToNode && scope != RunScopeSelection {
		return apperr.Validation("force 必须指定目标节点")
	}
	if req.Force && (scope == RunScopeNode || scope == RunScopeToNode) && (req.NodeID == nil || strings.TrimSpace(*req.NodeID) == "") {
		return apperr.Validation("force 必须指定目标节点")
	}
	if req.Force && scope == RunScopeSelection && len(normalizeRunNodeIDs(req.NodeIDs)) == 0 {
		return apperr.Validation("force 必须指定目标节点")
	}
	return nil
}

func activateQueuedRun(ctx context.Context, tx *gorm.DB, productID, runID string) error {
	var rec schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&rec).Error
	if err != nil {
		return err
	}
	if rec.Status != RunStatusQueued {
		return nil
	}
	row, err := loadGraphForUpdate(ctx, tx, productID, rec.GraphID)
	if err != nil {
		return err
	}
	run := graphRunFromSchema(rec)
	req := GraphRunRequest{
		Scope:          run.RunScope,
		NodeID:         run.RequestedNodeID,
		NodeIDs:        run.RequestedNodeIDs,
		Force:          run.Force,
		RegenerateMode: run.RegenerateMode,
	}
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return err
	}
	sources, _, _, err := loadGraphSources(ctx, tx, row, applied)
	if err != nil {
		return err
	}
	selected, err := SelectRunNodeIDsWithMode(applied, req.Scope, ptrStr(req.NodeID), req.NodeIDs, sources, req.Force, validRegenerateMode(req.RegenerateMode))
	if err != nil {
		now := time.Now().UTC()
		reason := err.Error()
		if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).Where("id = ? AND status = ?", runID, RunStatusQueued).Updates(map[string]any{
			"status":         RunStatusFailed,
			"failure_reason": reason,
			"finished_at":    now,
			"is_retryable":   true,
		}).Error; err != nil {
			return err
		}
		return appendGraphRunEvent(ctx, tx, runID, "run.failed", nil, map[string]any{
			"status": RunStatusFailed, "failure_reason": reason,
		})
	}
	preview, _ := PlanRun(applied, req.Scope, ptrStr(req.NodeID), req.NodeIDs, sources, req.Force, validRegenerateMode(req.RegenerateMode))
	actionByNode := map[string]string{}
	for _, item := range preview {
		actionByNode[item.NodeID] = item.Action
	}
	snapshot := SnapshotGraph(applied, sources)
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).Where("id = ? AND status = ?", runID, RunStatusQueued).Updates(map[string]any{
		"status":         RunStatusRunning,
		"graph_revision": row.Revision,
		"snapshot_json":  string(snapshotJSON),
		"started_at":     now,
	}).Error; err != nil {
		if uniqueViolation(err) {
			return nil
		}
		return err
	}
	if err := insertNodeRunsForSelection(ctx, tx, runID, selected, snapshot, actionByNode, now); err != nil {
		return err
	}
	if err := appendGraphRunEvent(ctx, tx, runID, "run.started", nil, map[string]any{
		"status": "running", "run_scope": run.RunScope, "graph_revision": row.Revision,
	}); err != nil {
		return err
	}
	_, err = queue.StageForActor(ctx, tx, queue.ActorGraphRun, runID, 0)
	return err
}

func promoteNextQueuedRun(ctx context.Context, tx *gorm.DB, graphID string) error {
	active, err := loadActiveRun(ctx, tx, graphID)
	if err != nil || active != nil {
		return err
	}
	var rec schema.WorkflowGraphRuns
	err = tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("graph_id = ? AND status = ?", graphID, RunStatusQueued).
		Order("started_at, id").
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var graph schema.WorkflowGraphs
	if err := tx.WithContext(ctx).Select("product_id").Where("id = ?", graphID).Take(&graph).Error; err != nil {
		return err
	}
	if err := activateQueuedRun(ctx, tx, graph.ProductID, rec.ID); err != nil {
		return err
	}
	var after schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Select("status").Where("id = ?", rec.ID).Take(&after).Error; err != nil {
		return err
	}
	if after.Status == RunStatusFailed || after.Status == RunStatusCancelled {
		return promoteNextQueuedRun(ctx, tx, graphID)
	}
	return nil
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
	if rec.ProgressMetadata != nil && *rec.ProgressMetadata != "" {
		meta := map[string]any{}
		if err := json.Unmarshal([]byte(*rec.ProgressMetadata), &meta); err == nil {
			if force, ok := meta["force"].(bool); ok {
				run.Force = force
			}
			run.RegenerateMode = validRegenerateMode(asString(meta["regenerate_mode"]))
			run.RequestedNodeIDs = stringSliceField(meta["requested_node_ids"])
		}
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
		AttemptCount:    rec.AttemptCount,
		ActiveAttemptID: rec.ActiveAttemptID,
		ProgressPhase:   rec.ProgressPhase,
		ProgressUpdated: rec.ProgressUpdatedAt,
		PlannedAction:   rec.PlannedAction,
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
	var nodeRuns []schema.WorkflowGraphNodeRuns
	if err := tx.WithContext(ctx).Select("id", "node_id").Where("graph_run_id = ? AND status IN ?", run.ID, []string{"queued", "running"}).Find(&nodeRuns).Error; err != nil {
		return graphRunRow{}, err
	}
	result := tx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).Where("id = ? AND status IN ?", run.ID, []string{RunStatusQueued, RunStatusRunning}).Updates(map[string]any{
		"status":         "cancelled",
		"failure_reason": reason,
		"finished_at":    now,
	})
	if result.Error != nil {
		return graphRunRow{}, result.Error
	}
	if result.RowsAffected != 1 {
		return loadGraphRun(ctx, tx, productID, graphID, runID)
	}
	for _, node := range nodeRuns {
		result := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
			Where("id = ? AND status IN ?", node.ID, []string{"queued", "running"}).
			Updates(map[string]any{
				"status":              NodeRunCancelled,
				"failure_reason":      reason,
				"finished_at":         now,
				"active_attempt_id":   nil,
				"progress_updated_at": now,
			})
		if result.Error != nil {
			return graphRunRow{}, result.Error
		}
		if result.RowsAffected != 1 {
			continue
		}
		if err := appendGraphRunEvent(ctx, tx, run.ID, "node.cancelled", &node.ID, map[string]any{
			"status": NodeRunCancelled, "node_id": node.NodeID, "reason": reason,
		}); err != nil {
			return graphRunRow{}, err
		}
	}
	if err := appendGraphRunEvent(ctx, tx, run.ID, "run.cancelled", nil, map[string]any{
		"status": RunStatusCancelled, "reason": reason,
	}); err != nil {
		return graphRunRow{}, err
	}
	if err := promoteNextQueuedRun(ctx, tx, graphID); err != nil {
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
	return submitGraphRun(ctx, tx, productID, graphID, GraphRunRequest{
		Scope:          source.RunScope,
		NodeID:         source.RequestedNodeID,
		NodeIDs:        source.RequestedNodeIDs,
		Force:          source.Force,
		RegenerateMode: source.RegenerateMode,
	})
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
