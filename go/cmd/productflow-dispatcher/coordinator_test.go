package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestRunOneShotRecoversThenDispatchesAndJoinsErrors(t *testing.T) {
	recoveryErr := errors.New("recovery failed")
	dispatchErr := errors.New("dispatch failed")
	var order []string

	err := runOneShot(
		context.Background(),
		func(context.Context) error {
			order = append(order, "recovery")
			return recoveryErr
		},
		func(context.Context) error {
			order = append(order, "dispatch")
			return dispatchErr
		},
	)

	if !reflect.DeepEqual(order, []string{"recovery", "dispatch"}) {
		t.Fatalf("order = %v", order)
	}
	if !errors.Is(err, recoveryErr) || !errors.Is(err, dispatchErr) {
		t.Fatalf("error = %v, want both failures", err)
	}
}

func TestRunRecoveryStepsContinuesAfterDomainFailure(t *testing.T) {
	wantErr := errors.New("graph unavailable")
	var called []string
	var reported []recoveryStepResult
	steps := []recoveryStep{
		{
			domain: "graph", errorContext: "workflow recovery",
			run: func(context.Context) error {
				called = append(called, "graph")
				return wantErr
			},
		},
		{
			domain: "image_session", errorContext: "image session recovery",
			run: func(context.Context) error {
				called = append(called, "image_session")
				return nil
			},
		},
		{
			domain: "agent", errorContext: "agent recovery",
			run: func(context.Context) error {
				called = append(called, "agent")
				return nil
			},
		},
	}

	err := runRecoverySteps(context.Background(), steps, func(result recoveryStepResult) {
		reported = append(reported, result)
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want graph failure", err)
	}
	if !reflect.DeepEqual(called, []string{"graph", "image_session", "agent"}) {
		t.Fatalf("called = %v", called)
	}
	if len(reported) != len(steps) {
		t.Fatalf("reported %d results, want %d", len(reported), len(steps))
	}
	if reported[0].domain != "graph" || !errors.Is(reported[0].err, wantErr) {
		t.Fatalf("first report = %+v", reported[0])
	}
}

func TestRunWatchLoopsDispatchWakeIsNotBlockedByRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)
	dispatched := make(chan struct{}, 3)
	recoveryStarted := make(chan struct{})
	var startOnce sync.Once
	done := make(chan struct{})

	go func() {
		defer close(done)
		runWatchLoops(
			ctx,
			time.Hour,
			time.Hour,
			wake,
			func(context.Context) error {
				dispatched <- struct{}{}
				return nil
			},
			func(ctx context.Context) error {
				startOnce.Do(func() { close(recoveryStarted) })
				<-ctx.Done()
				return ctx.Err()
			},
			nil,
			nil,
		)
	}()

	waitForSignal(t, recoveryStarted, "initial recovery")
	waitForSignal(t, dispatched, "initial dispatch")
	wake <- struct{}{}
	waitForSignal(t, dispatched, "woken dispatch while recovery is blocked")

	cancel()
	waitForSignal(t, done, "watch loops to stop")
}

func TestRunWatchLoopsDispatchTickerIsNotBlockedByRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dispatched := make(chan struct{}, 3)
	recoveryStarted := make(chan struct{})
	var startOnce sync.Once
	done := make(chan struct{})

	go func() {
		defer close(done)
		runWatchLoops(
			ctx,
			10*time.Millisecond,
			time.Hour,
			nil,
			func(context.Context) error {
				dispatched <- struct{}{}
				return nil
			},
			func(ctx context.Context) error {
				startOnce.Do(func() { close(recoveryStarted) })
				<-ctx.Done()
				return ctx.Err()
			},
			nil,
			nil,
		)
	}()

	waitForSignal(t, recoveryStarted, "initial recovery")
	waitForSignal(t, dispatched, "initial dispatch")
	waitForSignal(t, dispatched, "ticked dispatch while recovery is blocked")

	cancel()
	waitForSignal(t, done, "watch loops to stop")
}

func TestForwardWakeCoalescesAndExitsWhenInputCloses(t *testing.T) {
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)
	wake := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardWake(in, wake)
	}()
	waitForSignal(t, done, "forwardWake to exit after input close")
	select {
	case <-wake:
	default:
		t.Fatal("expected a coalesced wake")
	}
	select {
	case <-wake:
		t.Fatal("wake flood should drop while a signal is already pending")
	default:
	}
}

func TestDrainWakeEmptiesPendingSignals(t *testing.T) {
	wake := make(chan struct{}, 4)
	wake <- struct{}{}
	wake <- struct{}{}
	wake <- struct{}{}
	drainWake(wake)
	select {
	case <-wake:
		t.Fatal("wake channel still has signals after drain")
	default:
	}
	drainWake(nil)
}

func TestDispatchWhileHasMoreContinuesUntilCaughtUp(t *testing.T) {
	var calls int
	err := dispatchWhileHasMore(context.Background(), func(context.Context) (bool, error) {
		calls++
		return calls < 3, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDispatchWhileHasMoreStopsOnError(t *testing.T) {
	wantErr := errors.New("claim failed")
	var calls int
	err := dispatchWhileHasMore(context.Background(), func(context.Context) (bool, error) {
		calls++
		return true, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want claim failure", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDispatchWhileHasMoreStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	err := dispatchWhileHasMore(ctx, func(context.Context) (bool, error) {
		calls++
		return true, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestRunWatchLoopsDispatchDrainsBacklogBeforeTicker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	batches := make(chan int, 8)
	remaining := 3
	done := make(chan struct{})

	go func() {
		defer close(done)
		runWatchLoops(
			ctx,
			time.Hour,
			time.Hour,
			nil,
			func(ctx context.Context) error {
				return dispatchWhileHasMore(ctx, func(context.Context) (bool, error) {
					batches <- remaining
					remaining--
					return remaining > 0, nil
				})
			},
			func(context.Context) error { return nil },
			nil,
			nil,
		)
	}()

	for want := 3; want >= 1; want-- {
		select {
		case got := <-batches:
			if got != want {
				t.Fatalf("batch remaining = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for backlog batch %d", want)
		}
	}

	cancel()
	waitForSignal(t, done, "watch loops to stop")
}

func TestStartDispatchWakeExitsOnCancel(t *testing.T) {
	pool, _ := testdb.Open(t)
	ctx, cancel := context.WithCancel(context.Background())
	wake, waitListen := startDispatchWake(ctx, pool, nil)
	if wake == nil {
		t.Fatal("LISTEN failed; cannot assert cancel unblocks the listener")
	}
	cancel()
	done := make(chan struct{})
	go func() {
		waitListen()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch LISTEN did not exit after cancel")
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
