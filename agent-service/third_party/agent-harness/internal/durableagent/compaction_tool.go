package durableagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/contextmgr"
	"github.com/yuqie6/agent-harness/internal/llm"
)

type compactionTool struct {
	client                ChatClient
	stored                StoredChatClient
	workspace             string
	provider              ProviderSnapshot
	policy                Policy
	catalogSHA256         string
	toolContractSHA256    string
	catalogTokens         int
	storedResponseTimeout time.Duration
}

func newCompactionTool(
	client ChatClient,
	workspace string,
	provider ProviderSnapshot,
	policy Policy,
	catalog []llm.Tool,
	toolContractSHA256 string,
	storedResponseTimeout time.Duration,
) (*compactionTool, error) {
	digest, err := catalogDigest(catalog)
	if err != nil {
		return nil, err
	}
	tool := &compactionTool{
		client: client, workspace: workspace, provider: provider, policy: policy,
		catalogSHA256: digest, toolContractSHA256: toolContractSHA256,
		catalogTokens:         contextmgr.EstimateToolsTokens(catalog),
		storedResponseTimeout: storedResponseTimeout,
	}
	tool.stored, _ = client.(StoredChatClient)
	return tool, nil
}

func (t *compactionTool) Name() string { return compactionToolName }
func (t *compactionTool) Effect() durable.EffectClass {
	if t.stored != nil {
		return durable.EffectReconcilable
	}
	return durable.EffectOpaque
}

func (t *compactionTool) Prepare(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	input, err := decodeCompactionInput(raw)
	if err != nil {
		return nil, err
	}
	if _, err := t.validate(input); err != nil {
		return nil, err
	}
	return json.Marshal(input)
}

func (t *compactionTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	input, err := decodeCompactionInput(invocation.Prepared)
	if err != nil {
		return nil, err
	}
	layout, err := t.validate(input)
	if err != nil {
		return nil, err
	}
	messages, err := compactionRequestMessages(input, layout)
	if err != nil {
		return nil, err
	}
	var response llm.Message
	var status string
	if t.stored != nil {
		response, status, err = createStoredChat(ctx, t.stored, invocation, messages, nil, t.storedResponseTimeout)
	} else {
		response, status, err = t.client.Chat(ctx, messages, nil)
	}
	if err != nil {
		return nil, durableModelError(err)
	}
	return t.resultJSON(input, layout, response, status)
}

func (t *compactionTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	if t.stored == nil {
		return durable.ReconcileResult{}, errors.New("上下文压缩是 opaque effect,失联后不能自动重放")
	}
	input, err := decodeCompactionInput(invocation.Prepared)
	if err != nil {
		return durable.ReconcileResult{}, err
	}
	layout, err := t.validate(input)
	if err != nil {
		return durable.ReconcileResult{}, err
	}
	response, status, state, err := reconcileStoredChat(ctx, t.stored, invocation, t.storedResponseTimeout)
	if err != nil {
		return durable.ReconcileResult{}, err
	}
	if state == durable.ReconcileUnknown {
		return durable.ReconcileResult{State: state, Detail: storedCheckpointDetail(invocation)}, nil
	}
	result, err := t.resultJSON(input, layout, response, status)
	if err != nil {
		return durable.ReconcileResult{}, err
	}
	return durable.ReconcileResult{State: durable.ReconcileApplied, Result: result}, nil
}

func (t *compactionTool) resultJSON(
	input compactionInput,
	layout durableContextLayout,
	response llm.Message,
	status string,
) (json.RawMessage, error) {
	if len(response.ToolCalls) != 0 {
		return nil, errors.New("durable agent 压缩请求返回了工具调用")
	}
	summary, err := t.fitSummary(input, layout, response.String())
	if err != nil {
		return nil, err
	}
	return json.Marshal(compactionResult{
		SummaryVersion: input.SummaryVersion,
		SourceDigest:   input.SourceDigest,
		Summary:        summary,
		Status:         status,
	})
}

