package productflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxErrorBodyBytes = 8 << 10
	maxJSONBodyBytes  = 2 << 20
	maxImageBytes     = 20 << 20
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type Contract struct {
	SchemaVersion       int            `json:"schema_version"`
	ScopeType           string         `json:"scope_type"`
	ConversationID      string         `json:"conversation_id"`
	TaskID              *string        `json:"task_id"`
	TaskGoal            *string        `json:"task_goal"`
	ProductID           *string        `json:"product_id"`
	WorkflowDraftID     *string        `json:"workflow_draft_id"`
	HarnessRunID        string         `json:"harness_run_id"`
	CurrentDraftVersion int            `json:"current_draft_version"`
	SystemPrompt        string         `json:"system_prompt"`
	DraftKind           string         `json:"draft_kind"`
	DraftSchema         map[string]any `json:"draft_schema"`
	WorkflowDraftSchema map[string]any `json:"workflow_draft_schema"`
	ToolContractVersion int            `json:"tool_contract_version"`
}

type AgentProviderConfig struct {
	SchemaVersion    int     `json:"schema_version"`
	ProviderKind     string  `json:"provider_kind"`
	APIKey           string  `json:"api_key"`
	BaseURL          *string `json:"base_url"`
	Model            string  `json:"model"`
	ReasoningEffort  *string `json:"reasoning_effort"`
	ReasoningSummary *string `json:"reasoning_summary"`
	TextVerbosity    *string `json:"text_verbosity"`
	ServiceTier      *string `json:"service_tier"`
}

type AssetMetadata struct {
	ID                 string          `json:"id"`
	DisplayName        string          `json:"display_name"`
	OriginalFilename   string          `json:"original_filename"`
	OriginType         string          `json:"origin_type"`
	ImageTypeKey       *string         `json:"image_type_key"`
	ImageTypeTitle     *string         `json:"image_type_title"`
	UserFolderID       *string         `json:"user_folder_id"`
	UserFolderName     *string         `json:"user_folder_name"`
	MIMEType           string          `json:"mime_type"`
	ByteSize           *int64          `json:"byte_size"`
	Width              *int            `json:"width"`
	Height             *int            `json:"height"`
	VerificationStatus string          `json:"verification_status"`
	ParentAssetID      *string         `json:"parent_asset_id"`
	Generation         json.RawMessage `json:"generation"`
	CreatedAt          string          `json:"created_at"`
}

