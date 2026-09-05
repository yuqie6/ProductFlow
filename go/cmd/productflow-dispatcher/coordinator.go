package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"go.uber.org/zap"
)

type cycleFunc func(context.Context) error
type cycleReporter func(error)

// runOneShot preserves the command's one-shot contract: recovery runs first,
// dispatch still runs after a recovery failure, and both errors are returned.
func runOneShot(ctx context.Context, recoverCycle, dispatchCycle cycleFunc) error {
	recoveryErr := recoverCycle(ctx)
	dispatchErr := dispatchCycle(ctx)
	return errors.Join(recoveryErr, dispatchErr)
}

// Each recovery domain has its own serial schedule; neither dispatch nor a
// healthy domain waits for another domain's current batch or next tick.
func runWatchLoops(
	ctx context.Context,
	dispatchInterval time.Duration,
	recoveryInterval time.Duration,
	dispatchWake <-chan struct{},
	dispatchCycle cycleFunc,
	recoverySteps []recoveryStep,
	reportDispatch cycleReporter,
	reportRecovery func(recoveryStepResult),
) {
	var wg sync.WaitGroup
	wg.Add(1 + len(recoverySteps))
	go func() {
		defer wg.Done()
		runScheduledLoop(ctx, dispatchInterval, dispatchWake, dispatchCycle, reportDispatch)
	}()
	for _, step := range recoverySteps {
		go func() {
			defer wg.Done()
			runScheduledLoop(ctx, recoveryInterval, nil, func(ctx context.Context) error {
				return runRecoveryStep(ctx, step, reportRecovery)
			}, nil)
		}()
	}
	wg.Wait()
}

func runScheduledLoop(
	ctx context.Context,
	interval time.Duration,
	wake <-chan struct{},
	run cycleFunc,
	report cycleReporter,
) {
	runAndReport := func() {
		if ctx.Err() != nil {
			return
		}
		err := run(ctx)
		if report != nil {
			report(err)
		}
	}
	runAndReport()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			drainWake(wake)
			runAndReport()
		case <-wake:
			drainWake(wake)
			runAndReport()
		}
	}
}

// startDispatchWake LISTENs on ChannelDispatch and coalesces notifications onto a
// size-1 wake channel. Listen failure falls back to ticker-only (nil wake).
func startDispatchWake(ctx context.Context, pool *pgxpool.Pool, logger *zap.Logger) (<-chan struct{}, func()) {
	notes, err := notify.Listen(ctx, pool, notify.ChannelDispatch)
	if err != nil {
		if logger != nil {
			logger.Error("dispatcher listen", zap.Error(err))
		}
		return nil, func() {}
	}
	wake := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardWake(notes, wake)
	}()
	return wake, wg.Wait
}

func forwardWake[T any](in <-chan T, wake chan<- struct{}) {
	if in == nil {
		return
	}
	for range in {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// dispatchWhileHasMore keeps claiming due PENDING after a full batch instead of
// waiting for the next ticker or NOTIFY. Recovery must not use this helper.
func dispatchWhileHasMore(ctx context.Context, run func(context.Context) (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hasMore, err := run(ctx)
		if err != nil || !hasMore {
			return err
		}
	}
}

func drainWake(wake <-chan struct{}) {
	for {
		select {
		case <-wake:
		default:
			return
		}
	}
}

type recoveryStep struct {
	domain       string
	errorContext string
	run          cycleFunc
}

type recoveryStepResult struct {
	domain   string
	duration time.Duration
	err      error
}

// runRecoverySteps isolates domain failures. A domain error is reported and
// joined into the result, but does not prevent the remaining domains from
// running. Context cancellation stops the cycle instead of starting new work.
func runRecoverySteps(
	ctx context.Context,
	steps []recoveryStep,
	report func(recoveryStepResult),
) error {
	var result error
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		err := runRecoveryStep(ctx, step, report)
		if err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func runRecoveryStep(ctx context.Context, step recoveryStep, report func(recoveryStepResult)) error {
	started := time.Now()
	err := step.run(ctx)
	if report != nil {
		report(recoveryStepResult{domain: step.domain, duration: time.Since(started), err: err})
	}
	if err != nil {
		return fmt.Errorf("%s: %w", step.errorContext, err)
	}
	return nil
}
