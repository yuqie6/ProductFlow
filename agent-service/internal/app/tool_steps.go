package app

import (
	"unicode/utf8"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/turn"
)

const (
	toolStepRunning    = "running"
	toolStepSucceeded  = "succeeded"
	toolStepFailed     = "failed"
	toolStepUnknown    = "unknown"
	maxToolStepSummary = 160
)

func productFlowToolProjector(job durable.Job) []turn.ToolStep {
	steps := make([]turn.ToolStep, 0, len(job.Steps))
	for _, step := range job.Steps {
		kind, summary, ok := productFlowToolStep(step.Tool)
		if !ok {
			continue
		}
		steps = append(steps, turn.ToolStep{
			StepID: step.ID, Kind: kind, Summary: boundedToolStepSummary(summary), Status: projectStepStatus(job.Status, step.Status),
		})
	}
	if len(steps) == 0 {
		return nil
	}
	return steps
}

func boundedToolStepSummary(summary string) string {
	if len(summary) <= maxToolStepSummary {
		return summary
	}
	end := maxToolStepSummary
	for end > 0 && !utf8.RuneStart(summary[end]) {
		end--
	}
	return summary[:end]
}

func productFlowToolStep(tool string) (string, string, bool) {
	switch tool {
	case inspectAssetsToolName:
		return "inspect_image", "Inspect product image assets", true
	case "propose_workflow_draft":
		return "propose_draft", "Propose workflow draft", true
	case productContextToolName, listAssetsToolName:
		return "inspect_context", "Inspect product context and assets", true
	case listGlobalProductsToolName, inspectGlobalProductsToolName, inspectGlobalWorkflowRunsToolName:
		return "inspect_context", "Inspect products and workflows", true
	case listGlobalMediaAssetsToolName, inspectGlobalMediaAssetsToolName:
		return "inspect_context", "Inspect global media assets", true
	case createProductWorkspaceToolName:
		return "create_product", "Create product onboarding workspace", true
	case listLegacyArchivesToolName, inspectLegacyArchiveToolName:
		return "read_history", "Read product history", true
	case createFolderToolName, renameFolderToolName, renameAssetToolName, moveAssetsToolName:
		return "organize_assets", "Organize product image assets", true
	case requestWorkflowRunToolName:
		return "request_workflow_run", "Request workflow execution for human confirmation", true
	default:
		return "", "", false
	}
}

func projectStepStatus(jobStatus durable.JobStatus, status durable.StepStatus) string {
	switch status {
	case durable.StepPending, durable.StepPrepared, durable.StepRunning:
		return toolStepRunning
	case durable.StepSucceeded:
		return toolStepSucceeded
	case durable.StepFailed:
		return toolStepFailed
	case durable.StepUnknown, durable.StepRequiresAction:
		return toolStepUnknown
	default:
		switch jobStatus {
		case durable.JobUnknown, durable.JobRequiresAction:
			return toolStepUnknown
		case durable.JobFailed:
			return toolStepFailed
		default:
			return toolStepRunning
		}
	}
}
