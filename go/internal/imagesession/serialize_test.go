package imagesession

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestQueueOverviewMatchesSharedSnapshot(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	snap, err := generation.LoadQueueOverview(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	ov := Service{}.queueOverview(ctx, gdb)
	if ov.Max != snap.Max || ov.Running != snap.OverviewRunning || ov.Queued != snap.OverviewQueued || ov.Active != snap.OverviewActive() {
		t.Fatalf("queueOverview=%+v snapshot=%+v", ov, snap)
	}
}
