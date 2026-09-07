package schema_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
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

func TestApplyRetiresInviteTableAndCreatesRegistrationChallenges(t *testing.T) {
	pool := testdb.Pool(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS merchant_invites (id varchar(36) PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'registration_challenges'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("registration_challenges missing: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'password_recovery_challenges'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("password_recovery_challenges missing: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'merchant_invites'
	`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("merchant_invites still present: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'uq_registration_challenges_active_email'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("active email unique index missing: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'uq_password_recovery_challenges_active_user'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("active user recovery unique index missing: n=%d err=%v", n, err)
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

func TestIdentityApplyFreshRemovesMembershipsAndFillers(t *testing.T) {
	pool, _ := testdb.IsolatedMigrated(t, identityTestDatabaseName("fresh"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var tableCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'memberships'
	`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatalf("memberships table still present: %d", tableCount)
	}

	var nullable string
	if err := pool.QueryRow(ctx, `
		SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'merchant_id'
	`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if nullable != "YES" {
		t.Fatalf("users.merchant_id nullable=%q, want YES for Operator without a home", nullable)
	}

	for _, name := range []string{
		"fk_users_merchant_id",
		"ck_users_merchant_required_for_ordinary",
		"uq_users_ordinary_merchant_id",
	} {
		if err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_constraint c
			JOIN pg_class r ON r.oid = c.conrelid
			WHERE c.conname = $1
		`, name).Scan(&tableCount); err != nil {
			t.Fatal(err)
		}
		if name == "uq_users_ordinary_merchant_id" {
			if err := pool.QueryRow(ctx, `
				SELECT count(*)
				FROM pg_indexes
				WHERE schemaname = 'public' AND indexname = $1
			`, name).Scan(&tableCount); err != nil {
				t.Fatal(err)
			}
		}
		if tableCount != 1 {
			t.Fatalf("identity object %s missing", name)
		}
	}

	var triggerCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_trigger
		WHERE NOT tgisinternal AND tgname LIKE '%fill_merchant_id%'
	`).Scan(&triggerCount); err != nil {
		t.Fatal(err)
	}
	if triggerCount != 0 {
		t.Fatalf("merchant fill triggers remain: %d", triggerCount)
	}
	var functionCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_proc
		WHERE oid = to_regprocedure('public.productflow_fill_merchant_id()')
	`).Scan(&functionCount); err != nil {
		t.Fatal(err)
	}
	if functionCount != 0 {
		t.Fatalf("merchant fill function remains: %d", functionCount)
	}
}

func TestIdentityApplyRepeatKeepsDirectOwnership(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, identityTestDatabaseName("repeat"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := pool.Exec(ctx, `
		INSERT INTO merchants (id, name, status, created_at, updated_at)
		VALUES ('identity-repeat-merchant', 'repeat merchant', 'active', NOW(), NOW());
		INSERT INTO users (id, email, password_hash, display_name, is_operator, merchant_id, status, created_at, updated_at)
		VALUES ('identity-repeat-user', 'identity-repeat@example.com', 'hash', 'repeat', FALSE, 'identity-repeat-merchant', 'active', NOW(), NOW());
	`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}

	var merchantID string
	if err := pool.QueryRow(ctx, `SELECT merchant_id FROM users WHERE id = 'identity-repeat-user'`).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if merchantID != "identity-repeat-merchant" {
		t.Fatalf("direct ownership changed to %q", merchantID)
	}
	var membershipCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'memberships'
	`).Scan(&membershipCount); err != nil {
		t.Fatal(err)
	}
	if membershipCount != 0 {
		t.Fatalf("memberships table recreated: %d", membershipCount)
	}
}

func TestIdentityApplyConcurrentCallsSerialize(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, identityTestDatabaseName("concurrent"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := pool.Exec(ctx, `ALTER TABLE users DROP COLUMN merchant_id CASCADE`); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- schema.Apply(gdb)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent schema.Apply: %v", err)
		}
	}

	var columnCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'merchant_id'
	`).Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if columnCount != 1 {
		t.Fatalf("users.merchant_id missing after concurrent Apply: %d", columnCount)
	}
}

func TestIdentityApplyMigratesUnambiguousMembershipHistory(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, identityTestDatabaseName("legacy"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prepareLegacyIdentityTables(t, ctx, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO merchants (id, name, status, created_at, updated_at) VALUES
			('identity-legacy-owned', 'legacy owned', 'active', NOW(), NOW()),
			('identity-legacy-home', 'legacy home', 'active', NOW(), NOW()),
			('identity-legacy-other', 'legacy other', 'active', NOW(), NOW()),
			('identity-legacy-revoked-only', 'legacy revoked only', 'active', NOW(), NOW());
		INSERT INTO users (id, email, password_hash, display_name, is_operator, status, created_at, updated_at) VALUES
			('identity-legacy-ordinary', 'identity-legacy-ordinary@example.com', 'hash', 'ordinary', FALSE, 'active', NOW(), NOW()),
			('identity-legacy-operator', 'identity-legacy-operator@example.com', 'hash', 'operator', TRUE, 'active', NOW(), NOW()),
			('identity-legacy-ambiguous', 'identity-legacy-ambiguous@example.com', 'hash', 'ambiguous', TRUE, 'active', NOW(), NOW()),
			('identity-legacy-revoked-only', 'identity-legacy-revoked-only@example.com', 'hash', 'revoked only', TRUE, 'active', NOW(), NOW());
		INSERT INTO memberships (id, merchant_id, user_id, role, status, created_at, updated_at, revoked_at) VALUES
			('identity-membership-ordinary-active', 'identity-legacy-owned', 'identity-legacy-ordinary', 'owner', 'active', NOW(), NOW(), NULL),
			('identity-membership-ordinary-revoked', 'identity-legacy-owned', 'identity-legacy-ordinary', 'owner', 'revoked', NOW(), NOW(), NOW()),
			('identity-membership-operator-home', 'identity-legacy-home', 'identity-legacy-operator', 'viewer', 'active', NOW(), NOW(), NULL),
			('identity-membership-ambiguous-home', 'identity-legacy-home', 'identity-legacy-ambiguous', 'viewer', 'active', NOW(), NOW(), NULL),
			('identity-membership-ambiguous-other', 'identity-legacy-other', 'identity-legacy-ambiguous', 'viewer', 'revoked', NOW(), NOW(), NOW()),
			('identity-membership-revoked-only', 'identity-legacy-revoked-only', 'identity-legacy-revoked-only', 'viewer', 'revoked', NOW(), NOW(), NOW());
	`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `
		SELECT id, merchant_id FROM users
		WHERE id IN ('identity-legacy-ordinary', 'identity-legacy-operator', 'identity-legacy-ambiguous', 'identity-legacy-revoked-only')
		ORDER BY id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]*string{}
	for rows.Next() {
		var id string
		var merchantID *string
		if err := rows.Scan(&id, &merchantID); err != nil {
			t.Fatal(err)
		}
		got[id] = merchantID
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got["identity-legacy-ordinary"] == nil || *got["identity-legacy-ordinary"] != "identity-legacy-owned" {
		t.Fatalf("ordinary ownership %#v", got["identity-legacy-ordinary"])
	}
	if got["identity-legacy-operator"] == nil || *got["identity-legacy-operator"] != "identity-legacy-home" {
		t.Fatalf("operator home %#v", got["identity-legacy-operator"])
	}
	if got["identity-legacy-ambiguous"] != nil {
		t.Fatalf("ambiguous operator unexpectedly mapped: %#v", got["identity-legacy-ambiguous"])
	}
	if got["identity-legacy-revoked-only"] != nil {
		t.Fatalf("revoked-only operator unexpectedly mapped: %#v", got["identity-legacy-revoked-only"])
	}
	assertMembershipsRemoved(t, ctx, pool)
}

func TestIdentityApplyAmbiguityRollsBackAndCanRetry(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, identityTestDatabaseName("rollback"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prepareLegacyIdentityTables(t, ctx, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO merchants (id, name, status, created_at, updated_at)
		VALUES ('identity-rollback-merchant-a', 'rollback A', 'active', NOW(), NOW()),
		       ('identity-rollback-merchant-b', 'rollback B', 'active', NOW(), NOW());
		INSERT INTO users (id, email, password_hash, display_name, is_operator, status, created_at, updated_at)
		VALUES ('identity-rollback-user', 'identity-rollback@example.com', 'hash', 'rollback', FALSE, 'active', NOW(), NOW()),
		       ('identity-rollback-shared-user', 'identity-rollback-shared@example.com', 'hash', 'rollback shared', FALSE, 'active', NOW(), NOW());
		INSERT INTO memberships (id, merchant_id, user_id, role, status, created_at, updated_at)
		VALUES ('identity-rollback-a', 'identity-rollback-merchant-a', 'identity-rollback-user', 'owner', 'active', NOW(), NOW()),
		       ('identity-rollback-b', 'identity-rollback-merchant-b', 'identity-rollback-user', 'owner', 'active', NOW(), NOW()),
		       ('identity-rollback-shared', 'identity-rollback-merchant-a', 'identity-rollback-shared-user', 'owner', 'active', NOW(), NOW());
	`); err != nil {
		t.Fatal(err)
	}

	if err := schema.Apply(gdb); err == nil {
		t.Fatal("ambiguous membership migration unexpectedly succeeded")
	}
	var columnCount, membershipCount, rowCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'merchant_id'
	`).Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if columnCount != 0 {
		t.Fatalf("failed migration left users.merchant_id column: %d", columnCount)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'memberships'
	`).Scan(&membershipCount); err != nil {
		t.Fatal(err)
	}
	if membershipCount != 1 {
		t.Fatalf("failed migration removed memberships: %d", membershipCount)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM memberships`).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 3 {
		t.Fatalf("failed migration changed membership rows: %d", rowCount)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE memberships
		SET merchant_id = 'identity-rollback-merchant-a', status = 'revoked'
		WHERE id = 'identity-rollback-b';
		UPDATE memberships
		SET merchant_id = 'identity-rollback-merchant-b'
		WHERE id = 'identity-rollback-shared';
	`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatalf("retry after repairing history: %v", err)
	}
	var merchantID string
	if err := pool.QueryRow(ctx, `SELECT merchant_id FROM users WHERE id = 'identity-rollback-user'`).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if merchantID != "identity-rollback-merchant-a" {
		t.Fatalf("repaired ownership %q", merchantID)
	}
	if err := pool.QueryRow(ctx, `SELECT merchant_id FROM users WHERE id = 'identity-rollback-shared-user'`).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if merchantID != "identity-rollback-merchant-b" {
		t.Fatalf("repaired shared ownership %q", merchantID)
	}
	assertMembershipsRemoved(t, ctx, pool)
}

