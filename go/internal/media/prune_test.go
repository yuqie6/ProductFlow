package media

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestPruneUnreferencedDeletesOrphanMedia(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	tx := gdb.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback() }()

	root := t.TempDir()
	store := Store{Files: storage.Local{Root: root}}
	var compensation storage.Compensation
	obj, err := store.Stage(ctx, tx, pngBytes(t, 4, 4), "image/png", &compensation)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := PruneUnreferenced(ctx, tx, []string{obj.ID, obj.ID, ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].ID != obj.ID {
		t.Fatalf("%+v", deleted)
	}
	if _, err := store.Get(ctx, tx, obj.ID); err == nil {
		t.Fatal("orphaned media should be gone")
	}
	compensation.Release()
}

func TestPruneKeepsReferencedMedia(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	tx := gdb.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback() }()

	root := t.TempDir()
	store := Store{Files: storage.Local{Root: root}}
	var compensation storage.Compensation
	obj, err := store.Stage(ctx, tx, pngBytes(t, 4, 4), "image/png", &compensation)
	if err != nil {
		t.Fatal(err)
	}
	merchantID := clockid.New()
	if _, err := pfdb.Exec(ctx, tx, `
		INSERT INTO merchants (id, name, status, created_at, updated_at) VALUES ($1, '引用商家', 'active', NOW(), NOW())
	`, merchantID); err != nil {
		t.Fatal(err)
	}
	productID := clockid.New()
	assetID := clockid.New()
	if _, err := pfdb.Exec(ctx, tx, `
		INSERT INTO products (id, name, merchant_id, created_at, updated_at) VALUES ($1, '引用商品', $2, NOW(), NOW())
	`, productID, merchantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pfdb.Exec(ctx, tx, `
		INSERT INTO product_image_assets (
			id, product_id, media_object_id, origin_type, display_name, original_filename, created_at, updated_at
		) VALUES ($1, $2, $3, 'upload', 'ref.png', 'ref.png', NOW(), NOW())
	`, assetID, productID, obj.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := PruneUnreferenced(ctx, tx, []string{obj.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("%+v", deleted)
	}
	if _, err := store.Get(ctx, tx, obj.ID); err != nil {
		t.Fatal(err)
	}
	compensation.Rollback()
}
