package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// HTTPGateway 用 HTTP 实现 Gateway，调用 Node.js/Pi agent-service。
type HTTPGateway struct {
	BaseURL        string        // env AGENT_SERVICE_BASE_URL
	Token          string        // env AGENT_SERVICE_INTERNAL_TOKEN，env-only 密钥
	ConnectTimeout time.Duration // 来自 AGENT_SERVICE_CONNECT_TIMEOUT_SECONDS
	ReadTimeout    time.Duration // 来自 AGENT_SERVICE_READ_TIMEOUT_SECONDS；≤0 时 request 用 90s
}

// Configured 报告 BaseURL 与 Token 是否已设置。
func (g HTTPGateway) Configured() bool {
	return g.BaseURL != "" && g.Token != ""
}

func (g HTTPGateway) configured() bool {
	return g.Configured()
}

// StartTurn 实现 Gateway，向 agent-service POST 一轮 Turn。未配置返回 not_configured。连接失败或超时返回 unavailable；agent-service HTTP 非 2xx 或响应无效返回 error。
func (g HTTPGateway) StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error) {
	body := map[string]any{
		"input_text":      inputText,
		"asset_ids":       assetIDs,
		"idempotency_key": idempotencyKey,
		"page_context":    pageContext,
	}
	return g.request("POST", g.executionPath(conversationID, taskID)+"/turns", body)
}

// GetTurn 实现 Gateway，读取 agent-service 当前 Turn。未配置返回 not_configured。连接失败或超时返回 unavailable；agent-service HTTP 非 2xx 或响应无效返回 error。
func (g HTTPGateway) GetTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.request("GET", g.turnPath(conversationID, turnID, taskID), nil)
}

// CancelTurn 实现 Gateway，请求 agent-service 取消 Turn。未配置返回 not_configured。连接失败或超时返回 unavailable；agent-service HTTP 非 2xx 或响应无效返回 error。
func (g HTTPGateway) CancelTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.request("POST", g.turnPath(conversationID, turnID, taskID)+"/cancel", map[string]any{})
}

// ResumeTurn 实现 Gateway，请求 agent-service 恢复 Turn。未配置返回 not_configured。连接失败或超时返回 unavailable；agent-service HTTP 非 2xx 或响应无效返回 error。
func (g HTTPGateway) ResumeTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.request("POST", g.turnPath(conversationID, turnID, taskID)+"/resume", map[string]any{})
}

// AnswerQuestion 实现 Gateway，把答案交给 agent-service。未配置返回 not_configured。连接失败或超时返回 unavailable；agent-service HTTP 非 2xx 或响应无效返回 error。
func (g HTTPGateway) AnswerQuestion(conversationID, turnID, questionID string, answer map[string]any, taskID *string) (TurnState, error) {
	return g.request("POST", g.turnPath(conversationID, turnID, taskID)+"/questions/"+url.PathEscape(questionID)+"/answer", map[string]any{"answer": answer})
}

func (g HTTPGateway) executionPath(conversationID string, taskID *string) string {
	if taskID != nil && *taskID != "" {
		return "/internal/v1/tasks/" + url.PathEscape(*taskID)
	}
	return "/internal/v1/conversations/" + url.PathEscape(conversationID)
}

func (g HTTPGateway) turnPath(conversationID, turnID string, taskID *string) string {
	return g.executionPath(conversationID, taskID) + "/turns/" + url.PathEscape(turnID)
}

// request 调用 Node.js/Pi agent-service 并解码 TurnState。返回值不是 journal 权威；投影与 SSE 仍以 PostgreSQL 为准。
//
// 未配置返回 not_configured。非 2xx 收成 GatewayError（带上游 code）。响应体超过 1MiB 截断读取。超时默认 90s。
func (g HTTPGateway) request(method, path string, body any) (TurnState, error) {
	if !g.configured() {
		return TurnState{}, GatewayError{Code: "not_configured", Detail: "Agent 服务尚未配置"}
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return TurnState{}, err
		}
		reader = bytes.NewReader(raw)
	}
	timeout := g.ReadTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(method, g.BaseURL+path, reader)
	if err != nil {
		return TurnState{}, GatewayError{Code: "unavailable", Detail: "Agent 服务暂时不可用"}
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return TurnState{}, GatewayError{Code: "unavailable", Detail: "Agent 服务暂时不可用"}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := "upstream_error"
		msg := "Agent 服务请求失败"
		var payload map[string]any
		if json.Unmarshal(data, &payload) == nil {
			if errObj, ok := payload["error"].(map[string]any); ok {
				if c, ok := errObj["code"].(string); ok && c != "" {
					code = c
				}
				if m, ok := errObj["message"].(string); ok && m != "" {
					msg = m
				}
			}
		}
		return TurnState{}, GatewayError{Status: resp.StatusCode, Code: code, Detail: msg}
	}
	var state TurnState
	if err := json.Unmarshal(data, &state); err != nil {
		return TurnState{}, GatewayError{Status: 502, Code: "invalid_response", Detail: "Agent 服务返回了无效响应"}
	}
	return state, nil
}

// String 返回含 BaseURL 的调试标识，给日志 fmt 用。不要把 Token 拼进去。
func (g HTTPGateway) String() string {
	return fmt.Sprintf("agent-gateway %s", g.BaseURL)
}
