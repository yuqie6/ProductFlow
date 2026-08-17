package agenttask

import (
	"context"
	"testing"
	"time"
)

func TestSemaphorePrioritizesInteractiveWaiters(t *testing.T) {
	semaphore := NewSemaphore(1)
	if err := semaphore.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	backgroundResult := make(chan error, 1)
	go func() {
		backgroundResult <- semaphore.AcquirePriority(context.Background(), AdmissionPriorityBackground)
	}()
	waitForSemaphoreWaiters(t, semaphore, 1)

	interactiveResult := make(chan error, 1)
	go func() {
		interactiveResult <- semaphore.AcquirePriority(context.Background(), AdmissionPriorityInteractive)
	}()
	waitForSemaphoreWaiters(t, semaphore, 2)

	semaphore.Release()
	select {
	case err := <-interactiveResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("interactive waiter did not receive the released slot")
	}
	select {
	case <-backgroundResult:
		t.Fatal("background waiter bypassed the interactive waiter")
	default:
	}

	semaphore.Release()
	select {
	case err := <-backgroundResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("background waiter did not receive the next slot")
	}
	semaphore.Release()
}

func TestSemaphoreCancellationRemovesQueuedWaiter(t *testing.T) {
	semaphore := NewSemaphore(1)
	if err := semaphore.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- semaphore.AcquirePriority(ctx, AdmissionPriorityBackground)
	}()
	waitForSemaphoreWaiters(t, semaphore, 1)
	cancel()
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("canceled acquisition error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter did not exit")
	}

	semaphore.Release()
	semaphore.mu.Lock()
	active, waiters := semaphore.active, len(semaphore.waiters)
	semaphore.mu.Unlock()
	if active != 0 || waiters != 0 {
		t.Fatalf("semaphore retained canceled waiter: active=%d waiters=%d", active, waiters)
	}
}

func waitForSemaphoreWaiters(t *testing.T, semaphore *Semaphore, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		semaphore.mu.Lock()
		waiters := len(semaphore.waiters)
		semaphore.mu.Unlock()
		if waiters == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	semaphore.mu.Lock()
	waiters := len(semaphore.waiters)
	semaphore.mu.Unlock()
	t.Fatalf("semaphore waiters = %d, want %d", waiters, count)
}
