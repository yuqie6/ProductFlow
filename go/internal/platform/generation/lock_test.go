package generation

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestLockAdmissionUsesSharedTransactionLock(t *testing.T) {
	pool, db := testdb.Open(t)
	ctx := context.Background()
	first := db.Begin()
	if first.Error != nil {
		t.Fatal(first.Error)
	}
	if err := LockAdmission(ctx, first); err != nil {
		_ = first.Rollback().Error
		t.Fatal(err)
	}
	second, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if _, err := second.Exec(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	var acquired bool
	if err := second.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", AdmissionLockKey).Scan(&acquired); err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("shared generation lock was not held by the first transaction")
	}
	if err := first.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if _, err := second.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}
