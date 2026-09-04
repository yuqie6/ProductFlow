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

// runWatchLoops keeps recovery work off the dispatch loop. Each loop is
// internally serial, while dispatch ticks or wake-ups can run during recovery.
func runWatchLoops(
	ctx context.Context,
	dispatchInterval time.Duration,
	recoveryInterval time.Duration,
	dispatchWake <-chan struct{},
	dispatchCycle cycleFunc,
	recoverCycle cycleFunc,
	reportDispatch cycleReporter,
	reportRecovery cycleReporter,
) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		runScheduledLoop(ctx, dispatchInterval, dispatchWake, dispatchCycle, reportDispatch)
	}()
	go func() {
		defer wg.Done()
		runScheduledLoop(ctx, recoveryInterval, nil, recoverCycle, reportRecovery)
	}()
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
		err := run(ctx)
		if report != nil {
			report(err)
		}
	}
	if ctx.Err() != nil {
		return
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
		started := time.Now()
		err := step.run(ctx)
		if report != nil {
			report(recoveryStepResult{
				domain:   step.domain,
				duration: time.Since(started),
				err:      err,
			})
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("%s: %w", step.errorContext, err))
		}
	}
	return result
}
