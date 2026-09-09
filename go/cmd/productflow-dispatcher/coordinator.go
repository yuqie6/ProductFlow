package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type cycleFunc func(context.Context) error

func runRecoveryLoops(ctx context.Context, interval time.Duration, steps []recoveryStep, report func(recoveryStepResult)) {
	var wg sync.WaitGroup
	for _, step := range steps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				_ = runRecoveryStep(ctx, step, report)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	wg.Wait()
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
