package localedit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestQuotaMerchantLookupPreservesDatabaseCause(t *testing.T) {
	_, db := testdb.IsolatedMigrated(t, "pf_localedit_merchant_"+strings.ReplaceAll(clockid.New(), "-", ""))
	ctx := context.Background()
	if err := db.Exec("ALTER TABLE products RENAME TO unavailable_merchant_source").Error; err != nil {
		t.Fatal(err)
	}
	_, err := merchantIDForProduct(ctx, db, clockid.New())
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("database cause lost: %v", err)
	}
	var appErr apperr.Error
	if !errors.As(err, &appErr) || appErr.Detail != "读取商品商家失败" || appErr.Status != 500 {
		t.Errorf("public contract changed: %v", err)
	}
	if err := db.Exec("ALTER TABLE unavailable_merchant_source RENAME TO products").Error; err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = merchantIDForProduct(cancelled, db, clockid.New())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation cause lost: %v", err)
	}
}
