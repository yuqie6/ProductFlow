package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
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
			runAndReport()
		case <-wake:
			runAndReport()
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
