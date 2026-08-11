package turn_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/yuqie6/agent-harness/turn"
)

func TestToolResultImagePersistenceContract(t *testing.T) {
	png := tinyPNG(t)
	result := turn.ToolResult{
		SchemaVersion: turn.ToolResultSchemaVersion,
		Content: []turn.ToolResultContent{
			{Type: turn.ContentInputText, Text: "selected gallery images"},
			{Type: turn.ContentInputImage, Image: &turn.InputImage{
				Data: png, MediaType: "image/png", SizeBytes: int64(len(png)),
				CheckpointMode: turn.ImageCheckpointEmbed, Detail: turn.ImageDetailHigh,
			}},
			{Type: turn.ContentInputImage, Image: &turn.InputImage{
				URL: "https://cdn.example/gallery.webp", MediaType: "image/webp", SizeBytes: 42,
				CheckpointMode: turn.ImageCheckpointReference,
			}},
		},
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "data:image/") {
		t.Fatalf("persistent result contains a wire data URL: %s", encoded)
	}
	var restored turn.ToolResult
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, restored) {
		t.Fatalf("restored result = %#v, want %#v", restored, result)
	}
	if got := result.Text(); got != "selected gallery images" {
		t.Fatalf("Text = %q", got)
	}
}

func TestToolResultEnforcesImageCountAndByteLimits(t *testing.T) {
	image := func(size int64) turn.ToolResultContent {
		return turn.ToolResultContent{Type: turn.ContentInputImage, Image: &turn.InputImage{
			URL: "https://cdn.example/gallery.png", MediaType: "image/png", SizeBytes: size,
			CheckpointMode: turn.ImageCheckpointReference,
		}}
	}
	tooMany := turn.ToolResult{SchemaVersion: turn.ToolResultSchemaVersion}
	for range turn.MaxImagesPerToolResult + 1 {
		tooMany.Content = append(tooMany.Content, image(1))
	}
	if err := tooMany.Validate(); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("too many images error = %v", err)
	}

	tooLarge := turn.ToolResult{SchemaVersion: turn.ToolResultSchemaVersion}
	for range 4 {
		tooLarge.Content = append(tooLarge.Content, image(turn.MaxToolResultImageBytes))
	}
	if err := tooLarge.Validate(); err == nil || !strings.Contains(err.Error(), "bytes total") {
		t.Fatalf("total image bytes error = %v", err)
	}

	oversized := turn.ToolResult{
		SchemaVersion: turn.ToolResultSchemaVersion,
		Content:       []turn.ToolResultContent{image(turn.MaxToolResultImageBytes + 1)},
	}
	if err := oversized.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("single image bytes error = %v", err)
	}
}
