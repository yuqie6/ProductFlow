package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// Python 工作流生图默认 15 分钟；120s 会把长 Responses 调用标成 unknown。
const defaultTimeout = 15 * time.Minute

// Responses 出图的 JSON 里带整段 base64，8MiB 会把已经生成的图截断成 unknown。
var maxProviderJSONBytes int64 = 64 << 20

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultTimeout}
}

func endpoint(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://api.openai.com"
	}
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		base = strings.TrimRight(base[:len(base)-3], "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func doJSON(ctx context.Context, client *http.Client, method, url, apiKey string, body io.Reader, contentType string) (int, []byte, error) {
	if client == nil {
		client = newHTTPClient()
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, graph.ErrProviderUnknown()
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, mapTransport(err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProviderJSONBytes+1))
	if readErr != nil {
		return resp.StatusCode, raw, graph.ErrProviderUnknown()
	}
	if int64(len(raw)) > maxProviderJSONBytes {
		return resp.StatusCode, raw[:maxProviderJSONBytes], graph.ErrProviderUnknown()
	}
	return resp.StatusCode, raw, nil
}

func mapTransport(err error) error {
	if err == nil {
		return nil
	}
	if isTimeoutTransport(err) {
		return retryableTransportError{sentinel: imagesession.ErrTimeout}
	}
	if isConnectionTransport(err) {
		return retryableTransportError{sentinel: imagesession.ErrConnection}
	}
	return graph.ErrProviderUnknown()
}

// retryableTransportError 让 Chat 能按超时/断连重试，图路径仍能 errors.As 成 unknown。
type retryableTransportError struct {
	sentinel error
}

func (e retryableTransportError) Error() string { return e.sentinel.Error() }

func (e retryableTransportError) Unwrap() []error {
	return []error{e.sentinel, graph.ErrProviderUnknown()}
}

func isTimeoutTransport(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out")
}

func isConnectionTransport(err error) bool {
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"connection reset", "connection refused", "broken pipe", "econnreset"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func asGraphUnknown(err error) error {
	if err == nil {
		return nil
	}
	if imagesession.IsRetryableProviderFailure(err) {
		return graph.ErrProviderUnknown()
	}
	return err
}

func mapGraphStatus(status int, body []byte) error {
	if status >= 500 || status == http.StatusTooManyRequests {
		return graph.ErrProviderUnknown()
	}
	if status >= 400 {
		return fmt.Errorf("供应商拒绝请求（HTTP %d）", status)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return graph.ErrProviderUnknown()
	}
	return nil
}

func mapChatStatus(status int, body []byte) error {
	if status == http.StatusTooManyRequests {
		return imagesession.ErrRateLimit
	}
	if status >= 500 {
		return imagesession.ErrProvider5xx
	}
	if status >= 400 {
		if chatBodyRateLimited(body) {
			return imagesession.ErrRateLimit
		}
		return apperr.Validation("图片供应商拒绝了本次请求，请调整提示词、参考图或参数后重试")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return imagesession.ErrUnknown()
	}
	return nil
}

func chatBodyRateLimited(body []byte) bool {
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		code := strings.ToLower(parsed.Error.Code)
		typ := strings.ToLower(parsed.Error.Type)
		if code == "insufficient_quota" || strings.Contains(code, "rate_limit") {
			return true
		}
		if typ == "insufficient_quota" || strings.Contains(typ, "rate_limit") {
			return true
		}
	}
	msg := strings.ToLower(string(body))
	return strings.Contains(msg, "insufficient_quota") ||
		strings.Contains(msg, "rate_limit_exceeded") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "rate-limit")
}

func decodeB64(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if comma := strings.Index(trimmed, ","); comma >= 0 && strings.Contains(trimmed[:comma], "base64") {
		trimmed = trimmed[comma+1:]
	}
	decoded, err := decodeStdB64(trimmed)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("供应商没有返回图片结果")
	}
	return decoded, nil
}
