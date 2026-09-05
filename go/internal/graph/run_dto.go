package graph

import (
	"encoding/json"
	"strings"
	"time"
)

// GraphRunRequest 是提交或预览 GraphRun 的 HTTP 体。Scope 空则视为 graph。
type GraphRunRequest struct {
	Scope   string   `json:"scope"` // graph|node|to_node|selection；空视为 graph
	NodeID  *string  `json:"node_id"`
	NodeIDs []string `json:"node_ids"` // selection 的显式目标；空列表是 [] 不是 nil
	// Force 仅对 node|to_node|selection 的显式目标生效；全图携带 force 为 Validation。
	Force bool `json:"force"`
	// DocumentAction 是 complete|rewrite|replace；只允许配合 force 跑单个文稿节点。
	DocumentAction  string `json:"document_action"`
	DocumentSection string `json:"document_section"`
}

// GraphRunInputTraceEntry 记录编译时实际用到的一条入边，不是整图扫描结果。
type GraphRunInputTraceEntry struct {
	EdgeID       string  `json:"edge_id"`
	SourceNodeID *string `json:"source_node_id"`
	SourceTitle  *string `json:"source_title"`
	Role         string  `json:"role"`  // 等于入边 handle id
	Order        int     `json:"order"` // 同一 role 下的次序
	ArtifactID   *string `json:"artifact_id,omitempty"`
	ArtifactType *string `json:"artifact_type,omitempty"` // 入边当前产物类型，如 image
	AssetID      *string `json:"asset_id,omitempty"`
	VersionID    *string `json:"version_id,omitempty"`
}

// GraphNodeRunResponse 是单个节点运行的 HTTP 投影。unknown 与 failed 都是终态。
type GraphNodeRunResponse struct {
	ID              string                    `json:"id"`
	NodeID          *string                   `json:"node_id"`
	NodeTitle       *string                   `json:"node_title"`
	Status          string                    `json:"status"`
	SortOrder       int                       `json:"sort_order"`       // 入队次序，不是拓扑序号
	CompiledContext map[string]any            `json:"compiled_context"` // 编译快照；含 node_title / input_trace
	InputTrace      []GraphRunInputTraceEntry `json:"input_trace"`      // 空列表是 [] 不是 nil
	Output          map[string]any            `json:"output"`           // 成功产物摘要；失败可空
	FailureReason   *string                   `json:"failure_reason"`   // unknown 时也有说明
	AttemptCount    int                       `json:"attempt_count"`    // claim 次数，不是 GraphRun 重试次数
	ProgressPhase   *string                   `json:"progress_phase"`   // 非 queued/running 时为 nil
	PlannedAction   *string                   `json:"planned_action"`   // generate|reuse|frozen|blocked
	StartedAt       time.Time                 `json:"started_at"`
	FinishedAt      *time.Time                `json:"finished_at"`
}

// GraphRunResponse 是 WorkflowGraphRun 的 HTTP 投影。IsRetryable 仅 failed 可为 true。
type GraphRunResponse struct {
	ID               string                 `json:"id"`
	GraphID          string                 `json:"graph_id"`
	Status           string                 `json:"status"`
	Scope            string                 `json:"scope"` // graph|node|to_node|selection
	RequestedNodeID  *string                `json:"requested_node_id"`
	RequestedNodeIDs []string               `json:"requested_node_ids"` // 空列表是 [] 不是 nil
	GraphRevision    int                    `json:"graph_revision"`     // 快照时的 live revision
	FailureReason    *string                `json:"failure_reason"`     // 仅 failed 有值；unknown/成功为 nil
	IsRetryable      bool                   `json:"is_retryable"`       // 仅 failed 可为 true；unknown 必须 false
	NodeRuns         []GraphNodeRunResponse `json:"node_runs"`          // 空列表是 [] 不是 nil
	StartedAt        time.Time              `json:"started_at"`
	FinishedAt       *time.Time             `json:"finished_at"`
}

// GraphRunSummaryResponse 是列表项的轻量投影。它只含运行状态和节点进度，不含编译上下文、输入追踪或输出；详情走 GET 单个 run。
type GraphRunSummaryResponse struct {
	ID               string                        `json:"id"`
	GraphID          string                        `json:"graph_id"`
	Status           string                        `json:"status"`
	Scope            string                        `json:"scope"`
	RequestedNodeID  *string                       `json:"requested_node_id"`
	RequestedNodeIDs []string                      `json:"requested_node_ids"`
	GraphRevision    int                           `json:"graph_revision"`
	FailureReason    *string                       `json:"failure_reason"`
	IsRetryable      bool                          `json:"is_retryable"`
	NodeRuns         []GraphNodeRunSummaryResponse `json:"node_runs"`
	StartedAt        time.Time                     `json:"started_at"`
	FinishedAt       *time.Time                    `json:"finished_at"`
}

