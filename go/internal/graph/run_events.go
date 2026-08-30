package graph

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const maxRunEventPageSize = 250

// GraphRunEventResponse is the durable UI event for one graph run. The
// payload is deliberately opaque to the event store; node-specific details
// stay behind the graph event vocabulary.
type GraphRunEventResponse struct {
	SchemaVersion int            `json:"schema_version"`
	RunID         string         `json:"run_id"`
	Sequence      int            `json:"sequence"`
	Kind          string         `json:"kind"`
	NodeRunID     *string        `json:"node_run_id,omitempty"`
	Payload       map[string]any `json:"payload"`
	CreatedAt     time.Time      `json:"created_at"`
}

type graphRunEventRow struct {
	ID        string
	RunID     string
	Sequence  int
	Kind      string
	NodeRunID *string
	Payload   []byte
	CreatedAt time.Time
}

var graphRunEventKinds = map[string]struct{}{
	"run.queued": {}, "run.started": {}, "run.completed": {}, "run.failed": {}, "run.cancelled": {}, "run.unknown": {},
	"node.claimed": {}, "node.started": {}, "node.progress": {}, "node.succeeded": {}, "node.failed": {}, "node.skipped": {}, "node.cancelled": {},
}

func appendGraphRunEvent(ctx context.Context, tx *gorm.DB, runID, kind string, nodeRunID *string, payload map[string]any) error {
	if _, ok := graphRunEventKinds[kind]; !ok {
		return errors.New("unsupported graph run event kind")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var run schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id").Where("id = ?", runID).Take(&run).Error; err != nil {
		return err
	}
	var last int
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphRunEvents{}).
		Where("graph_run_id = ?", runID).Select("COALESCE(MAX(sequence), 0)").Scan(&last).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	return tx.WithContext(ctx).Create(&schema.WorkflowGraphRunEvents{
		ID: newGraphRunEventID(), GraphRunID: runID, Sequence: last + 1, Kind: kind,
		NodeRunID: nodeRunID, PayloadJSON: string(raw), CreatedAt: now,
	}).Error
}

func listGraphRunEvents(ctx context.Context, tx *gorm.DB, runID string, after, limit int) ([]graphRunEventRow, error) {
	if after < 0 {
		return nil, errors.New("graph run event cursor must not be negative")
	}
	if limit < 1 || limit > maxRunEventPageSize {
		return nil, errors.New("graph run event page size is invalid")
	}
	var rows []schema.WorkflowGraphRunEvents
	if err := tx.WithContext(ctx).Where("graph_run_id = ? AND sequence > ?", runID, after).
		Order("sequence").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]graphRunEventRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, graphRunEventRow{
			ID: row.ID, RunID: row.GraphRunID, Sequence: row.Sequence, Kind: row.Kind,
			NodeRunID: row.NodeRunID, Payload: []byte(row.PayloadJSON), CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

func serializeGraphRunEvent(row graphRunEventRow) (GraphRunEventResponse, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(row.Payload, &payload); err != nil || payload == nil {
		return GraphRunEventResponse{}, errors.New("graph run event payload is invalid")
	}
	return GraphRunEventResponse{
		SchemaVersion: 1, RunID: row.RunID, Sequence: row.Sequence, Kind: row.Kind,
		NodeRunID: row.NodeRunID, Payload: payload, CreatedAt: row.CreatedAt,
	}, nil
}

func newGraphRunEventID() string {
	return clockid.New()
}
