package turn_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/yuqie6/agent-harness/turn"
)

func TestTurnInputImagePersistenceContract(t *testing.T) {
	png := tinyPNG(t)
	input := turn.TurnInput{
		SchemaVersion: turn.InputSchemaVersion,
		Content: []turn.InputContent{
			{Type: turn.ContentInputText, Text: "inspect the image"},
			{Type: turn.ContentInputImage, Image: &turn.InputImage{
				Data: png, MediaType: "image/png", SizeBytes: int64(len(png)),
				CheckpointMode: turn.ImageCheckpointEmbed, Detail: turn.ImageDetailHigh,
			}},
			{Type: turn.ContentInputImage, Image: &turn.InputImage{
				URL: "https://cdn.example/image.webp", MediaType: "image/webp", SizeBytes: 42,
				CheckpointMode: turn.ImageCheckpointReference,
			}},
		},
	}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored turn.TurnInput
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !input.Equal(restored) || len(restored.Content[1].Image.Data) != len(png) || restored.Content[2].Image.URL == "" {
		t.Fatalf("restored input = %#v", restored)
	}
}

func TestTurnInputRejectsAmbiguousOrUnverifiedImages(t *testing.T) {
	png := tinyPNG(t)
	gif, err := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==")
	if err != nil {
		t.Fatal(err)
	}
	tests := []turn.InputImage{
		{URL: "http://cdn.example/a.png", MediaType: "image/png", SizeBytes: 1, CheckpointMode: turn.ImageCheckpointReference},
		{URL: "https://cdn.example/a.png", Data: png, MediaType: "image/png", SizeBytes: int64(len(png)), CheckpointMode: turn.ImageCheckpointEmbed},
		{URL: "https://cdn.example/a.png", MediaType: "image/png", CheckpointMode: turn.ImageCheckpointReference},
		{Data: png, MediaType: "image/jpeg", SizeBytes: int64(len(png)), CheckpointMode: turn.ImageCheckpointEmbed},
		{Data: png, MediaType: "image/png", SizeBytes: int64(len(png)) + 1, CheckpointMode: turn.ImageCheckpointEmbed},
		{Data: gif, MediaType: "image/gif", SizeBytes: int64(len(gif)), CheckpointMode: turn.ImageCheckpointEmbed},
	}
	for index, image := range tests {
		input := turn.TurnInput{SchemaVersion: turn.InputSchemaVersion, Content: []turn.InputContent{{Type: turn.ContentInputImage, Image: &image}}}
		if err := input.Validate(); err == nil {
			t.Fatalf("case %d accepted invalid image", index)
		}
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	value, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return value
}
