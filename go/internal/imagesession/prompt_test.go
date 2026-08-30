package imagesession

import (
	"strings"
	"testing"
)

func TestRenderChatPromptMatchesPythonPlaceholders(t *testing.T) {
	got := RenderChatPrompt(
		"Create an image from the current user request.\nOutput size: {size}\n{history_block}\nCurrent user request:\n{prompt}\nGenerate the image directly. Do not return explanatory text.",
		"小猫",
		"1024x1024",
		"",
	)
	if got == "小猫" {
		t.Fatal("template should wrap the user request")
	}
	for _, part := range []string{"Create an image", "1024x1024", "小猫", "Do not return explanatory text"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
}
