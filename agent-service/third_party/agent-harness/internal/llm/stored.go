package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
)

// StoredResponse identifies a provider-side Responses object. Complete is set
// only after RetrieveStored parsed a completed assistant message.
type StoredResponse struct {
	ID       string
	Status   string
	Message  Message
	Complete bool
}

// StoredTerminalError means the provider returned a durable terminal state or
// a request error that retrying GET cannot repair.
type StoredTerminalError struct {
	Status string
	Err    error
}

func (e *StoredTerminalError) Error() string { return e.Err.Error() }
func (e *StoredTerminalError) Unwrap() error { return e.Err }

func IsStoredTerminal(err error) bool {
	var terminal *StoredTerminalError
	return errors.As(err, &terminal)
}

// RequestError records whether an HTTP request may have reached the provider.
// Ambiguous errors must not trigger another response.create automatically.
type RequestError struct {
	Operation string
	Ambiguous bool
	Err       error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("%s: %v", e.Operation, e.Err)
}

func (e *RequestError) Unwrap() error { return e.Err }

func IsAmbiguousRequest(err error) bool {
	var requestErr *RequestError
	return errors.As(err, &requestErr) && requestErr.Ambiguous
}

// CreateStored starts one background Responses request. It deliberately
// returns only the remote ID; callers must journal that ID before retrieval.
func (c *Client) CreateStored(
	ctx context.Context,
	messages []Message,
	tools []Tool,
	clientRequestID string,
) (StoredResponse, error) {
	input, err := messagesToInput(messages)
	if err != nil {
		return StoredResponse{}, fmt.Errorf("构造 Responses input 失败: %w", err)
	}
	body, err := json.Marshal(responsesRequest{
		Model: c.Model, Input: input, Tools: responseTools(tools), Reasoning: c.reasoningConfig(),
		Text: c.textConfig(), ServiceTier: c.ServiceTier, Store: true, Background: true,
	})
	if err != nil {
		return StoredResponse{}, fmt.Errorf("构造 stored Responses 请求失败: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return StoredResponse{}, err
	}
	setHeaders(request, c.APIKey)
	clientRequestID = strings.TrimSpace(clientRequestID)
	if err := validateClientRequestID(clientRequestID); err != nil {
		return StoredResponse{}, err
	}
	request.Header.Set("X-Client-Request-Id", clientRequestID)
	if c.BeforeCreate != nil {
		if err := c.BeforeCreate(ctx, clientRequestID); err != nil {
			return StoredResponse{}, fmt.Errorf("response.create 预算拒绝: %w", err)
		}
	}

	response, err := c.doTracked(request, "创建 stored response")
	if err != nil {
		return StoredResponse{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return StoredResponse{}, &RequestError{Operation: "读取 stored response 创建结果", Ambiguous: true, Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return StoredResponse{}, fmt.Errorf("接口返回 %d: %s", response.StatusCode, truncate(string(data), 500))
	}
	var decoded responsesResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return StoredResponse{}, &RequestError{Operation: "解析 stored response 创建结果", Ambiguous: true, Err: err}
	}
	decoded.ID = strings.TrimSpace(decoded.ID)
	if decoded.ID == "" {
		return StoredResponse{}, &RequestError{Operation: "解析 stored response 创建结果", Ambiguous: true, Err: errors.New("响应缺少 id")}
	}
	return StoredResponse{ID: decoded.ID, Status: decoded.Status}, nil
}

// RetrieveStored reads a stored response without creating new model work.
func (c *Client) RetrieveStored(ctx context.Context, responseID string) (StoredResponse, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return StoredResponse{}, errors.New("stored response id 不能为空")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/responses/"+url.PathEscape(responseID), nil)
	if err != nil {
		return StoredResponse{}, err
	}
	setHeaders(request, c.APIKey)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return StoredResponse{}, fmt.Errorf("查询 stored response: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return StoredResponse{}, fmt.Errorf("读取 stored response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		requestErr := fmt.Errorf("查询 stored response 返回 %d: %s", response.StatusCode, truncate(string(data), 500))
		if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusNotFound {
			return StoredResponse{}, &StoredTerminalError{Status: "http_error", Err: requestErr}
		}
		return StoredResponse{}, requestErr
	}
	var decoded responsesResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return StoredResponse{}, fmt.Errorf("解析 stored response: %w", err)
	}
	if strings.TrimSpace(decoded.ID) == "" {
		decoded.ID = responseID
	}
	result := StoredResponse{ID: decoded.ID, Status: decoded.Status}
	if err := c.recordResponseUsage(ctx, decoded); err != nil {
		if decoded.Status == "completed" || decoded.Status == "failed" || decoded.Status == "incomplete" {
			return result, &StoredTerminalError{Status: decoded.Status, Err: err}
		}
		return result, err
	}
	switch decoded.Status {
	case "queued", "in_progress":
		return result, nil
	case "completed":
		message, _, err := parsedResponse(decoded)
		if err != nil {
			return result, err
		}
		result.Message = message
		result.Complete = true
		return result, nil
	case "failed", "incomplete":
		_, _, err := parsedResponse(decoded)
		return result, &StoredTerminalError{Status: decoded.Status, Err: err}
	default:
		return result, fmt.Errorf("stored response %s 返回未知状态 %q", responseID, decoded.Status)
	}
}

func (c *Client) doTracked(request *http.Request, operation string) (*http.Response, error) {
	var mayHaveReachedProvider atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteHeaders: func() { mayHaveReachedProvider.Store(true) },
		WroteRequest: func(httptrace.WroteRequestInfo) { mayHaveReachedProvider.Store(true) },
	}
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, &RequestError{Operation: operation, Ambiguous: mayHaveReachedProvider.Load(), Err: err}
	}
	return response, nil
}

func setHeaders(request *http.Request, apiKey string) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
}

func validateClientRequestID(value string) error {
	if value == "" || len(value) > 512 {
		return errors.New("X-Client-Request-Id 必须为 1 到 512 个 ASCII 字符")
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return errors.New("X-Client-Request-Id 只能包含可见 ASCII 字符")
		}
	}
	return nil
}
