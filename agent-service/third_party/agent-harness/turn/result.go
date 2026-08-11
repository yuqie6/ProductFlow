package turn

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ToolResultSchemaVersion            = 1
	MaxImagesPerToolResult             = MaxImagesPerTurn
	MaxToolResultImageBytes      int64 = MaxImageBytes
	MaxTotalToolResultImageBytes int64 = MaxTotalImageBytes
)

// ToolResultContent uses the Responses function_call_output content types.
// Images share the same URL, media, size, detail, and checkpoint contract as
// images supplied in a TurnInput.
type ToolResultContent struct {
	Type  ContentType `json:"type"`
	Text  string      `json:"text,omitempty"`
	Image *InputImage `json:"image,omitempty"`
}

// ToolResult is the versioned structured result returned by a ResultHandler.
// Legacy string handlers continue to use the Responses string output form.
type ToolResult struct {
	SchemaVersion int                 `json:"schema_version"`
	Content       []ToolResultContent `json:"content"`
}

func TextToolResult(text string) ToolResult {
	return ToolResult{
		SchemaVersion: ToolResultSchemaVersion,
		Content:       []ToolResultContent{{Type: ContentInputText, Text: text}},
	}
}

func (result ToolResult) Validate() error {
	if result.SchemaVersion != ToolResultSchemaVersion {
		return fmt.Errorf("tool result schema_version=%d is unsupported; expected %d", result.SchemaVersion, ToolResultSchemaVersion)
	}
	if len(result.Content) == 0 {
		return errors.New("tool result content cannot be empty")
	}
	images := 0
	var totalImageBytes int64
	for index, content := range result.Content {
		switch content.Type {
		case ContentInputText:
			if content.Image != nil || strings.TrimSpace(content.Text) == "" {
				return fmt.Errorf("tool result content[%d] input_text must contain non-empty text only", index)
			}
		case ContentInputImage:
			if content.Text != "" || content.Image == nil {
				return fmt.Errorf("tool result content[%d] input_image must contain image only", index)
			}
			size, err := validateImage(*content.Image)
			if err != nil {
				return fmt.Errorf("tool result content[%d]: %w", index, err)
			}
			images++
			totalImageBytes += size
		default:
			return fmt.Errorf("tool result content[%d] has unsupported type %q", index, content.Type)
		}
	}
	if images > MaxImagesPerToolResult {
		return fmt.Errorf("tool result has %d images; maximum is %d", images, MaxImagesPerToolResult)
	}
	if totalImageBytes > MaxTotalToolResultImageBytes {
		return fmt.Errorf("tool result image bytes total %d exceeds %d", totalImageBytes, MaxTotalToolResultImageBytes)
	}
	return nil
}

// Text returns text parts only. Image URLs and bytes are never flattened into
// transcript text.
func (result ToolResult) Text() string {
	var values []string
	for _, content := range result.Content {
		if content.Type == ContentInputText && strings.TrimSpace(content.Text) != "" {
			values = append(values, strings.TrimSpace(content.Text))
		}
	}
	return strings.Join(values, "\n")
}
