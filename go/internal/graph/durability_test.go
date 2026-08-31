package graph

import (
	"strings"
	"testing"
	"time"
)

func TestTerminalNodeRunUpdatesClearLiveProgress(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{NodeRunSucceeded, NodeRunSkipped, NodeRunFailed, NodeRunCancelled, NodeRunUnknown} {
		t.Run(status, func(t *testing.T) {
			updates := terminalNodeRunUpdates(status, now)
			if updates["status"] != status || updates["finished_at"] != now || updates["progress_updated_at"] != now {
				t.Fatalf("unexpected terminal updates: %#v", updates)
			}
			if value, ok := updates["progress_phase"]; !ok || value != nil {
				t.Fatalf("terminal progress_phase must be explicitly cleared: %#v", updates)
			}
			if value, ok := updates["active_attempt_id"]; !ok || value != nil {
				t.Fatalf("terminal active_attempt_id must be explicitly cleared: %#v", updates)
			}
		})
	}
}

func TestAggregateGraphRunTerminalStatusPriority(t *testing.T) {
	longReason := strings.Repeat("x", 1001)
	tests := []struct {
		name       string
		statuses   []string
		reasons    []string
		want       string
		wantReason string
		retryable  bool
	}{
		{name: "success and skipped", statuses: []string{NodeRunSucceeded, NodeRunSkipped}, reasons: []string{"", ""}, want: RunStatusSucceeded},
		{name: "mixed cancellation", statuses: []string{NodeRunSucceeded, NodeRunCancelled, NodeRunSkipped}, reasons: []string{"", "", ""}, want: RunStatusCancelled, wantReason: GraphCancelledReason},
		{name: "failure beats cancellation", statuses: []string{NodeRunCancelled, NodeRunFailed}, reasons: []string{"", "provider rejected"}, want: RunStatusFailed, wantReason: "provider rejected", retryable: true},
		{name: "unknown beats failure", statuses: []string{NodeRunFailed, NodeRunUnknown}, reasons: []string{"failed", ""}, want: RunStatusUnknown, wantReason: ProviderUnknownDetail},
		{name: "reason is bounded", statuses: []string{NodeRunUnknown}, reasons: []string{longReason}, want: RunStatusUnknown, wantReason: longReason[:1000]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason, retryable := aggregateGraphRunTerminalStatus(tt.statuses, tt.reasons)
			if status != tt.want || retryable != tt.retryable {
				t.Fatalf("got status=%s retryable=%t, want status=%s retryable=%t", status, retryable, tt.want, tt.retryable)
			}
			if tt.wantReason == "" {
				if reason != nil {
					t.Fatalf("unexpected reason %q", *reason)
				}
				return
			}
			if reason == nil || *reason != tt.wantReason {
				t.Fatalf("got reason=%v, want %q", reason, tt.wantReason)
			}
		})
	}
}
