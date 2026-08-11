package durableagent

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
	questionpkg "github.com/yuqie6/agent-harness/internal/question"
)

func TestRunnerPersistsQuestionAndResumesWithAnswer(t *testing.T) {
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("call-question", questionpkg.ToolName, `{
			"header":"范围","question":"应该修改哪一部分?","options":[
				{"label":"核心","description":"只改执行内核"},{"label":"全部","description":"同时改界面"}
			]}`),
		textMessage("已按核心范围继续"),
	}}
	first, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Submit(t.Context(), "ask before changing")
	if err != nil {
		t.Fatal(err)
	}
	paused, err := first.Resume(t.Context(), job.ID)
	if !errors.Is(err, durable.ErrRequiresAction) || paused.Job.Status != durable.JobRequiresAction {
		t.Fatalf("paused = %#v, err = %v", paused, err)
	}
	question, found, err := QuestionFromJob(paused.Job)
	if err != nil || !found {
		t.Fatalf("question = %#v, found = %v, err = %v", question, found, err)
	}
	if question.Header != "范围" || question.Question != "应该修改哪一部分?" || len(question.Options) != 2 {
		t.Fatalf("question = %#v", question)
	}
	answer, err := question.EncodeAnswer(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := Open(testConfig(database, workspace, client, false, Policy{}))
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	target, err := resumed.engine.PendingResolution(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err = resumed.engine.ResolveRequiredAction(t.Context(), job.ID, durable.RequiredActionResolution{
		StepID: question.StepID, AttemptID: target.AttemptID,
		Outcome: durable.ResolutionApplied, Actor: "tester", Reason: "selected core scope", Result: answer,
	})
	if err != nil || job.Status != durable.JobAwaitingSteps {
		t.Fatalf("answered job = %#v, err = %v", job, err)
	}
	result, err := resumed.Resume(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != durable.JobSucceeded || result.Output != "已按核心范围继续" ||
		result.ModelCalls != 2 || result.ToolCalls != 1 {
		t.Fatalf("result = %#v", result)
	}
	last := client.calls[1][len(client.calls[1])-1].String()
	if !strings.Contains(last, `"option":0`) || !strings.Contains(last, `"label":"核心"`) {
		t.Fatalf("answer sent to model = %q", last)
	}
}

func TestRunnerReturnsInvalidQuestionToModelAsToolError(t *testing.T) {
	client := &scriptedClient{responses: []llm.Message{
		toolCallMessage("bad-question", questionpkg.ToolName, `{
			"question":"choose","options":[
				{"label":"1"},{"label":"2"},{"label":"3"},{"label":"4"},{"label":"5"},
				{"label":"6"},{"label":"7"},{"label":"8"},{"label":"9"}
			]
		}`),
		textMessage("question corrected"),
	}}
	runner := openRunner(t, filepath.Join(t.TempDir(), "jobs.db"), t.TempDir(), client, false, Policy{})
	result, err := runner.Start(t.Context(), "ask a valid question")
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != durable.JobSucceeded || result.Job.Steps[1].Tool != rejectionToolName {
		t.Fatalf("result = %#v", result)
	}
	last := client.calls[1][len(client.calls[1])-1].String()
	if !strings.Contains(last, "问题必须提供 1 到 8 个建议选项") {
		t.Fatalf("tool error = %q", last)
	}
}

func TestQuestionAnswerMustMatchPersistedOption(t *testing.T) {
	input := json.RawMessage(`{
		"question":"choose","options":[{"label":"A"},{"label":"B"}]
	}`)
	step := durable.Step{Tool: questionpkg.ToolName, Input: input, Result: json.RawMessage(`{"option":0,"label":"B"}`)}
	if _, err := questionResultText(step); err == nil || !strings.Contains(err.Error(), "需要 \"A\"") {
		t.Fatalf("error = %v", err)
	}
}
