// Package llm 提供 OpenAI Responses API 客户端。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Message 是 harness 内部的一条会话消息。
type Message struct {
	Role         string        `json:"role"`
	Content      *string       `json:"content,omitempty"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	// ResponseItems 保留 Responses API 的原始 output items,供后续轮次原样回传。
	ResponseItems []json.RawMessage `json:"response_items,omitempty"`
}

// String 返回消息文本内容(工具调用消息返回空)。
func (m Message) String() string {
	if m.Content != nil {
		return *m.Content
	}
	var text strings.Builder
	for _, part := range m.ContentParts {
		if part.Type == "input_text" {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

// ContentPart is persisted in checkpoints and durable model inputs.
// EmbeddedData becomes a data URL only while encoding a provider request.
type ContentPart struct {
	Type           string `json:"type"`
	Text           string `json:"text,omitempty"`
	ImageURL       string `json:"image_url,omitempty"`
	EmbeddedData   []byte `json:"embedded_data,omitempty"`
	MediaType      string `json:"media_type,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
	Detail         string `json:"detail,omitempty"`
	CheckpointMode string `json:"checkpoint_mode,omitempty"`
}

func (part ContentPart) wireImageURL() string {
	if len(part.EmbeddedData) == 0 {
		return part.ImageURL
	}
	return "data:" + part.MediaType + ";base64," + base64.StdEncoding.EncodeToString(part.EmbeddedData)
}

// ToolCall 模型请求调用的一个工具。
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 工具调用的名称与参数。
type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Tool 是 harness 内部的工具声明。
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction 工具的函数签名(参数为 JSON Schema 风格)。
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict,omitempty"`
}

type responsesRequest struct {
	Model       string              `json:"model"`
	Input       []json.RawMessage   `json:"input"`
	Tools       []responsesTool     `json:"tools,omitempty"`
	Reasoning   *responsesReasoning `json:"reasoning,omitempty"`
	Text        *responsesText      `json:"text,omitempty"`
	ServiceTier string              `json:"service_tier,omitempty"`
	Store       bool                `json:"store"`
	Background  bool                `json:"background,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

type responsesReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type responsesText struct {
	Verbosity string `json:"verbosity,omitempty"`
}

type responsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

type responsesResponse struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Error  *responseError    `json:"error"`
	Usage  *responseUsage    `json:"usage"`
}

// Usage is provider-reported Responses token accounting. ResponseID is filled
// from the response envelope before the record hook is called.
type Usage struct {
	ResponseID      string `json:"response_id"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	TotalTokens     int64  `json:"total_tokens"`
	CachedTokens    int64  `json:"cached_tokens,omitempty"`
	ReasoningTokens int64  `json:"reasoning_tokens,omitempty"`
}

type responseUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	InputDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

type responseOutputItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Content   []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
}

// Client 是 Responses API 的 HTTP 客户端。
type Client struct {
	APIKey           string
	BaseURL          string
	Model            string
	ReasoningEffort  string
	ReasoningSummary string
	TextVerbosity    string
	ServiceTier      string
	HTTP             *http.Client
	// BeforeCreate is called before response.create. requestKey is stable for
	// stored durable calls and empty for ordinary/streaming calls.
	BeforeCreate func(ctx context.Context, requestKey string) error
	// RecordUsage is called for terminal responses. Callers should deduplicate
	// by ResponseID because stored responses may be retrieved more than once.
	RecordUsage func(ctx context.Context, usage Usage) error
}

// New 构造客户端。baseURL 应包含 provider 所需前缀,客户端只追加 /responses。
func New(apiKey, baseURL, model string) *Client {
	return &Client{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		HTTP:    http.DefaultClient,
	}
}

