package main

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

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

func TestRecoveryDomainsProgressIndependentlyAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	done := make(chan struct{})
	var count atomic.Int32
	go func() {
		runRecoveryLoops(ctx, 5*time.Millisecond, []recoveryStep{
			{domain: "blocked", run: func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }},
			{domain: "healthy", run: func(context.Context) error { count.Add(1); return nil }},
		}, nil)
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("blocked domain not entered")
	}
	deadline := time.Now().Add(time.Second)
	for count.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if count.Load() < 3 {
		t.Fatal("healthy domain blocked")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery did not stop")
	}
	before := count.Load()
	time.Sleep(10 * time.Millisecond)
	if count.Load() != before {
		t.Fatal("work after cancellation")
	}
}
