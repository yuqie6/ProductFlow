package schema

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const schemaMigrationAdvisoryLock int64 = 712450012

// Apply 按当前模型把头库建齐或补列。先 EnumDDL，再 CreateTable/AddColumn，最后 ExtraDDL。
// 禁止 AutoMigrate：它会改已有库上的唯一索引名。身份收敛所需的旧表清理也在此事务内完成；
// 事务级 advisory lock 将迁移串行化，避免两个进程同时探测缺表/缺列。
//
// EnumDDL、建表、补列或 ExtraDDL 失败会回滚并 wrap 返回。
func Apply(gdb *gorm.DB) error {
	db := gdb.Session(&gorm.Session{
		Logger: logger.Default.LogMode(logger.Silent),
	})

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", schemaMigrationAdvisoryLock).Error; err != nil {
			return fmt.Errorf("migration lock: %w", err)
		}
		if err := applyPrefix(tx, EnumDDL); err != nil {
			return fmt.Errorf("create enums: %w", err)
		}
		if err := createMissing(tx); err != nil {
			return fmt.Errorf("tables: %w", err)
		}
		if err := applyPrefix(tx, ExtraDDL); err != nil {
			return fmt.Errorf("constraints: %w", err)
		}
		return nil
	})
}

// createMissing 缺表则 CreateTable，已有表只 AddColumn。Apply 已在事务级 advisory lock 下串行执行。
func createMissing(db *gorm.DB) error {
	migrator := db.Migrator()
	for _, model := range AllModels() {
		if !migrator.HasTable(model) {
			if err := migrator.CreateTable(model); err != nil {
				return err
			}
			continue
		}
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		for _, field := range stmt.Schema.Fields {
			if skipColumn(field) {
				continue
			}
			if migrator.HasColumn(model, field.DBName) {
				continue
			}
			if err := migrator.AddColumn(model, field.Name); err != nil {
				return fmt.Errorf("%s.%s: %w", stmt.Schema.Table, field.DBName, err)
			}
		}
	}
	return nil
}

func skipColumn(field *schema.Field) bool {
	if field == nil || field.DBName == "" {
		return true
	}
	if field.IgnoreMigration {
		return true
	}
	return false
}

func applyPrefix(db *gorm.DB, stmts []string) error {
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("%s: %w", trimSQL(stmt), err)
		}
	}
	return nil
}

func trimSQL(stmt string) string {
	line := strings.TrimSpace(strings.ReplaceAll(stmt, "\n", " "))
	if len(line) > 120 {
		return line[:120] + "…"
	}
	return line
}
