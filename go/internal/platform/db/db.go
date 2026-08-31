// Package db 提供 PostgreSQL 连接池与 GORM 句柄。
// 命令路径用 schema 模型加本包 lock 子句写入，禁止把 Query/Exec 裸 SQL 当主写入；池留给 healthz 与 recovery。
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect 解析 DATABASE_URL 建 pgx 池（MaxConns=16，MinConns=0）。Ping 失败会 Close 再返回 error，避免把坏池交给调用方。
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 0
	cfg.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
