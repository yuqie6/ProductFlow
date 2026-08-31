// Package tx 提供命令路径的 GORM 事务入口。查询或 recovery 可以直接拿会话；写业务必须走这里或已有事务。
package tx

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// WithGorm 在一条 GORM 事务里跑 fn。GORM 对嵌套 Transaction 用 savepoint；
// 调用方若已经持有事务，必须把那条 *gorm.DB 传进来，不要对根会话再开一层以免锁/可见性分叉。
// gdb 为 nil 返回 error，不会 panic。fn 返回的 error 原样冒泡并回滚。
func WithGorm(ctx context.Context, gdb *gorm.DB, fn func(tx *gorm.DB) error) error {
	if gdb == nil {
		return fmt.Errorf("begin: nil gorm db")
	}
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}
