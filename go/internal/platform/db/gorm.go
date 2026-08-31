package db

import (
	"errors"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errNilPool = errors.New("gorm: nil postgres pool")

// gormByPool 按 pgx 池缓存一条 GORM 句柄，保证 OpenGorm 幂等，避免双池。
var gormByPool sync.Map

// OpenGorm 把已有 pgx 池包成一条 GORM 句柄。关 GORM 的 sql.DB 不会关池；调用方必须共用这条句柄。
// pool 为 nil 返回 error。并发两次 OpenGorm 只保留 Map 里先存进去的那条，后打开的 sql.DB 会关掉。
// PreferSimpleProtocol=true、SkipDefaultTransaction=true：命令事务由 [tx.WithGorm] 显式开。
func OpenGorm(pool *pgxpool.Pool) (*gorm.DB, error) {
	if pool == nil {
		return nil, errNilPool
	}
	if existing, ok := gormByPool.Load(pool); ok {
		return existing.(*gorm.DB), nil
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	gdb, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
		SkipDefaultTransaction:                   true,
		PrepareStmt:                              false,
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	actual, loaded := gormByPool.LoadOrStore(pool, gdb)
	if loaded {
		_ = sqlDB.Close()
		return actual.(*gorm.DB), nil
	}
	return gdb, nil
}
