package agent

import "testing"

func TestTurnNeedsSyncOnlyForStartOrAnswer(t *testing.T) {
	if turnNeedsSync(turnRow{ID: "t1", Status: "running", HarnessTurnID: ptr("h1")}) {
		t.Fatal("bound running Turn is projected from the PG journal")
	}
	if !turnNeedsSync(turnRow{ID: "t1", Status: "running"}) {
		t.Fatal("unbound running Turn must retry start")
	}
	if !turnNeedsSync(turnRow{ID: "t1", Status: "queued"}) {
		t.Fatal("unbound queued Turn must retry start")
	}
	if turnNeedsSync(turnRow{ID: "t1", Status: "queued", HarnessTurnID: ptr("h1")}) {
		t.Fatal("bound queued Turn must wait for journal events")
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
	if turnNeedsSync(turnRow{ID: "t1", Status: "requires_input"}) {
		t.Fatal("unanswered requires_input must not loop ErrLater")
	}
	if !turnNeedsSync(turnRow{ID: "t1", Status: "requires_input", QuestionAnswerJSON: []byte(`{"text":"筋膜枪"}`)}) {
		t.Fatal("answered requires_input must keep syncing until resume")
	}
}

func TestEventKindsUseTheJournalVocabulary(t *testing.T) {
	for _, kind := range []string{
		"turn/start", "text.chunk", "thinking.chunk", "assistant/message",
		"tool/call", "tool/result", "approval/requested", "approval/resolved",
		"turn/cancel_requested", "turn/resume_requested", "turn/end",
	} {
		if !inSet(eventKinds, kind) {
			t.Fatalf("journal kind %q is missing", kind)
		}
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
