// Package question defines the shared structured question contract used by
// interactive and durable drivers.
package question

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	ToolName            = "ask_user"
	FreeTextOption      = -1
	FreeTextLabel       = "其他"
	MaxSuggestedChoices = 8
)

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Prompt struct {
	Header   string   `json:"header"`
	Question string   `json:"question"`
	Options  []Option `json:"options"`
}

type Answer struct {
	Option int    `json:"option"`
	Label  string `json:"label"`
	Text   string `json:"text,omitempty"`
}

func Normalize(header, text string, options []Option) (Prompt, error) {
	header = strings.TrimSpace(header)
	text = strings.TrimSpace(text)
	if text == "" {
		return Prompt{}, errors.New("问题内容不能为空")
	}
	if len(text) > 1000 {
		return Prompt{}, errors.New("问题内容不能超过 1000 字符")
	}
	if len(options) < 1 || len(options) > MaxSuggestedChoices {
		return Prompt{}, fmt.Errorf("问题必须提供 1 到 %d 个建议选项", MaxSuggestedChoices)
	}
	if header == "" {
		header = "需要确认"
	}
	if len(header) > 80 {
		return Prompt{}, errors.New("问题主题不能超过 80 字符")
	}
	normalized := make([]Option, len(options))
	seen := make(map[string]bool, len(options))
	for index, option := range options {
		option.Label = strings.TrimSpace(option.Label)
		option.Description = strings.TrimSpace(option.Description)
		if option.Label == "" {
			return Prompt{}, fmt.Errorf("第 %d 个问题选项为空", index+1)
		}
		if len(option.Label) > 120 || len(option.Description) > 500 {
			return Prompt{}, fmt.Errorf("第 %d 个问题选项过长", index+1)
		}
		if seen[option.Label] {
			return Prompt{}, fmt.Errorf("问题选项重复: %s", option.Label)
		}
		seen[option.Label] = true
		normalized[index] = option
	}
	return Prompt{Header: header, Question: text, Options: normalized}, nil
}

func Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"header":   map[string]any{"type": "string", "description": "简短主题"},
			"question": map[string]any{"type": "string", "description": "要用户决定的问题"},
			"options": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": MaxSuggestedChoices,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
					},
					"required":             []string{"label"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"question", "options"},
		"additionalProperties": false,
	}
}

func EncodeAnswer(prompt Prompt, option int) (json.RawMessage, error) {
	if option < 0 || option >= len(prompt.Options) {
		return nil, fmt.Errorf("问题答案索引越界: %d", option)
	}
	return json.Marshal(Answer{Option: option, Label: prompt.Options[option].Label})
}

func NewFreeTextAnswer(text string) (Answer, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Answer{}, errors.New("自由回答不能为空")
	}
	if len(text) > 4000 {
		return Answer{}, errors.New("自由回答不能超过 4000 字符")
	}
	return Answer{Option: FreeTextOption, Label: FreeTextLabel, Text: text}, nil
}

func DecodeAnswer(raw json.RawMessage, prompt Prompt) (Answer, error) {
	var answer Answer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return answer, fmt.Errorf("解析问题答案: %w", err)
	}
	if answer.Option == FreeTextOption {
		normalized, err := NewFreeTextAnswer(answer.Text)
		if err != nil {
			return answer, err
		}
		if answer.Label != normalized.Label {
			return answer, fmt.Errorf("自由回答标签必须为 %q", normalized.Label)
		}
		return normalized, nil
	}
	if answer.Option < 0 || answer.Option >= len(prompt.Options) {
		return answer, fmt.Errorf("问题答案索引越界: %d", answer.Option)
	}
	want := prompt.Options[answer.Option].Label
	if answer.Label != want {
		return answer, fmt.Errorf("问题答案标签为 %q,选项 %d 需要 %q", answer.Label, answer.Option, want)
	}
	return answer, nil
}
