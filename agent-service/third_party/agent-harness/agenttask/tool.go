package agenttask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/durableagent"
	"github.com/yuqie6/agent-harness/internal/llm"
	internaltools "github.com/yuqie6/agent-harness/internal/tools"
	"github.com/yuqie6/agent-harness/internal/turninput"
)

var validToolName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Tool is a caller-trusted, side-effect-free extension such as a read-only
// application or MCP lookup. Its Handler or ResultHandler may be replayed after
// worker loss; exactly one must be configured.
type Tool struct {
	Name          string
	Description   string
	Parameters    map[string]any
	Strict        bool
	Handler       func(context.Context, json.RawMessage) (string, error)
	ResultHandler func(context.Context, json.RawMessage) (ToolResult, error)
}

// DurableTool explicitly exposes one caller-owned side effect to the model.
// Registering it grants unattended execution under the implementation's
// durable.Tool recovery contract; credentials must remain in the implementation
// and must never be returned from Prepare. Tool.Name is the persisted semantic
// identity; use a new name when Prepare, Execute, or Reconcile semantics change
// incompatibly for in-flight jobs.
type DurableTool struct {
	Description string
	Parameters  map[string]any
	Tool        durable.Tool
}

func internalReadTools(public []Tool) ([]internaltools.Tool, error) {
	result := make([]internaltools.Tool, 0, len(public))
	seen := make(map[string]bool, len(public))
	for _, candidate := range public {
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.Description = strings.TrimSpace(candidate.Description)
		if !validToolName.MatchString(candidate.Name) {
			return nil, fmt.Errorf("invalid agenttask tool name %q", candidate.Name)
		}
		if seen[candidate.Name] {
			return nil, fmt.Errorf("duplicate agenttask tool %q", candidate.Name)
		}
		seen[candidate.Name] = true
		if candidate.Description == "" || (candidate.Handler == nil) == (candidate.ResultHandler == nil) {
			return nil, fmt.Errorf("agenttask tool %q requires a description and exactly one of Handler or ResultHandler", candidate.Name)
		}
		schema, err := cloneToolSchema(candidate.Parameters)
		if err != nil {
			return nil, fmt.Errorf("agenttask tool %q parameters: %w", candidate.Name, err)
		}
		tool := internaltools.Tool{
			Name: candidate.Name, Description: candidate.Description, Parameters: schema, Strict: candidate.Strict,
			Capabilities: internaltools.CapabilityRead, Effect: internaltools.EffectPure,
			AutoApprove: validToolArguments,
		}
		if candidate.Handler != nil {
			handler := candidate.Handler
			tool.Handler = func(ctx context.Context, raw json.RawMessage) (string, error) {
				arguments, err := canonicalToolArguments(raw)
				if err != nil {
					return "", err
				}
				return handler(ctx, arguments)
			}
		} else {
			handler := candidate.ResultHandler
			tool.ResultHandler = func(ctx context.Context, raw json.RawMessage) (llm.ToolResult, error) {
				arguments, err := canonicalToolArguments(raw)
				if err != nil {
					return llm.ToolResult{}, err
				}
				result, handlerErr := handler(ctx, arguments)
				internal, resultErr := turninput.Result(result)
				return internal, errors.Join(handlerErr, resultErr)
			}
		}
		result = append(result, tool)
	}
	return result, nil
}

func internalDurableTools(public []DurableTool) ([]durableagent.ExternalTool, error) {
	result := make([]durableagent.ExternalTool, 0, len(public))
	seen := make(map[string]bool, len(public))
	for _, candidate := range public {
		if candidate.Tool == nil {
			return nil, errors.New("agenttask durable tool implementation is required")
		}
		name := candidate.Tool.Name()
		if !validToolName.MatchString(name) {
			return nil, fmt.Errorf("invalid agenttask durable tool name %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate agenttask durable tool %q", name)
		}
		seen[name] = true
		description := strings.TrimSpace(candidate.Description)
		if description == "" {
			return nil, fmt.Errorf("agenttask durable tool %q requires a description", name)
		}
		switch candidate.Tool.Effect() {
		case durable.EffectIdempotent, durable.EffectReconcilable, durable.EffectOpaque:
		case durable.EffectPure:
			return nil, fmt.Errorf("agenttask durable tool %q is pure; register it in Tools", name)
		default:
			return nil, fmt.Errorf("agenttask durable tool %q has invalid effect %q", name, candidate.Tool.Effect())
		}
		schema, err := cloneToolSchema(candidate.Parameters)
		if err != nil {
			return nil, fmt.Errorf("agenttask durable tool %q parameters: %w", name, err)
		}
		result = append(result, durableagent.ExternalTool{
			Schema: internaltools.Tool{Name: name, Description: description, Parameters: schema},
			Tool:   candidate.Tool,
		})
	}
	return result, nil
}

func cloneToolSchema(schema map[string]any) (map[string]any, error) {
	if schema == nil {
		schema = map[string]any{"type": "object", "additionalProperties": false}
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 128<<10 {
		return nil, errors.New("schema exceeds 128 KiB")
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, err
	}
	if value, _ := clone["type"].(string); value != "object" {
		return nil, errors.New("schema type must be object")
	}
	return clone, nil
}

func canonicalToolArguments(raw json.RawMessage) (json.RawMessage, error) {
	var arguments map[string]any
	if err := internaltools.ParseArguments(raw, &arguments); err != nil {
		return nil, fmt.Errorf("tool arguments must be a JSON object: %w", err)
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	return json.Marshal(arguments)
}

func validToolArguments(raw json.RawMessage) bool {
	_, err := canonicalToolArguments(raw)
	return err == nil
}
