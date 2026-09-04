// Package testdb 把测试接到隔离库 <dbname>_gotest_<package>，绝不写 just-dev 的 productflow_dev。
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
	// 会话级 advisory lock，避免并行 go test ./... 抢 CreateTable。
	gotestMigrateLock int64 = 712450011
)

var (
	ensureMu   sync.Mutex
	ensuredURL string
)

// Pool 连到从 DATABASE_URL 派生的隔离测试库。不用线上 just-dev（productflow_dev）：
// 夹具写在 <dbname>_gotest_<package>，避免跑着的 API/dispatcher 回收测试数据，
// 也避免别的包留下的 async_dispatches 抢走 dispatcher claim。
// DATABASE_URL 未设或 Postgres 不可达会 Skip，不是 Fatal。
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

// Open 返回隔离库的 pgx 池和同一组连接上的 GORM。OpenGorm 失败 Fatal，因为后面写库已经假定句柄可用。
func Open(t *testing.T) (*pgxpool.Pool, *gorm.DB) {
	t.Helper()
	pool := Pool(t)
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatalf("gorm: %v", err)
	}
	return pool, gdb
}

// Gorm 只返回 [Open] 的 GORM 句柄，给不需要直接用池的测试。
func Gorm(t *testing.T) *gorm.DB {
	t.Helper()
	_, gdb := Open(t)
	return gdb
}

// ensureTestDatabase 在 advisory lock 下 CREATE DATABASE（已存在忽略 42P04）并 schema.Apply。
// 进程内只做一次；库名来自测试二进制，所以不同包不会共用一张表。
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

// testBinaryIdent 把 os.Args[0] 收成安全 ident，用作 _gotest_<ident> 后缀。空结果回落到 pkg。
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

// IsolatedMigrated 新建一次性已迁移库，测完 DROP DATABASE。name 必须是安全 ident。
// 给目标规模 query plan 等不能污染包级 gotest 库的闸门用。CREATE DATABASE 失败则 Skip。
func IsolatedMigrated(t *testing.T, name string) (*pgxpool.Pool, *gorm.DB) {
	t.Helper()
	if !safeIdent(name) {
		t.Fatalf("unsafe isolated database name %q", name)
	}
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	head := Pool(t)
	normalized := config.NormalizePostgresURL(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := head.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_, _ = head.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	dbURL, err := rewriteDatabase(normalized, name)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	return pool, gdb
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
