package db

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// Query, QueryRow, and Exec are test-fixture helpers. Command paths must use
// schema models (Create / Updates / Take). []string args become PostgreSQL
// text[] so existing ANY($n) fixtures keep working through stdlib.
func Query(ctx context.Context, tx *gorm.DB, q string, args ...any) (*sql.Rows, error) {
	return tx.WithContext(ctx).Raw(q, normalizeArgs(args)...).Rows()
}

func QueryRow(ctx context.Context, tx *gorm.DB, q string, args ...any) *sql.Row {
	return tx.WithContext(ctx).Raw(q, normalizeArgs(args)...).Row()
}

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
