package media

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestStageInsertsVerifiedRow(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	root := t.TempDir()
	store := Store{Files: storage.Local{Root: root}}
	var compensation storage.Compensation
	obj, err := store.Stage(ctx, tx, pngBytes(t, 6, 4), "image/png", &compensation)
	if err != nil {
		t.Fatal(err)
	}
	if obj.VerificationStatus != StatusVerified || obj.Width != 6 || obj.SHA256 == "" {
		t.Fatalf("%+v", obj)
	}
	got, err := store.Get(ctx, tx, obj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StoragePath != obj.StoragePath {
		t.Fatalf("path %q", got.StoragePath)
	}
	compensation.Rollback()
}
