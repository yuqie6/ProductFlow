package localedit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestSnapshotDatabaseFailurePreservesRecovery(t *testing.T) {
	ctx := context.Background()
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_editsnapshot_%d", time.Now().UnixNano()))
	provider := &capturingEditProvider{MockProvider: MockProvider{Cap: SupportedCapability("mock-local")}}
	es := newEditServerWithDatabase(t, provider, pool, db)
	taskID := createQueuedLocalEdit(t, es, es.createProduct(t), "snapshot-db-failure")
	if _, err := restageLocalEditTask(ctx, db, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE product_image_assets RENAME TO unavailable_product_image_assets"); err != nil {
		t.Fatal(err)
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			_, err := pool.Exec(context.Background(), "ALTER TABLE unavailable_product_image_assets RENAME TO product_image_assets")
			if err != nil {
				t.Error(err)
			}
		}
	})
	executor := Executor{DB: db, Media: es.media, Provider: provider}
	err := runLocalEditRiverWorker(t, ctx, pool, taskID, executor)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Fatalf("snapshot read cause lost: %v", err)
	}
	var status, phase string
	if err := pool.QueryRow(ctx, `SELECT status,progress_phase FROM local_image_edit_tasks WHERE id=$1`, taskID).Scan(&status, &phase); err != nil {
		t.Fatal(err)
	}
	if status != "running" || phase != "claimed" || provider.lastSize != "" {
		t.Fatalf("task=%s phase=%s provider=%s", status, phase, provider.lastSize)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE unavailable_product_image_assets RENAME TO product_image_assets"); err != nil {
		t.Fatal(err)
	}
	restored = true
	if outcome, err := recoverLocalEditState(ctx, db, taskID, time.Minute, time.Now().UTC().Add(2*time.Minute)); err != nil || outcome != "requeued" {
		t.Fatalf("recovery=%s err=%v", outcome, err)
	}
	if err := executor.Execute(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM local_image_edit_tasks WHERE id=$1", taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || provider.lastSize == "" {
		t.Fatalf("recovered status=%s provider=%s", status, provider.lastSize)
	}
}

func TestSnapshotReadIOPreservesCause(t *testing.T) {
	cause := errors.New("temporary storage failure")
	for name, translate := range map[string]func(error) error{"source": mapLocalEditSourceRead, "mask": mapLocalEditMaskRead, "reference": mapLocalEditRefRead} {
		t.Run(name, func(t *testing.T) {
			err := translate(&media.ReadError{Kind: media.ReadIO, Detail: "read failed", Err: cause})
			if !errors.Is(err, cause) {
				t.Fatalf("read cause lost: %v", err)
			}
		})
	}
}
