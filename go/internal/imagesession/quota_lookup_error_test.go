package imagesession

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestQuotaFinalizersPreserveLookupFailure(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_quotaread_%d", time.Now().UnixNano()))
	ctx := context.Background()
	merchantID := auth.MustDevMerchantID(t, db)
	ss := newSessionServerWithDatabase(t, pool, db)
	_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "quota lookup failure", "size": "1024x1024"})
	if _, err := (&quota.Service{DB: db}).Adjust(ctx, merchantID, clockid.New(), 10, "lookup fixture", ""); err != nil {
		t.Fatal(err)
	}
	if err := (Service{DB: db}).reserveGenerationQuota(ctx, merchantID, taskID, 0); err != nil {
		t.Fatal(err)
	}
	before := loadQuotaAccount(t, db, merchantID)
	// Rename only this disposable test database's table. A missing relation must
	// stop key resolution rather than starting another quota operation on a fallback key.
	if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds RENAME TO unavailable_quota_holds"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "ALTER TABLE unavailable_quota_holds RENAME TO merchant_quota_holds"); err != nil {
			t.Error(err)
		}
	})
	e := Executor{DB: db}
	for name, finalize := range map[string]func(context.Context, string, string) error{"settle": e.settleGenerationQuota, "release": e.releaseGenerationQuota, "unknown": e.markGenerationQuotaUnknown} {
		t.Run(name, func(t *testing.T) {
			err := finalize(ctx, merchantID, taskID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
				t.Fatalf("lookup cause lost: %v", err)
			}
		})
	}
	after := loadQuotaAccount(t, db, merchantID)
	if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
		t.Fatal("lookup failure changed account")
	}
	var status string
	if err := pool.QueryRow(ctx, "SELECT status FROM unavailable_quota_holds WHERE idempotency_key=$1", generationQuotaKey(taskID, 0)).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != quota.StatusReserved {
		t.Fatalf("hold=%s", status)
	}
}
