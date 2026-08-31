package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	pfmetrics "github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type projectedEventPage struct {
	Items       []map[string]any `json:"items"`
	NextAfter   int              `json:"next_after"`
	HasMore     bool             `json:"has_more"`
	StreamState string           `json:"stream_state"`
}

var turnSSEHeartbeatInterval = 15 * time.Second

// ListProjectedEventPage 处理 GET .../turns/:projection_id/events/page 的数据面：从 PostgreSQL agent_turn_events 读一页，再经 projectTurnEvent 翻成浏览器协议。
//
// 对话历史接口调用（pageProductEvents / pageGlobalEvents）。权威是 PG journal，不是 Pi session files。after 是 sequence 游标不是页码。
// stream_state 由投影状态决定：等待确认/回答为 parked，终态为 terminal，其余 live。不延长 lease，不改 Goal。
// Turn 不存在返回 NotFound。after/limit 越界返回 Validation。
func (s Service) ListProjectedEventPage(ctx context.Context, productID *string, conversationID, projectionID string, after, limit int) (projectedEventPage, error) {
	var row turnRow
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		loaded, err := loadTurn(ctx, gdb, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		row = loaded
		return nil
	})
	if err != nil {
		return projectedEventPage{}, err
	}
	events, err := s.ListEvents(ctx, projectionID, after, limit+1)
	if err != nil {
		return projectedEventPage{}, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	items := make([]map[string]any, 0, len(events))
	nextAfter := after
	for _, event := range events {
		items = append(items, projectedTurnEvent(event))
		nextAfter = event.Sequence
	}
	streamState := "live"
	if row.Status == "awaiting_confirmation" || row.Status == "requires_input" {
		streamState = "parked"
	} else if inSet(terminalTurn, row.Status) {
		streamState = "terminal"
	}
	return projectedEventPage{Items: items, NextAfter: nextAfter, HasMore: hasMore, StreamState: streamState}, nil
}

