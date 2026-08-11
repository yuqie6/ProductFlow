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
	ConversationID      string         `json:"conversation_id"`
	ProductID           string         `json:"product_id"`
	WorkflowDraftID     string         `json:"workflow_draft_id"`
	HarnessRunID        string         `json:"harness_run_id"`
	CurrentDraftVersion int            `json:"current_draft_version"`
	SystemPrompt        string         `json:"system_prompt"`
	WorkflowDraftSchema map[string]any `json:"workflow_draft_schema"`
	ToolContractVersion int            `json:"tool_contract_version"`
}

type AssetMetadata struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	OriginalFilename   string `json:"original_filename"`
	OriginType         string `json:"origin_type"`
	MIMEType           string `json:"mime_type"`
	ByteSize           int64  `json:"byte_size"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	VerificationStatus string `json:"verification_status"`
	CreatedAt          string `json:"created_at"`
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

type ReconcileResult struct {
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Detail string          `json:"detail,omitempty"`
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

func (client *Client) ProductContext(ctx context.Context, conversationID string) (json.RawMessage, error) {
	var result json.RawMessage
	err := client.json(ctx, http.MethodGet, client.conversationPath(conversationID)+"/product-context", nil, &result, "")
	return result, err
}

func (client *Client) ListAssets(
	ctx context.Context,
	conversationID, query, cursor string,
	limit int,
) (AssetList, error) {
	values := url.Values{}
	if strings.TrimSpace(query) != "" {
		values.Set("query", strings.TrimSpace(query))
	}
	if strings.TrimSpace(cursor) != "" {
		values.Set("after", strings.TrimSpace(cursor))
	}
	values.Set("limit", strconv.Itoa(limit))
	path := client.conversationPath(conversationID) + "/assets?" + values.Encode()
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

func (client *Client) AssetContent(ctx context.Context, conversationID, assetID string) (AssetContent, error) {
	request, err := client.request(ctx, http.MethodGet, client.conversationPath(conversationID)+"/assets/"+url.PathEscape(assetID)+"/content", nil)
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

func (client *Client) conversationPath(conversationID string) string {
	return "/api/internal/v1/agent-conversations/" + url.PathEscape(conversationID)
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
