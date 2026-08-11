package turninput

import (
	"errors"
	"strings"

	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/turn"
)

func Message(input turn.TurnInput) (llm.Message, error) {
	if err := input.Validate(); err != nil {
		return llm.Message{}, err
	}
	parts := make([]llm.ContentPart, 0, len(input.Content))
	for _, content := range input.Content {
		part := llm.ContentPart{Type: string(content.Type)}
		if content.Type == turn.ContentInputText {
			part.Text = content.Text
		} else {
			image := content.Image
			part.ImageURL = strings.TrimSpace(image.URL)
			part.EmbeddedData = append([]byte(nil), image.Data...)
			part.MediaType = strings.ToLower(strings.TrimSpace(image.MediaType))
			part.SizeBytes = image.SizeBytes
			part.Detail = string(image.Detail)
			part.CheckpointMode = string(image.CheckpointMode)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return llm.Message{}, errors.New("turn input has no content")
	}
	return llm.Message{Role: "user", ContentParts: parts}, nil
}

func Label(input turn.TurnInput) string {
	if text := strings.TrimSpace(input.Text()); text != "" {
		return text
	}
	return "[image input]"
}

func Result(result turn.ToolResult) (llm.ToolResult, error) {
	if err := result.Validate(); err != nil {
		return llm.ToolResult{}, err
	}
	parts := make([]llm.ContentPart, 0, len(result.Content))
	for _, content := range result.Content {
		part := llm.ContentPart{Type: string(content.Type), Text: content.Text}
		if content.Type == turn.ContentInputImage {
			image := content.Image
			part.ImageURL = strings.TrimSpace(image.URL)
			part.EmbeddedData = append([]byte(nil), image.Data...)
			part.MediaType = strings.ToLower(strings.TrimSpace(image.MediaType))
			part.SizeBytes = image.SizeBytes
			part.Detail = string(image.Detail)
			part.CheckpointMode = string(image.CheckpointMode)
		}
		parts = append(parts, part)
	}
	return llm.ContentToolResult(parts), nil
}
