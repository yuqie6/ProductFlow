package agenttask

import "context"

// Admission limits the number of durable Turns that may actively call the
// model across all Service instances owned by one process.
type Admission interface {
	Acquire(context.Context) error
	Release()
}

// Semaphore is a process-local admission gate. Turns remain durable and
// queued in their owning Service while waiting for a slot.
type Semaphore struct {
	slots chan struct{}
}

func NewSemaphore(limit int) *Semaphore {
	if limit <= 0 {
		return nil
	}
	return &Semaphore{slots: make(chan struct{}, limit)}
}

func (semaphore *Semaphore) Acquire(ctx context.Context) error {
	if semaphore == nil {
		return nil
	}
	select {
	case semaphore.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (semaphore *Semaphore) Release() {
	if semaphore == nil {
		return
	}
	<-semaphore.slots
}
