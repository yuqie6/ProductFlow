package tx

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// WithGorm runs fn inside one GORM transaction. Nested calls use savepoints;
// callers that already hold a transaction must pass that session instead.
func WithGorm(ctx context.Context, gdb *gorm.DB, fn func(tx *gorm.DB) error) error {
	if gdb == nil {
		return fmt.Errorf("begin: nil gorm db")
	}
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}
