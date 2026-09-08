package schema_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestApplyAddsAccountPreferenceColumnsAndDefaults(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_prefs_0908_schema_%d", time.Now().UnixNano()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := pool.Exec(ctx, `
		ALTER TABLE users DROP COLUMN locale;
		ALTER TABLE users DROP COLUMN theme;
		INSERT INTO merchants (id, name, status, created_at, updated_at)
		VALUES ('prefs-schema-merchant', '迁移测试商家', 'active', NOW(), NOW());
		INSERT INTO users (id, email, password_hash, display_name, is_operator, merchant_id, status, created_at, updated_at)
		VALUES ('prefs-schema-user', 'prefs-schema@example.test', 'hash', '迁移测试用户', FALSE, 'prefs-schema-merchant', 'active', NOW(), NOW());
	`); err != nil {
		t.Fatal(err)
	}

	if err := schema.Apply(gdb); err != nil {
		t.Fatalf("re-apply schema with missing preference columns: %v", err)
	}

	var locale, theme string
	if err := pool.QueryRow(ctx, `SELECT locale, theme FROM users WHERE id = 'prefs-schema-user'`).Scan(&locale, &theme); err != nil {
		t.Fatal(err)
	}
	if locale != "zh-CN" || theme != "system" {
		t.Fatalf("migration defaults locale=%q theme=%q", locale, theme)
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET locale = 'fr-FR' WHERE id = 'prefs-schema-user'`); err == nil {
		t.Fatal("invalid locale accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET theme = 'sepia' WHERE id = 'prefs-schema-user'`); err == nil {
		t.Fatal("invalid theme accepted")
	}
	var constraintCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'public.users'::regclass
		  AND conname IN ('ck_users_locale', 'ck_users_theme')
	`).Scan(&constraintCount); err != nil {
		t.Fatal(err)
	}
	if constraintCount != 2 {
		t.Fatalf("preference constraints=%d", constraintCount)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET locale = 'en-US', theme = 'dark' WHERE id = 'prefs-schema-user'`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatalf("re-apply schema changed saved preferences: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT locale, theme FROM users WHERE id = 'prefs-schema-user'`).Scan(&locale, &theme); err != nil {
		t.Fatal(err)
	}
	if locale != "en-US" || theme != "dark" {
		t.Fatalf("saved preferences changed after migration locale=%q theme=%q", locale, theme)
	}
}
