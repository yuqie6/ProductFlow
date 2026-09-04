package queue_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const stagingFieldRedisDB = 14

// TestReplicaFieldDispatcherSIGKILLSurvivor 起两个真实 dispatcher --watch 进程，
// SIGKILL 副本 A 后，副本 B 仍能把新的 PENDING 标 SENT。
func TestReplicaFieldDispatcherSIGKILLSurvivor(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_STAGING_FIELD") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_STAGING_FIELD=1 to spawn two dispatcher processes and SIGKILL one")
	}
	if os.Getenv("REDIS_URL") == "" {
		t.Skip("REDIS_URL not set")
	}
	if os.Getenv("SESSION_SECRET") == "" {
		t.Skip("SESSION_SECRET not set")
	}

	name := fmt.Sprintf("pf_drepl_%d", time.Now().UnixNano()%1_000_000_000)
	_, gdb := testdb.IsolatedMigrated(t, name)
	pgURL := rewritePostgresDB(t, config.NormalizePostgresURL(os.Getenv("DATABASE_URL")), name)
	redisURL := rewriteRedisDB(t, os.Getenv("REDIS_URL"), stagingFieldRedisDB)
	bin := buildDispatcher(t)
	storage := t.TempDir()

	logDir := filepath.Join(storage, "dispatcher-logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			dumpDispatcherLogs(t, logDir)
		}
	})
	procA := startDispatcher(t, bin, pgURL, redisURL, storage, "127.0.0.1:39495", filepath.Join(logDir, "a.log"))
	procB := startDispatcher(t, bin, pgURL, redisURL, storage, "127.0.0.1:39496", filepath.Join(logDir, "b.log"))
	t.Cleanup(func() {
		_ = procA.Process.Kill()
		_ = procB.Process.Kill()
		_, _ = procA.Process.Wait()
		_, _ = procB.Process.Wait()
	})
	waitDispatcherAlive(t, procA, 3*time.Second)
	waitDispatcherAlive(t, procB, 3*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	first := stagePending(t, ctx, gdb, "sigkill-first")
	waitDispatchSent(t, ctx, gdb, first)
	if err := procA.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := procA.Process.Wait(); err != nil && !isWaitExitError(err) {
		t.Fatalf("wait A: %v", err)
	}

	second := stagePending(t, ctx, gdb, "sigkill-second")
	waitDispatchSent(t, ctx, gdb, second)
}

func dumpDispatcherLogs(t *testing.T, logDir string) {
	t.Helper()
	for _, name := range []string{"a.log", "b.log"} {
		body, err := os.ReadFile(filepath.Join(logDir, name))
		if err != nil {
			t.Logf("%s: %v", name, err)
			continue
		}
		t.Logf("%s:\n%s", name, body)
	}
}

func buildDispatcher(t *testing.T) string {
	t.Helper()
	mod := strings.TrimSpace(string(mustOutput(t, exec.Command("go", "env", "GOMOD"))))
	root := filepath.Dir(mod)
	bin := filepath.Join(t.TempDir(), "productflow-dispatcher")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/productflow-dispatcher")
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build dispatcher: %v\n%s", err, out)
	}
	return bin
}

func startDispatcher(t *testing.T, bin, pgURL, redisURL, storage, metricsAddr, logPath string) *exec.Cmd {
	t.Helper()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	cmd := exec.Command(bin, "--watch", "--interval", "0.2", "--recovery-interval", "60")
	cmd.Env = append(os.Environ(),
		"DATABASE_URL="+pgURL,
		"REDIS_URL="+redisURL,
		"STORAGE_ROOT="+storage,
		"LOG_DIR="+filepath.Join(storage, "logs"),
		"LOG_LEVEL=ERROR",
		"DISPATCHER_METRICS_ADDR="+metricsAddr,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func waitDispatcherAlive(t *testing.T, cmd *exec.Cmd, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
			time.Sleep(400 * time.Millisecond)
			if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("dispatcher pid %d exited before becoming ready", cmd.Process.Pid)
}

func stagePending(t *testing.T, ctx context.Context, gdb *gorm.DB, label string) string {
	t.Helper()
	agg := uniqueID(t)
	var id string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		d, err := queue.Stage(ctx, pgxTx, "replica-kill:"+label+":"+agg, queue.ActorGraphRun, agg, nil, nil)
		if err != nil {
			return err
		}
		id = d.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func waitDispatchSent(t *testing.T, ctx context.Context, gdb *gorm.DB, id string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		var status string
		if err := gdb.WithContext(ctx).Raw(`SELECT status FROM async_dispatches WHERE id = ?`, id).Scan(&status).Error; err != nil {
			t.Fatal(err)
		}
		if status == queue.StatusSent {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("dispatch %s did not become SENT", id)
}

func rewritePostgresDB(t *testing.T, raw, name string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func rewriteRedisDB(t *testing.T, raw string, db int) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = fmt.Sprintf("/%d", db)
	return parsed.String()
}

func mustOutput(t *testing.T, cmd *exec.Cmd) []byte {
	t.Helper()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func isWaitExitError(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}
