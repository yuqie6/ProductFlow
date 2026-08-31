package graph

import "testing"

func TestSerializedProgressPhaseOnlyExposesLiveNodeProgress(t *testing.T) {
	phase := "provider_call"
	for _, status := range []string{NodeRunSucceeded, NodeRunSkipped, NodeRunFailed, NodeRunCancelled, NodeRunUnknown} {
		t.Run(status, func(t *testing.T) {
			response := serializeNodeRun(graphNodeRunRow{Status: status, ProgressPhase: &phase}, nil)
			if response.ProgressPhase != nil {
				t.Fatalf("terminal %s exposed stale progress phase %q", status, *response.ProgressPhase)
			}
		})
	}
	for _, status := range []string{NodeRunQueued, NodeRunRunning} {
		t.Run(status, func(t *testing.T) {
			response := serializeNodeRun(graphNodeRunRow{Status: status, ProgressPhase: &phase}, nil)
			if response.ProgressPhase == nil || *response.ProgressPhase != phase {
				t.Fatalf("live %s lost progress phase: %#v", status, response.ProgressPhase)
			}
		})
	}
}
