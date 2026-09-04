// Command productflow-agent-evals 只读聚合生产 Agent 行，或导出脱敏任务骨架。
//
// 需要 DATABASE_URL。不写业务表，不把原始转录提交进仓库。结果落 STORAGE_ROOT/agent-evals/。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "mine":
		os.Exit(runMine(os.Args[2:]))
	case "export":
		os.Exit(runExport(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  productflow-agent-evals mine --days 7
  productflow-agent-evals export --turn <turn_projection_id>`)
}

func runMine(args []string) int {
	days := 7
	for i := 0; i < len(args); i++ {
		if args[i] == "--days" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--days requires a value")
				return 2
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				fmt.Fprintln(os.Stderr, "--days must be a positive integer")
				return 2
			}
			days = n
			i++
		}
	}
	pool, closer, err := connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	defer closer()
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	report, err := agent.MineEvalWindow(ctx, pool, days)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mine: %v\n", err)
		return 1
	}
	path, err := agent.WriteEvalMineReport(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write mine report: %v\n", err)
		return 1
	}
	raw, _ := json.MarshalIndent(report, "", "  ")
	fmt.Printf("%s\n", raw)
	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	return 0
}

func runExport(args []string) int {
	var turnID string
	for i := 0; i < len(args); i++ {
		if args[i] == "--turn" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--turn requires a value")
				return 2
			}
			turnID = args[i+1]
			i++
		}
	}
	if turnID == "" {
		fmt.Fprintln(os.Stderr, "export --turn <id> is required")
		return 2
	}
	pool, closer, err := connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	defer closer()
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	export, err := agent.ExportEvalTurn(ctx, pool, turnID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export: %v\n", err)
		return 1
	}
	path, err := agent.WriteEvalTurnExport(export)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write export: %v\n", err)
		return 1
	}
	raw, _ := json.MarshalIndent(export, "", "  ")
	fmt.Printf("%s\n", raw)
	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	return 0
}

func connect() (*pgxpool.Pool, func(), error) {
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		return nil, nil, fmt.Errorf("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := db.Connect(ctx, config.NormalizePostgresURL(raw))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("postgres: %w", err)
	}
	return pool, func() {
		pool.Close()
		cancel()
	}, nil
}