// Chat 发起一次非流式 Responses 请求,返回 assistant 消息与响应状态。
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, string, error) {
	resp, err := c.request(ctx, messages, tools, false)
	if err != nil {
		return Message{}, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return Message{}, "", &RequestError{Operation: "读取 response.create 结果", Ambiguous: true, Err: err}
		}
		return Message{}, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, "", fmt.Errorf("接口返回 %d: %s", resp.StatusCode, truncate(string(data), 500))
	}

	var rr responsesResponse
	if err := json.Unmarshal(data, &rr); err != nil {
		return Message{}, "", &RequestError{Operation: "解析 response.create 结果", Ambiguous: true, Err: err}
	}
	message, status, parseErr := parsedResponse(rr)
	if usageErr := c.recordResponseUsage(ctx, rr); usageErr != nil {
		return Message{}, status, &RequestError{Operation: "记录 response.create 终态", Ambiguous: true, Err: usageErr}
	}
	return message, status, parseErr
}

// ChatStream 发起流式 Responses 请求。文本增量通过 onDelta 按到达顺序回调，
// 返回值仍是完整 assistant 消息，供会话原样保存 output items。
func (c *Client) ChatStream(ctx context.Context, messages []Message, tools []Tool, onDelta func(string)) (Message, string, error) {
	return c.chatStream(ctx, messages, tools, func(delta string) error {
		if onDelta != nil {
			onDelta(delta)
		}
		return nil
	})
}

// ChatStreamDurable is the error-aware streaming variant used by durable
// control planes. Returning an error from onDelta aborts the provider stream;
// because response.create is already in flight, that failure is ambiguous.
func (c *Client) ChatStreamDurable(
	ctx context.Context,
	messages []Message,
	tools []Tool,
	onDelta func(string) error,
) (Message, string, error) {
	return c.chatStream(ctx, messages, tools, onDelta)
}

func (c *Client) chatStream(
	ctx context.Context,
	messages []Message,
	tools []Tool,
	onDelta func(string) error,
) (Message, string, error) {
	resp, err := c.request(ctx, messages, tools, true)
	if err != nil {
		return Message{}, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if readErr != nil {
			return Message{}, "", readErr
		}
		return Message{}, "", fmt.Errorf("接口返回 %d: %s", resp.StatusCode, truncate(string(data), 500))
	}

	message, status, completed, streamErr := readResponseStreamResponseWithSink(resp.Body, onDelta)
	if completed != nil {
		if usageErr := c.recordResponseUsage(ctx, *completed); usageErr != nil {
			return Message{}, status, &RequestError{Operation: "记录流式 response.create 终态", Ambiguous: true, Err: usageErr}
		}
	}
	if streamErr != nil && completed == nil {
		return Message{}, status, &RequestError{Operation: "处理流式 response.create", Ambiguous: true, Err: streamErr}
	}
	return message, status, streamErr
}

func (c *Client) request(ctx context.Context, messages []Message, tools []Tool, stream bool) (*http.Response, error) {
	input, err := messagesToInput(messages)
	if err != nil {
		return nil, fmt.Errorf("构造 Responses input 失败: %w", err)
	}

	body, err := json.Marshal(responsesRequest{
		Model:       c.Model,
		Input:       input,
		Tools:       responseTools(tools),
		Reasoning:   c.reasoningConfig(),
		Text:        c.textConfig(),
		ServiceTier: c.ServiceTier,
		Store:       false,
		Stream:      stream,
	})
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if c.BeforeCreate != nil {
		if err := c.BeforeCreate(ctx, ""); err != nil {
			return nil, fmt.Errorf("response.create 预算拒绝: %w", err)
		}
	}

	return c.doTracked(req, "创建 response")
}

