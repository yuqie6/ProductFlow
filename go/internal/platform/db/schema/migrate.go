package schema

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Apply creates or upgrades the current ProductFlow schema.
// Missing tables and columns are created from GORM models. ExtraDDL owns
// PostgreSQL enums, CHECKs, unique constraints, foreign keys, and indexes.
// AutoMigrate is not used: it rewrites unique index names on an existing head.
// This function does not drop retired tables or columns.
func Apply(gdb *gorm.DB) error {
	db := gdb.Session(&gorm.Session{
		Logger: logger.Default.LogMode(logger.Silent),
	})

	if err := applyPrefix(db, EnumDDL); err != nil {
		return fmt.Errorf("create enums: %w", err)
	}
	if err := createMissing(db); err != nil {
		return fmt.Errorf("tables: %w", err)
	}
	if err := applyPrefix(db, ExtraDDL); err != nil {
		return fmt.Errorf("constraints: %w", err)
	}
	return nil
}

func createMissing(db *gorm.DB) error {
	migrator := db.Migrator()
	for _, model := range AllModels() {
		if !migrator.HasTable(model) {
			if err := migrator.CreateTable(model); err != nil && !alreadyExists(err) {
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
			if err := migrator.AddColumn(model, field.Name); err != nil && !alreadyExists(err) {
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

func alreadyExists(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "42P07" || pgErr.Code == "42701") {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists")
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
