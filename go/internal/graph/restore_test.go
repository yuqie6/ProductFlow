package graph_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

func TestBackupRestoreThenExecuteGraph(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		t.Skip("psql not installed")
	}
	head := testdb.Pool(t)
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	stamp := time.Now().UnixNano() % 1_000_000_000
	srcName := fmt.Sprintf("pf_bak_src_%d", stamp)
	dstName := fmt.Sprintf("pf_bak_dst_%d", stamp)
	srcURL, srcPool, srcDB := isolatedMigratedDB(t, head, raw, srcName)
	dstURL := isolatedEmptyDB(t, head, raw, dstName)

	root := t.TempDir()
	merchantID := auth.MustDevMerchantID(t, srcDB)
	ctx := auth.WithMerchantID(context.Background(), merchantID)
	created, err := (product.Service{
		DB:    srcDB,
		Media: media.Store{Files: storage.Local{Root: root}},
	}).CreateDirect(ctx, product.CreateInput{
		Name: "备份恢复商品",
		Uploads: []product.Upload{{
			Content:  pngBytes(t),
			Filename: "hero.png",
			MIMEType: "image/png",
		}},
	}, []graph.DirectCreateImageType{{Key: "hero", Quantity: 1, Title: "主图"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	graphID, _ := created.Graph["id"].(string)
	if created.Product.ID == "" || graphID == "" {
		t.Fatalf("direct create %+v", created.Graph)
	}

	dumpRestore(t, srcURL, dstURL)
	srcPool.Close()

	dstPool, err := pgxpool.New(ctx, dstURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dstPool.Close)
	dstDB, err := db.OpenGorm(dstPool)
	if err != nil {
		t.Fatal(err)
	}

	opened, err := (product.Service{DB: dstDB}).Get(ctx, created.Product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Name != "备份恢复商品" {
		t.Fatalf("restored name %q", opened.Name)
	}
	graphs := graph.Service{DB: dstDB, Products: product.GraphGuard{}}
	current, err := graphs.Current(ctx, created.Product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != graphID {
		t.Fatalf("restored graph %s want %s", current.ID, graphID)
	}

	run, err := graphs.SubmitRun(ctx, created.Product.ID, graphID, graph.GraphRunRequest{Scope: "graph"})
	if err != nil {
		t.Fatal(err)
	}
	executor := graph.Executor{
		DB: dstDB,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: dstDB, Media: media.Store{Files: storage.Local{Root: root}}},
		},
		Products: product.GraphGuard{},
	}
	if err := executor.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	finished, err := graphs.GetRun(ctx, created.Product.ID, graphID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "succeeded" {
		reason := ""
		if finished.FailureReason != nil {
			reason = *finished.FailureReason
		}
		t.Fatalf("status %s reason %s nodes %+v", finished.Status, reason, finished.NodeRuns)
	}
}

func isolatedMigratedDB(t *testing.T, head *pgxpool.Pool, raw, name string) (string, *pgxpool.Pool, *gorm.DB) {
	t.Helper()
	dbURL := isolatedEmptyDB(t, head, raw, name)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	return dbURL, pool, gdb
}

func isolatedEmptyDB(t *testing.T, head *pgxpool.Pool, raw, name string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := head.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_, _ = head.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	parsed, err := url.Parse(config.NormalizePostgresURL(raw))
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func dumpRestore(t *testing.T, srcURL, dstURL string) {
	t.Helper()
	dump := exec.Command("pg_dump", "--no-owner", "--no-acl", srcURL)
	restore := exec.Command("psql", "--set", "ON_ERROR_STOP=1", "--quiet", dstURL)
	var dumpErr, restoreErr bytes.Buffer
	dump.Stderr = &dumpErr
	restore.Stderr = &restoreErr
	stdout, err := dump.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	restore.Stdin = stdout
	if err := dump.Start(); err != nil {
		t.Fatal(err)
	}
	if err := restore.Start(); err != nil {
		t.Fatal(err)
	}
	if err := dump.Wait(); err != nil {
		t.Fatalf("pg_dump: %v %s", err, dumpErr.String())
	}
	if err := restore.Wait(); err != nil {
		t.Fatalf("psql restore: %v %s", err, restoreErr.String())
	}
}
