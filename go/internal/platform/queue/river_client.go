package queue

// This file owns the River adapter used by both command paths and workers.
// Business rows remain the source of truth for execution state; River only
// stores a typed, durable invocation and its infrastructure lifecycle.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
	"github.com/yuqie6/productflow/internal/auth"
	"gorm.io/gorm"
)

const (
	// RiverTaskKind is intentionally stable across deploys. Existing jobs are
	// decoded by kind from the persisted river_job row.
	RiverTaskKind = "productflow_task"

	// RiverTaskTimeout bounds a normal handler invocation. Business recovery owns
	// long-running provider ambiguity; a River timeout must not turn an effect
	// into a safe retry.
	RiverTaskTimeout = 30 * time.Minute

	// RescueStuckJobsAfter is deliberately longer than the provider timeout.
	// It only controls River's infrastructure rescue window and is not a
	// substitute for a business stale-running policy.
	RescueStuckJobsAfter = 35 * time.Minute

	// RiverTaskMaxAttempts bounds infrastructure retries while leaving room for
	// transient database or provider transport failures to recover. Business
	// terminal/unknown outcomes are persisted by the executor and return nil.
	RiverTaskMaxAttempts = 10
)

// TaskArgs is the immutable business identity carried by every River job.
// ExecutionID is assigned by the business record on initial submit and by an
// explicit user retry. Automatic snooze/recovery keeps it unchanged. Payload
// is diagnostic/business input only and is excluded from uniqueness.
type TaskArgs struct {
	Actor       string          `json:"actor" river:"unique"`
	MerchantID  string          `json:"merchant_id" river:"unique"`
	AggregateID string          `json:"aggregate_id" river:"unique"`
	ExecutionID string          `json:"execution_id" river:"unique"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

func (TaskArgs) Kind() string { return RiverTaskKind }

type taskContextKey struct{}

// TaskArgsFromContext returns the immutable queue identity for a business
// executor. A missing value means a direct unit invocation rather than a River
// invocation and is intentionally accepted by existing executor tests.
func TaskArgsFromContext(ctx context.Context) (TaskArgs, bool) {
	args, ok := ctx.Value(taskContextKey{}).(TaskArgs)
	return args, ok
}

func withTaskArgs(ctx context.Context, args TaskArgs) context.Context {
	return context.WithValue(ctx, taskContextKey{}, args)
}

// Producer is a River insert-only client. It has no worker queues and uses
// InsertTx exclusively, which makes queue acceptance atomic with the caller's
// GORM transaction.
type Producer struct {
	client *river.Client[*sql.Tx]
}

func NewProducer() (*Producer, error) {
	client, err := river.NewClient(riverdatabasesql.New(nil), &river.Config{
		MaxAttempts:                 RiverTaskMaxAttempts,
		DiscardedJobRetentionPeriod: time.Duration(-1),
		CancelledJobRetentionPeriod: time.Duration(-1),
		Logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return nil, err
	}
	return &Producer{client: client}, nil
}

var (
	producerOnce sync.Once
	producer     *Producer
	producerErr  error
)

func defaultProducer() (*Producer, error) {
	producerOnce.Do(func() { producer, producerErr = NewProducer() })
	return producer, producerErr
}

// RiverJob is the small receipt returned after a River insert. It intentionally
// contains no broker/status mirror; callers use River's job id for observability
// and the business row for terminal state.
type RiverJob struct {
	ID    int64
	Kind  string
	Queue string
	Args  TaskArgs
	State rivertype.JobState
}

func riverJobFromResult(res *rivertype.JobInsertResult, args TaskArgs) RiverJob {
	if res == nil || res.Job == nil {
		return RiverJob{Kind: args.Kind(), Args: args}
	}
	return RiverJob{ID: res.Job.ID, Kind: res.Job.Kind, Queue: res.Job.Queue, Args: args, State: res.Job.State}
}

// riverTx extracts the database/sql transaction GORM is already using. Queue
// submission outside an explicit transaction is rejected because it would
// violate the business-row and job atomicity contract.
func riverTx(tx *gorm.DB) (*sql.Tx, error) {
	if tx == nil || tx.Statement == nil {
		return nil, errors.New("queue: nil gorm transaction")
	}
	sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx)
	if wrapped, wrapsSQL := tx.Statement.ConnPool.(interface{ SQLTx() *sql.Tx }); wrapsSQL {
		sqlTx = wrapped.SQLTx()
		ok = sqlTx != nil
	}
	if !ok || sqlTx == nil {
		return nil, fmt.Errorf("queue: River submission requires a GORM sql.Tx (got %T)", tx.Statement.ConnPool)
	}
	return sqlTx, nil
}

// insertRiverJob inserts the job into the caller-owned transaction. The
// unique states include completed jobs. Recovery may retry the same invocation
// for unfinished business; explicit user retries obtain a new execution identity.
func insertRiverJob(ctx context.Context, tx *gorm.DB, args TaskArgs, scheduledAt *time.Time) (RiverJob, error) {
	sqlTx, err := riverTx(tx)
	if err != nil {
		return RiverJob{}, err
	}
	p, err := defaultProducer()
	if err != nil {
		return RiverJob{}, fmt.Errorf("queue producer: %w", err)
	}
	opts := &river.InsertOpts{
		Queue:      queueForActor(args.Actor),
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: retainedJobStates},
	}
	if scheduledAt != nil {
		opts.ScheduledAt = scheduledAt.UTC()
	}
	res, err := p.client.InsertTx(ctx, sqlTx, args, opts)
	if err != nil {
		return RiverJob{}, err
	}
	if res.Job.State == rivertype.JobStateCompleted {
		// Recovery can repair an unfinished business projection after an invocation
		// completed. Reuse River's native retry transition, preserving business epoch.
		retried, err := p.client.JobRetryTx(ctx, sqlTx, res.Job.ID)
		if err != nil {
			return RiverJob{}, err
		}
		res.Job = retried
	}
	return riverJobFromResult(res, args), nil
}

// WorkerConfig controls River infrastructure without exposing River types to
// command packages. Generation keeps a small shared pool; other task classes
// retain independent capacity so delivery/sync can make progress under image
// load.
type WorkerConfig struct {
	GenerationWorkers int
	DeliveryWorkers   int
	LocalEditWorkers  int
	AgentWorkers      int
	JobTimeout        time.Duration
	RescueAfter       time.Duration
	Logger            *slog.Logger
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.GenerationWorkers < 1 {
		c.GenerationWorkers = 3
	}
	if c.DeliveryWorkers < 1 {
		c.DeliveryWorkers = 2
	}
	if c.LocalEditWorkers < 1 {
		c.LocalEditWorkers = 2
	}
	if c.AgentWorkers < 1 {
		c.AgentWorkers = 2
	}
	if c.JobTimeout <= 0 {
		c.JobTimeout = RiverTaskTimeout
	}
	if c.RescueAfter <= 0 {
		c.RescueAfter = RescueStuckJobsAfter
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return c
}

func queueForActor(actor string) string {
	switch actor {
	case ActorGraphRun, ActorImageSession:
		return "generation"
	case ActorDelivery:
		return "delivery"
	case ActorLocalEdit:
		return "local_edit"
	case ActorAgentTurnSync:
		return "agent"
	default:
		return river.QueueDefault
	}
}

type taskWorker struct {
	river.WorkerDefaults[TaskArgs]
	actors map[string]ActorFunc
}

// NewWorker binds the five business executors to River's typed handler contract.
func NewWorker(actors map[string]ActorFunc) river.Worker[TaskArgs] {
	return &taskWorker{actors: actors}
}

func (w *taskWorker) Kind() string { return RiverTaskKind }

func (w *taskWorker) Work(ctx context.Context, job *river.Job[TaskArgs]) error {
	actor := w.actors[job.Args.Actor]
	if actor == nil {
		return river.JobCancel(fmt.Errorf("unknown productflow actor %q", job.Args.Actor))
	}
	ctx = auth.WithMerchantID(withTaskArgs(ctx, job.Args), job.Args.MerchantID)
	err := actor(ctx, job.Args.AggregateID)
	switch {
	case err == nil, errors.Is(err, ErrSuperseded):
		return nil
	case errors.Is(err, ErrBusy):
		return river.JobSnooze(2 * time.Second)
	case errors.Is(err, ErrLater):
		return river.JobSnooze(time.Second)
	default:
		// Infrastructure errors use River's ordinary bounded retry path.
		// Business executors persist terminal/unknown state and return nil after
		// that durable decision; this layer never guesses their outcome.
		return err
	}
}

// Client owns a River worker process. The listener uses the existing pgx pool;
// database/sql is used for job operations so business GORM transactions and
// River InsertTx share the same PostgreSQL connections.
type Client struct {
	client *river.Client[*sql.Tx]
	sqlDB  *sql.DB
}

func NewClient(pool *pgxpool.Pool, actors map[string]ActorFunc, cfg WorkerConfig) (*Client, error) {
	if pool == nil {
		return nil, errors.New("queue: nil postgres pool")
	}
	cfg = cfg.withDefaults()
	sqlDB := stdlib.OpenDBFromPool(pool)
	workers := river.NewWorkers()
	if err := river.AddWorkerSafely(workers, NewWorker(actors)); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	riverClient, err := river.NewClient(riverdatabasesql.NewWithPgxListener(sqlDB, pool), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			"generation":       {MaxWorkers: cfg.GenerationWorkers},
			"delivery":         {MaxWorkers: cfg.DeliveryWorkers},
			"local_edit":       {MaxWorkers: cfg.LocalEditWorkers},
			"agent":            {MaxWorkers: cfg.AgentWorkers},
			river.QueueDefault: {MaxWorkers: 1},
		},
		JobTimeout:           cfg.JobTimeout,
		RescueStuckJobsAfter: cfg.RescueAfter,
		MaxAttempts:          RiverTaskMaxAttempts,
		// Retain stopped rows as evidence for their business execution identity;
		// recovery must not revive a discarded/cancelled epoch indefinitely.
		DiscardedJobRetentionPeriod: time.Duration(-1),
		CancelledJobRetentionPeriod: time.Duration(-1),
		Logger:                      cfg.Logger,
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return &Client{client: riverClient, sqlDB: sqlDB}, nil
}

func (c *Client) Start(ctx context.Context) error {
	if c == nil || c.client == nil {
		return errors.New("queue: nil River client")
	}
	return c.client.Start(ctx)
}

func (c *Client) Stop(ctx context.Context) error {
	if c == nil || c.client == nil {
		return nil
	}
	err := c.client.Stop(ctx)
	if err != nil {
		// A timed-out graceful stop leaves workers running. Cancel them while
		// keeping SQL available for their final business checkpoints.
		cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cancelErr := c.client.StopAndCancel(cancelCtx); cancelErr != nil {
			return errors.Join(err, cancelErr)
		}
	}
	if c.sqlDB != nil {
		return errors.Join(err, c.sqlDB.Close())
	}
	return err
}
