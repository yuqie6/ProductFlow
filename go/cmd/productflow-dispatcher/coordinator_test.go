package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
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

func waitForSignal(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
