package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

func (s Service) StreamTurnEvents(c *gin.Context, productID *string, conversationID, projectionID string, after int, lastEventID string) {
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
	if _, err := s.GetTurn(c.Request.Context(), productID, conversationID, projectionID, false); err != nil {
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
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		events, err := s.ListEvents(ctx, projectionID, cursor)
		if err != nil {
			return
		}
		var proj schema.AgentTurnProjections
		_ = s.DB.WithContext(ctx).Select("status").Where("id = ?", projectionID).Take(&proj).Error
		status := proj.Status
		if len(events) == 0 && inSet(terminalTurn, status) {
			return
		}
		for _, event := range events {
			payload := map[string]any{
				"schema_version": event.SchemaVersion,
				"run_id":         event.RunID,
				"turn_id":        event.TurnID,
				"sequence":       event.Sequence,
				"created_at":     event.CreatedAt.Format(time.RFC3339Nano),
				"kind":           event.Kind,
				"payload":        json.RawMessage(event.Payload),
			}
			body, _ := json.Marshal(payload)
			fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Kind, body)
			cursor = event.Sequence
		}
		fmt.Fprint(c.Writer, ": heartbeat\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
