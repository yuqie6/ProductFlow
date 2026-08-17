package app

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yuqie6/agent-harness/durable"
)

func TestProductFlowToolMapping(t *testing.T) {
	cases := []struct {
		name, kind, summary string
	}{
		{inspectAssetsToolName, "inspect_image", "Inspect product image assets"},
		{"propose_workflow_draft", "propose_draft", "Propose workflow draft"},
		{productContextToolName, "inspect_context", "Inspect product context and assets"},
		{listAssetsToolName, "inspect_context", "Inspect product context and assets"},
		{listGlobalProductsToolName, "inspect_context", "Inspect products and workflows"},
		{inspectGlobalProductsToolName, "inspect_context", "Inspect products and workflows"},
		{inspectGlobalWorkflowRunsToolName, "inspect_context", "Inspect products and workflows"},
		{listGlobalMediaAssetsToolName, "inspect_context", "Inspect global media assets"},
		{inspectGlobalMediaAssetsToolName, "inspect_context", "Inspect global media assets"},
		{createProductWorkspaceToolName, "create_product", "Create product onboarding workspace"},
		{listLegacyArchivesToolName, "read_history", "Read product history"},
		{inspectLegacyArchiveToolName, "read_history", "Read product history"},
		{createFolderToolName, "organize_assets", "Organize product image assets"},
		{renameFolderToolName, "organize_assets", "Organize product image assets"},
		{renameAssetToolName, "organize_assets", "Organize product image assets"},
		{moveAssetsToolName, "organize_assets", "Organize product image assets"},
	}
	for _, tc := range cases {
		kind, summary, ok := productFlowToolStep(tc.name)
		if !ok || kind != tc.kind || summary != tc.summary {
			t.Fatalf("%s => %q, %q, %v", tc.name, kind, summary, ok)
		}
	}
	for _, name := range []string{"model", "internal", "rejection", "question", "generate_image", "unmapped"} {
		if _, _, ok := productFlowToolStep(name); ok {
			t.Fatalf("unmapped tool %q was projected", name)
		}
	}
}

func TestProductFlowToolStepSnapshotIsBoundedAndSafe(t *testing.T) {
	job := durable.Job{Status: durable.JobSucceeded, Steps: []durable.Step{
		{ID: "inspect-1", Tool: inspectAssetsToolName, Status: durable.StepSucceeded, Input: json.RawMessage(`{"secret":"SECRET_SENTINEL"}`), Result: json.RawMessage(`{"raw":"SECRET_SENTINEL"}`)},
		{ID: "draft-1", Tool: "propose_workflow_draft", Status: durable.StepFailed, Error: "SECRET_SENTINEL"},
		{ID: "unknown-1", Tool: "model_internal_tool", Status: durable.StepRunning},
	}}
	steps := productFlowToolProjector(job)
	encoded, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(encoded)
	if strings.Contains(wire, "SECRET_SENTINEL") || strings.Contains(wire, "input") || strings.Contains(wire, "result") || strings.Contains(wire, "checkpoint") || strings.Contains(wire, "lease") {
		t.Fatalf("unsafe public projection: %s", wire)
	}
	if len(steps) != 2 || steps[0].StepID != "inspect-1" || steps[0].Status != toolStepSucceeded || steps[1].Status != toolStepFailed {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestBoundedToolStepSummaryPreservesUTF8Boundary(t *testing.T) {
	summary := strings.Repeat("a", maxToolStepSummary-1) + "界tail"
	got := boundedToolStepSummary(summary)
	if !utf8.ValidString(got) {
		t.Fatalf("summary is invalid UTF-8: %q", got)
	}
	if len(got) > maxToolStepSummary || got != strings.Repeat("a", maxToolStepSummary-1) {
		t.Fatalf("summary = %q (%d bytes)", got, len(got))
	}
}

func TestProductFlowToolStepStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status durable.StepStatus
		want   string
	}{
		{durable.StepPending, toolStepRunning}, {durable.StepPrepared, toolStepRunning}, {durable.StepRunning, toolStepRunning},
		{durable.StepSucceeded, toolStepSucceeded}, {durable.StepFailed, toolStepFailed}, {durable.StepUnknown, toolStepUnknown}, {durable.StepRequiresAction, toolStepUnknown},
	} {
		if got := projectStepStatus(durable.JobSucceeded, tc.status); got != tc.want {
			t.Fatalf("%s => %s, want %s", tc.status, got, tc.want)
		}
	}
}
