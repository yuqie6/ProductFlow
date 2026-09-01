package graph

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"gorm.io/gorm"
)

const maxRunEventPageSize = 250

// GraphRunEventResponse 是 GraphRun 的持久化 UI 事件。payload 对事件表不透明。
// kind 必须是 graphRunEventKinds 闭集。sequence 单调递增，SSE 用 after=sequence 翻页。
type GraphRunEventResponse struct {
	SchemaVersion int            `json:"schema_version"` // 事件合同版本
	RunID         string         `json:"run_id"`
	Sequence      int            `json:"sequence"` // 单调递增；SSE 用 after=sequence 翻页
	Kind          string         `json:"kind"`     // graphRunEventKinds 闭集
	NodeRunID     *string        `json:"node_run_id,omitempty"`
	Payload       map[string]any `json:"payload"` // 对事件表不透明
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

// appendGraphRunEventLocked 追加 workflow_graph_run_events 并 notify ChannelRun。
// 调用方必须已经按 Graph 锁序 FOR UPDATE 住 run；本函数不再隐式取 run 锁，避免 node -> run 的隐藏反向边。
// 未知 kind 返回普通 error（不是 apperr）。sequence 取 MAX+1。
func appendGraphRunEventLocked(ctx context.Context, tx *gorm.DB, runID, kind string, nodeRunID *string, payload map[string]any) error {
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
	if err := tx.WithContext(ctx).Select("id").Where("id = ?", runID).Take(&run).Error; err != nil {
		return err
	}
	var last int
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphRunEvents{}).
		Where("graph_run_id = ?", runID).Select("COALESCE(MAX(sequence), 0)").Scan(&last).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := tx.WithContext(ctx).Create(&schema.WorkflowGraphRunEvents{
		ID: newGraphRunEventID(), GraphRunID: runID, Sequence: last + 1, Kind: kind,
		NodeRunID: nodeRunID, PayloadJSON: string(raw), CreatedAt: now,
	}).Error; err != nil {
		return err
	}
	return notify.Publish(ctx, tx, notify.ChannelRun, runID)
}

// listGraphRunEvents 按 sequence 翻页。after<0 或 limit 越界返回普通 error。给 SSE 用，不是 HTTP 校验。
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
