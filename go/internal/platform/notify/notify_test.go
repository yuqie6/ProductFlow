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
