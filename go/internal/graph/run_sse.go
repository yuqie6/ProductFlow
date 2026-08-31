package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func (h HTTP) streamRunEvents(c *gin.Context) {
	productID := c.Param("product_id")
	graphID := c.Param("workflow_id")
	runID := c.Param("run_id")
	run, err := h.Service.GetRun(c.Request.Context(), productID, graphID, runID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	cursor, err := runEventCursor(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	ctx := c.Request.Context()
	notes, listenErr := notify.Listen(ctx, h.Service.Pool, notify.ChannelRun)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	fallback := 2 * time.Second
	if listenErr != nil {
		notes = nil
		fallback = 250 * time.Millisecond
	}
	eventTicker := time.NewTicker(fallback)
	defer eventTicker.Stop()
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()
	for {
		events, err := h.listRunEvents(ctx, runID, cursor)
		if err != nil {
			return
		}
		for _, event := range events {
			payload, err := serializeGraphRunEvent(event)
			if err != nil {
				return
			}
			if err := writeRunEvent(c, payload); err != nil {
				return
			}
			cursor = event.Sequence
			if isTerminalRunEvent(event.Kind) {
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
		}
		if flusher != nil && len(events) > 0 {
			flusher.Flush()
		}
		if len(events) == 0 {
			if isTerminalRun(run.Status) {
				return
			}
			run, err = h.Service.GetRun(ctx, productID, graphID, runID)
			if err != nil || isTerminalRun(run.Status) {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case note, ok := <-notesOrNil(notes):
			if !ok && notes != nil {
				notes = nil
				eventTicker.Reset(250 * time.Millisecond)
			}
			if ok && note.Payload != "" && note.Payload != runID {
				continue
			}
		case <-eventTicker.C:
		case <-heartbeatTicker.C:
			if _, err := c.Writer.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func notesOrNil(notes <-chan notify.Notification) <-chan notify.Notification {
	if notes == nil {
		return nil
	}
	return notes
}

func (h HTTP) listRunEvents(ctx context.Context, runID string, after int) ([]graphRunEventRow, error) {
	var out []graphRunEventRow
	err := tx.WithGorm(ctx, h.Service.DB, func(dbTx *gorm.DB) error {
		var err error
		out, err = listGraphRunEvents(ctx, dbTx, runID, after, maxRunEventPageSize)
		return err
	})
	return out, err
}

func writeRunEvent(c *gin.Context, payload GraphRunEventResponse) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.Writer, "retry: 1000\nevent: run.event\nid: %d\ndata: %s\n\n", payload.Sequence, body)
	return err
}

func runEventCursor(c *gin.Context) (int, error) {
	cursor := 0
	for _, raw := range []string{c.Query("after"), c.GetHeader("Last-Event-ID")} {
		if raw == "" {
			continue
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return 0, apperr.Validation("图运行事件游标无效")
		}
		if parsed > cursor {
			cursor = parsed
		}
	}
	return cursor, nil
}

func isTerminalRunEvent(kind string) bool {
	return kind == "run.completed" || kind == "run.failed" || kind == "run.cancelled" || kind == "run.unknown"
}
