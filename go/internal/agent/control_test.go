package agent

import (
	"testing"
	"time"
)

func TestControlHubPublishSubscribe(t *testing.T) {
	hub := newControlHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	hub.Publish(controlEvent{Kind: controlEventSessionChanged, SessionID: "sess-1"})
	select {
	case event := <-ch:
		if event.Kind != controlEventSessionChanged || event.SessionID != "sess-1" || event.TaskID != "" {
			t.Fatalf("session event %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session.changed")
	}

	hub.Publish(controlEvent{Kind: controlEventTaskChanged, TaskID: "task-1", SessionID: "sess-1"})
	select {
	case event := <-ch:
		if event.Kind != controlEventTaskChanged || event.TaskID != "task-1" || event.SessionID != "sess-1" {
			t.Fatalf("task event %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task.changed")
	}

	unsubscribe()
	hub.Publish(controlEvent{Kind: controlEventSessionChanged, SessionID: "sess-2"})
	select {
	case event := <-ch:
		t.Fatalf("unsubscribed receiver got %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestControlHubPublishDoesNotBlockSlowSubscriber(t *testing.T) {
	hub := newControlHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 32; i++ {
			hub.Publish(controlEvent{Kind: controlEventSessionChanged, SessionID: "s"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	drained := 0
	for {
		select {
		case <-ch:
			drained++
		default:
			if drained == 0 {
				t.Fatal("buffered subscriber received nothing")
			}
			return
		}
	}
}
