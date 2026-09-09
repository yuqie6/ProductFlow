package notify

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/testdb"
)

// TestReplicaFieldTwoListenersSurviveNotifyLoss 模拟两个 API 副本各自 LISTEN：
// 关掉副本 A 的 listener 后，Publish 仍能唤醒副本 B。
func TestReplicaFieldTwoListenersSurviveNotifyLoss(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	t.Cleanup(cancelA)
	ctxB, cancelB := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelB()

	notesA, err := Listen(ctxA, pool, ChannelRun)
	if err != nil {
		t.Fatal(err)
	}
	notesB, err := Listen(ctxB, pool, ChannelRun)
	if err != nil {
		t.Fatal(err)
	}

	if err := Publish(ctxB, gdb, ChannelRun, "both-replicas"); err != nil {
		t.Fatal(err)
	}
	waitNote(t, ctxB, notesA, "both-replicas")
	waitNote(t, ctxB, notesB, "both-replicas")

	cancelA()
	select {
	case <-notesA:
	case <-time.After(2 * time.Second):
		t.Fatal("replica A listener did not exit after cancel")
	}

	if err := Publish(ctxB, gdb, ChannelRun, "after-a-lost"); err != nil {
		t.Fatal(err)
	}
	waitNote(t, ctxB, notesB, "after-a-lost")
}

// TestReplicaFieldTwoAPIPoolsKeepIndependentLISTEN 模拟两个 API 副本各自一个 pool：
// 每副本一条 LISTEN；停掉 A 后 B 仍能收到 Run 通知。
func TestReplicaFieldTwoAPIPoolsKeepIndependentLISTEN(t *testing.T) {
	poolA := testdb.Pool(t)
	poolB := testdb.Pool(t)
	_, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	notesA, stopA := Subscribe(poolA, ChannelRun)
	notesB, stopB := Subscribe(poolB, ChannelRun)
	defer stopB()

	baseline := ListenerConnections.Load()
	deadline := time.Now().Add(2 * time.Second)
	for ListenerConnections.Load() < baseline+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if err := Publish(ctx, gdb, ChannelRun, "run-both"); err != nil {
		t.Fatal(err)
	}
	waitNote(t, ctx, notesA, "run-both")
	waitNote(t, ctx, notesB, "run-both")

	stopA()
	if err := Publish(ctx, gdb, ChannelRun, "run-after-a"); err != nil {
		t.Fatal(err)
	}
	waitNote(t, ctx, notesB, "run-after-a")
}

func waitNote(t *testing.T, ctx context.Context, notes <-chan Notification, payload string) {
	t.Helper()
	for {
		select {
		case n, ok := <-notes:
			if !ok {
				t.Fatal("listener closed before " + payload)
			}
			if n.Payload == payload {
				return
			}
		case <-ctx.Done():
			t.Fatalf("did not receive %s", payload)
		}
	}
}
