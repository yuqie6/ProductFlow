package agenttask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/yuqie6/agent-harness/durable"
	turnprotocol "github.com/yuqie6/agent-harness/turn"
)

const WorkflowDraftToolName = "propose_workflow_draft"

var ErrRequiredArtifactMissing = turnprotocol.ErrRequiredArtifactMissing

// RequiredArtifact makes one strict, locally validated pure tool call a
// precondition for successful task completion.
type RequiredArtifact struct {
	Name        string
	Description string
	Schema      map[string]any
}

func requiredArtifactTool(contract *RequiredArtifact) (Tool, string, error) {
	if contract == nil {
		return Tool{}, "", nil
	}
	name := strings.TrimSpace(contract.Name)
	if name == "" {
		name = WorkflowDraftToolName
	}
	description := strings.TrimSpace(contract.Description)
	if description == "" {
		description = "Submit the complete structured terminal artifact for application review."
	}
	schema, err := cloneToolSchema(contract.Schema)
	if err != nil {
		return Tool{}, "", fmt.Errorf("required artifact schema: %w", err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return Tool{}, "", err
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return Tool{}, "", fmt.Errorf("decode required artifact schema: %w", err)
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		return Tool{}, "", fmt.Errorf("resolve required artifact schema: %w", err)
	}
	return Tool{
		Name: name, Description: description, Parameters: schema, Strict: true,
		Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", fmt.Errorf("decode required artifact: %w", err)
			}
			if err := resolved.Validate(value); err != nil {
				return "", fmt.Errorf("required artifact does not match schema: %w", err)
			}
			return `{"accepted":true}`, nil
		},
	}, name, nil
}

// ArtifactFromJob returns the last successfully journaled call to name.
func ArtifactFromJob(job durable.Job, name string) (turnprotocol.Artifact, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return turnprotocol.Artifact{}, false, errors.New("artifact tool name is required")
	}
	var lastFailure error
	for index := len(job.Steps) - 1; index >= 0; index-- {
		step := job.Steps[index]
		if step.Tool != name || step.Status != durable.StepSucceeded {
			continue
		}
		var result struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(step.Result, &result); err != nil {
			lastFailure = fmt.Errorf("artifact step %s has invalid execution result: %w", step.ID, err)
			continue
		}
		if result.Error != "" {
			lastFailure = errors.New(result.Error)
			continue
		}
		if !json.Valid(step.Input) {
			return turnprotocol.Artifact{}, false, fmt.Errorf("artifact step %s has invalid JSON input", step.ID)
		}
		return turnprotocol.Artifact{
			Name: name, Value: append(json.RawMessage(nil), step.Input...), StepID: step.ID,
		}, true, nil
	}
	return turnprotocol.Artifact{}, false, lastFailure
}
