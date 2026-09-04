package imagesession

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
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

func TestQueuedPositionsReturnsOnlyRequestedGlobalRanks(t *testing.T) {
	_, gdb := testdb.Open(t)
	tx := gdb.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	// 测试事务内清空 queued 状态，使跨会话排名不受其它夹具影响；结束后回滚。
	if err := tx.Model(&schema.ImageSessionGenerationTasks{}).Where("status = ?", "queued").Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	sessionIDs := []string{clockid.New(), clockid.New()}
	for _, id := range sessionIDs {
		if err := tx.Create(&schema.ImageSessions{ID: id, Title: "queue ranks", CreatedAt: start, UpdatedAt: start}).Error; err != nil {
			t.Fatal(err)
		}
	}
	prefix := clockid.New()[:24]
	for i, suffix := range []string{"a", "b", "c", "d"} {
		row := schema.ImageSessionGenerationTasks{
			ID: prefix + suffix, SessionID: sessionIDs[i%2], Status: "queued",
			Prompt: "queue rank", Size: "1024x1024", GenerationCount: 1, CreatedAt: start,
		}
		if i == 3 {
			row.Status = "succeeded"
		}
		if err := tx.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	positions := queuedPositions(ctx, tx, []string{prefix + "b", prefix + "c", prefix + "c", prefix + "d", "missing", ""})
	if len(positions) != 2 || positions[prefix+"b"] != 2 || positions[prefix+"c"] != 3 {
		t.Fatalf("requested positions must retain global timestamp/id rank: %v", positions)
	}
	if positions := queuedPositions(ctx, tx, nil); len(positions) != 0 {
		t.Fatalf("empty request returned positions: %v", positions)
	}
}
