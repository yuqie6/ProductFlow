package llm

import "encoding/json"

// ToolResult keeps legacy string output distinct from native Responses content
// output. Embedded image bytes remain structured until request encoding.
type ToolResult struct {
	Text         *string
	ContentParts []ContentPart
}

func TextToolResult(text string) ToolResult { return ToolResult{Text: &text} }

func ContentToolResult(parts []ContentPart) ToolResult {
	return ToolResult{ContentParts: cloneContentParts(parts)}
}

func (result ToolResult) String() string {
	if result.Text != nil {
		return *result.Text
	}
	return Message{ContentParts: result.ContentParts}.String()
}

func (result ToolResult) HasContentParts() bool { return len(result.ContentParts) != 0 }

func (result ToolResult) Message(callID string) Message {
	if len(result.ContentParts) != 0 {
		return Message{Role: "tool", ToolCallID: callID, ContentParts: cloneContentParts(result.ContentParts)}
	}
	text := ""
	if result.Text != nil {
		text = *result.Text
	}
	if text == "" {
		text = "(工具返回空输出)"
	}
	return Message{Role: "tool", ToolCallID: callID, Content: &text}
}

func cloneContentParts(parts []ContentPart) []ContentPart {
	cloned := make([]ContentPart, len(parts))
	copy(cloned, parts)
	for index := range cloned {
		cloned[index].EmbeddedData = append([]byte(nil), parts[index].EmbeddedData...)
	}
	return cloned
}

// CloneMessages copies persisted conversation messages without sharing image
// bytes, tool arguments, or raw Responses output items with the caller.
func CloneMessages(messages []Message) []Message {
	cloned := make([]Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		if message.Content != nil {
			content := *message.Content
			cloned[index].Content = &content
		}
		cloned[index].ContentParts = cloneContentParts(message.ContentParts)
		cloned[index].ToolCalls = make([]ToolCall, len(message.ToolCalls))
		copy(cloned[index].ToolCalls, message.ToolCalls)
		for callIndex := range cloned[index].ToolCalls {
			cloned[index].ToolCalls[callIndex].Function.Arguments = append(
				json.RawMessage(nil), message.ToolCalls[callIndex].Function.Arguments...,
			)
		}
		cloned[index].ResponseItems = make([]json.RawMessage, len(message.ResponseItems))
		for itemIndex := range message.ResponseItems {
			cloned[index].ResponseItems[itemIndex] = append(
				json.RawMessage(nil), message.ResponseItems[itemIndex]...,
			)
		}
	}
	return cloned
}
