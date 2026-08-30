package imagesession

import (
	"regexp"
	"strings"
)

var chatPromptPlaceholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func RenderChatPrompt(template, prompt, size, historyBlock string) string {
	if strings.TrimSpace(template) == "" {
		return strings.TrimSpace(prompt)
	}
	values := map[string]string{
		"prompt":        strings.TrimSpace(prompt),
		"size":          size,
		"history_block": historyBlock,
	}
	rendered := chatPromptPlaceholder.ReplaceAllStringFunc(template, func(match string) string {
		key := match[1 : len(match)-1]
		if value, ok := values[key]; ok {
			return value
		}
		return match
	})
	lines := make([]string, 0)
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	if out == "" {
		return strings.TrimSpace(prompt)
	}
	return out
}
