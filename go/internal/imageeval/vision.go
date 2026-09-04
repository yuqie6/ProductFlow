package imageeval

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// VisionJudge 调用当前 prompt 绑定的 OpenAI 兼容接口打分。优先 /v1/responses（gpt-5 系），失败再试 chat.completions。
type VisionJudge struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func (v VisionJudge) Judge(ctx context.Context, in JudgeInput) (SlotJudgement, error) {
	if strings.TrimSpace(v.APIKey) == "" || strings.TrimSpace(v.Model) == "" {
		return SlotJudgement{}, fmt.Errorf("vision judge missing model or api key")
	}
	parts := []map[string]any{
		{"type": "input_text", "text": fmt.Sprintf("图种 %s。职责：%s", in.ImageType, in.TypeJob)},
	}
	attach := func(label, path string) error {
		if path == "" {
			return nil
		}
		body, mime, err := readDataURL(path)
		if err != nil {
			return err
		}
		parts = append(parts, map[string]any{"type": "input_text", "text": label})
		parts = append(parts, map[string]any{
			"type":      "input_image",
			"image_url": "data:" + mime + ";base64," + body,
		})
		return nil
	}
	for i, path := range in.ReferencePaths {
		if err := attach(fmt.Sprintf("参考图 %d", i+1), path); err != nil {
			return SlotJudgement{}, err
		}
	}
	if in.GoldPath != "" {
		if err := attach("对家金标", in.GoldPath); err != nil {
			return SlotJudgement{}, err
		}
	}
	if err := attach("工作台成片", in.WorkbenchPath); err != nil {
		return SlotJudgement{}, err
	}
	if err := attach("一句直调成片", in.NaivePath); err != nil {
		return SlotJudgement{}, err
	}
	client := v.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	text, respErr := v.callResponses(ctx, client, parts)
	if respErr != nil {
		var chatErr error
		text, chatErr = v.callChatCompletions(ctx, client, parts)
		if chatErr != nil {
			return SlotJudgement{}, fmt.Errorf("%v; chat fallback: %v", respErr, chatErr)
		}
	}
	wb, gold, naive, notes, err := ParseJudgeJSON(text)
	if err != nil {
		return SlotJudgement{}, err
	}
	if in.GoldPath == "" {
		gold = nil
	}
	slot := SlotJudgement{
		ImageType: in.ImageType,
		Workbench: wb,
		Gold:      gold,
		Naive:     naive,
		Notes:     notes,
	}
	return GateSlot(slot), nil
}

func (v VisionJudge) callResponses(ctx context.Context, client *http.Client, parts []map[string]any) (string, error) {
	payload := map[string]any{
		"model":        v.Model,
		"instructions": JudgeSystemPrompt,
		"input":        []map[string]any{{"role": "user", "content": parts}},
	}
	body, status, err := v.postJSON(ctx, client, judgeEndpoint(v.BaseURL, "/v1/responses"), payload)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("judge responses http %d: %s", status, truncate(string(body), 400))
	}
	text, err := parseResponsesText(body)
	if err != nil {
		return "", fmt.Errorf("judge responses: %w", err)
	}
	return text, nil
}

func (v VisionJudge) callChatCompletions(ctx context.Context, client *http.Client, parts []map[string]any) (string, error) {
	chatParts := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part["type"] {
		case "input_text":
			chatParts = append(chatParts, map[string]any{"type": "text", "text": part["text"]})
		case "input_image":
			url, _ := part["image_url"].(string)
			chatParts = append(chatParts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]string{"url": url},
			})
		}
	}
	payload := map[string]any{
		"model": v.Model,
		"messages": []map[string]any{
			{"role": "system", "content": JudgeSystemPrompt},
			{"role": "user", "content": chatParts},
		},
	}
	body, status, err := v.postJSON(ctx, client, judgeEndpoint(v.BaseURL, "/v1/chat/completions"), payload)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("judge chat http %d: %s", status, truncate(string(body), 400))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := unmarshalJSON(body, &parsed); err != nil {
		return "", fmt.Errorf("judge chat: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("judge empty choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

func (v VisionJudge) postJSON(ctx context.Context, client *http.Client, url string, payload map[string]any) ([]byte, int, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return body, resp.StatusCode, nil
}

func parseResponsesText(body []byte) (string, error) {
	var parsed struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := unmarshalJSON(body, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.OutputText) != "" {
		return parsed.OutputText, nil
	}
	var b strings.Builder
	for _, item := range parsed.Output {
		for _, c := range item.Content {
			if c.Type == "output_text" || c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("empty responses text")
	}
	return b.String(), nil
}

func unmarshalJSON(body []byte, dest any) error {
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 {
		return fmt.Errorf("empty json body")
	}
	if trim[0] != '{' && trim[0] != '[' {
		return fmt.Errorf("non-json body: %s", truncate(string(trim), 180))
	}
	return json.Unmarshal(trim, dest)
}

func judgeEndpoint(baseURL, path string) string {
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

func readDataURL(path string) (string, string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	mime := http.DetectContentType(body)
	if !strings.HasPrefix(mime, "image/") {
		mime = "image/jpeg"
	}
	return base64.StdEncoding.EncodeToString(body), mime, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
