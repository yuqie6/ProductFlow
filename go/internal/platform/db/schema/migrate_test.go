package schema_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestApplyExistingHeadKeepsSchema(t *testing.T) {
	pool := testdb.Pool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before := schemaFingerprint(t, ctx, pool)
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	after := schemaFingerprint(t, ctx, pool)
	if before != after {
		t.Fatalf("schema fingerprint changed\nbefore %s\nafter  %s", before, after)
	}
}

func TestApplyTwiceDoesNotDeleteRows(t *testing.T) {
	head := testdb.Pool(t)
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	name := fmt.Sprintf("pf_gorm_rows_%d", time.Now().UnixNano()%1_000_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := head.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create empty database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_, _ = head.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	emptyURL, err := rewriteDBName(raw, name)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := pgxpool.New(ctx, emptyURL)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	gdb, err := db.OpenGorm(empty)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Exec(ctx, `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('migrate_probe', '1', NOW(), NOW())
	`); err != nil {
		t.Fatal(err)
	}
	// Exercise AddColumn against a product table from before recipe creation keys.
	if _, err := empty.Exec(ctx, `
		ALTER TABLE products DROP COLUMN creation_idempotency_key CASCADE;
		ALTER TABLE products DROP COLUMN creation_request_hash CASCADE;
		INSERT INTO merchants (id, name, status, created_at, updated_at)
		VALUES ('migrate-merchant', 'probe', 'active', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING;
		INSERT INTO products (id, name, merchant_id, created_at, updated_at)
		VALUES ('recipe-migration-probe', 'existing product', 'migrate-merchant', NOW(), NOW());
	`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := empty.QueryRow(ctx, `SELECT COUNT(*) FROM app_settings WHERE key = 'migrate_probe'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("probe row count %d", n)
	}
	if err := empty.QueryRow(ctx, `SELECT count(*) FROM products WHERE id='recipe-migration-probe' AND creation_idempotency_key IS NULL AND creation_request_hash IS NULL AND merchant_id='migrate-merchant'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("existing product: %d %v", n, err)
	}
	if _, err := empty.Exec(ctx, `UPDATE products SET creation_idempotency_key='key' WHERE id='recipe-migration-probe'`); err == nil {
		t.Fatal("unpaired creation key accepted")
	}
	if _, err := empty.Exec(ctx, `UPDATE products SET creation_idempotency_key='key', creation_request_hash=repeat('a',64) WHERE id='recipe-migration-probe'`); err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Exec(ctx, `INSERT INTO products (id,name,merchant_id,created_at,updated_at,creation_idempotency_key,creation_request_hash) VALUES ('duplicate-recipe-key','duplicate','migrate-merchant',NOW(),NOW(),'key',repeat('a',64))`); err == nil {
		t.Fatal("duplicate creation key accepted")
	}
}

func schemaFingerprint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	cols := columnSet(t, ctx, pool)
	checks := nameSet(t, ctx, pool, `
		SELECT con.conname
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND con.contype = 'c'
	`)
	uniq := nameSet(t, ctx, pool, `
		SELECT indexname FROM pg_indexes
		WHERE schemaname = 'public' AND indexdef LIKE '%UNIQUE%' AND tablename <> 'alembic_version'
	`)
	return "cols=" + joinKeys(cols) + " checks=" + joinKeys(checks) + " uniq=" + joinKeys(uniq)
}

func joinKeys(set map[string]struct{}) string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func TestApplyEmptyDatabaseMatchesHeadConstraints(t *testing.T) {
	head := testdb.Pool(t)
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	name := fmt.Sprintf("pf_gorm_%d", time.Now().UnixNano()%1_000_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := head.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create empty database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_, _ = head.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	emptyURL, err := rewriteDBName(raw, name)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := pgxpool.New(ctx, emptyURL)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	gdb, err := db.OpenGorm(empty)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}

	wantTables := tableSet(t, ctx, head)
	gotTables := tableSet(t, ctx, empty)
	delete(wantTables, "alembic_version")
	delete(gotTables, "alembic_version")
	assertSame(t, "tables", wantTables, gotTables)

	wantCols := columnSet(t, ctx, head)
	gotCols := columnSet(t, ctx, empty)
	assertSame(t, "columns", wantCols, gotCols)

	wantChecks := nameSet(t, ctx, head, `
		SELECT con.conname
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND con.contype = 'c'
	`)
	gotChecks := nameSet(t, ctx, empty, `
		SELECT con.conname
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND con.contype = 'c'
	`)
	assertSame(t, "checks", wantChecks, gotChecks)

	wantUniq := nameSet(t, ctx, head, `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = 'public' AND indexdef LIKE '%UNIQUE%'
		  AND tablename <> 'alembic_version'
	`)
	gotUniq := nameSet(t, ctx, empty, `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = 'public' AND indexdef LIKE '%UNIQUE%'
		  AND tablename <> 'alembic_version'
	`)
	assertSame(t, "unique indexes", wantUniq, gotUniq)

	wantPartial := nameSet(t, ctx, head, `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = 'public' AND indexdef ILIKE '% WHERE %'
	`)
	gotPartial := nameSet(t, ctx, empty, `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = 'public' AND indexdef ILIKE '% WHERE %'
	`)
	assertSame(t, "partial indexes", wantPartial, gotPartial)
}

func tableSet(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]struct{} {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func columnSet(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]struct{} {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT table_name || '.' || column_name || ':' || udt_name || ':' || is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name <> 'alembic_version'
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		out[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func nameSet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, q string) map[string]struct{} {
	t.Helper()
	rows, err := pool.Query(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBrandsTablePresentAfterApply(t *testing.T) {
	pool := testdb.Pool(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'brands'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("brands table missing: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'brands'
		  AND column_name IN ('id', 'merchant_id', 'name', 'visual_system_id', 'created_at', 'updated_at')
	`).Scan(&n); err != nil || n != 6 {
		t.Fatalf("brands columns: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname = 'fk_brands_merchant_id'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("fk_brands_merchant_id: n=%d err=%v", n, err)
	}
}


func TestQuotaPriceCatalogPresentAfterApply(t *testing.T) {
	pool := testdb.Pool(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name IN ('quota_price_versions', 'quota_price_entries')
	`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("price catalog tables missing: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM quota_price_versions
		WHERE id = 'pv-placeholder-v0' AND is_default = TRUE AND currency = 'iu'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("default price version seed missing: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM quota_price_entries
		WHERE price_version_id = 'pv-placeholder-v0' AND unit_price = 1
	`).Scan(&n); err != nil || n != 5 {
		t.Fatalf("default price entries seed: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname = 'fk_quota_price_entries_version_id'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("fk_quota_price_entries_version_id: n=%d err=%v", n, err)
	}
}

func assertSame[T comparable](t *testing.T, label string, want, got map[string]T) {
	t.Helper()
	var missing, extra []string
	for key, wantVal := range want {
		gotVal, ok := got[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		if wantVal != gotVal {
			t.Errorf("%s %s\n  want %v\n  got  %v", label, key, wantVal, gotVal)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			extra = append(extra, key)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("%s mismatch missing=%v extra=%v", label, missing, extra)
	}
}

func rewriteDBName(raw, name string) (string, error) {
	parsed, err := url.Parse(config.NormalizePostgresURL(raw))
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}
