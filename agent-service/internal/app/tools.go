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
	productContextToolName       = "get_product_workflow_context_v1"
	inspectWorkflowRunsToolName  = "inspect_workflow_runs_v1"
	requestWorkflowRunToolName   = "request_workflow_run_v1"
	listLegacyArchivesToolName   = "list_legacy_archives_v1"
	inspectLegacyArchiveToolName = "inspect_legacy_archive_v1"
	listAssetsToolName           = "list_product_image_assets_v2"
	inspectAssetsToolName        = "inspect_product_image_assets_v1"
	createFolderToolName         = "create_product_image_folder_v1"
	renameFolderToolName         = "rename_product_image_folder_v1"
	renameAssetToolName          = "rename_product_image_asset_v1"
	moveAssetsToolName           = "move_product_image_assets_v1"
	maxInspectedAssets           = 6
	maxListedLegacyArchives      = 50
	maxInspectedArchiveItems     = 10
	maxListedAssets              = 100
	maxMovedAssets               = 100
	maxListedWorkflowRuns        = 20
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
			Name:        inspectWorkflowRunsToolName,
			Description: "Read a bounded list of the current product workflow's recent WorkflowRun and WorkflowNodeRun statuses. This is read-only and does not start, cancel, or retry a run.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxListedWorkflowRuns},
				},
				"required": []string{"limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					Limit int `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				if arguments.Limit < 1 || arguments.Limit > maxListedWorkflowRuns {
					return "", fmt.Errorf("limit must be between 1 and %d", maxListedWorkflowRuns)
				}
				result, err := client.ListWorkflowRuns(ctx, scope.ConversationID, arguments.Limit)
				return string(result), err
			},
		},
		{
			Name:        listLegacyArchivesToolName,
			Description: "List one bounded page of legacy archive metadata. Workflow and old Agent records are scoped to this product; user templates are global. Payloads, image bytes, URLs, and run details are excluded.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"kind": map[string]any{
						"type": "string", "enum": []string{"workflow", "canvas_agent_thread", "user_template"},
					},
					"query": map[string]any{"type": "string", "maxLength": 255},
					"after": map[string]any{"type": "string", "maxLength": 4096},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxListedLegacyArchives},
				},
				"required": []string{"kind", "query", "after", "limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					Kind  string `json:"kind"`
					Query string `json:"query"`
					After string `json:"after"`
					Limit int    `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				if !validLegacyArchiveKind(arguments.Kind) {
					return "", errors.New("kind must be workflow, canvas_agent_thread, or user_template")
				}
				if arguments.Limit < 1 || arguments.Limit > maxListedLegacyArchives {
					return "", fmt.Errorf("limit must be between 1 and %d", maxListedLegacyArchives)
				}
				result, err := client.ListLegacyArchives(
					ctx, scope.ConversationID, arguments.Kind, arguments.Query, arguments.After, arguments.Limit,
				)
				return string(result), err
			},
		},
		{
			Name:        inspectLegacyArchiveToolName,
			Description: "Inspect one explicit legacy archive section with offset pagination. Read only the sections needed for redesign; never request an entire payload or all run history. Asset sections return canonical asset metadata only, after which explicitly selected images can be inspected with inspect_product_image_assets_v1.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"kind": map[string]any{
						"type": "string", "enum": []string{"workflow", "canvas_agent_thread", "user_template"},
					},
					"archive_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
					"section": map[string]any{
						"type": "string", "enum": []string{
							"summary", "product", "workflow", "nodes", "edges", "creative_briefs", "copy_sets",
							"poster_variants", "source_assets", "runs", "node_runs", "assets", "thread", "messages",
							"tool_events", "plans", "task_plans", "visible_timeline", "template", "template_nodes",
							"template_edges", "diagnostics",
						},
					},
					"offset": map[string]any{"type": "integer", "minimum": 0},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxInspectedArchiveItems},
				},
				"required": []string{"kind", "archive_id", "section", "offset", "limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					Kind      string `json:"kind"`
					ArchiveID string `json:"archive_id"`
					Section   string `json:"section"`
					Offset    int    `json:"offset"`
					Limit     int    `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				arguments.ArchiveID = strings.TrimSpace(arguments.ArchiveID)
				if !validLegacyArchiveKind(arguments.Kind) || arguments.ArchiveID == "" ||
					arguments.Offset < 0 || arguments.Limit < 1 || arguments.Limit > maxInspectedArchiveItems {
					return "", errors.New("invalid bounded legacy archive inspection arguments")
				}
				result, err := client.InspectLegacyArchive(
					ctx, scope.ConversationID, arguments.Kind, arguments.ArchiveID,
					arguments.Section, arguments.Offset, arguments.Limit,
				)
				return string(result), err
			},
		},
		{
			Name:        listAssetsToolName,
			Description: "List a bounded page of image metadata from this conversation's product. This never returns image bytes or URLs.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"directory_kind": map[string]any{
						"type": "string", "enum": []string{
							"all", "recent_generated", "uploads", "generated", "image_type", "source", "unorganized", "user_folder",
						},
					},
					"directory_key": map[string]any{"type": "string", "maxLength": 120},
					"query":         map[string]any{"type": "string", "maxLength": 255},
					"sort": map[string]any{
						"type": "string", "enum": []string{"created_desc", "created_asc", "name_asc", "name_desc"},
					},
					"after": map[string]any{"type": "string", "maxLength": 1024},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxListedAssets},
				},
				"required": []string{"directory_kind", "directory_key", "query", "sort", "after", "limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					DirectoryKind string `json:"directory_kind"`
					DirectoryKey  string `json:"directory_key"`
					Query         string `json:"query"`
					Sort          string `json:"sort"`
					After         string `json:"after"`
					Limit         int    `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				if arguments.Limit < 1 || arguments.Limit > maxListedAssets {
					return "", fmt.Errorf("limit must be between 1 and %d", maxListedAssets)
				}
				result, err := client.ListAssets(
					ctx, scope.ConversationID, arguments.DirectoryKind, arguments.DirectoryKey,
					arguments.Query, arguments.Sort, arguments.After, arguments.Limit,
				)
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

const (
	listGlobalMediaAssetsToolName        = "list_global_media_library_assets_v1"
	inspectGlobalMediaAssetsToolName     = "inspect_global_media_library_assets_v1"
	listGlobalProductsToolName           = "list_products_v1"
	inspectGlobalProductsToolName        = "inspect_products_v1"
	inspectGlobalWorkflowContextToolName = "inspect_global_workflow_context_v1"
	inspectGlobalWorkflowRunsToolName    = "inspect_global_workflow_runs_v1"
	createProductWorkspaceToolName       = "create_product_workspace_v1"
	maxListedGlobalProducts              = 100
	maxInspectedGlobalProducts           = 20
	maxInspectedGlobalWorkflows          = 20
	maxGlobalWorkflowRunsPerWorkflow     = 10
)

func scopedGlobalReadTools(client *productflow.Client, scope Scope) []agenttask.Tool {
	return []agenttask.Tool{
		{
			Name:        listGlobalProductsToolName,
			Description: "List a bounded page of ProductFlow products with the current active workflow summary. This is read-only and never returns image URLs or node configuration.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"query":  map[string]any{"type": "string", "maxLength": 255},
					"cursor": map[string]any{"type": "string", "maxLength": 4096},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxListedGlobalProducts},
				},
				"required": []string{"query", "cursor", "limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					Query  string `json:"query"`
					Cursor string `json:"cursor"`
					Limit  int    `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				if arguments.Limit < 1 || arguments.Limit > maxListedGlobalProducts {
					return "", fmt.Errorf("limit must be between 1 and %d", maxListedGlobalProducts)
				}
				result, err := client.ListGlobalProducts(
					ctx, scope.ConversationID, arguments.Query, arguments.Cursor, arguments.Limit,
				)
				if err != nil {
					return "", err
				}
				encoded, err := json.Marshal(result)
				return string(encoded), err
			},
		},
		{
			Name:        inspectGlobalProductsToolName,
			Description: "Inspect up to twenty explicitly selected products and their current active workflow summaries. This is read-only and does not edit or run a workflow.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"product_ids": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxInspectedGlobalProducts,
						"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
					},
				},
				"required": []string{"product_ids"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					ProductIDs []string `json:"product_ids"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				productIDs, err := strictProductIDs(arguments.ProductIDs)
				if err != nil {
					return "", err
				}
				result, err := client.InspectGlobalProducts(ctx, scope.ConversationID, productIDs)
				if err != nil {
					return "", err
				}
				if len(result) != len(productIDs) {
					return "", errors.New("ProductFlow returned an incomplete product inspection result")
				}
				encoded, err := json.Marshal(result)
				return string(encoded), err
			},
		},
		{
			Name:        inspectGlobalWorkflowContextToolName,
			Description: "Read the bounded editable WorkflowDraft context for one explicit product, including the exact product conversation, WorkflowDraft ID, current version, intake, facts, and reference assets. This is read-only.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"product_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
				},
				"required": []string{"product_id"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					ProductID string `json:"product_id"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				arguments.ProductID = strings.TrimSpace(arguments.ProductID)
				if arguments.ProductID == "" {
					return "", errors.New("product_id is required")
				}
				result, err := client.GlobalWorkflowContext(ctx, scope.ConversationID, arguments.ProductID)
				return string(result), err
			},
		},
		{
			Name:        inspectGlobalWorkflowRunsToolName,
			Description: "Inspect recent WorkflowRun status summaries for up to twenty explicitly selected workflows. This is read-only and never starts, cancels, retries, or returns node configuration or image URLs.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"workflow_ids": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxInspectedGlobalWorkflows,
						"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
					},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxGlobalWorkflowRunsPerWorkflow},
				},
				"required": []string{"workflow_ids", "limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					WorkflowIDs []string `json:"workflow_ids"`
					Limit       int      `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				workflowIDs, err := strictWorkflowIDs(arguments.WorkflowIDs)
				if err != nil {
					return "", err
				}
				if arguments.Limit < 1 || arguments.Limit > maxGlobalWorkflowRunsPerWorkflow {
					return "", fmt.Errorf("limit must be between 1 and %d", maxGlobalWorkflowRunsPerWorkflow)
				}
				result, err := client.InspectGlobalWorkflowRuns(
					ctx, scope.ConversationID, workflowIDs, arguments.Limit,
				)
				if err != nil {
					return "", err
				}
				encoded, err := json.Marshal(result)
				return string(encoded), err
			},
		},
		{
			Name:        listGlobalMediaAssetsToolName,
			Description: "List a bounded page of metadata from the canonical global media library. This never returns image bytes or URLs.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"query":  map[string]any{"type": "string", "maxLength": 255},
					"cursor": map[string]any{"type": "string", "maxLength": 4096},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxListedAssets},
				},
				"required": []string{"query", "cursor", "limit"},
			},
			Strict: true,
			Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var arguments struct {
					Query  string `json:"query"`
					Cursor string `json:"cursor"`
					Limit  int    `json:"limit"`
				}
				if err := decodeStrictObject(raw, &arguments); err != nil {
					return "", err
				}
				if arguments.Limit < 1 || arguments.Limit > maxListedAssets {
					return "", fmt.Errorf("limit must be between 1 and %d", maxListedAssets)
				}
				result, err := client.ListGlobalMediaAssets(
					ctx, scope.ConversationID, arguments.Query, arguments.Cursor, arguments.Limit,
				)
				if err != nil {
					return "", err
				}
				encoded, err := json.Marshal(result)
				return string(encoded), err
			},
		},
		{
			Name:        inspectGlobalMediaAssetsToolName,
			Description: "Inspect up to six explicitly selected images from the canonical global media library as native multimodal content.",
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
				metadata, err := client.InspectGlobalMediaAssets(ctx, scope.ConversationID, assetIDs)
				if err != nil {
					return agenttask.ToolResult{}, err
				}
				if len(metadata) != len(assetIDs) {
					return agenttask.ToolResult{}, errors.New("ProductFlow returned an incomplete global asset inspection result")
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
						return agenttask.ToolResult{}, fmt.Errorf("ProductFlow omitted requested global asset %s", assetID)
					}
					image, err := client.GlobalMediaAssetContent(ctx, scope.ConversationID, assetID)
					if err != nil {
						return agenttask.ToolResult{}, err
					}
					totalBytes += image.SizeBytes
					if totalBytes > agenttask.MaxTotalToolResultImageBytes {
						return agenttask.ToolResult{}, errors.New("selected global asset bytes exceed the tool result limit")
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

func scopedGlobalDurableTools(client *productflow.Client, scope Scope) []agenttask.DurableTool {
	return []agenttask.DurableTool{
		{
			Description: "Create an interactive product onboarding workspace under the current Agent Session. This creates only a blank product draft and never uploads images, submits intake, generates a workflow, or starts a run.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
				},
				"required": []string{"name"},
			},
			Tool: &createProductWorkspaceTool{client: client, scope: scope},
		},
		{
			Description: "Prepare a request to run one explicitly selected product's active workflow. This never starts the workflow; a human must confirm the request in ProductFlow.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"product_id":                 map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
					"workflow_id":                map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
					"expected_workflow_revision": map[string]any{"type": "integer", "minimum": 1},
				},
				"required": []string{"product_id", "workflow_id", "expected_workflow_revision"},
			},
			Tool: &requestGlobalWorkflowRunTool{client: client, scope: scope},
		},
	}
}

func scopedDurableTools(client *productflow.Client, scope Scope) []agenttask.DurableTool {
	return []agenttask.DurableTool{
		{
			Description: "Prepare a request to run the current active workflow. This never starts the workflow; a human must confirm the request in ProductFlow.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"expected_workflow_revision": map[string]any{"type": "integer", "minimum": 1},
				},
				"required": []string{"expected_workflow_revision"},
			},
			Tool: &requestWorkflowRunTool{client: client, scope: scope},
		},
		{
			Description: "Create one top-level image folder in this conversation's product gallery.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
				},
				"required": []string{"name"},
			},
			Tool: &createFolderTool{client: client, scope: scope},
		},
		{
			Description: "Rename one top-level image folder in this conversation's product gallery.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"folder_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 36},
					"name":      map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
				},
				"required": []string{"folder_id", "name"},
			},
			Tool: &renameFolderTool{client: client, scope: scope},
		},
		{
			Description: "Rename one image asset's display name within this conversation's product. The asset file and lineage are unchanged.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"asset_id":     map[string]any{"type": "string", "minLength": 1, "maxLength": 36},
					"display_name": map[string]any{"type": "string", "minLength": 1, "maxLength": 255},
				},
				"required": []string{"asset_id", "display_name"},
			},
			Tool: &renameAssetTool{client: client, scope: scope},
		},
		{
			Description: "Move one or more image assets to one top-level folder, or to the unorganized directory when folder_id is null.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"asset_ids": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxMovedAssets,
						"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 36},
					},
					"folder_id": map[string]any{"type": []string{"string", "null"}, "maxLength": 36},
				},
				"required": []string{"asset_ids", "folder_id"},
			},
			Tool: &moveAssetsTool{client: client, scope: scope},
		},
	}
}

type createProductWorkspaceTool struct {
	client *productflow.Client
	scope  Scope
}

func (*createProductWorkspaceTool) Name() string                { return createProductWorkspaceToolName }
func (*createProductWorkspaceTool) Effect() durable.EffectClass { return durable.EffectReconcilable }

func (tool *createProductWorkspaceTool) Prepare(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var arguments struct {
		Name string `json:"name"`
	}
	if err := decodeStrictObject(raw, &arguments); err != nil {
		return nil, err
	}
	arguments.Name = strings.TrimSpace(arguments.Name)
	if arguments.Name == "" || utf8.RuneCountInString(arguments.Name) > 255 {
		return nil, errors.New("a product name of at most 255 characters is required")
	}
	return json.Marshal(arguments)
}

func (tool *createProductWorkspaceTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("decode prepared product workspace: %w", err)
	}
	result, err := tool.client.CreateAgentProductWorkspace(
		ctx,
		tool.scope.ConversationID,
		invocation.IdempotencyKey,
		prepared.Name,
	)
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return durableExecutionResult(encoded, err, "product workspace create")
}

func (tool *createProductWorkspaceTool) Reconcile(
	ctx context.Context,
	invocation durable.Invocation,
) (durable.ReconcileResult, error) {
	var prepared struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared product workspace: %w", err)
	}
	result, err := tool.client.CreateAgentProductWorkspace(
		ctx,
		tool.scope.ConversationID,
		invocation.IdempotencyKey,
		prepared.Name,
	)
	if err == nil {
		encoded, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return durable.ReconcileResult{}, marshalErr
		}
		return durable.ReconcileResult{State: durable.ReconcileApplied, Result: encoded}, nil
	}
	var httpErr *productflow.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode >= http.StatusBadRequest && httpErr.StatusCode < http.StatusInternalServerError {
		state := durable.ReconcileNotApplied
		if httpErr.StatusCode == http.StatusConflict {
			state = durable.ReconcileConflict
		}
		return durable.ReconcileResult{State: state, Detail: err.Error()}, nil
	}
	return durable.ReconcileResult{State: durable.ReconcileUnknown, Detail: err.Error()}, nil
}

type requestWorkflowRunTool struct {
	client *productflow.Client
	scope  Scope
}

type requestGlobalWorkflowRunTool struct {
	client *productflow.Client
	scope  Scope
}

func (*requestGlobalWorkflowRunTool) Name() string                { return requestWorkflowRunToolName }
func (*requestGlobalWorkflowRunTool) Effect() durable.EffectClass { return durable.EffectReconcilable }

func (tool *requestGlobalWorkflowRunTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var arguments struct {
		ProductID                string `json:"product_id"`
		WorkflowID               string `json:"workflow_id"`
		ExpectedWorkflowRevision int    `json:"expected_workflow_revision"`
	}
	if err := decodeStrictObject(raw, &arguments); err != nil {
		return nil, err
	}
	arguments.ProductID = strings.TrimSpace(arguments.ProductID)
	arguments.WorkflowID = strings.TrimSpace(arguments.WorkflowID)
	if arguments.ProductID == "" || arguments.WorkflowID == "" {
		return nil, errors.New("product_id and workflow_id are required")
	}
	if arguments.ExpectedWorkflowRevision < 1 {
		return nil, errors.New("expected_workflow_revision must be at least 1")
	}
	var taskID *string
	if tool.scope.TaskID != "" {
		value := tool.scope.TaskID
		taskID = &value
	}
	prepared, err := tool.client.PrepareGlobalWorkflowRunRequest(
		ctx,
		tool.scope.ConversationID,
		arguments.ProductID,
		arguments.WorkflowID,
		arguments.ExpectedWorkflowRevision,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prepared)
}

func (tool *requestGlobalWorkflowRunTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared productflow.WorkflowRunRequestPrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("decode prepared global workflow run request: %w", err)
	}
	result, err := tool.client.ExecuteGlobalWorkflowRunRequest(
		ctx,
		tool.scope.ConversationID,
		invocation.IdempotencyKey,
		invocation.StepID,
		prepared,
	)
	return durableExecutionResult(result, err, "global workflow run request")
}

func (tool *requestGlobalWorkflowRunTool) Reconcile(
	ctx context.Context,
	invocation durable.Invocation,
) (durable.ReconcileResult, error) {
	var prepared productflow.WorkflowRunRequestPrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared global workflow run request: %w", err)
	}
	result, err := tool.client.ReconcileGlobalWorkflowRunRequest(
		ctx,
		tool.scope.ConversationID,
		invocation.IdempotencyKey,
		invocation.StepID,
		prepared,
	)
	return durableReconcileResult(result, err)
}

func (*requestWorkflowRunTool) Name() string                { return requestWorkflowRunToolName }
func (*requestWorkflowRunTool) Effect() durable.EffectClass { return durable.EffectReconcilable }

func (tool *requestWorkflowRunTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var arguments struct {
		ExpectedWorkflowRevision int `json:"expected_workflow_revision"`
	}
	if err := decodeStrictObject(raw, &arguments); err != nil {
		return nil, err
	}
	if arguments.ExpectedWorkflowRevision < 1 {
		return nil, errors.New("expected_workflow_revision must be at least 1")
	}
	var taskID *string
	if tool.scope.TaskID != "" {
		value := tool.scope.TaskID
		taskID = &value
	}
	prepared, err := tool.client.PrepareWorkflowRunRequest(
		ctx,
		tool.scope.ConversationID,
		arguments.ExpectedWorkflowRevision,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prepared)
}

func (tool *requestWorkflowRunTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared productflow.WorkflowRunRequestPrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("decode prepared workflow run request: %w", err)
	}
	result, err := tool.client.ExecuteWorkflowRunRequest(
		ctx,
		tool.scope.ConversationID,
		invocation.IdempotencyKey,
		invocation.StepID,
		prepared,
	)
	return durableExecutionResult(result, err, "workflow run request")
}

func (tool *requestWorkflowRunTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	var prepared productflow.WorkflowRunRequestPrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared workflow run request: %w", err)
	}
	result, err := tool.client.ReconcileWorkflowRunRequest(
		ctx,
		tool.scope.ConversationID,
		invocation.IdempotencyKey,
		invocation.StepID,
		prepared,
	)
	return durableReconcileResult(result, err)
}

type createFolderTool struct {
	client *productflow.Client
	scope  Scope
}

func (*createFolderTool) Name() string                { return createFolderToolName }
func (*createFolderTool) Effect() durable.EffectClass { return durable.EffectReconcilable }

func (tool *createFolderTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var arguments struct {
		Name string `json:"name"`
	}
	if err := decodeStrictObject(raw, &arguments); err != nil {
		return nil, err
	}
	arguments.Name = strings.TrimSpace(arguments.Name)
	if arguments.Name == "" || utf8.RuneCountInString(arguments.Name) > 120 {
		return nil, errors.New("a folder name of at most 120 characters is required")
	}
	prepared, err := tool.client.PrepareFolderCreate(ctx, tool.scope.ConversationID, arguments.Name)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prepared)
}

func (tool *createFolderTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared productflow.FolderCreatePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("decode prepared folder create: %w", err)
	}
	result, err := tool.client.ExecuteFolderCreate(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	return durableExecutionResult(result, err, "folder create")
}

func (tool *createFolderTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	var prepared productflow.FolderCreatePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared folder create: %w", err)
	}
	result, err := tool.client.ReconcileFolderCreate(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	return durableReconcileResult(result, err)
}

type renameFolderTool struct {
	client *productflow.Client
	scope  Scope
}

func (*renameFolderTool) Name() string                { return renameFolderToolName }
func (*renameFolderTool) Effect() durable.EffectClass { return durable.EffectReconcilable }

func (tool *renameFolderTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var arguments struct {
		FolderID string `json:"folder_id"`
		Name     string `json:"name"`
	}
	if err := decodeStrictObject(raw, &arguments); err != nil {
		return nil, err
	}
	arguments.FolderID = strings.TrimSpace(arguments.FolderID)
	arguments.Name = strings.TrimSpace(arguments.Name)
	if arguments.FolderID == "" || arguments.Name == "" || utf8.RuneCountInString(arguments.Name) > 120 {
		return nil, errors.New("folder_id and a folder name of at most 120 characters are required")
	}
	prepared, err := tool.client.PrepareFolderRename(
		ctx, tool.scope.ConversationID, arguments.FolderID, arguments.Name,
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prepared)
}

func (tool *renameFolderTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared productflow.FolderRenamePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("decode prepared folder rename: %w", err)
	}
	result, err := tool.client.ExecuteFolderRename(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	return durableExecutionResult(result, err, "folder rename")
}

func (tool *renameFolderTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	var prepared productflow.FolderRenamePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared folder rename: %w", err)
	}
	result, err := tool.client.ReconcileFolderRename(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	return durableReconcileResult(result, err)
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
	return durableExecutionResult(result, err, "asset rename")
}

func (tool *renameAssetTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	var prepared productflow.RenamePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared rename: %w", err)
	}
	result, err := tool.client.ReconcileRename(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	return durableReconcileResult(result, err)
}

type moveAssetsTool struct {
	client *productflow.Client
	scope  Scope
}

func (*moveAssetsTool) Name() string                { return moveAssetsToolName }
func (*moveAssetsTool) Effect() durable.EffectClass { return durable.EffectReconcilable }

func (tool *moveAssetsTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var arguments struct {
		AssetIDs []string `json:"asset_ids"`
		FolderID *string  `json:"folder_id"`
	}
	if err := decodeStrictObject(raw, &arguments); err != nil {
		return nil, err
	}
	assetIDs, err := strictUniqueIDs(arguments.AssetIDs, maxMovedAssets)
	if err != nil {
		return nil, err
	}
	if arguments.FolderID != nil {
		normalized := strings.TrimSpace(*arguments.FolderID)
		if normalized == "" {
			return nil, errors.New("folder_id cannot be empty")
		}
		arguments.FolderID = &normalized
	}
	prepared, err := tool.client.PrepareAssetMove(
		ctx, tool.scope.ConversationID, assetIDs, arguments.FolderID,
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prepared)
}

func (tool *moveAssetsTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared productflow.AssetMovePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("decode prepared asset move: %w", err)
	}
	result, err := tool.client.ExecuteAssetMove(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	return durableExecutionResult(result, err, "asset move")
}

func (tool *moveAssetsTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	var prepared productflow.AssetMovePrepared
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return durable.ReconcileResult{}, fmt.Errorf("decode prepared asset move: %w", err)
	}
	result, err := tool.client.ReconcileAssetMove(ctx, tool.scope.ConversationID, invocation.IdempotencyKey, prepared)
	return durableReconcileResult(result, err)
}

func durableExecutionResult(result json.RawMessage, err error, operation string) (json.RawMessage, error) {
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
	return nil, fmt.Errorf("%w: %s outcome requires reconciliation: %v", durable.ErrOutcomeUnknown, operation, err)
}

func durableReconcileResult(result productflow.ReconcileResult, err error) (durable.ReconcileResult, error) {
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
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func validLegacyArchiveKind(value string) bool {
	switch value {
	case "workflow", "canvas_agent_thread", "user_template":
		return true
	default:
		return false
	}
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

func strictUniqueIDs(values []string, maximum int) ([]string, error) {
	if len(values) == 0 || len(values) > maximum {
		return nil, fmt.Errorf("asset_ids must contain between 1 and %d values", maximum)
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("asset_ids cannot contain empty values")
		}
		if seen[value] {
			return nil, errors.New("asset_ids cannot contain duplicate values")
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func strictProductIDs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > maxInspectedGlobalProducts {
		return nil, fmt.Errorf("product_ids must contain between 1 and %d values", maxInspectedGlobalProducts)
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("product_ids cannot contain empty values")
		}
		if seen[value] {
			return nil, errors.New("product_ids cannot contain duplicate values")
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func strictWorkflowIDs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > maxInspectedGlobalWorkflows {
		return nil, fmt.Errorf("workflow_ids must contain between 1 and %d values", maxInspectedGlobalWorkflows)
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("workflow_ids cannot contain empty values")
		}
		if seen[value] {
			return nil, errors.New("workflow_ids cannot contain duplicate values")
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}
