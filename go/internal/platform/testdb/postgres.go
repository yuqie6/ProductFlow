package testdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.NormalizePostgresURL(raw))
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
