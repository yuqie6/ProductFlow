package queue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type dispatchLatencySample struct {
	ID         string    `json:"id"`
	Dispatcher string    `json:"dispatcher"`
	ClaimedAt  time.Time `json:"claimed_at"`
	SentAt     time.Time `json:"sent_transition_at"`
}

// The probe exists only in a disposable test database. Production sent_at is a
// cycle-start timestamp, so it cannot measure time spent sending a claimed batch.
func installDispatchLatencyProbe(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE dispatch_latency_probe (
			dispatch_id text NOT NULL, phase text NOT NULL, observed_at timestamptz NOT NULL,
			dispatcher text NOT NULL,
			PRIMARY KEY (dispatch_id, phase)
		);
		CREATE FUNCTION record_dispatch_latency() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.status = 'pending' AND OLD.lease_token IS NULL AND NEW.lease_token IS NOT NULL THEN
				INSERT INTO dispatch_latency_probe VALUES (NEW.id, 'claim', clock_timestamp(), current_setting('application_name'));
			ELSIF OLD.status = 'pending' AND NEW.status = 'sent' THEN
				INSERT INTO dispatch_latency_probe VALUES (NEW.id, 'sent', clock_timestamp(), current_setting('application_name'));
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER dispatch_latency_observer AFTER UPDATE ON async_dispatches
		FOR EACH ROW EXECUTE FUNCTION record_dispatch_latency();
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func stageDispatchLatencyLoad(t *testing.T, ctx context.Context, gdb *gorm.DB, ready, deferred int) (map[string]bool, time.Time) {
	t.Helper()
	ids := make(map[string]bool, ready+deferred)
	var releaseAt time.Time
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		later := time.Now().UTC().Add(time.Hour)
		for i := 0; i < ready+deferred; i++ {
			var availableAt *time.Time
			if i >= ready {
				availableAt = &later
			}
			aggregateID := uniqueID(t)
			d, err := queue.Stage(ctx, dbTx, "latency:"+aggregateID, queue.ActorGraphRun, aggregateID, nil, availableAt)
			if err != nil {
				return err
			}
			ids[d.ID] = i < ready
		}
		// Sample on the DB clock immediately before commit: all PENDING rows and
		// notifications become visible together. This includes the commit tail.
		return dbTx.Raw("SELECT clock_timestamp()").Scan(&releaseAt).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	return ids, releaseAt
}

func readDispatchLatencySamples(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []dispatchLatencySample {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT c.dispatch_id, c.dispatcher, c.observed_at, s.observed_at
		FROM dispatch_latency_probe c JOIN dispatch_latency_probe s USING (dispatch_id)
		WHERE c.phase = 'claim' AND s.phase = 'sent'
		ORDER BY c.dispatch_id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var samples []dispatchLatencySample
	for rows.Next() {
		var sample dispatchLatencySample
		if err := rows.Scan(&sample.ID, &sample.Dispatcher, &sample.ClaimedAt, &sample.SentAt); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return samples
}

func dispatchLatencyQuantile(samples []time.Duration, q float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[int(math.Ceil(q*float64(len(ordered))))-1]
}

func TestDispatchLatencyQuantile(t *testing.T) {
	values := make([]time.Duration, 20)
	for i := range values {
		values[i] = time.Duration(20-i) * time.Millisecond
	}
	if got := dispatchLatencyQuantile(values, 0.5); got != 10*time.Millisecond {
		t.Fatalf("p50=%s", got)
	}
	if got := dispatchLatencyQuantile(values, 0.95); got != 19*time.Millisecond {
		t.Fatalf("p95=%s", got)
	}
	if values[0] != 20*time.Millisecond || dispatchLatencyQuantile(nil, 0.95) != 0 {
		t.Fatal("quantile mutated samples or mishandled empty input")
	}
}

func TestDispatchLatencyProbeTracksClaimSentAndDeferred(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_dprobe_%d", time.Now().UnixNano()))
	installDispatchLatencyProbe(t, pool)
	ctx := context.Background()
	ids, releaseAt := stageDispatchLatencyLoad(t, ctx, gdb, 2, 1)
	var mu sync.Mutex
	enqueued := map[string]int{}
	summary, err := queue.RunDispatcherOnce(ctx, pool, func(id, aggregateID string) error {
		var status string
		if err := pool.QueryRow(ctx, "SELECT status FROM async_dispatches WHERE id=$1", id).Scan(&status); err != nil {
			return err
		}
		if status != queue.StatusSent {
			return fmt.Errorf("enqueue before SENT: %s", status)
		}
		mu.Lock()
		enqueued[id]++
		mu.Unlock()
		return nil
	}, queue.DefaultClaimLimit)
	if err != nil || summary.Pending != 2 || summary.Sent != 2 {
		t.Fatalf("dispatch summary=%+v err=%v", summary, err)
	}
	samples := readDispatchLatencySamples(t, ctx, pool)
	if len(samples) != 2 {
		t.Fatalf("sample count=%d", len(samples))
	}
	for _, sample := range samples {
		if !ids[sample.ID] || enqueued[sample.ID] != 1 || sample.ClaimedAt.Before(releaseAt) || sample.SentAt.Before(sample.ClaimedAt) {
			t.Fatalf("invalid transition sample: %+v", sample)
		}
	}
	assertDeferredDispatches(t, ctx, pool, 1)
}

func assertDeferredDispatches(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_dispatches d
		WHERE status='pending' AND attempts=0 AND lease_token IS NULL AND sent_at IS NULL
		AND available_at > clock_timestamp()
		AND NOT EXISTS (SELECT 1 FROM dispatch_latency_probe p WHERE p.dispatch_id=d.id)`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("unclaimed deferred=%d, want %d", count, want)
	}
}

func TestDispatcherPendingSentLatency(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_DISPATCH_LATENCY") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_DISPATCH_LATENCY=1 to measure real dispatcher processes")
	}
	if os.Getenv("SESSION_SECRET") == "" {
		t.Setenv("SESSION_SECRET", "dispatch-latency-test-only")
	}
	bin := buildDispatcher(t)
	for _, replicas := range []int{1, 2} {
		t.Run(fmt.Sprintf("replicas_%d", replicas), func(t *testing.T) {
			const ready, deferred = 500, 25
			name := fmt.Sprintf("pf_dlat_%d", time.Now().UnixNano())
			pool, gdb := testdb.IsolatedMigrated(t, name)
			installDispatchLatencyProbe(t, pool)
			redisURL := startDispatchLatencyRedis(t)
			opt, err := queue.ParseRedis(redisURL)
			if err != nil {
				t.Fatal(err)
			}
			inspector := asynq.NewInspector(opt)
			t.Cleanup(func() { _ = inspector.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			for i := 0; i < replicas; i++ {
				pgURL := rewritePostgresDB(t, config.NormalizePostgresURL(os.Getenv("DATABASE_URL")), name)
				parsed, err := url.Parse(pgURL)
				if err != nil {
					t.Fatal(err)
				}
				appName := fmt.Sprintf("dispatch_latency_%d", i)
				query := parsed.Query()
				query.Set("application_name", appName)
				parsed.RawQuery = query.Encode()
				storage := t.TempDir()
				proc := startDispatcher(t, bin, parsed.String(), redisURL, storage, "127.0.0.1:0", filepath.Join(storage, "dispatcher.log"),
					"--interval", "1", "--recovery-interval", "10", "--limit", "100")
				t.Cleanup(func() {
					_ = proc.Process.Kill()
					_ = proc.Wait()
					if t.Failed() {
						body, _ := os.ReadFile(filepath.Join(storage, "dispatcher.log"))
						t.Logf("dispatcher %d: %s", i, body)
					}
				})
				waitDispatchLatencyListener(t, ctx, pool, appName)
			}
			ids, releaseAt := stageDispatchLatencyLoad(t, ctx, gdb, ready, deferred)
			var samples []dispatchLatencySample
			for {
				samples = readDispatchLatencySamples(t, ctx, pool)
				if len(samples) == ready {
					break
				}
				if ctx.Err() != nil {
					t.Fatalf("only %d/%d SENT samples: %v", len(samples), ready, ctx.Err())
				}
				time.Sleep(10 * time.Millisecond)
			}
			// SENT precedes enqueue, so separately wait for and inspect actual Redis envelopes.
			for {
				info, err := inspector.GetQueueInfo("default")
				if err == nil && info.Pending == ready {
					break
				}
				if ctx.Err() != nil {
					t.Fatalf("Redis envelopes incomplete: info=%+v err=%v", info, err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			envelopes, err := inspector.ListPendingTasks("default", asynq.PageSize(ready))
			if err != nil || len(envelopes) != ready {
				t.Fatalf("Redis envelope count=%d err=%v", len(envelopes), err)
			}
			seen := map[string]bool{}
			for _, envelope := range envelopes {
				var payload queue.TaskPayload
				if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if !ids[payload.DispatchID] || seen[payload.DispatchID] || envelope.MaxRetry != 0 {
					t.Fatalf("unexpected/duplicate/retryable envelope: %+v", envelope)
				}
				seen[payload.DispatchID] = true
			}
			assertDeferredDispatches(t, ctx, pool, deferred)
			var invalid int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_dispatches
				WHERE status='sent' AND (attempts<>1 OR last_error IS NOT NULL)`).Scan(&invalid); err != nil || invalid != 0 {
				t.Fatalf("invalid SENT rows=%d err=%v", invalid, err)
			}
			pendingToSent := make([]time.Duration, 0, ready)
			claimToSent := make([]time.Duration, 0, ready)
			claimsByReplica := map[string]int{}
			for _, sample := range samples {
				if !seen[sample.ID] || sample.ClaimedAt.Before(releaseAt) || sample.SentAt.Before(sample.ClaimedAt) {
					t.Fatalf("invalid sample: %+v", sample)
				}
				pendingToSent = append(pendingToSent, sample.SentAt.Sub(releaseAt))
				claimToSent = append(claimToSent, sample.SentAt.Sub(sample.ClaimedAt))
				claimsByReplica[sample.Dispatcher]++
			}
			if len(claimsByReplica) != replicas {
				t.Fatalf("expected all %d dispatchers to participate: %v", replicas, claimsByReplica)
			}
			t.Logf("DISPATCH_LATENCY_SUMMARY replicas=%d ready=%d deferred_unclaimed=%d interval=1s recovery=10s limit=100 pending_sent_p50=%s pending_sent_p95=%s claim_sent_p50=%s claim_sent_p95=%s target_p95_lt_1s=%t claims_by_replica=%v",
				replicas, ready, deferred, dispatchLatencyQuantile(pendingToSent, .5), dispatchLatencyQuantile(pendingToSent, .95),
				dispatchLatencyQuantile(claimToSent, .5), dispatchLatencyQuantile(claimToSent, .95), dispatchLatencyQuantile(pendingToSent, .95) < time.Second, claimsByReplica)
			raw, err := json.Marshal(struct {
				Replicas int                     `json:"replicas"`
				Release  time.Time               `json:"release_before_commit"`
				Samples  []dispatchLatencySample `json:"samples"`
			}{replicas, releaseAt, samples})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("DISPATCH_LATENCY_SAMPLES %s", raw)
		})
	}
}

func waitDispatchLatencyListener(t *testing.T, ctx context.Context, pool *pgxpool.Pool, appName string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname=current_database() AND application_name=$1 AND query=$2`, appName, "LISTEN "+notify.ChannelDispatch).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("dispatcher %s did not establish LISTEN", appName)
}

func startDispatchLatencyRedis(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("redis-server")
	if err != nil {
		t.Fatal("redis-server is required for the opt-in isolated latency test")
	}
	// Unix socket paths have a short OS limit; testing.T.TempDir names can exceed it.
	dir, err := os.MkdirTemp("", "pf-dlat-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "redis.sock")
	cmd := exec.Command(bin, "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no", "--dir", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	redisURL := (&url.URL{Scheme: "redis-socket", Path: socket}).String()
	opt, err := queue.ParseRedis(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := asynq.NewClient(opt)
	defer client.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if client.Ping() == nil {
			return redisURL
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("isolated redis-server did not become ready")
	return ""
}
