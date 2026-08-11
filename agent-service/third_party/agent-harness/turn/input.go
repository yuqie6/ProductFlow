// Package turn defines the application-facing alpha protocol shared by the
// session runtime and the recoverable agenttask runtime.
package turn

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	APIVersion               = "v1alpha1"
	InputSchemaVersion       = 1
	MaxImagesPerTurn         = 16
	MaxImageBytes      int64 = 20 << 20
	MaxTotalImageBytes int64 = 64 << 20
)

type ContentType string

const (
	ContentInputText  ContentType = "input_text"
	ContentInputImage ContentType = "input_image"
)

type ImageDetail string

const (
	ImageDetailAuto ImageDetail = "auto"
	ImageDetailLow  ImageDetail = "low"
	ImageDetailHigh ImageDetail = "high"
)

// ImageCheckpointMode makes image persistence explicit. Reference stores only
// the HTTPS URL; Embed stores Data in the checkpoint and sends a data URL.
type ImageCheckpointMode string

const (
	ImageCheckpointReference ImageCheckpointMode = "reference"
	ImageCheckpointEmbed     ImageCheckpointMode = "embed"
)

type InputImage struct {
	URL            string              `json:"url,omitempty"`
	Data           []byte              `json:"data,omitempty"`
	MediaType      string              `json:"media_type"`
	SizeBytes      int64               `json:"size_bytes"`
	Detail         ImageDetail         `json:"detail,omitempty"`
	CheckpointMode ImageCheckpointMode `json:"checkpoint_mode"`
}

type InputContent struct {
	Type  ContentType `json:"type"`
	Text  string      `json:"text,omitempty"`
	Image *InputImage `json:"image,omitempty"`
}

type TurnInput struct {
	SchemaVersion int            `json:"schema_version"`
	Content       []InputContent `json:"content"`
}

func TextInput(text string) TurnInput {
	return TurnInput{
		SchemaVersion: InputSchemaVersion,
		Content:       []InputContent{{Type: ContentInputText, Text: text}},
	}
}

func (input TurnInput) Validate() error {
	if input.SchemaVersion != InputSchemaVersion {
		return fmt.Errorf("turn input schema_version=%d is unsupported; expected %d", input.SchemaVersion, InputSchemaVersion)
	}
	if len(input.Content) == 0 {
		return errors.New("turn input content cannot be empty")
	}
	images := 0
	var totalImageBytes int64
	for index, content := range input.Content {
		switch content.Type {
		case ContentInputText:
			if content.Image != nil || strings.TrimSpace(content.Text) == "" {
				return fmt.Errorf("turn input content[%d] input_text must contain non-empty text only", index)
			}
		case ContentInputImage:
			if content.Text != "" || content.Image == nil {
				return fmt.Errorf("turn input content[%d] input_image must contain image only", index)
			}
			size, err := validateImage(*content.Image)
			if err != nil {
				return fmt.Errorf("turn input content[%d]: %w", index, err)
			}
			images++
			totalImageBytes += size
		default:
			return fmt.Errorf("turn input content[%d] has unsupported type %q", index, content.Type)
		}
	}
	if images > MaxImagesPerTurn {
		return fmt.Errorf("turn input has %d images; maximum is %d", images, MaxImagesPerTurn)
	}
	if totalImageBytes > MaxTotalImageBytes {
		return fmt.Errorf("turn input image bytes total %d exceeds %d", totalImageBytes, MaxTotalImageBytes)
	}
	return nil
}

func validateImage(image InputImage) (int64, error) {
	mediaType := strings.ToLower(strings.TrimSpace(image.MediaType))
	if !supportedMediaType(mediaType) {
		return 0, fmt.Errorf("unsupported image media_type %q", image.MediaType)
	}
	if image.Detail != "" && image.Detail != ImageDetailAuto && image.Detail != ImageDetailLow && image.Detail != ImageDetailHigh {
		return 0, fmt.Errorf("unsupported image detail %q", image.Detail)
	}
	hasURL := strings.TrimSpace(image.URL) != ""
	hasData := len(image.Data) != 0
	if hasURL == hasData {
		return 0, errors.New("input_image must set exactly one of url or data")
	}
	if hasURL {
		parsed, err := url.Parse(image.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return 0, errors.New("referenced input_image url must be an absolute HTTPS URL without userinfo")
		}
		if image.CheckpointMode != ImageCheckpointReference {
			return 0, errors.New("URL input_image checkpoint_mode must be reference")
		}
		if image.SizeBytes <= 0 {
			return 0, errors.New("URL input_image requires a positive caller-verified size_bytes")
		}
		if image.SizeBytes > MaxImageBytes {
			return 0, fmt.Errorf("input_image size_bytes %d exceeds %d", image.SizeBytes, MaxImageBytes)
		}
		return image.SizeBytes, nil
	}
	if image.CheckpointMode != ImageCheckpointEmbed {
		return 0, errors.New("data input_image checkpoint_mode must be embed")
	}
	actual := int64(len(image.Data))
	if actual > MaxImageBytes {
		return 0, fmt.Errorf("input_image data bytes %d exceeds %d", actual, MaxImageBytes)
	}
	if image.SizeBytes != actual {
		return 0, fmt.Errorf("embedded input_image size_bytes=%d does not match data length %d", image.SizeBytes, actual)
	}
	detected := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(image.Data), ";")[0]))
	if detected != mediaType {
		return 0, fmt.Errorf("embedded input_image media_type %q does not match detected %q", mediaType, detected)
	}
	return actual, nil
}

func supportedMediaType(value string) bool {
	switch value {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

// Text returns the concatenated text parts for logs and task labels. Image
// bytes and URLs are deliberately excluded.
func (input TurnInput) Text() string {
	var values []string
	for _, content := range input.Content {
		if content.Type == ContentInputText && strings.TrimSpace(content.Text) != "" {
			values = append(values, strings.TrimSpace(content.Text))
		}
	}
	return strings.Join(values, "\n")
}

func (input TurnInput) Equal(other TurnInput) bool {
	left, leftErr := input.canonicalJSON()
	right, rightErr := other.canonicalJSON()
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}
