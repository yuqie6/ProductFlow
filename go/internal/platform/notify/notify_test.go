package notify

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestPublishListenRoundTrip(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	notes, err := Listen(ctx, pool, ChannelTurn)
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(ctx, gdb, ChannelTurn, "projection-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-notes:
		if n.Channel != ChannelTurn || n.Payload != "projection-1" {
			t.Fatalf("note %+v", n)
		}
	case <-ctx.Done():
		t.Fatal("did not receive notify")
	}
}

func TestPublishImageSessionChannel(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	notes, err := Listen(ctx, pool, ChannelImageSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(ctx, gdb, ChannelImageSession, "session-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-notes:
		if n.Channel != ChannelImageSession || n.Payload != "session-1" {
			t.Fatalf("note %+v", n)
		}
	case <-ctx.Done():
		t.Fatal("did not receive image-session notify")
	}
}

func TestSubscribeSharesOneListenerAcrossChannels(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	baseline := pool.Stat().AcquiredConns()
	listenerBaseline := ListenerConnections.Load()
	runNotes, stopRun := Subscribe(pool, ChannelRun)
	imageNotes, stopImage := Subscribe(pool, ChannelImageSession)
	defer stopRun()
	defer stopImage()

	deadline := time.Now().Add(2 * time.Second)
	for (pool.Stat().AcquiredConns() < baseline+1 || ListenerConnections.Load() < listenerBaseline+1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := pool.Stat().AcquiredConns(); got != baseline+1 {
		t.Fatalf("listener connections %d want %d", got, baseline+1)
	}
	if got := ListenerConnections.Load(); got != listenerBaseline+1 {
		t.Fatalf("listener metric %d want %d", got, listenerBaseline+1)
	}
	if err := Publish(ctx, gdb, ChannelRun, "run-shared"); err != nil {
		t.Fatal(err)
	}
	if err := Publish(ctx, gdb, ChannelImageSession, "image-shared"); err != nil {
		t.Fatal(err)
	}
	select {
	case note := <-runNotes:
		if note.Channel != ChannelRun || note.Payload != "run-shared" {
			t.Fatalf("run note %+v", note)
		}
	case <-ctx.Done():
		t.Fatal("did not receive run notification")
	}
	select {
	case note := <-imageNotes:
		if note.Channel != ChannelImageSession || note.Payload != "image-shared" {
			t.Fatalf("image note %+v", note)
		}
	case <-ctx.Done():
		t.Fatal("did not receive image notification")
	}
}

func TestPublishListenDispatchChannel(t *testing.T) {
	if !ValidChannel(ChannelDispatch) {
		t.Fatal("ChannelDispatch must be in the closed channel set")
	}
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	notes, err := Listen(ctx, pool, ChannelDispatch)
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(ctx, gdb, ChannelDispatch, "dispatch-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-notes:
		if n.Channel != ChannelDispatch || n.Payload != "dispatch-1" {
			t.Fatalf("note %+v", n)
		}
	case <-ctx.Done():
		t.Fatal("did not receive dispatch notify")
	}
}

func TestListenExitsOnCancel(t *testing.T) {
	pool, _ := testdb.Open(t)
	baseline := ListenerConnections.Load()
	ctx, cancel := context.WithCancel(context.Background())
	notes, err := Listen(ctx, pool, ChannelDispatch)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-notes:
		if ok {
			t.Fatal("expected Listen to close after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not unblock after cancel")
	}
	deadline := time.Now().Add(2 * time.Second)
	for ListenerConnections.Load() > baseline && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := ListenerConnections.Load(); got != baseline {
		t.Fatalf("listener metric %d want %d", got, baseline)
	}
}

func TestPublishRejectsUnknownChannel(t *testing.T) {
	_, gdb := testdb.Open(t)
	if err := Publish(context.Background(), gdb, "not_a_channel", "x"); err == nil {
		t.Fatal("expected invalid channel")
	}
}

func TestPublishTruncatesPayloadAtUTF8Boundary(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	notes, err := Listen(ctx, pool, ChannelTurn)
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("界", maxPayload)
	if err := Publish(ctx, gdb, ChannelTurn, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case note := <-notes:
		if !strings.HasPrefix(payload, note.Payload) {
			t.Fatal("truncated payload is not a prefix of the input")
		}
		if len(note.Payload) > maxPayload {
			t.Fatalf("payload has %d bytes", len(note.Payload))
		}
	case <-ctx.Done():
		t.Fatal("did not receive notify")
	}
}