// StreamTurnEvents 处理 GET /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id/events
// 以及 GET /api/v2/agent-conversations/:conversation_id/turns/:projection_id/events。
//
// 浏览器已鉴权后，从 PostgreSQL journal 游标以 SSE 回放。终态或尚未绑定 harness 时只冲刷已落库事件并 stream.complete。进行中则先回放存量，再 LISTEN ChannelTurn（慢订阅回落轮询）。
// Last-Event-ID 与 after 取较大者，都不是页码；断线重连不丢序、不跳序。连接数超限 503；Last-Event-ID 非法 400。
//
// agent-service 不提供本地事件流。禁区：不要改成读 Pi 文件；不要在 SSE 路径写 Turn / Task / Goal；不要延长 lease。
func (s Service) StreamTurnEvents(c *gin.Context, productID *string, conversationID, projectionID string, after int, lastEventID string) {
	if pfmetrics.AgentSSEConnections.Add(1) > maxSSEConnections {
		pfmetrics.AgentSSEConnections.Add(-1)
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "Agent SSE 连接数已达到上限")
		return
	}
	defer pfmetrics.AgentSSEConnections.Add(-1)
	cursor := after
	if lastEventID != "" {
		parsed, err := strconv.Atoi(lastEventID)
		if err != nil || parsed < 0 || parsed > maxEventSequence {
			httpx.AbortDetail(c, http.StatusBadRequest, "Last-Event-ID 无效")
			return
		}
		if parsed > cursor {
			cursor = parsed
		}
	}
	var row turnRow
	err := tx.WithGorm(c.Request.Context(), s.DB, func(gdb *gorm.DB) error {
		loaded, err := loadTurn(c.Request.Context(), gdb, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		row = loaded
		return nil
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	parked := row.Status == "awaiting_confirmation"
	settled := (inSet(terminalTurn, row.Status) && !parked) || row.HarnessTurnID == nil
	if settled {
		s.writePersistedTurnEvents(c, projectionID, cursor, true)
		return
	}
	cursor = s.writePersistedTurnEvents(c, projectionID, cursor, false)
	s.waitForPersistedTurnEvents(c, projectionID, cursor)
}

// writePersistedTurnEvents 从 cursor 之后按页写出已落库 journal。settled 为 true 时补 stream.complete。
//
// 只读 ListEvents。写失败或上下文取消则停在当前 cursor，由调用方决定是否继续等。
func (s Service) writePersistedTurnEvents(c *gin.Context, projectionID string, cursor int, settled bool) int {
	ctx := c.Request.Context()
	flusher, _ := c.Writer.(http.Flusher)
	const pageSize = 250
	for {
		events, err := s.ListEvents(ctx, projectionID, cursor, pageSize)
		if err != nil {
			return cursor
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			if err := writeTurnSSE(c.Writer, event); err != nil {
				return cursor
			}
			cursor = event.Sequence
		}
		if len(events) < pageSize {
			break
		}
	}
	if settled {
		if _, err := fmt.Fprintf(c.Writer, "event: stream.complete\ndata: {\"reason\":\"settled\"}\n\n"); err != nil {
			return cursor
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
	return cursor
}

// waitForPersistedTurnEvents 等 ChannelTurn 通知或轮询间隔，继续从同一 cursor 读 PG journal。
//
// LISTEN 失败或订阅通道关闭后改为 250ms 轮询，保证不丢事件。approval/resolved 或投影已进非 awaiting_confirmation 的终态时发 stream.complete。
//
// 禁区：不要把通知当权威（通知只是唤醒）；漏通知必须靠轮询补上。
func (s Service) waitForPersistedTurnEvents(c *gin.Context, projectionID string, cursor int) {
	ctx := c.Request.Context()
	flusher, _ := c.Writer.(http.Flusher)
	notes, unsubscribe := subscribeAgentNotifications(s.Pool, notify.ChannelTurn)
	defer unsubscribe()
	fallback := 2 * time.Second
	if notes == nil {
		fallback = 250 * time.Millisecond
	}
	eventTicker := time.NewTicker(fallback)
	defer eventTicker.Stop()
	heartbeatTicker := time.NewTicker(turnSSEHeartbeatInterval)
	defer heartbeatTicker.Stop()
	for {
		events, err := s.ListEvents(ctx, projectionID, cursor, 250)
		if err != nil {
			return
		}
		complete := false
		for _, event := range events {
			if err := writeTurnSSE(c.Writer, event); err != nil {
				return
			}
			cursor = event.Sequence
			if event.Kind == "approval/resolved" {
				complete = true
			}
		}
		if flusher != nil && len(events) > 0 {
			flusher.Flush()
		}
		if !complete {
			var status string
			_ = s.DB.WithContext(ctx).Model(&schema.AgentTurnProjections{}).
				Select("status").Where("id = ?", projectionID).Scan(&status).Error
			if status != "" && status != "awaiting_confirmation" && inSet(terminalTurn, status) {
				complete = true
			}
		}
		if complete {
			if _, err := fmt.Fprintf(c.Writer, "event: stream.complete\ndata: {\"reason\":\"settled\"}\n\n"); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case note, ok := <-parkedNotes(notes):
			if !ok && notes != nil {
				notes = nil
				eventTicker.Reset(250 * time.Millisecond)
			}
			if ok && note.Payload != "" && note.Payload != projectionID {
				continue
			}
		case <-eventTicker.C:
		case <-heartbeatTicker.C:
			if _, err := fmt.Fprintf(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func parkedNotes(notes <-chan notify.Notification) <-chan notify.Notification {
	if notes == nil {
		return nil
	}
	return notes
}

func writeTurnSSE(w io.Writer, event eventRow) error {
	payload := projectedTurnEvent(event)
	kind, _ := payload["kind"].(string)
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, kind, body)
	return err
}

func projectedTurnEvent(event eventRow) map[string]any {
	kind, projectedPayload := projectTurnEvent(event)
	return map[string]any{
		"schema_version": event.SchemaVersion,
		"run_id":         event.RunID,
		"turn_id":        event.TurnID,
		"sequence":       event.Sequence,
		"created_at":     event.CreatedAt.Format(time.RFC3339Nano),
		"kind":           kind,
		"ignorable":      event.Ignorable,
		"payload":        projectedPayload,
	}
}

// projectTurnEvent 是服务端唯一把 append-only journal 词表翻成浏览器协议的地方。
//
// text.chunk → item.delta，turn/end → turn.completed / failed / unknown 等。compacted 行变成 agent.ignored，保留 raw_kind。payload 只拷进对应稳定形状，不扩写、不补第二份 transcript。
//
// 禁区：不要在其它 handler 再翻译一套 kind；不要把 ignorable 未知 kind 当成有效 UI 事件（应发 agent.ignored / agent.invalid）。
func projectTurnEvent(event eventRow) (string, map[string]any) {
	var raw map[string]any
	if err := json.Unmarshal(event.Payload, &raw); err != nil || raw == nil {
		raw = map[string]any{}
	}
	copyPayload := func() map[string]any {
		out := make(map[string]any, len(raw))
		for key, value := range raw {
			out[key] = value
		}
		return out
	}
	if compacted, _ := raw["compacted"].(bool); compacted {
		return "agent.ignored", map[string]any{"raw_kind": event.Kind, "compacted": true}
	}
	itemPayload := func(itemID, itemKind string) map[string]any {
		out := copyPayload()
		out["item_id"] = itemID
		out["item_kind"] = itemKind
		return out
	}
	stringValue := func(key string) string {
		value, _ := raw[key].(string)
		return value
	}
	switch event.Kind {
	case "turn/start":
		return "turn.started", copyPayload()
	case "step/start":
		stepID := stringValue("step_id")
		return "item.started", map[string]any{"item_id": stepID, "item_kind": "step", "step_id": stepID}
	case "text.chunk":
		itemID := stringValue("attempt_id")
		if itemID == "" {
			itemID = stringValue("step_id")
		}
		return "item.delta", itemPayload(itemID, "assistant_text")
	case "thinking.chunk":
		return "item.delta", itemPayload(stringValue("attempt_id"), "thinking")
	case "assistant/message":
		itemID := stringValue("attempt_id")
		return "item.completed", itemPayload(itemID, "assistant_text")
	case "tool/call":
		return "item.started", itemPayload(stringValue("step_id"), "tool_call")
	case "tool/result":
		return "item.completed", itemPayload(stringValue("step_id"), "tool_call")
	case "question/requested":
		out := copyPayload()
		out["approval_id"] = stringValue("id")
		out["approval_kind"] = "question"
		out["item_kind"] = "question"
		return "approval.requested", out
	case "question/answered":
		out := copyPayload()
		out["approval_id"] = stringValue("question_id")
		out["approval_kind"] = "question"
		return "approval.resolved", out
	case "approval/requested":
		out := copyPayload()
		if _, ok := out["approval_id"]; !ok {
			out["approval_id"] = stringValue("id")
		}
		return "approval.requested", out
	case "approval/resolved":
		return "approval.resolved", copyPayload()
	case "turn/end":
		reason := stringValue("reason")
		switch reason {
		case "completed", "succeeded":
			return "turn.completed", copyPayload()
		case "awaiting_confirmation":
			return "turn.awaiting_confirmation", copyPayload()
		case "failed":
			return "turn.failed", copyPayload()
		case "canceled", "cancelled":
			return "turn.canceled", copyPayload()
		default:
			return "turn.unknown", copyPayload()
		}
	case "turn/cancel_requested", "turn/resume_requested":
		return "agent.ignored", map[string]any{"raw_kind": event.Kind}
	default:
		if event.Ignorable {
			return "agent.ignored", map[string]any{"raw_kind": event.Kind}
		}
		return "agent.invalid", map[string]any{"raw_kind": event.Kind}
	}
}