func TestApplyRejectsNullRootOwnership(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, identityTestDatabaseName("root_null"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := pool.Exec(ctx, `
		ALTER TABLE products ALTER COLUMN merchant_id DROP NOT NULL;
		INSERT INTO products (id, name, merchant_id, created_at, updated_at)
		VALUES ('identity-root-null-product', 'root null', NULL, NOW(), NOW());
	`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err == nil {
		t.Fatal("NULL root ownership unexpectedly accepted")
	}
	var nullable string
	if err := pool.QueryRow(ctx, `
		SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'products' AND column_name = 'merchant_id'
	`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if nullable != "YES" {
		t.Fatalf("failed root migration changed nullability to %q", nullable)
	}
	var merchantID *string
	if err := pool.QueryRow(ctx, `SELECT merchant_id FROM products WHERE id = 'identity-root-null-product'`).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if merchantID != nil {
		t.Fatalf("NULL root ownership was backfilled to %q", *merchantID)
	}
}

func identityTestDatabaseName(suffix string) string {
	return fmt.Sprintf("pf_identity_%s_%d", suffix, time.Now().UnixNano()%1_000_000_000)
}

func prepareLegacyIdentityTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		ALTER TABLE users DROP COLUMN merchant_id CASCADE;
		CREATE TABLE memberships (
			id varchar(36) PRIMARY KEY,
			merchant_id varchar(36) NOT NULL,
			user_id varchar(36) NOT NULL,
			role varchar(32) NOT NULL,
			status varchar(32) NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			revoked_at timestamptz
		);
	`); err != nil {
		t.Fatal(err)
	}
}

func assertMembershipsRemoved(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'memberships'
	`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("memberships table remains: %d", count)
	}
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

func TestProductsBrandIDPresentAfterApply(t *testing.T) {
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
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'products'
		  AND column_name = 'brand_id'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("products.brand_id missing: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname = 'fk_products_brand_id'
	`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("fk_products_brand_id: n=%d err=%v", n, err)
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