func (c *Client) recordResponseUsage(ctx context.Context, response responsesResponse) error {
	if c.RecordUsage == nil || response.Status == "queued" || response.Status == "in_progress" {
		return nil
	}
	if strings.TrimSpace(response.ID) == "" {
		return errors.New("终态 Responses usage 缺少 response ID")
	}
	if response.Usage == nil {
		return fmt.Errorf("终态 Responses %s 缺少 provider usage", response.ID)
	}
	usage := Usage{
		ResponseID: strings.TrimSpace(response.ID), InputTokens: response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.TotalTokens,
		CachedTokens:    response.Usage.InputDetails.CachedTokens,
		ReasoningTokens: response.Usage.OutputDetails.ReasoningTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if err := c.RecordUsage(ctx, usage); err != nil {
		return fmt.Errorf("记录 Responses usage: %w", err)
	}
	return nil
}

func (c *Client) reasoningConfig() *responsesReasoning {
	summary := c.ReasoningSummary
	if strings.EqualFold(summary, "none") {
		summary = ""
	}
	if c.ReasoningEffort == "" && summary == "" {
		return nil
	}
	return &responsesReasoning{Effort: c.ReasoningEffort, Summary: summary}
}

func (c *Client) textConfig() *responsesText {
	if c.TextVerbosity == "" {
		return nil
	}
	return &responsesText{Verbosity: c.TextVerbosity}
}

func parsedResponse(rr responsesResponse) (Message, string, error) {
	if rr.Error != nil {
		return Message{}, rr.Status, fmt.Errorf("模型错误: %s", rr.Error.Message)
	}
	if rr.Status == "failed" {
		return Message{}, rr.Status, fmt.Errorf("Responses 请求失败")
	}
	if rr.Status == "incomplete" {
		return Message{}, rr.Status, fmt.Errorf("Responses 请求未完整结束")
	}
	if len(rr.Output) == 0 {
		return Message{}, rr.Status, fmt.Errorf("响应中没有 output")
	}

	message, err := responseMessage(rr.Output)
	if err != nil {
		return Message{}, rr.Status, err
	}
	return message, rr.Status, nil
}

type responseStreamEvent struct {
	Type     string             `json:"type"`
	Delta    string             `json:"delta"`
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	Response *responsesResponse `json:"response"`
}

func readResponseStream(r io.Reader, onDelta func(string)) (Message, string, error) {
	message, status, _, err := readResponseStreamResponse(r, onDelta)
	return message, status, err
}

func readResponseStreamResponse(r io.Reader, onDelta func(string)) (Message, string, *responsesResponse, error) {
	return readResponseStreamResponseWithSink(r, func(delta string) error {
		if onDelta != nil {
			onDelta(delta)
		}
		return nil
	})
}

func readResponseStreamResponseWithSink(
	r io.Reader,
	onDelta func(string) error,
) (Message, string, *responsesResponse, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
			continue
		}

		message, status, response, done, err := consumeStreamEvent(dataLines, onDelta)
		dataLines = dataLines[:0]
		if err != nil {
			return Message{}, status, response, err
		}
		if done {
			return message, status, response, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return Message{}, "", nil, fmt.Errorf("读取流式响应失败: %w", err)
	}
	if len(dataLines) > 0 {
		message, status, response, done, err := consumeStreamEvent(dataLines, onDelta)
		if err != nil {
			return Message{}, status, response, err
		}
		if done {
			return message, status, response, nil
		}
	}
	return Message{}, "", nil, fmt.Errorf("流式响应在 response.completed 前结束")
}

func consumeStreamEvent(dataLines []string, onDelta func(string) error) (Message, string, *responsesResponse, bool, error) {
	if len(dataLines) == 0 {
		return Message{}, "", nil, false, nil
	}
	data := strings.Join(dataLines, "\n")
	if data == "[DONE]" {
		return Message{}, "", nil, false, nil
	}

	var event responseStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return Message{}, "", nil, false, fmt.Errorf("解析流式事件失败: %w", err)
	}

	switch event.Type {
	case "response.output_text.delta", "response.refusal.delta":
		if onDelta != nil && event.Delta != "" {
			if err := onDelta(event.Delta); err != nil {
				return Message{}, "", nil, false, fmt.Errorf("处理流式文本增量: %w", err)
			}
		}
	case "response.completed", "response.incomplete", "response.failed":
		if event.Response == nil {
			return Message{}, "", nil, false, fmt.Errorf("%s 缺少 response", event.Type)
		}
		message, status, err := parsedResponse(*event.Response)
		return message, status, event.Response, true, err
	case "error":
		if event.Message == "" {
			event.Message = event.Code
		}
		return Message{}, "failed", nil, false, fmt.Errorf("流式响应错误: %s", event.Message)
	}
	return Message{}, "", nil, false, nil
}

