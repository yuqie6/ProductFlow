package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

const (
	productContextToolName = "get_product_workflow_context_v1"
	listAssetsToolName     = "list_product_image_assets_v1"
	inspectAssetsToolName  = "inspect_product_image_assets_v1"
	renameAssetToolName    = "rename_product_image_asset_v1"
	maxInspectedAssets     = 6
	maxListedAssets        = 50
)

func scopedReadTools(client *productflow.Client, scope Scope) []agenttask.Tool {
	return []agenttask.Tool{
		{
			Name:        productContextToolName,
			Description: "Read the current product facts, workflow draft summary, missing information, and bound reference asset IDs.",
			Parameters:  emptyObjectSchema(),
			Strict:      true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				if err := decodeStrictObject(raw, &struct{}{}); err != nil {
					return "", err
				}
				result, err := client.ProductContext(ctx, scope.ConversationID)
				return string(result), err
			},
		},
		{
			Name:        listAssetsToolName,
			Description: "List a bounded page of image metadata from this conversation's product. This never returns image bytes or URLs.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "maxLength": 120},
					"after": map[string]any{"type": "string", "maxLength": 200},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxListedAssets},
				},
				"required": []string{"query", "after", "limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					Query string `json:"query"`
					After string `json:"after"`
					Limit int    `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				if arguments.Limit == 0 {
					arguments.Limit = 20
				}
				if arguments.Limit < 1 || arguments.Limit > maxListedAssets {
					return "", fmt.Errorf("limit must be between 1 and %d", maxListedAssets)
				}
				result, err := client.ListAssets(ctx, scope.ConversationID, arguments.Query, arguments.After, arguments.Limit)
				if err != nil {
					return "", err
				}
				encoded, err := json.Marshal(result)
				return string(encoded), err
			},
		},
		{
			Name:        inspectAssetsToolName,
			Description: "Inspect up to six explicitly selected image assets from this conversation's product as native multimodal content.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"asset_ids": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxInspectedAssets,
						"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
					},
				},
				"required": []string{"asset_ids"},
			},
			Strict: true,
			ResultHandler: func(ctx context.Context, raw json.RawMessage) (agenttask.ToolResult, error) {
				var arguments struct {
					AssetIDs []string `json:"asset_ids"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return agenttask.ToolResult{}, err
				}
				assetIDs, err := uniqueAssetIDs(arguments.AssetIDs)
				if err != nil {
					return agenttask.ToolResult{}, err
				}
				metadata, err := client.InspectAssets(ctx, scope.ConversationID, assetIDs)
				if err != nil {
					return agenttask.ToolResult{}, err
				}
				if len(metadata) != len(assetIDs) {
					return agenttask.ToolResult{}, errors.New("ProductFlow returned an incomplete asset inspection result")
				}
				byID := make(map[string]productflow.AssetMetadata, len(metadata))
				for _, asset := range metadata {
					byID[asset.ID] = asset
				}
				content := make([]agenttask.ToolResultContent, 0, len(assetIDs)*2)
				var totalBytes int64
				for _, assetID := range assetIDs {
					asset, ok := byID[assetID]
					if !ok {
						return agenttask.ToolResult{}, fmt.Errorf("ProductFlow omitted requested asset %s", assetID)
					}
					image, err := client.AssetContent(ctx, scope.ConversationID, assetID)
					if err != nil {
						return agenttask.ToolResult{}, err
					}
					totalBytes += image.SizeBytes
					if totalBytes > agenttask.MaxTotalToolResultImageBytes {
						return agenttask.ToolResult{}, errors.New("selected asset bytes exceed the tool result limit")
					}
					label, err := json.Marshal(asset)
					if err != nil {
						return agenttask.ToolResult{}, err
					}
					content = append(content,
						agenttask.ToolResultContent{Type: agenttask.ContentInputText, Text: string(label)},
						agenttask.ToolResultContent{Type: agenttask.ContentInputImage, Image: &agenttask.InputImage{
							Data: image.Data, MediaType: image.MediaType, SizeBytes: image.SizeBytes,
							Detail: agenttask.ImageDetailHigh, CheckpointMode: agenttask.ImageCheckpointEmbed,
						}},
					)
				}
				return agenttask.ToolResult{SchemaVersion: agenttask.ToolResultSchemaVersion, Content: content}, nil
			},
		},
	}
}

func scopedDurableTools(client *productflow.Client, scope Scope) []agenttask.DurableTool {
	tool := &renameAssetTool{client: client, scope: scope}
	return []agenttask.DurableTool{{
		Description: "Rename one image asset's display name within this conversation's product. The asset file and lineage are unchanged.",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"asset_id":     map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
				"display_name": map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
			},
			"required": []string{"asset_id", "display_name"},
		},
		Tool: tool,
	}}
}

type renameAssetTool struct {
	client *productflow.Client
	scope  Scope
}

func (*renameAssetTool) Name() string                { return renameAssetToolName }
func (*renameAssetTool) Effect() durable.EffectClass { return durable.EffectReconcilable }

func (tool *renameAssetTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var arguments struct {
		AssetID     string `json:"asset_id"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeStrictObject(raw, &arguments); err != nil {
		return nil, err
	}
	arguments.AssetID = strings.TrimSpace(arguments.AssetID)
	arguments.DisplayName = strings.TrimSpace(arguments.DisplayName)
	if arguments.AssetID == "" || arguments.DisplayName == "" || utf8.RuneCountInString(arguments.DisplayName) > 255 {
		return nil, errors.New("asset_id and a display_name of at most 255 characters are required")
	}
	prepared, err := tool.client.PrepareRename(ctx, tool.scope.ConversationID, arguments.AssetID, arguments.DisplayName)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prepared)
}

func (tool *renameAssetTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared productflow.RenamePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("decode prepared rename: %w", err)
	}
	result, err := tool.client.ExecuteRename(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	if err == nil {
		return result, nil
	}
	var httpErr *productflow.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode >= http.StatusBadRequest && httpErr.StatusCode < http.StatusInternalServerError {
		if httpErr.StatusCode == http.StatusConflict {
			return nil, fmt.Errorf("%w: %v", durable.ErrConflict, err)
		}
		return nil, err
	}
	return nil, fmt.Errorf("%w: rename outcome requires reconciliation: %v", durable.ErrOutcomeUnknown, err)
}

func (tool *renameAssetTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	var prepared productflow.RenamePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared rename: %w", err)
	}
	result, err := tool.client.ReconcileRename(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	if err != nil {
		return durable.ReconcileResult{State: durable.ReconcileUnknown, Detail: err.Error()}, nil
	}
	state := durable.ReconcileState(result.State)
	switch state {
	case durable.ReconcileApplied, durable.ReconcileNotApplied, durable.ReconcileConflict, durable.ReconcileUnknown:
	default:
		return durable.ReconcileResult{}, fmt.Errorf("ProductFlow returned invalid reconcile state %q", result.State)
	}
	return durable.ReconcileResult{State: state, Result: result.Result, Detail: result.Detail}, nil
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false}
}

func decodeStrictObject(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("tool arguments must contain exactly one JSON object")
	}
	return nil
}

func uniqueAssetIDs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > maxInspectedAssets {
		return nil, fmt.Errorf("asset_ids must contain between 1 and %d values", maxInspectedAssets)
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("asset_ids cannot contain empty values")
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}
