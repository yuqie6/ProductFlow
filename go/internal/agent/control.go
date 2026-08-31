package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"gorm.io/gorm"
)

const (
	controlEventSessionChanged = "session.changed"
	controlEventTaskChanged    = "task.changed"
	controlEventLeaseChanged   = "lease.changed"
	controlHeartbeatInterval   = 15 * time.Second
	controlWatermarkInterval   = 2 * time.Second
)

type controlEvent struct {
	Kind        string
	SessionID   string
	TaskID      string
	ExecutionID string
	Phase       string
}

type controlHub struct {
	mu   sync.Mutex
	next uint64
	subs map[uint64]chan controlEvent
}

func newControlHub() *controlHub {
	return &controlHub{subs: make(map[uint64]chan controlEvent)}
}

var processControl = newControlHub()

func (s Service) hub() *controlHub {
	if s.control != nil {
		return s.control
	}
	return processControl
}

func (h *controlHub) Subscribe() (<-chan controlEvent, func()) {
	ch := make(chan controlEvent, 16)
	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = ch
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()
		})
	}
}

func (h *controlHub) Publish(event controlEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func publishControl(db *gorm.DB, event controlEvent) {
	processControl.Publish(event)
	payload, err := json.Marshal(map[string]string{
		"kind":         event.Kind,
		"session_id":   event.SessionID,
		"task_id":      event.TaskID,
		"execution_id": event.ExecutionID,
		"phase":        event.Phase,
	})
	if err != nil {
		return
	}
	_ = notify.Publish(context.Background(), db, notify.ChannelControl, string(payload))
}

func publishSessionChanged(db *gorm.DB, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	publishControl(db, controlEvent{Kind: controlEventSessionChanged, SessionID: sessionID})
}

func publishTaskChanged(db *gorm.DB, taskID, sessionID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	publishControl(db, controlEvent{
		Kind:      controlEventTaskChanged,
		TaskID:    taskID,
		SessionID: strings.TrimSpace(sessionID),
	})
}

func publishLeaseChanged(db *gorm.DB, executionID, phase string) {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return
	}
	publishControl(db, controlEvent{
		Kind:        controlEventLeaseChanged,
		ExecutionID: executionID,
		Phase:       strings.TrimSpace(phase),
	})
}

func controlEventFromNotify(payload string) (controlEvent, bool) {
	var raw map[string]string
	if json.Unmarshal([]byte(payload), &raw) != nil {
		return controlEvent{}, false
	}
	kind := strings.TrimSpace(raw["kind"])
	if kind == "" {
		return controlEvent{}, false
	}
	return controlEvent{
		Kind:        kind,
		SessionID:   raw["session_id"],
		TaskID:      raw["task_id"],
		ExecutionID: raw["execution_id"],
		Phase:       raw["phase"],
	}, true
}

func (s Service) StreamControlEvents(c *gin.Context) {
	ctx := c.Request.Context()
	notes, listenErr := notify.Listen(ctx, s.Pool, notify.ChannelControl)
	var hubCh <-chan controlEvent
	if listenErr != nil {
		ch, unsubscribe := s.hub().Subscribe()
		defer unsubscribe()
		hubCh = ch
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	if err := writeControlHeartbeat(c.Writer, flusher); err != nil {
		return
	}
	ticker := time.NewTicker(controlHeartbeatInterval)
	defer ticker.Stop()
	var watermarkTicker *time.Ticker
	if listenErr != nil {
		watermarkTicker = time.NewTicker(controlWatermarkInterval)
		defer watermarkTicker.Stop()
	}
	sessionsAt, tasksAt := s.controlListWatermarks(ctx)
	for {
		var watermarkC <-chan time.Time
		if watermarkTicker != nil {
			watermarkC = watermarkTicker.C
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writeControlHeartbeat(c.Writer, flusher); err != nil {
				return
			}
		case <-watermarkC:
			nextSessions, nextTasks := s.controlListWatermarks(ctx)
			if nextSessions.After(sessionsAt) {
				sessionsAt = nextSessions
				if err := writeControlEvent(c.Writer, controlEvent{Kind: controlEventSessionChanged}); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if nextTasks.After(tasksAt) {
				tasksAt = nextTasks
				if err := writeControlEvent(c.Writer, controlEvent{Kind: controlEventTaskChanged}); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
		case note, ok := <-controlNotes(notes):
			if !ok {
				notes = nil
				if watermarkTicker == nil {
					watermarkTicker = time.NewTicker(controlWatermarkInterval)
					defer watermarkTicker.Stop()
				}
				if hubCh == nil {
					ch, unsubscribe := s.hub().Subscribe()
					defer unsubscribe()
					hubCh = ch
				}
				continue
			}
			event, parsed := controlEventFromNotify(note.Payload)
			if !parsed {
				continue
			}
			if err := writeControlEvent(c.Writer, event); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case event, ok := <-hubCh:
			if !ok {
				return
			}
			if err := writeControlEvent(c.Writer, event); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func controlNotes(notes <-chan notify.Notification) <-chan notify.Notification {
	if notes == nil {
		return nil
	}
	return notes
}

func writeControlHeartbeat(w io.Writer, flusher http.Flusher) error {
	if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

func writeControlEvent(w io.Writer, event controlEvent) error {
	payload := map[string]string{}
	if event.SessionID != "" {
		payload["session_id"] = event.SessionID
	}
	if event.TaskID != "" {
		payload["task_id"] = event.TaskID
	}
	if event.ExecutionID != "" {
		payload["execution_id"] = event.ExecutionID
	}
	if event.Phase != "" {
		payload["phase"] = event.Phase
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Kind, body)
	return err
}

func (s Service) controlListWatermarks(ctx context.Context) (time.Time, time.Time) {
	var sessions, tasks time.Time
	if s.DB == nil {
		return sessions, tasks
	}
	_ = s.DB.WithContext(ctx).Raw("SELECT COALESCE(MAX(updated_at), TIMESTAMPTZ 'epoch') FROM agent_sessions").Scan(&sessions).Error
	_ = s.DB.WithContext(ctx).Raw("SELECT COALESCE(MAX(updated_at), TIMESTAMPTZ 'epoch') FROM agent_tasks").Scan(&tasks).Error
	return sessions, tasks
}