func (t *compactionTool) validate(input compactionInput) (durableContextLayout, error) {
	if input.ProtocolVersion != compactionProtocolVersion {
		return durableContextLayout{}, fmt.Errorf("%w: durable compaction 协议版本为 %d,当前需要 %d", durable.ErrConflict, input.ProtocolVersion, compactionProtocolVersion)
	}
	if input.Workspace != t.workspace {
		return durableContextLayout{}, fmt.Errorf("%w: durable agent workspace 从 %q 变为 %q", durable.ErrConflict, input.Workspace, t.workspace)
	}
	if normalizeProvider(input.Provider) != t.provider {
		return durableContextLayout{}, fmt.Errorf("%w: durable agent provider 配置已改变", durable.ErrConflict)
	}
	if input.Policy != t.policy {
		return durableContextLayout{}, fmt.Errorf("%w: durable agent 运行策略已改变", durable.ErrConflict)
	}
	if input.CatalogSHA256 != t.catalogSHA256 || input.CatalogTokens != t.catalogTokens {
		return durableContextLayout{}, fmt.Errorf("%w: durable agent 工具目录已改变", durable.ErrConflict)
	}
	if input.ToolContractSHA256 != "" && input.ToolContractSHA256 != t.toolContractSHA256 {
		return durableContextLayout{}, fmt.Errorf("%w: durable agent 工具执行契约已改变", durable.ErrConflict)
	}
	if input.MaxSummaryRunes <= 0 || input.MaxSummaryRunes > input.Policy.CompactionSummaryMaxChars {
		return durableContextLayout{}, errors.New("durable agent 压缩摘要长度无效")
	}
	digest, err := digestDurableMessages(input.Messages)
	if err != nil {
		return durableContextLayout{}, err
	}
	if digest != input.SourceDigest {
		return durableContextLayout{}, fmt.Errorf("%w: durable agent 压缩来源已改变", durable.ErrConflict)
	}
	layout, err := parseDurableContext(input.Messages)
	if err != nil {
		return durableContextLayout{}, err
	}
	if input.SummaryVersion != layout.previousVersion+1 {
		return durableContextLayout{}, errors.New("durable agent 压缩摘要版本不连续")
	}
	retired := 0
	for index, round := range layout.rounds {
		if round.end == input.RetireThrough {
			retired = index + 1
			break
		}
	}
	if retired == 0 || len(layout.rounds)-retired < layout.minimumRetained {
		return durableContextLayout{}, errors.New("durable agent 压缩范围不是安全的完整对话边界")
	}
	request, err := compactionRequestMessages(input, layout)
	if err != nil {
		return durableContextLayout{}, err
	}
	if tokens := contextmgr.TotalTokens(request); tokens > input.Policy.ModelContextWindow {
		return durableContextLayout{}, fmt.Errorf("%w: 压缩请求估算 %d tokens,窗口 %d", ErrContextBudget, tokens, input.Policy.ModelContextWindow)
	}
	return layout, nil
}

func (t *compactionTool) fitSummary(input compactionInput, layout durableContextLayout, content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("durable agent 压缩模型返回了空摘要")
	}
	if utf8.RuneCountInString(content) > input.MaxSummaryRunes {
		return "", fmt.Errorf("durable agent 压缩摘要超过 %d 个字符；拒绝静默截断", input.MaxSummaryRunes)
	}
	messages := assembleCompactedMessages(input, layout, content)
	if tokens := contextmgr.TotalTokens(messages) + input.CatalogTokens; tokens > input.Policy.ModelContextWindow {
		return "", fmt.Errorf("%w: 完整摘要使请求达到 %d tokens,窗口 %d；拒绝静默截断",
			ErrContextBudget, tokens, input.Policy.ModelContextWindow)
	}
	return content, nil
}

func (t *compactionTool) compactedMessages(input compactionInput, result compactionResult) ([]llm.Message, error) {
	layout, err := t.validate(input)
	if err != nil {
		return nil, err
	}
	if result.SummaryVersion != input.SummaryVersion || result.SourceDigest != input.SourceDigest {
		return nil, fmt.Errorf("%w: durable agent 压缩结果与来源不匹配", durable.ErrConflict)
	}
	result.Summary = strings.TrimSpace(result.Summary)
	if result.Summary == "" || utf8.RuneCountInString(result.Summary) > input.MaxSummaryRunes {
		return nil, errors.New("durable agent 压缩结果正文无效")
	}
	messages := assembleCompactedMessages(input, layout, result.Summary)
	if tokens := contextmgr.TotalTokens(messages) + input.CatalogTokens; tokens > input.Policy.ModelContextWindow {
		return nil, fmt.Errorf("%w: 压缩后估算 %d tokens,窗口 %d", ErrContextBudget, tokens, input.Policy.ModelContextWindow)
	}
	return messages, nil
}
