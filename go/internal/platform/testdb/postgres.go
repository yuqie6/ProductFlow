package testdb

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const (
	testDBSuffix = "_gotest"
	// Session lock so parallel `go test ./...` processes do not race CreateTable.
	gotestMigrateLock int64 = 712450011
)

var (
	ensureMu   sync.Mutex
	ensuredURL string
)

// Pool returns a connection to an isolated test database derived from
// DATABASE_URL. The live just-dev database (productflow_dev) is never used:
// tests write to <dbname>_gotest_<package> so fixtures cannot be recovered
// by the running API/dispatcher, and leftover async_dispatches from another
// package cannot steal dispatcher claims.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	testURL := ensureTestDatabase(t, raw)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Open returns the live test pool and a GORM handle on the same connections.
func Open(t *testing.T) (*pgxpool.Pool, *gorm.DB) {
	t.Helper()
	pool := Pool(t)
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatalf("gorm: %v", err)
	}
	return pool, gdb
}

func Gorm(t *testing.T) *gorm.DB {
	t.Helper()
	_, gdb := Open(t)
	return gdb
}

func ensureTestDatabase(t *testing.T, raw string) string {
	t.Helper()
	ensureMu.Lock()
	defer ensureMu.Unlock()
	if ensuredURL != "" {
		return ensuredURL
	}
	normalized := config.NormalizePostgresURL(raw)
	liveName, err := databaseName(normalized)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	testName := isolatedName(liveName)
	if !safeIdent(testName) {
		t.Fatalf("unsafe test database name %q", testName)
	}
	testURL, err := rewriteDatabase(normalized, testName)
	if err != nil {
		t.Fatalf("rewrite DATABASE_URL: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, normalized)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(ctx); err != nil {
		t.Skipf("postgres ping: %v", err)
	}
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatalf("advisory lock connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, gotestMigrateLock); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, gotestMigrateLock)
	}()
	if testName != liveName {
		if _, err := conn.Exec(ctx, `CREATE DATABASE `+testName); err != nil {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42P04" {
				t.Fatalf("cannot create test database %s: %v", testName, err)
			}
		}
	}
	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatalf("gorm: %v", err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	ensuredURL = testURL
	return testURL
}

func isolatedName(liveName string) string {
	suffix := testDBSuffix + "_" + testBinaryIdent()
	if strings.HasSuffix(liveName, suffix) {
		return liveName
	}
	return liveName + suffix
}

func testBinaryIdent() string {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".test")
	var b strings.Builder
	for i, r := range base {
		switch {
		case r >= 'a' && r <= 'z' || r == '_':
			b.WriteRune(r)
		case i > 0 && r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '.':
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "pkg"
	}
	return b.String()
}

func databaseName(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if name == "" {
		return "", errors.New("DATABASE_URL has no database name")
	}
	return name, nil
}

func rewriteDatabase(raw, name string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

func safeIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
