package contextmgr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/agent-harness/internal/llm"
)

const (
	minimumRecentSpans = 2
)

// Summary 是从原始历史派生出的累积压缩产物。原始消息不会被覆盖或删除。
type Summary struct {
	Version             int       `json:"version"`
	ThroughMessageCount int       `json:"through_message_count"`
	Content             string    `json:"content"`
	SourceDigest        string    `json:"source_digest"`
	CreatedAt           time.Time `json:"created_at"`
}

// verbatimUserMessage 是每次构造请求时从原始历史重建的用户输入。
type verbatimUserMessage struct {
	MessageIndex int    `json:"message_index"`
	Content      string `json:"content"`
}

// CompactionPlan 描述本轮应纳入摘要的新消息边界。
type CompactionPlan struct {
	Version             int
	ThroughMessageCount int
	ExistingSummary     string
	Messages            []llm.Message
	SourceDigest        string
}

// PlanCompaction 在工作上下文超过显式阈值时选择最旧的完整用户 Turn
// 或工具轮次进行累积压缩。
func PlanCompaction(messages []llm.Message, current *Summary, tokenLimit int) (CompactionPlan, bool) {
	if tokenLimit <= 0 {
		return CompactionPlan{}, false
	}
	systemEnd := leadingSystemEnd(messages)
	start := systemEnd
	version := 1
	existing := ""
	if SummaryMatchesMessages(current, messages) && current.ThroughMessageCount >= systemEnd {
		start = current.ThroughMessageCount
		version = current.Version + 1
		existing = current.Content
	}
	spans := conversationSpans(messages, start)
	if len(spans) <= minimumRecentSpans {
		return CompactionPlan{}, false
	}
	prefix := append([]llm.Message(nil), messages[:systemEnd]...)
	if existing != "" {
		prefix = append(prefix, summaryMessages(*current, verbatimUserMessages(messages, current.ThroughMessageCount))...)
	}
	if windowTokens(prefix, messages, spans) <= tokenLimit {
		return CompactionPlan{}, false
	}

	remainingTokens := windowTokens(nil, messages, spans)
	targetTokens := max(1, tokenLimit-TotalTokens(prefix))
	retireCount := 0
	for retireCount < len(spans)-minimumRecentSpans {
		remainingTokens -= TotalTokens(messages[spans[retireCount].start:spans[retireCount].end])
		retireCount++
		if remainingTokens <= targetTokens {
			break
		}
	}
	if retireCount == 0 {
		return CompactionPlan{}, false
	}
	through := spans[retireCount-1].end
	return CompactionPlan{
		Version:             version,
		ThroughMessageCount: through,
		ExistingSummary:     existing,
		Messages:            append([]llm.Message(nil), messages[start:through]...),
		SourceDigest:        digestMessages(messages[:through]),
	}, true
}

// CompactionInput 把待压缩消息转换成不含 opaque reasoning 的结构化文本。
func CompactionInput(plan CompactionPlan) string {
	type compactCall struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	type compactMessage struct {
		Role       string        `json:"role"`
		Content    string        `json:"content,omitempty"`
		ToolCallID string        `json:"tool_call_id,omitempty"`
		ToolCalls  []compactCall `json:"tool_calls,omitempty"`
	}
	payload := struct {
		PreviousSummary string           `json:"previous_summary,omitempty"`
		Messages        []compactMessage `json:"new_messages"`
	}{PreviousSummary: plan.ExistingSummary, Messages: make([]compactMessage, 0, len(plan.Messages))}
	for _, message := range plan.Messages {
		compact := compactMessage{Role: message.Role, Content: message.String(), ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			compact.ToolCalls = append(compact.ToolCalls, compactCall{
				ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments,
			})
		}
		payload.Messages = append(payload.Messages, compact)
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

// CompleteCompaction 将模型输出绑定到原始消息边界和摘要版本。
func CompleteCompaction(plan CompactionPlan, content string, createdAt time.Time, maxSummaryChars int) (Summary, bool) {
	content = strings.TrimSpace(content)
	if content == "" || maxSummaryChars <= 0 || plan.Version <= 0 || plan.ThroughMessageCount <= 0 || plan.SourceDigest == "" {
		return Summary{}, false
	}
	if utf8.RuneCountInString(content) > maxSummaryChars {
		return Summary{}, false
	}
	return Summary{
		Version:             plan.Version,
		ThroughMessageCount: plan.ThroughMessageCount,
		Content:             content,
		SourceDigest:        plan.SourceDigest,
		CreatedAt:           createdAt,
	}, true
}

func ValidSummary(summary *Summary, messageCount int) bool {
	return summary != nil && summary.Version > 0 && summary.ThroughMessageCount > 0 &&
		summary.ThroughMessageCount <= messageCount && strings.TrimSpace(summary.Content) != "" && summary.SourceDigest != ""
}

// SummaryMatchesMessages 验证摘要声明的来源边界确实对应当前原始历史。
func SummaryMatchesMessages(summary *Summary, messages []llm.Message) bool {
	return ValidSummary(summary, len(messages)) &&
		digestMessages(messages[:summary.ThroughMessageCount]) == summary.SourceDigest
}

func CloneSummary(summary *Summary) *Summary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	return &cloned
}

// VerbatimUserMessageCount 返回有效摘要覆盖范围内会逐字注入的历史用户消息数。
func VerbatimUserMessageCount(messages []llm.Message, summary *Summary) int {
	if !SummaryMatchesMessages(summary, messages) {
		return 0
	}
	return len(verbatimUserMessages(messages, summary.ThroughMessageCount))
}

func verbatimUserMessages(messages []llm.Message, through int) []verbatimUserMessage {
	through = min(max(through, 0), len(messages))
	pinned := make([]verbatimUserMessage, 0)
	for index, message := range messages[:through] {
		if message.Role == "user" {
			pinned = append(pinned, verbatimUserMessage{MessageIndex: index, Content: message.String()})
		}
	}
	return pinned
}

func digestMessages(messages []llm.Message) string {
	hash := sha256.New()
	for _, message := range messages {
		hash.Write([]byte(message.Role))
		hash.Write([]byte{0})
		hash.Write([]byte(message.String()))
		hash.Write([]byte{0})
		hash.Write([]byte(message.ToolCallID))
		for _, call := range message.ToolCalls {
			hash.Write([]byte(call.ID))
			hash.Write([]byte(call.Function.Name))
			hash.Write(call.Function.Arguments)
		}
		for _, item := range message.ResponseItems {
			hash.Write(item)
		}
		for _, part := range message.ContentParts {
			encoded, _ := json.Marshal(part)
			hash.Write(encoded)
		}
		hash.Write([]byte{0xff})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
