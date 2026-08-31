// Command productflow-migrate 按 schema 权威升级 PostgreSQL：CreateTable/AddColumn 加 ExtraDDL，不用 AutoMigrate。
//
// 需要 DATABASE_URL。不会删退役表/列。跑完会回填 graph document_origin。开发环境用 `just go-migrate`。
// 改表先改 go/internal/platform/db/schema 模型与 ExtraDDL，再跑本命令；不要手写 AutoMigrate。
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func main() {
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.Connect(ctx, config.NormalizePostgresURL(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gorm: %v\n", err)
		os.Exit(1)
	}
	if err := schema.Apply(gdb); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	if err := graph.BackfillDocumentOrigin(gdb); err != nil {
		fmt.Fprintf(os.Stderr, "document_origin: %v\n", err)
		os.Exit(1)
	}
}
