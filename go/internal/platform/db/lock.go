package db

import "gorm.io/gorm/clause"

// ForUpdate 是 PostgreSQL FOR UPDATE，锁查询主表整行，给命令路径「先锁再改」用。
func ForUpdate() clause.Expression {
	return clause.Locking{Strength: "UPDATE"}
}

// ForUpdateOf 是 FOR UPDATE OF table。join 时只锁指定别名，避免把关联表一起锁死。
func ForUpdateOf(table string) clause.Expression {
	return clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: table}}
}

// ForUpdateOfSkipLocked 是 FOR UPDATE OF table SKIP LOCKED。多 dispatcher 并发 claim 时跳过已锁行，不互相等待。
func ForUpdateOfSkipLocked(table string) clause.Expression {
	return clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: table}, Options: "SKIP LOCKED"}
}

// SkipLocked 是 FOR UPDATE SKIP LOCKED，锁主表且跳过已被别人锁的行。用于允许跳过其它事务已锁定行的批量领取。
func SkipLocked() clause.Expression {
	return clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}
}
