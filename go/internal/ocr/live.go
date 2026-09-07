package ocr

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

// LiveVisionExtractor 可选 live OCR：OpenAI 兼容 /v1/chat/completions 识图抽字。
// 仅当 PRODUCTFLOW_OCR_LIVE=1 且配置了密钥时由 DefaultExtractor 选用；默认测试不走此路径。
type LiveVisionExtractor struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func (v LiveVisionExtractor) Extract(ctx context.Context, pngBytes []byte) (ExtractResult, error) {
	base := strings.TrimRight(firstNonEmpty(v.BaseURL, os.Getenv("PRODUCTFLOW_OCR_BASE_URL"), os.Getenv("OPENAI_BASE_URL")), "/")
	key := firstNonEmpty(v.APIKey, os.Getenv("PRODUCTFLOW_OCR_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	model := firstNonEmpty(v.Model, os.Getenv("PRODUCTFLOW_OCR_MODEL"), os.Getenv("OPENAI_MODEL"))
	if key == "" || model == "" {
		return ExtractResult{}, fmt.Errorf("ocr live: 缺少 API key 或 model")
	}
	if base == "" {
		base = "https://api.openai.com"
	}
	client := v.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "Extract all visible product text from this PNG. Reply with plain text only, one line per text block, no commentary."},
					{"type": "image_url", "image_url": map[string]any{
						"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes),
					}},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ExtractResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ExtractResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ExtractResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExtractResult{}, err
	}
	if resp.StatusCode >= 300 {
		return ExtractResult{}, fmt.Errorf("ocr live: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 240))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ExtractResult{}, err
	}
	if len(parsed.Choices) == 0 {
		return ExtractResult{}, fmt.Errorf("ocr live: empty choices")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	return ExtractResult{Text: text, Engine: engineLiveVision}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