// GraphNodeRunSummaryResponse 是列表中的节点进度投影，不携带 provider 输入/输出大字段。
type GraphNodeRunSummaryResponse struct {
	ID            string     `json:"id"`
	NodeID        *string    `json:"node_id"`
	Status        string     `json:"status"`
	SortOrder     int        `json:"sort_order"`
	FailureReason *string    `json:"failure_reason"`
	AttemptCount  int        `json:"attempt_count"`
	ProgressPhase *string    `json:"progress_phase"`
	PlannedAction *string    `json:"planned_action"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

// GraphRunPreviewResponse 是 POST .../runs/preview 的 200 体，由 PreviewRun 填写，不创建 workflow_graph_runs。
// Nodes 只含范围内处理节点的 planned_action（generate/reuse/frozen/blocked）。Force 仅预览显式目标。
// 不要和 GraphRunResponse（真实 run 行）搞混。改字段只动预览 JSON，不动 runs 表。
type GraphRunPreviewResponse struct {
	Scope            string   `json:"scope"` // graph|node|to_node|selection
	RequestedNodeID  *string  `json:"requested_node_id"`
	RequestedNodeIDs []string `json:"requested_node_ids"` // 空列表是 [] 不是 nil
	// Force 仅预览显式目标；全图请求里必须为 false。
	Force bool `json:"force"`
	// DocumentAction 是 complete|rewrite|replace；空视为 complete。
	DocumentAction  string           `json:"document_action"`
	DocumentSection string           `json:"document_section"`
	Nodes           []RunPreviewNode `json:"nodes"` // 空列表是 [] 不是 nil
}

// GraphRunListResponse 包装最近的 GraphRun 摘要列表。
type GraphRunListResponse struct {
	Items []GraphRunSummaryResponse `json:"items"` // 空列表是 [] 不是 nil；最多 20 条，不是游标分页
}

type graphRunRow struct {
	ID                      string
	GraphID                 string
	Status                  string
	RunScope                string
	RequestedNodeID         *string
	RequestedNodeIDs        []string
	GraphRevision           int
	Snapshot                map[string]any
	FailureReason           *string
	IsRetryable             bool
	ExecutionLeaseToken     *string
	ExecutionLeaseExpiresAt *time.Time
	StartedAt               time.Time
	FinishedAt              *time.Time
	Force                   bool
	DocumentAction          string
	DocumentSection         string
	NodeRuns                []graphNodeRunRow
}

type graphNodeRunRow struct {
	ID              string
	GraphRunID      string
	NodeID          *string
	Status          string
	SortOrder       int
	CompiledContext []byte
	OutputJSON      []byte
	FailureReason   *string
	AttemptCount    int
	ActiveAttemptID *string
	ProgressPhase   *string
	ProgressUpdated *time.Time
	PlannedAction   *string
	StartedAt       time.Time
	FinishedAt      *time.Time
}

// serializeGraphRun 投 HTTP 合同。unknown 的 IsRetryable 必须是 false，前端靠它隐藏重试。
func serializeGraphRun(run graphRunRow) GraphRunResponse {
	nodeRuns := make([]GraphNodeRunResponse, 0, len(run.NodeRuns))
	for _, item := range run.NodeRuns {
		nodeRuns = append(nodeRuns, serializeNodeRun(item, run.Snapshot))
	}
	return GraphRunResponse{
		ID:               run.ID,
		GraphID:          run.GraphID,
		Status:           run.Status,
		Scope:            run.RunScope,
		RequestedNodeID:  run.RequestedNodeID,
		RequestedNodeIDs: requestedNodeIDsOrEmpty(run.RequestedNodeIDs),
		GraphRevision:    run.GraphRevision,
		FailureReason:    run.FailureReason,
		IsRetryable:      run.IsRetryable,
		NodeRuns:         nodeRuns,
		StartedAt:        run.StartedAt,
		FinishedAt:       run.FinishedAt,
	}
}

// serializeGraphRunSummary 投列表中的轻量运行项；详情字段只在 serializeGraphRun 中读取。
func serializeGraphRunSummary(run graphRunRow) GraphRunSummaryResponse {
	nodeRuns := make([]GraphNodeRunSummaryResponse, 0, len(run.NodeRuns))
	for _, item := range run.NodeRuns {
		nodeRuns = append(nodeRuns, serializeNodeRunSummary(item))
	}
	return GraphRunSummaryResponse{
		ID:               run.ID,
		GraphID:          run.GraphID,
		Status:           run.Status,
		Scope:            run.RunScope,
		RequestedNodeID:  run.RequestedNodeID,
		RequestedNodeIDs: requestedNodeIDsOrEmpty(run.RequestedNodeIDs),
		GraphRevision:    run.GraphRevision,
		FailureReason:    run.FailureReason,
		IsRetryable:      run.IsRetryable,
		NodeRuns:         nodeRuns,
		StartedAt:        run.StartedAt,
		FinishedAt:       run.FinishedAt,
	}
}

func serializeNodeRunSummary(nodeRun graphNodeRunRow) GraphNodeRunSummaryResponse {
	return GraphNodeRunSummaryResponse{
		ID:            nodeRun.ID,
		NodeID:        nodeRun.NodeID,
		Status:        nodeRun.Status,
		SortOrder:     nodeRun.SortOrder,
		FailureReason: nodeRun.FailureReason,
		AttemptCount:  nodeRun.AttemptCount,
		ProgressPhase: serializedProgressPhase(nodeRun),
		PlannedAction: nodeRun.PlannedAction,
		StartedAt:     nodeRun.StartedAt,
		FinishedAt:    nodeRun.FinishedAt,
	}
}

// serializeNodeRun 投节点运行行。标题优先 compiled_context.node_title，否则回落 snapshot。
func serializeNodeRun(nodeRun graphNodeRunRow, snapshot map[string]any) GraphNodeRunResponse {
	compiled := map[string]any{}
	if len(nodeRun.CompiledContext) > 0 {
		_ = json.Unmarshal(nodeRun.CompiledContext, &compiled)
	}
	var nodeTitle *string
	if title, ok := compiled["node_title"].(string); ok && strings.TrimSpace(title) != "" {
		nodeTitle = &title
	} else {
		nodeTitle = snapshotNodeTitle(snapshot, ptrStr(nodeRun.NodeID))
	}
	var rawTrace []map[string]any
	if trace, ok := compiled["input_trace"].([]any); ok && len(trace) > 0 {
		for _, item := range trace {
			if m, ok := item.(map[string]any); ok {
				rawTrace = append(rawTrace, m)
			}
		}
	}
	if len(rawTrace) == 0 {
		rawTrace = snapshotInputTrace(snapshot, ptrStr(nodeRun.NodeID))
	}
	inputTrace := make([]GraphRunInputTraceEntry, 0, len(rawTrace))
	for _, item := range rawTrace {
		inputTrace = append(inputTrace, GraphRunInputTraceEntry{
			EdgeID:       strField(item["edge_id"]),
			SourceNodeID: strPtrField(item["source_node_id"]),
			SourceTitle:  strPtrField(item["source_title"]),
			Role:         strField(item["role"]),
			Order:        intField(item["order"]),
			ArtifactID:   strPtrField(item["artifact_id"]),
			ArtifactType: strPtrField(item["artifact_type"]),
			AssetID:      strPtrField(item["asset_id"]),
			VersionID:    strPtrField(item["version_id"]),
		})
	}
	var output map[string]any
	if len(nodeRun.OutputJSON) > 0 {
		_ = json.Unmarshal(nodeRun.OutputJSON, &output)
	}
	var compiledOut map[string]any
	if len(nodeRun.CompiledContext) > 0 {
		compiledOut = compiled
	}
	return GraphNodeRunResponse{
		ID:              nodeRun.ID,
		NodeID:          nodeRun.NodeID,
		NodeTitle:       nodeTitle,
		Status:          nodeRun.Status,
		SortOrder:       nodeRun.SortOrder,
		CompiledContext: compiledOut,
		InputTrace:      inputTrace,
		Output:          output,
		FailureReason:   nodeRun.FailureReason,
		AttemptCount:    nodeRun.AttemptCount,
		ProgressPhase:   serializedProgressPhase(nodeRun),
		PlannedAction:   nodeRun.PlannedAction,
		StartedAt:       nodeRun.StartedAt,
		FinishedAt:      nodeRun.FinishedAt,
	}
}

func serializedProgressPhase(nodeRun graphNodeRunRow) *string {
	if nodeRun.Status != NodeRunQueued && nodeRun.Status != NodeRunRunning {
		return nil
	}
	return nodeRun.ProgressPhase
}

func requestedNodeIDsOrEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func ptrStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func stringSliceField(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return normalizeRunNodeIDs(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return normalizeRunNodeIDs(out)
	default:
		return nil
	}
}
