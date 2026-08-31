package db

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// Query 给测试夹具跑多行 SELECT。命令路径必须用 schema 模型（Create / Updates / Take），不要当主写入。
// []string 参数会变成 PostgreSQL text[]，让现有 ANY($n) 夹具能走 database/sql。
// SQL 非法或 ctx 取消时返回 error。
func Query(ctx context.Context, tx *gorm.DB, q string, args ...any) (*sql.Rows, error) {
	return tx.WithContext(ctx).Raw(q, normalizeArgs(args)...).Rows()
}

// QueryRow 给测试夹具跑单行 SELECT。[]string 同样变成 text[]。命令路径禁止调用。
func QueryRow(ctx context.Context, tx *gorm.DB, q string, args ...any) *sql.Row {
	return tx.WithContext(ctx).Raw(q, normalizeArgs(args)...).Row()
}

// Exec 给测试夹具跑非查询语句，返回 RowsAffected。命令路径禁止调用，避免绕过模型和锁子句。
// SQL 非法或 ctx 取消时返回 error。
func Exec(ctx context.Context, tx *gorm.DB, q string, args ...any) (int64, error) {
	res := tx.WithContext(ctx).Exec(q, normalizeArgs(args)...)
	return res.RowsAffected, res.Error
}

func normalizeArgs(args []any) []any {
	out := make([]any, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case []string:
			out[i] = pgtype.FlatArray[string](v)
		default:
			out[i] = arg
		}
	}
	return out
}
