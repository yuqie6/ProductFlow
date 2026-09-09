package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
)

func TestProductTouchFailureRollsBackDeliveryResult(t *testing.T) {
	ctx := context.Background()
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	productID := created.Product.ID
	sourceID := created.CreatedAssets[0].ID
	jobID := insertQueuedDeliveryJob(t, ds, productID, sourceID, strings.Repeat("a", 64), time.Now().UTC())
	const constraint = "test_delivery_product_touch"
	// NOT VALID leaves this existing product readable but rejects its next write.
	// The condition isolates the fault to this test's product in the package DB.
	if _, err := ds.pool.Exec(ctx, "ALTER TABLE products ADD CONSTRAINT "+constraint+" CHECK (id <> '"+productID+"') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := ds.pool.Exec(context.Background(), "ALTER TABLE products DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	var before time.Time
	if err := ds.pool.QueryRow(ctx, "SELECT updated_at FROM products WHERE id=$1", productID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	executor := Executor{DB: ds.db, Media: ds.media}
	err := executor.Execute(ctx, jobID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != constraint {
		t.Fatalf("product write cause lost: %v", err)
	}
	var status string
	var resultID *string
	var assets int
	if err := ds.pool.QueryRow(ctx, "SELECT status,result_asset_id FROM delivery_rendition_jobs WHERE id=$1", jobID).Scan(&status, &resultID); err != nil {
		t.Fatal(err)
	}
	if err := ds.pool.QueryRow(ctx, "SELECT count(*) FROM product_image_assets WHERE parent_asset_id=$1", sourceID).Scan(&assets); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	if err := ds.pool.QueryRow(ctx, "SELECT updated_at FROM products WHERE id=$1", productID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || resultID != nil || assets != 0 || !before.Equal(after) {
		t.Fatalf("status=%s result=%v assets=%d before=%s after=%s", status, resultID, assets, before, after)
	}
	if _, err := ds.pool.Exec(ctx, "ALTER TABLE products DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{DB: ds.db}).Retry(auth.WithMerchantID(ctx, auth.MustDevMerchantID(t, ds.db)), jobID); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	if err := ds.pool.QueryRow(ctx, "SELECT updated_at FROM products WHERE id=$1", productID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Fatalf("successful delivery did not touch product: before=%s after=%s", before, after)
	}
}
