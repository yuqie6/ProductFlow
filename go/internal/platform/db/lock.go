package db

import "gorm.io/gorm/clause"

// ForUpdate is PostgreSQL FOR UPDATE on the query's primary table.
func ForUpdate() clause.Expression {
	return clause.Locking{Strength: "UPDATE"}
}

// ForUpdateOf is PostgreSQL FOR UPDATE OF table (join 时只锁指定别名).
func ForUpdateOf(table string) clause.Expression {
	return clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: table}}
}

func ForUpdateOfSkipLocked(table string) clause.Expression {
	return clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: table}, Options: "SKIP LOCKED"}
}

// SkipLocked is FOR UPDATE SKIP LOCKED，给 dispatcher claim 用.
func SkipLocked() clause.Expression {
	return clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}
}
