package main

import (
	"testing"

	"github.com/yuqie6/productflow/internal/platform/queue"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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

func TestRecoveryBatchLogsAreDomainLocalAndIdleIsDebug(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	logRecoveryWork(logger, "graph", 0, 0, false)
	logRecoveryWork(logger, "image_session", 0, 1, false)
	logRecoveryWork(logger, "agent", 0, 0, true)
	entries := logs.All()
	if len(entries) != 3 || entries[0].Level != zap.DebugLevel || entries[1].Level != zap.InfoLevel || entries[2].Level != zap.InfoLevel {
		t.Fatalf("unexpected batch log levels: %+v", entries)
	}
	fields := entries[1].ContextMap()
	if fields["domain"] != "image_session" || fields["unknown"] != int64(1) || fields["enqueued"] != int64(0) || fields["has_more"] != false {
		t.Fatalf("unexpected domain batch fields: %+v", fields)
	}
}
