package agenttask

import "testing"

func TestPublicToolStepStatusMapsAttemptLifecycleKinds(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"attempt.prepared", "running"},
		{"attempt.started", "running"},
		{"attempt.recovered", "running"},
		{"attempt.succeeded", "succeeded"},
		{"attempt.failed", "failed"},
		{"attempt.prepare_failed", "failed"},
		{"attempt.unknown", "unknown"},
		{"attempt.requires_action", "unknown"},
	}
	for _, tc := range cases {
		got, ok := publicToolStepStatus(tc.kind)
		if !ok || got != tc.want {
			t.Fatalf("%s => %q, %v; want %q, true", tc.kind, got, ok, tc.want)
		}
	}
}

func TestPublicToolStepStatusIgnoresNonLifecycleEvents(t *testing.T) {
	if _, ok := publicToolStepStatus("job.succeeded"); ok {
		t.Fatal("job event was projected as a tool step")
	}
}
