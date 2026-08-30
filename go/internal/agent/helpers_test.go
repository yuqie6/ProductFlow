package agent

import "testing"

func TestTurnNeedsSyncOnlyInFlight(t *testing.T) {
	if !turnNeedsSync(turnRow{ID: "t1", Status: "running"}) {
		t.Fatal("running must keep syncing")
	}
	if !turnNeedsSync(turnRow{ID: "t1", Status: "queued"}) {
		t.Fatal("queued must keep syncing")
	}
	if turnNeedsSync(turnRow{ID: "t1", Status: "awaiting_confirmation"}) {
		t.Fatal("parked awaiting_confirmation must not loop ErrLater")
	}
	if turnNeedsSync(turnRow{ID: "t1", Status: "succeeded"}) {
		t.Fatal("succeeded must consume")
	}
	if turnNeedsSync(turnRow{Status: "running"}) {
		t.Fatal("empty id must not sync")
	}
	if turnNeedsSync(turnRow{ID: "t1", Status: "running", ResumeRequired: true}) {
		t.Fatal("resume_required must not sync")
	}
}

func TestEventKindsRejectLiveDeltas(t *testing.T) {
	if inSet(eventKinds, "text.delta") || inSet(eventKinds, "thinking.delta") || inSet(eventKinds, "assistant.finish") {
		t.Fatal("live token events must not be durable Agent event kinds")
	}
	if !inSet(liveOnlyEventKinds, "text.delta") || !inSet(liveOnlyEventKinds, "thinking.delta") {
		t.Fatal("text.delta and thinking.delta stay live-only")
	}
	if inSet(eventKinds, "thinking_delta") {
		t.Fatal("Pi thinking_delta must be forwarded as thinking.delta")
	}
}

func TestGatewayQuestionNotLive(t *testing.T) {
	if gatewayQuestionNotLive(nil) {
		t.Fatal("nil")
	}
	if !gatewayQuestionNotLive(GatewayError{Status: 409, Code: "not_resumable"}) {
		t.Fatal("not_resumable")
	}
	if !gatewayQuestionNotLive(GatewayError{Status: 409, Code: "question_expired"}) {
		t.Fatal("question_expired")
	}
	if gatewayQuestionNotLive(GatewayError{Status: 409, Code: "question_already_answered"}) {
		t.Fatal("already answered is a conflict, not a dead waiter")
	}
	if gatewayQuestionNotLive(GatewayError{Status: 503, Code: "unavailable"}) {
		t.Fatal("unavailable must retry the live waiter")
	}
}