func responseTools(tools []Tool) []responsesTool {
	out := make([]responsesTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, responsesTool{
			Type:        "function",
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
			Strict:      tool.Function.Strict,
		})
	}
	return out
}

func messagesToInput(messages []Message) ([]json.RawMessage, error) {
	input := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role == "assistant" && len(message.ResponseItems) > 0 {
			input = append(input, message.ResponseItems...)
			continue
		}

		switch message.Role {
		case "system", "user", "assistant":
			if len(message.ContentParts) > 0 {
				content, err := contentPartsToWire(message.ContentParts)
				if err != nil {
					return nil, err
				}
				raw, err := json.Marshal(struct {
					Role    string           `json:"role"`
					Content []map[string]any `json:"content"`
				}{Role: message.Role, Content: content})
				if err != nil {
					return nil, err
				}
				input = append(input, raw)
			} else if message.Content != nil {
				raw, err := json.Marshal(struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: message.Role, Content: *message.Content})
				if err != nil {
					return nil, err
				}
				input = append(input, raw)
			}
			for _, toolCall := range message.ToolCalls {
				arguments, err := argumentsString(toolCall.Function.Arguments)
				if err != nil {
					return nil, fmt.Errorf("工具 %s 参数无效: %w", toolCall.Function.Name, err)
				}
				raw, err := json.Marshal(struct {
					Type      string `json:"type"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Type: "function_call", CallID: toolCall.ID, Name: toolCall.Function.Name, Arguments: arguments})
				if err != nil {
					return nil, err
				}
				input = append(input, raw)
			}
		case "tool":
			var output any = message.String()
			if len(message.ContentParts) > 0 {
				content, err := contentPartsToWire(message.ContentParts)
				if err != nil {
					return nil, fmt.Errorf("工具 %s 结果: %w", message.ToolCallID, err)
				}
				output = content
			}
			raw, err := json.Marshal(struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Output any    `json:"output"`
			}{Type: "function_call_output", CallID: message.ToolCallID, Output: output})
			if err != nil {
				return nil, err
			}
			input = append(input, raw)
		default:
			return nil, fmt.Errorf("不支持的消息角色 %q", message.Role)
		}
	}
	return input, nil
}

func contentPartsToWire(parts []ContentPart) ([]map[string]any, error) {
	content := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_text":
			content = append(content, map[string]any{"type": "input_text", "text": part.Text})
		case "input_image":
			item := map[string]any{"type": "input_image", "image_url": part.wireImageURL()}
			if part.Detail != "" {
				item["detail"] = part.Detail
			}
			content = append(content, item)
		default:
			return nil, fmt.Errorf("不支持的消息 content part %q", part.Type)
		}
	}
	return content, nil
}

func responseMessage(output []json.RawMessage) (Message, error) {
	message := Message{Role: "assistant", ResponseItems: output}
	var text strings.Builder

	for _, raw := range output {
		var item responseOutputItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return Message{}, fmt.Errorf("解析 output item 失败: %w", err)
		}
		switch item.Type {
		case "message":
			for _, content := range item.Content {
				switch content.Type {
				case "output_text":
					text.WriteString(content.Text)
				case "refusal":
					text.WriteString(content.Refusal)
				}
			}
		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			arguments := item.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage(`"{}"`)
			}
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID:   callID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      item.Name,
					Arguments: arguments,
				},
			})
		}
	}

	if text.Len() > 0 {
		content := text.String()
		message.Content = &content
	}
	if message.Content == nil && len(message.ToolCalls) == 0 {
		return Message{}, fmt.Errorf("响应中没有文本或函数调用")
	}
	return message, nil
}

func argumentsString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("不是有效 JSON")
	}
	return string(raw), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
