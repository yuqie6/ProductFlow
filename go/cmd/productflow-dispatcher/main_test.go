package main

import (
	"testing"

	"github.com/yuqie6/productflow/internal/platform/queue"
)

func TestDispatcherCycleIdle(t *testing.T) {
	if !dispatcherCycleIdle(queue.Summary{Dead: 12}, 0, 0, 0) {
		t.Fatal("dead-letter stock should stay idle")
	}
	if dispatcherCycleIdle(queue.Summary{Sent: 1}) {
		t.Fatal("sent work is not idle")
	}
	if dispatcherCycleIdle(queue.Summary{}, 0, 2) {
		t.Fatal("recovery work is not idle")
	}
}