type AssetList struct {
	Items      []AssetMetadata `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type AssetContent struct {
	Data      []byte
	MediaType string
	SizeBytes int64
}

type RenamePrepared struct {
	AssetID             string `json:"asset_id"`
	ExpectedDisplayName string `json:"expected_display_name"`
	TargetDisplayName   string `json:"target_display_name"`
}

type RenameResult struct {
	AssetID     string `json:"asset_id"`
	DisplayName string `json:"display_name"`
	Applied     bool   `json:"applied"`
}

type FolderCreatePrepared struct {
	FolderID string `json:"folder_id"`
	Name     string `json:"name"`
}

type FolderRenamePrepared struct {
	FolderID     string `json:"folder_id"`
	ExpectedName string `json:"expected_name"`
	TargetName   string `json:"target_name"`
}

type AssetMoveItem struct {
	AssetID          string  `json:"asset_id"`
	ExpectedFolderID *string `json:"expected_folder_id"`
}

type AssetMovePrepared struct {
	Moves          []AssetMoveItem `json:"moves"`
	TargetFolderID *string         `json:"target_folder_id"`
}

type ReconcileResult struct {
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Detail string          `json:"detail,omitempty"`
}

type WorkflowRunRequestPrepared struct {
	ProductID         string  `json:"product_id"`
	WorkflowID        string  `json:"workflow_id"`
	WorkflowTitle     string  `json:"workflow_title"`
	WorkflowRevision  int     `json:"workflow_revision"`
	RunnableNodeCount int     `json:"runnable_node_count"`
	TaskID            *string `json:"task_id"`
}

type WorkflowRunRequest struct {
	ID                       string  `json:"id"`
	ConversationID           string  `json:"conversation_id"`
	TaskID                   *string `json:"task_id"`
	ProductID                string  `json:"product_id"`
	WorkflowID               string  `json:"workflow_id"`
	WorkflowTitle            string  `json:"workflow_title"`
	ExpectedWorkflowRevision int     `json:"expected_workflow_revision"`
	Status                   string  `json:"status"`
	WorkflowRunID            *string `json:"workflow_run_id"`
	WorkflowRunStatus        *string `json:"workflow_run_status"`
	SourceStepID             string  `json:"source_step_id"`
	FailureReason            *string `json:"failure_reason"`
	ConfirmedAt              *string `json:"confirmed_at"`
	FinishedAt               *string `json:"finished_at"`
	CreatedAt                string  `json:"created_at"`
	UpdatedAt                string  `json:"updated_at"`
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
}

func (err *HTTPError) Error() string {
	return fmt.Sprintf("ProductFlow internal API returned %d %s: %s", err.StatusCode, err.Code, err.Message)
}

func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	token = strings.TrimSpace(token)
	if baseURL == "" || token == "" || httpClient == nil {
		return nil, errors.New("ProductFlow client requires base URL, token, and HTTP client")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("ProductFlow base URL must be absolute HTTP(S)")
	}
	return &Client{baseURL: baseURL, token: token, http: httpClient}, nil
}

func (client *Client) Contract(ctx context.Context, conversationID string) (Contract, error) {
	var result Contract
	err := client.json(ctx, http.MethodGet, client.conversationPath(conversationID)+"/contract", nil, &result, "")
	return result, err
}

func (client *Client) TaskContract(ctx context.Context, taskID string) (Contract, error) {
	var result Contract
	err := client.json(ctx, http.MethodGet, client.taskPath(taskID)+"/contract", nil, &result, "")
	return result, err
}

func (client *Client) AgentProviderConfig(ctx context.Context) (AgentProviderConfig, error) {
	var result AgentProviderConfig
	err := client.json(ctx, http.MethodGet, "/api/internal/v1/agent-runtime/provider-config", nil, &result, "")
	return result, err
}

func (client *Client) ProductContext(ctx context.Context, conversationID string) (json.RawMessage, error) {
	var result json.RawMessage
	err := client.json(ctx, http.MethodGet, client.conversationPath(conversationID)+"/product-context", nil, &result, "")
	return result, err
}

func (client *Client) ListWorkflowRuns(ctx context.Context, conversationID string, limit int) (json.RawMessage, error) {
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	var result json.RawMessage
	err := client.json(
		ctx,
		http.MethodGet,
		client.conversationPath(conversationID)+"/workflow-runs?"+values.Encode(),
		nil,
		&result,
		"",
	)
	return result, err
}

func (client *Client) PrepareWorkflowRunRequest(
	ctx context.Context,
	conversationID string,
	expectedWorkflowRevision int,
	taskID *string,
) (WorkflowRunRequestPrepared, error) {
	var result WorkflowRunRequestPrepared
	err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/workflow-run-requests/prepare",
		map[string]any{
			"expected_workflow_revision": expectedWorkflowRevision,
			"task_id":                    taskID,
		},
		&result,
		"",
	)
	return result, err
}

func (client *Client) ExecuteWorkflowRunRequest(
	ctx context.Context,
	conversationID, idempotencyKey, sourceStepID string,
	prepared WorkflowRunRequestPrepared,
) (json.RawMessage, error) {
	var result json.RawMessage
	err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/workflow-run-requests",
		workflowRunRequestPayload(prepared, sourceStepID),
		&result,
		idempotencyKey,
	)
	return result, err
}

func (client *Client) ReconcileWorkflowRunRequest(
	ctx context.Context,
	conversationID, idempotencyKey, sourceStepID string,
	prepared WorkflowRunRequestPrepared,
) (ReconcileResult, error) {
	var result ReconcileResult
	err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/workflow-run-requests/reconcile",
		workflowRunRequestPayload(prepared, sourceStepID),
		&result,
		idempotencyKey,
	)
	return result, err
}

func workflowRunRequestPayload(prepared WorkflowRunRequestPrepared, sourceStepID string) map[string]any {
	return map[string]any{
		"expected_workflow_revision": prepared.WorkflowRevision,
		"workflow_id":                prepared.WorkflowID,
		"source_step_id":             sourceStepID,
		"task_id":                    prepared.TaskID,
	}
}

func (client *Client) ListLegacyArchives(
	ctx context.Context,
	conversationID, kind, query, cursor string,
	limit int,
) (json.RawMessage, error) {
	values := url.Values{}
	values.Set("kind", strings.TrimSpace(kind))
	if strings.TrimSpace(query) != "" {
		values.Set("query", strings.TrimSpace(query))
	}
	if strings.TrimSpace(cursor) != "" {
		values.Set("after", strings.TrimSpace(cursor))
	}
	values.Set("limit", strconv.Itoa(limit))
	path := client.conversationPath(conversationID) + "/legacy-archives?" + values.Encode()
	var result json.RawMessage
	err := client.json(ctx, http.MethodGet, path, nil, &result, "")
	return result, err
}

func (client *Client) InspectLegacyArchive(
	ctx context.Context,
	conversationID, kind, archiveID, section string,
	offset, limit int,
) (json.RawMessage, error) {
	body := map[string]any{
		"kind": kind, "archive_id": archiveID, "section": section,
		"offset": offset, "limit": limit,
	}
	var result json.RawMessage
	err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/legacy-archives/inspect",
		body,
		&result,
		"",
	)
	return result, err
}

func (client *Client) ValidateWorkflowDraft(
	ctx context.Context,
	conversationID string,
	value json.RawMessage,
) error {
	if !json.Valid(value) {
		return errors.New("workflow draft validation value must be valid JSON")
	}
	var result struct {
		Accepted bool `json:"accepted"`
	}
	if err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/workflow-draft/validate",
		map[string]any{"value": value},
		&result,
		"",
	); err != nil {
		return err
	}
	if !result.Accepted {
		return errors.New("ProductFlow rejected workflow draft without an error")
	}
	return nil
}

func (client *Client) ValidateLibraryOrganizationDraft(
	ctx context.Context,
	conversationID string,
	value json.RawMessage,
) error {
	if !json.Valid(value) {
		return errors.New("library organization draft validation value must be valid JSON")
	}
	var result struct {
		Accepted bool `json:"accepted"`
	}
	if err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/library-organization-draft/validate",
		map[string]any{"value": value},
		&result,
		"",
	); err != nil {
		return err
	}
	if !result.Accepted {
		return errors.New("ProductFlow rejected library organization draft without an error")
	}
	return nil
}

func (client *Client) ListAssets(
	ctx context.Context,
	conversationID, directoryKind, directoryKey, query, sort, cursor string,
	limit int,
) (AssetList, error) {
	values := url.Values{}
	values.Set("directory_kind", strings.TrimSpace(directoryKind))
	if strings.TrimSpace(directoryKey) != "" {
		values.Set("directory_key", strings.TrimSpace(directoryKey))
	}
	if strings.TrimSpace(query) != "" {
		values.Set("query", strings.TrimSpace(query))
	}
	if strings.TrimSpace(cursor) != "" {
		values.Set("after", strings.TrimSpace(cursor))
	}
	values.Set("sort", strings.TrimSpace(sort))
	values.Set("limit", strconv.Itoa(limit))
	path := client.conversationPath(conversationID) + "/assets?" + values.Encode()
	var result AssetList
	err := client.json(ctx, http.MethodGet, path, nil, &result, "")
	return result, err
}

func (client *Client) ListGlobalMediaAssets(
	ctx context.Context,
	conversationID, query, cursor string,
	limit int,
) (AssetList, error) {
	values := url.Values{}
	if strings.TrimSpace(query) != "" {
		values.Set("query", strings.TrimSpace(query))
	}
	if strings.TrimSpace(cursor) != "" {
		values.Set("cursor", strings.TrimSpace(cursor))
	}
	values.Set("limit", strconv.Itoa(limit))
	path := client.conversationPath(conversationID) + "/media-library?" + values.Encode()
	var result AssetList
	err := client.json(ctx, http.MethodGet, path, nil, &result, "")
	return result, err
}

func (client *Client) InspectAssets(
	ctx context.Context,
	conversationID string,
	assetIDs []string,
) ([]AssetMetadata, error) {
	body := map[string]any{"asset_ids": assetIDs}
	var result struct {
		Items []AssetMetadata `json:"items"`
	}
	err := client.json(ctx, http.MethodPost, client.conversationPath(conversationID)+"/assets/inspect", body, &result, "")
	return result.Items, err
}

func (client *Client) InspectGlobalMediaAssets(
	ctx context.Context,
	conversationID string,
	assetIDs []string,
) ([]AssetMetadata, error) {
	body := map[string]any{"asset_ids": assetIDs}
	var result struct {
		Items []AssetMetadata `json:"items"`
	}
	err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/media-library/inspect",
		body,
		&result,
		"",
	)
	return result.Items, err
}

func (client *Client) AssetContent(ctx context.Context, conversationID, assetID string) (AssetContent, error) {
	return client.assetContent(
		ctx,
		client.conversationPath(conversationID)+"/assets/"+url.PathEscape(assetID)+"/content",
	)
}

func (client *Client) GlobalMediaAssetContent(ctx context.Context, conversationID, assetID string) (AssetContent, error) {
	return client.assetContent(
		ctx,
		client.conversationPath(conversationID)+"/media-library/"+url.PathEscape(assetID)+"/content",
	)
}

func (client *Client) assetContent(ctx context.Context, path string) (AssetContent, error) {
	request, err := client.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return AssetContent{}, err
	}
	request.Header.Set("Accept", "image/png, image/jpeg, image/webp")
	response, err := client.http.Do(request)
	if err != nil {
		return AssetContent{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return AssetContent{}, decodeHTTPError(response)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !supportedImageMediaType(mediaType) {
		return AssetContent{}, fmt.Errorf("ProductFlow asset content has unsupported media type %q", response.Header.Get("Content-Type"))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes+1))
	if err != nil {
		return AssetContent{}, err
	}
	if len(data) == 0 || len(data) > maxImageBytes {
		return AssetContent{}, fmt.Errorf("ProductFlow asset content size %d is outside the supported range", len(data))
	}
	return AssetContent{Data: data, MediaType: mediaType, SizeBytes: int64(len(data))}, nil
}

func (client *Client) PrepareRename(
	ctx context.Context,
	conversationID, assetID, targetDisplayName string,
) (RenamePrepared, error) {
	body := map[string]any{"asset_id": assetID, "target_display_name": targetDisplayName}
	var result RenamePrepared
	err := client.json(ctx, http.MethodPost, client.conversationPath(conversationID)+"/asset-renames/prepare", body, &result, "")
	return result, err
}

func (client *Client) ExecuteRename(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared RenamePrepared,
) (json.RawMessage, error) {
	var result json.RawMessage
	err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/asset-renames",
		prepared,
		&result,
		idempotencyKey,
	)
	return result, err
}

func (client *Client) ReconcileRename(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared RenamePrepared,
) (ReconcileResult, error) {
	var result ReconcileResult
	err := client.json(
		ctx,
		http.MethodPost,
		client.conversationPath(conversationID)+"/asset-renames/reconcile",
		prepared,
		&result,
		idempotencyKey,
	)
	return result, err
}

func (client *Client) PrepareFolderCreate(
	ctx context.Context,
	conversationID, name string,
) (FolderCreatePrepared, error) {
	var result FolderCreatePrepared
	err := client.json(
		ctx, http.MethodPost, client.conversationPath(conversationID)+"/folder-creates/prepare",
		map[string]any{"name": name}, &result, "",
	)
	return result, err
}

func (client *Client) ExecuteFolderCreate(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared FolderCreatePrepared,
) (json.RawMessage, error) {
	return client.executeMutation(ctx, conversationID, "folder-creates", idempotencyKey, prepared)
}

func (client *Client) ReconcileFolderCreate(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared FolderCreatePrepared,
) (ReconcileResult, error) {
	return client.reconcileMutation(ctx, conversationID, "folder-creates", idempotencyKey, prepared)
}

func (client *Client) PrepareFolderRename(
	ctx context.Context,
	conversationID, folderID, targetName string,
) (FolderRenamePrepared, error) {
	var result FolderRenamePrepared
	err := client.json(
		ctx, http.MethodPost, client.conversationPath(conversationID)+"/folder-renames/prepare",
		map[string]any{"folder_id": folderID, "target_name": targetName}, &result, "",
	)
	return result, err
}

func (client *Client) ExecuteFolderRename(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared FolderRenamePrepared,
) (json.RawMessage, error) {
	return client.executeMutation(ctx, conversationID, "folder-renames", idempotencyKey, prepared)
}

func (client *Client) ReconcileFolderRename(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared FolderRenamePrepared,
) (ReconcileResult, error) {
	return client.reconcileMutation(ctx, conversationID, "folder-renames", idempotencyKey, prepared)
}

func (client *Client) PrepareAssetMove(
	ctx context.Context,
	conversationID string,
	assetIDs []string,
	targetFolderID *string,
) (AssetMovePrepared, error) {
	var result AssetMovePrepared
	err := client.json(
		ctx, http.MethodPost, client.conversationPath(conversationID)+"/asset-moves/prepare",
		map[string]any{"asset_ids": assetIDs, "target_folder_id": targetFolderID}, &result, "",
	)
	return result, err
}

func (client *Client) ExecuteAssetMove(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared AssetMovePrepared,
) (json.RawMessage, error) {
	return client.executeMutation(ctx, conversationID, "asset-moves", idempotencyKey, prepared)
}

func (client *Client) ReconcileAssetMove(
	ctx context.Context,
	conversationID, idempotencyKey string,
	prepared AssetMovePrepared,
) (ReconcileResult, error) {
	return client.reconcileMutation(ctx, conversationID, "asset-moves", idempotencyKey, prepared)
}

func (client *Client) executeMutation(
	ctx context.Context,
	conversationID, operation, idempotencyKey string,
	prepared any,
) (json.RawMessage, error) {
	var result json.RawMessage
	err := client.json(
		ctx, http.MethodPost, client.conversationPath(conversationID)+"/"+operation,
		prepared, &result, idempotencyKey,
	)
	return result, err
}

func (client *Client) reconcileMutation(
	ctx context.Context,
	conversationID, operation, idempotencyKey string,
	prepared any,
) (ReconcileResult, error) {
	var result ReconcileResult
	err := client.json(
		ctx, http.MethodPost, client.conversationPath(conversationID)+"/"+operation+"/reconcile",
		prepared, &result, idempotencyKey,
	)
	return result, err
}

func (client *Client) conversationPath(conversationID string) string {
	return "/api/internal/v1/agent-conversations/" + url.PathEscape(conversationID)
}

func (client *Client) taskPath(taskID string) string {
	return "/api/internal/v1/agent-tasks/" + url.PathEscape(taskID)
}

func (client *Client) json(
	ctx context.Context,
	method, path string,
	body any,
	target any,
	idempotencyKey string,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := client.request(ctx, method, path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeHTTPError(response)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxJSONBodyBytes+1))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode ProductFlow internal response: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("ProductFlow internal response contains trailing JSON")
	}
	return nil
}

func (client *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func decodeHTTPError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
	var envelope struct {
		Detail string `json:"detail"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &envelope)
	message := strings.TrimSpace(envelope.Detail)
	if message == "" {
		message = strings.TrimSpace(envelope.Error.Message)
	}
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return &HTTPError{StatusCode: response.StatusCode, Code: envelope.Error.Code, Message: message}
}

func supportedImageMediaType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}
