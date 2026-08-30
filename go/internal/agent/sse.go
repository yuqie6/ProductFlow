package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
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
	ctx := c.Request.Context()
	if inSet(terminalTurn, row.Status) || row.HarnessTurnID == nil || !s.GatewayConfigured() {
		s.writePersistedTurnEvents(c, projectionID, cursor)
		return
	}
	writer := sseWriter{Writer: c.Writer, Flusher: flusher}
	if err := s.Gateway.StreamTurnEvents(ctx, conversationID, *row.HarnessTurnID, row.TaskID, cursor, writer); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.writePersistedTurnEvents(c, projectionID, cursor)
	}
}

func (s Service) writePersistedTurnEvents(c *gin.Context, projectionID string, cursor int) {
	ctx := c.Request.Context()
	flusher, _ := c.Writer.(http.Flusher)
	events, err := s.ListEvents(ctx, projectionID, cursor)
	if err != nil {
		return
	}
	for _, event := range events {
		if err := writeTurnSSE(c.Writer, event); err != nil {
			return
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func writeTurnSSE(w io.Writer, event eventRow) error {
	payload := map[string]any{
		"schema_version": event.SchemaVersion,
		"run_id":         event.RunID,
		"turn_id":        event.TurnID,
		"sequence":       event.Sequence,
		"created_at":     event.CreatedAt.Format(time.RFC3339Nano),
		"kind":           event.Kind,
		"payload":        json.RawMessage(event.Payload),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Kind, body)
	return err
}

type sseWriter struct {
	io.Writer
	http.Flusher
}

func (w sseWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if w.Flusher != nil {
		w.Flusher.Flush()
	}
	return n, err
}
