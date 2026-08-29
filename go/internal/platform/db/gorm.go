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

var gormByPool sync.Map

// OpenGorm wraps an existing pgx pool with one GORM handle. Closing GORM's
// sql.DB does not close the pool. Callers share this handle; do not open a
// second pool for GORM.
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
