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

// projectTurnEvent is the only server-side translation from the append-only
// journal vocabulary to the browser protocol. The raw payload remains bounded
// and is copied only into the corresponding stable item shape.
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
