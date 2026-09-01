package agent

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestLoadSessionsKeepsConversationCountAndPerSessionLimit(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	now := time.Now().UTC()
	productID := clockid.New()
	sessionID := clockid.New()
	productIDRef := productID
	createdIDs := make([]string, 0, 21)

	if err := gdb.WithContext(ctx).Exec(
		"INSERT INTO products (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)",
		productID, "批量投影商品", now, now,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.WithContext(ctx).Create(&schema.AgentSessions{
		ID: sessionID, Title: "批量投影", Status: "active", ProductID: &productIDRef,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gdb.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&schema.AgentConversations{}).Error
		_ = gdb.WithContext(ctx).Where("id = ?", sessionID).Delete(&schema.AgentSessions{}).Error
		_ = gdb.WithContext(ctx).Where("id = ?", productID).Delete(&schema.Products{}).Error
	})

	for index := 0; index < 21; index++ {
		id := clockid.New()
		createdIDs = append(createdIDs, id)
		at := now.Add(time.Duration(index) * time.Second)
		if err := gdb.WithContext(ctx).Create(&schema.AgentConversations{
			ID: id, ProductID: &productIDRef, HarnessRunID: "batch-harness-" + id,
			Status: "collecting", CreatedAt: at, UpdatedAt: at, SessionID: &sessionID,
			ScopeType: "product_workflow",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	items, err := loadSessions(ctx, gdb, []string{sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items %d", len(items))
	}
	if items[0].ConversationCount != len(createdIDs) {
		t.Fatalf("conversation count %d want %d", items[0].ConversationCount, len(createdIDs))
	}
	if len(items[0].Conversations) != sessionConversationLimit {
		t.Fatalf("conversation summaries %d want %d", len(items[0].Conversations), sessionConversationLimit)
	}
	if items[0].Conversations[0].ConversationID != createdIDs[len(createdIDs)-1] {
		t.Fatalf("latest conversation %s want %s", items[0].Conversations[0].ConversationID, createdIDs[len(createdIDs)-1])
	}
	if items[0].Conversations[len(items[0].Conversations)-1].ConversationID != createdIDs[1] {
		t.Fatalf("oldest returned conversation %s want %s", items[0].Conversations[len(items[0].Conversations)-1].ConversationID, createdIDs[1])
	}
}
