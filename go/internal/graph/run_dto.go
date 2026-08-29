package graph

import (
	"encoding/json"
	"strings"
	"time"
)

type GraphRunRequest struct {
	Scope  string  `json:"scope"`
	NodeID *string `json:"node_id"`
}

type GraphRunInputTraceEntry struct {
	EdgeID       string  `json:"edge_id"`
	SourceNodeID *string `json:"source_node_id"`
	SourceTitle  *string `json:"source_title"`
	Role         string  `json:"role"`
	Order        int     `json:"order"`
	ArtifactID   *string `json:"artifact_id,omitempty"`
	ArtifactType *string `json:"artifact_type,omitempty"`
	AssetID      *string `json:"asset_id,omitempty"`
	VersionID    *string `json:"version_id,omitempty"`
}

type GraphNodeRunResponse struct {
	ID              string                    `json:"id"`
	NodeID          *string                   `json:"node_id"`
	NodeTitle       *string                   `json:"node_title"`
	Status          string                    `json:"status"`
	SortOrder       int                       `json:"sort_order"`
	CompiledContext map[string]any            `json:"compiled_context"`
	InputTrace      []GraphRunInputTraceEntry `json:"input_trace"`
	Output          map[string]any            `json:"output"`
	FailureReason   *string                   `json:"failure_reason"`
	StartedAt       time.Time                 `json:"started_at"`
	FinishedAt      *time.Time                `json:"finished_at"`
}

type GraphRunResponse struct {
	ID              string                 `json:"id"`
	GraphID         string                 `json:"graph_id"`
	Status          string                 `json:"status"`
	Scope           string                 `json:"scope"`
	RequestedNodeID *string                `json:"requested_node_id"`
	GraphRevision   int                    `json:"graph_revision"`
	FailureReason   *string                `json:"failure_reason"`
	IsRetryable     bool                   `json:"is_retryable"`
	NodeRuns        []GraphNodeRunResponse `json:"node_runs"`
	StartedAt       time.Time              `json:"started_at"`
	FinishedAt      *time.Time             `json:"finished_at"`
}

type GraphRunListResponse struct {
	Items []GraphRunResponse `json:"items"`
}

type graphRunRow struct {
	ID              string
	GraphID         string
	Status          string
	RunScope        string
	RequestedNodeID *string
	GraphRevision   int
	Snapshot        map[string]any
	FailureReason   *string
	IsRetryable     bool
	StartedAt       time.Time
	FinishedAt      *time.Time
	NodeRuns        []graphNodeRunRow
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
	ActiveAttemptID *string
	ProgressPhase   *string
	ProgressUpdated *time.Time
	StartedAt       time.Time
	FinishedAt      *time.Time
}

func serializeGraphRun(run graphRunRow) GraphRunResponse {
	nodeRuns := make([]GraphNodeRunResponse, 0, len(run.NodeRuns))
	for _, item := range run.NodeRuns {
		nodeRuns = append(nodeRuns, serializeNodeRun(item, run.Snapshot))
	}
	return GraphRunResponse{
		ID:              run.ID,
		GraphID:         run.GraphID,
		Status:          run.Status,
		Scope:           run.RunScope,
		RequestedNodeID: run.RequestedNodeID,
		GraphRevision:   run.GraphRevision,
		FailureReason:   run.FailureReason,
		IsRetryable:     run.IsRetryable,
		NodeRuns:        nodeRuns,
		StartedAt:       run.StartedAt,
		FinishedAt:      run.FinishedAt,
	}
}

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
		StartedAt:       nodeRun.StartedAt,
		FinishedAt:      nodeRun.FinishedAt,
	}
}

func ptrStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
