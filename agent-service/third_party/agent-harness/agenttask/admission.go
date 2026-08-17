package agenttask

import (
	"context"
	"sync"
)

// Admission limits the number of durable Turns that may actively call the
// model across all Service instances owned by one process.
type Admission interface {
	Acquire(context.Context) error
	Release()
}

// AdmissionPriority controls which durable Turn gets the next model slot when
// several Service instances are waiting in the same process.
type AdmissionPriority uint8

const (
	AdmissionPriorityInteractive AdmissionPriority = iota
	AdmissionPriorityBackground
)

// PriorityAdmission is optional so existing Admission implementations keep
// working. ProductFlow uses it to keep direct human Turns responsive while
// background Agent Tasks continue to wait durably.
type PriorityAdmission interface {
	AcquirePriority(context.Context, AdmissionPriority) error
}

type semaphoreWaiter struct {
	priority AdmissionPriority
	sequence uint64
	ready    chan struct{}
	granted  bool
}

// Semaphore is a process-local admission gate. Turns remain durable and
// queued in their owning Service while waiting for a slot. Waiters are FIFO
// within a priority, and interactive Turns take precedence over background
// Tasks when capacity is constrained.
type Semaphore struct {
	mu       sync.Mutex
	limit    int
	active   int
	sequence uint64
	waiters  []*semaphoreWaiter
}

func NewSemaphore(limit int) *Semaphore {
	if limit <= 0 {
		return nil
	}
	return &Semaphore{limit: limit}
}

func (semaphore *Semaphore) Acquire(ctx context.Context) error {
	return semaphore.AcquirePriority(ctx, AdmissionPriorityInteractive)
}

func (semaphore *Semaphore) AcquirePriority(ctx context.Context, priority AdmissionPriority) error {
	if semaphore == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	waiter := &semaphoreWaiter{
		priority: priority,
		ready:    make(chan struct{}),
	}
	semaphore.mu.Lock()
	if semaphore.active < semaphore.limit && len(semaphore.waiters) == 0 {
		semaphore.active++
		semaphore.mu.Unlock()
		return nil
	}
	semaphore.sequence++
	waiter.sequence = semaphore.sequence
	semaphore.waiters = append(semaphore.waiters, waiter)
	semaphore.mu.Unlock()

	select {
	case <-waiter.ready:
		return nil
	case <-ctx.Done():
		semaphore.mu.Lock()
		if waiter.granted {
			semaphore.mu.Unlock()
			return nil
		}
		for index, candidate := range semaphore.waiters {
			if candidate == waiter {
				semaphore.waiters = append(semaphore.waiters[:index], semaphore.waiters[index+1:]...)
				break
			}
		}
		semaphore.mu.Unlock()
		return ctx.Err()
	}
}

func (semaphore *Semaphore) Release() {
	if semaphore == nil {
		return
	}
	var next *semaphoreWaiter
	semaphore.mu.Lock()
	if semaphore.active == 0 {
		semaphore.mu.Unlock()
		return
	}
	semaphore.active--
	if len(semaphore.waiters) > 0 {
		bestIndex := 0
		for index := 1; index < len(semaphore.waiters); index++ {
			candidate := semaphore.waiters[index]
			best := semaphore.waiters[bestIndex]
			if candidate.priority < best.priority ||
				(candidate.priority == best.priority && candidate.sequence < best.sequence) {
				bestIndex = index
			}
		}
		next = semaphore.waiters[bestIndex]
		semaphore.waiters = append(semaphore.waiters[:bestIndex], semaphore.waiters[bestIndex+1:]...)
		next.granted = true
		semaphore.active++
	}
	semaphore.mu.Unlock()
	if next != nil {
		close(next.ready)
	}
}
