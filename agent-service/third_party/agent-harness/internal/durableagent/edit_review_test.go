package durableagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
)

func TestReviewEditPausesBeforeWriteAndResumesAcrossProcess(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "value.txt")
	before := []byte("before\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(before)
	editArgs, _ := json.Marshal(durable.FileEditInput{
		Path: "value.txt", ExpectedSHA256: hex.EncodeToString(digest[:]), OldText: "before", NewText: "after",
	})
	readCall := toolCallMessage("call-read", "read_file", `{"path":"value.txt"}`).ToolCalls[0]
	editCall := toolCallMessage("call-edit", "edit_file", string(editArgs)).ToolCalls[0]
	client := &scriptedClient{responses: []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{readCall, editCall}},
		textMessage("approved edit completed"),
	}}
	database := filepath.Join(t.TempDir(), "jobs.db")
	config := testConfig(database, workspace, client, false, Policy{})
	config.ReviewEdit = true
	first, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "edit after review")
	if err != nil {
		t.Fatal(err)
	}
	paused, err := first.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrRequiresAction) || paused.Job.Status != durable.JobRequiresAction {
		t.Fatalf("paused = %#v, err = %v", paused, err)
	}
	if len(paused.Job.Steps) != 4 || paused.Job.Steps[1].Tool != "read_file" ||
		paused.Job.Steps[1].Status != durable.StepSucceeded || paused.Job.Steps[2].Tool != editReviewToolName ||
		paused.Job.Steps[2].Status != durable.StepRequiresAction || paused.Job.Steps[3].Tool != "edit_file" ||
		paused.Job.Steps[3].Status != durable.StepPending {
		t.Fatalf("steps = %#v", paused.Job.Steps)
	}
	assertFileContent(t, path, "before\n")
	review, found, err := EditReviewFromJob(paused.Job)
	if err != nil || !found || review.Path != "value.txt" || review.OldText != "before" || review.NewText != "after" {
		t.Fatalf("review = %#v, found = %v, err = %v", review, found, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	target, err := resumed.engine.PendingResolution(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err = resumed.engine.ResolveRequiredAction(t.Context(), job.ID, durable.RequiredActionResolution{
		StepID: review.StepID, AttemptID: target.AttemptID,
		Outcome: durable.ResolutionApplied, Actor: "reviewer", Reason: "diff matches request",
		Result: json.RawMessage(`{"approved":true}`),
	})
	if err != nil || job.Status != durable.JobPending {
		t.Fatalf("approved job = %#v, err = %v", job, err)
	}
	assertFileContent(t, path, "before\n")
	result, err := resumed.Resume(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != durable.JobSucceeded || result.Output != "approved edit completed" ||
		result.ModelCalls != 2 || result.ApprovalCalls != 1 || result.ToolCalls != 2 {
		t.Fatalf("result = %#v", result)
	}
	assertFileContent(t, path, "after\n")
	secondRequest := client.calls[1]
	if len(secondRequest) < 2 || secondRequest[len(secondRequest)-2].ToolCallID != "call-read" ||
		secondRequest[len(secondRequest)-1].ToolCallID != "call-edit" ||
		!strings.Contains(secondRequest[len(secondRequest)-1].String(), "after_sha256") {
		t.Fatalf("tool results sent to model = %#v", secondRequest)
	}
	events, err := resumed.engine.Events(t.Context(), job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countKind(events, "attempt.action_resolved") != 1 {
		t.Fatalf("events = %#v", events)
	}
}

func TestReviewEditRejectionLeavesFileUnchanged(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "value.txt")
	before := []byte("before\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(before)
	editArgs, _ := json.Marshal(durable.FileEditInput{
		Path: "value.txt", ExpectedSHA256: hex.EncodeToString(digest[:]), OldText: "before", NewText: "after",
	})
	client := &scriptedClient{responses: []llm.Message{toolCallMessage("call-edit", "edit_file", string(editArgs))}}
	config := testConfig(filepath.Join(t.TempDir(), "jobs.db"), workspace, client, false, Policy{})
	config.ReviewEdit = true
	runner, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	job, err := runner.Submit(t.Context(), "reject this edit")
	if err != nil {
		t.Fatal(err)
	}
	paused, err := runner.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrRequiresAction) {
		t.Fatalf("paused = %#v, err = %v", paused, err)
	}
	review, found, err := EditReviewFromJob(paused.Job)
	if err != nil || !found {
		t.Fatalf("review = %#v, found = %v, err = %v", review, found, err)
	}
	target, err := runner.engine.PendingResolution(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err = runner.engine.ResolveRequiredAction(t.Context(), job.ID, durable.RequiredActionResolution{
		StepID: review.StepID, AttemptID: target.AttemptID,
		Outcome: durable.ResolutionFailed, Actor: "reviewer", Reason: "wrong scope",
	})
	if err != nil || job.Status != durable.JobFailed {
		t.Fatalf("rejected job = %#v, err = %v", job, err)
	}
	assertFileContent(t, path, "before\n")
	if client.callCount() != 1 {
		t.Fatalf("model calls = %d", client.callCount())
	}
}

func TestReviewEditModeDriftIsRejectedBeforeModelCall(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &scriptedClient{responses: []llm.Message{textMessage("must not run")}}
	reviewConfig := testConfig(database, workspace, client, false, Policy{})
	reviewConfig.ReviewEdit = true
	first, err := Open(reviewConfig)
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "review mode snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	autoConfig := testConfig(database, workspace, client, true, Policy{})
	auto, err := Open(autoConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer auto.Close()
	result, err := auto.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrConflict) || result.Job.Status != durable.JobPending || client.callCount() != 0 {
		t.Fatalf("result = %#v, calls = %d, err = %v", result, client.callCount(), err)
	}
}

func TestEditModesAreMutuallyExclusive(t *testing.T) {
	config := testConfig(filepath.Join(t.TempDir(), "jobs.db"), t.TempDir(), &scriptedClient{}, true, Policy{})
	config.ReviewEdit = true
	if _, err := Open(config); err == nil || !strings.Contains(err.Error(), "不能同时启用") {
		t.Fatalf("error = %v", err)
	}
}

func TestEditReviewRejectsMismatchedPendingMutation(t *testing.T) {
	job := durable.Job{Steps: []durable.Step{
		{Tool: editReviewToolName, Status: durable.StepRequiresAction, Input: json.RawMessage(`{"path":"a","expected_sha256":"absent","old_text":"","new_text":"reviewed"}`)},
		{Tool: "edit_file", Status: durable.StepPending, Input: json.RawMessage(`{"path":"b","expected_sha256":"absent","old_text":"","new_text":"different"}`)},
	}}
	if _, _, err := EditReviewFromJob(job); !errors.Is(err, durable.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != want {
		t.Fatalf("content = %q, err = %v", content, err)
	}
}
