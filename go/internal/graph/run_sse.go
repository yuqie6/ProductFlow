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

// streamRunEvents 是 GET .../runs/:run_id/events：200 text/event-stream；缺 run 404。
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

// listRunEvents 按 seq 读取 workflow_graph_run_events，供 SSE 回放；不是独立 HTTP 路由。
func (h HTTP) listRunEvents(ctx context.Context, runID string, after int) ([]graphRunEventRow, error) {
	var out []graphRunEventRow
	err := tx.WithGorm(ctx, h.Service.DB, func(dbTx *gorm.DB) error {
		var err error
		out, err = listGraphRunEvents(ctx, dbTx, runID, after, maxRunEventPageSize)
		return err
	})
	return out, err
}

// writeRunEvent 写一条 SSE：event=run.event，id 用 Sequence，retry 1000ms。不要改事件名，前端按这个订阅。
func writeRunEvent(c *gin.Context, payload GraphRunEventResponse) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.Writer, "retry: 1000\nevent: run.event\nid: %d\ndata: %s\n\n", payload.Sequence, body)
	return err
}

// runEventCursor 取 after 查询或 Last-Event-ID 的较大值。这是事件序号不是页码；非法整数 400。
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
