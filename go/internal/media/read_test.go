package media

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestReadVerifiedReturnsInspectedBytes(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	want := pngBytes(t, 6, 4)
	obj, err := store.Stage(ctx, tx, want, "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadVerified(ctx, tx, obj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes, want) {
		t.Fatalf("bytes len %d want %d", len(got.Bytes), len(want))
	}
	if got.Verified.Width != 6 || got.Verified.Height != 4 || got.Verified.SHA256 != obj.SHA256 {
		t.Fatalf("%+v vs %+v", got.Verified, obj)
	}
	if got.Object.ID != obj.ID {
		t.Fatalf("object id %s", got.Object.ID)
	}
}

func TestReadVerifiedNotFound(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	_, err := store.ReadVerified(context.Background(), tx, clockid.New())
	re, ok := AsReadError(err)
	if !ok || re.Kind != ReadNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestReadVerifiedNotVerified(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	obj, err := store.Stage(ctx, tx, pngBytes(t, 4, 4), "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("UPDATE media_objects SET verification_status = ? WHERE id = ?", StatusMissing, obj.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadVerified(ctx, tx, obj.ID)
	re, ok := AsReadError(err)
	if !ok || re.Kind != ReadNotVerified || re.Field != "" {
		t.Fatalf("got %#v %v", re, err)
	}
}

func TestReadVerifiedMissingFile(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	obj, err := store.Stage(ctx, tx, pngBytes(t, 5, 3), "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := store.Files.Resolve(obj.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(abs); err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadVerified(ctx, tx, obj.ID)
	re, ok := AsReadError(err)
	if !ok || re.Kind != ReadMissingFile {
		t.Fatalf("got %v", err)
	}
}

func TestReadVerifiedByteSizeMismatch(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	obj, err := store.Stage(ctx, tx, pngBytes(t, 4, 4), "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := store.Files.Resolve(obj.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, append(mustRead(t, abs), 0x00), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadVerified(ctx, tx, obj.ID)
	re, ok := AsReadError(err)
	if !ok || re.Kind != ReadIdentity || re.Field != FieldByteSize {
		t.Fatalf("got %#v %v", re, err)
	}
}

func TestReadVerifiedSHAMismatch(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	obj, err := store.Stage(ctx, tx, pngBytes(t, 4, 4), "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	other := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := tx.Exec("UPDATE media_objects SET sha256 = ? WHERE id = ?", other, obj.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadVerified(ctx, tx, obj.ID)
	re, ok := AsReadError(err)
	if !ok || re.Kind != ReadIdentity || re.Field != FieldSHA256 {
		t.Fatalf("got %#v %v", re, err)
	}
}

func TestReadVerifiedDimensionMismatch(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	obj, err := store.Stage(ctx, tx, pngBytes(t, 4, 4), "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("UPDATE media_objects SET width = ?, height = ? WHERE id = ?", 9, 9, obj.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadVerified(ctx, tx, obj.ID)
	re, ok := AsReadError(err)
	if !ok || re.Kind != ReadIdentity || re.Field != FieldDims {
		t.Fatalf("got %#v %v", re, err)
	}
}

func TestReadVerifiedCorruptFile(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	obj, err := store.Stage(ctx, tx, pngBytes(t, 4, 4), "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := store.Files.Resolve(obj.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	garbage := bytes.Repeat([]byte("x"), obj.ByteSize)
	if err := os.WriteFile(abs, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadVerified(ctx, tx, obj.ID)
	re, ok := AsReadError(err)
	if !ok || re.Kind != ReadCorrupt {
		t.Fatalf("got %v", err)
	}
}

func TestReadVerifiedDoesNotReturnBytesOnFailure(t *testing.T) {
	store, tx, rollback := stageStore(t)
	defer rollback()
	ctx := context.Background()
	obj, err := store.Stage(ctx, tx, pngBytes(t, 3, 3), "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := store.Files.Resolve(obj.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(abs); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadVerified(ctx, tx, obj.ID)
	if err == nil {
		t.Fatal("expected error")
	}
	if got.Bytes != nil {
		t.Fatalf("must not return unverified bytes len %d", len(got.Bytes))
	}
}

func stageStore(t *testing.T) (Store, *gorm.DB, func()) {
	t.Helper()
	_, gdb := testdb.Open(t)
	tx := gdb.WithContext(context.Background()).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	store := Store{Files: storage.Local{Root: t.TempDir()}}
	return store, tx, func() { _ = tx.Rollback() }
}
