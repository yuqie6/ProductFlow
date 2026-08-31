package imagesession

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/notify"
)

// streamEvents 是 GET /api/image-sessions/:image_session_id/events：200 打开 text/event-stream。
func (h HTTP) streamEvents(c *gin.Context) {
	sessionID := c.Param("image_session_id")
	status, err := h.Service.Status(c.Request.Context(), sessionID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	ctx := c.Request.Context()
	notes, listenErr := notify.Listen(ctx, h.Service.Pool, notify.ChannelImageSession)

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

	if err := writeSessionStatus(c, status); err != nil {
		return
	}
	if !status.HasActiveGenerationTask {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case note, ok := <-sessionNotesOrNil(notes):
			if !ok && notes != nil {
				notes = nil
				eventTicker.Reset(250 * time.Millisecond)
			}
			if ok && note.Payload != "" && note.Payload != sessionID {
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
			continue
		}
		status, err = h.Service.Status(ctx, sessionID)
		if err != nil {
			return
		}
		if err := writeSessionStatus(c, status); err != nil {
			return
		}
		if !status.HasActiveGenerationTask {
			return
		}
	}
}

func sessionNotesOrNil(notes <-chan notify.Notification) <-chan notify.Notification {
	if notes == nil {
		return nil
	}
	return notes
}

// writeSessionStatus 写 SSE event=session.status。不要改事件名；失败时让上层关流，不要写半帧。
func writeSessionStatus(c *gin.Context, status StatusResponse) error {
	body, err := json.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.Writer, "retry: 1000\nevent: session.status\ndata: %s\n\n", body)
	if err != nil {
		return err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
