package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type HTTPGateway struct {
	BaseURL        string
	Token          string
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

func (g HTTPGateway) Configured() bool {
	return g.BaseURL != "" && g.Token != ""
}

func (g HTTPGateway) configured() bool {
	return g.Configured()
}

func (g HTTPGateway) StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error) {
	body := map[string]any{
		"input_text":      inputText,
		"asset_ids":       assetIDs,
		"idempotency_key": idempotencyKey,
		"page_context":    pageContext,
	}
	return g.request("POST", g.executionPath(conversationID, taskID)+"/turns", body)
}

func (g HTTPGateway) GetTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.request("GET", g.turnPath(conversationID, turnID, taskID), nil)
}

func (g HTTPGateway) CancelTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.request("POST", g.turnPath(conversationID, turnID, taskID)+"/cancel", map[string]any{})
}

func (g HTTPGateway) ResumeTurn(conversationID, turnID string, taskID *string) (TurnState, error) {
	return g.request("POST", g.turnPath(conversationID, turnID, taskID)+"/resume", map[string]any{})
}

func (g HTTPGateway) AnswerQuestion(conversationID, turnID, questionID string, answer map[string]any, taskID *string) (TurnState, error) {
	return g.request("POST", g.turnPath(conversationID, turnID, taskID)+"/questions/"+url.PathEscape(questionID)+"/answer", map[string]any{"answer": answer})
}

func (g HTTPGateway) StreamTurnEvents(ctx context.Context, conversationID, turnID string, taskID *string, after int, w io.Writer) error {
	if !g.configured() {
		return GatewayError{Code: "not_configured", Detail: "Agent 服务尚未配置"}
	}
	if after < 0 {
		after = 0
	}
	path := g.turnPath(conversationID, turnID, taskID) + "/events?after=" + strconv.Itoa(after)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.BaseURL+path, nil)
	if err != nil {
		return GatewayError{Code: "unavailable", Detail: "Agent 服务暂时不可用"}
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "text/event-stream")
	headerTimeout := g.ConnectTimeout
	if headerTimeout <= 0 {
		headerTimeout = 10 * time.Second
	}
	client := &http.Client{
		Transport: &http.Transport{ResponseHeaderTimeout: headerTimeout},
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return GatewayError{Code: "unavailable", Detail: "Agent 服务暂时不可用"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
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
		return GatewayError{Status: resp.StatusCode, Code: code, Detail: msg}
	}
	_, err = io.Copy(w, resp.Body)
	return err
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

func (g HTTPGateway) String() string {
	return fmt.Sprintf("agent-gateway %s", g.BaseURL)
}
