package durableagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/contextmgr"
	"github.com/yuqie6/agent-harness/internal/llm"
)

type modelTool struct {
	client                ChatClient
	stored                StoredChatClient
	streaming             StreamingChatClient
	textDeltaSink         TextDeltaSink
	workspace             string
	provider              ProviderSnapshot
	policy                Policy
	catalogSHA256         string
	toolContractSHA256    string
	legacyToolCompatible  bool
	editMode              string
	storedResponseTimeout time.Duration
}

func newModelTool(
	client ChatClient,
	workspace string,
	provider ProviderSnapshot,
	policy Policy,
	catalog []llm.Tool,
	toolContractSHA256 string,
	legacyToolCompatible bool,
	editMode string,
	storedResponseTimeout time.Duration,
	textDeltaSink TextDeltaSink,
) (*modelTool, error) {
	digest, err := catalogDigest(catalog)
	if err != nil {
		return nil, err
	}
	tool := &modelTool{
		client: client, workspace: workspace, provider: provider, policy: policy,
		catalogSHA256: digest, toolContractSHA256: toolContractSHA256,
		legacyToolCompatible: legacyToolCompatible, editMode: editMode,
		storedResponseTimeout: storedResponseTimeout, textDeltaSink: textDeltaSink,
	}
	tool.stored, _ = client.(StoredChatClient)
	tool.streaming, _ = client.(StreamingChatClient)
	return tool, nil
}

func (t *modelTool) Name() string { return modelToolName }
func (t *modelTool) Effect() durable.EffectClass {
	if t.stored != nil {
		return durable.EffectReconcilable
	}
	return durable.EffectOpaque
}

func (t *modelTool) Prepare(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	input, err := decodeModelInput(raw)
	if err != nil {
		return nil, err
	}
	if err := t.validate(input); err != nil {
		return nil, err
	}
	return json.Marshal(input)
}

func (t *modelTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	input, err := decodeModelInput(invocation.Prepared)
	if err != nil {
		return nil, err
	}
	if err := t.validate(input); err != nil {
		return nil, err
	}
	var message llm.Message
	var status string
	if t.stored != nil {
		message, status, err = createStoredChat(ctx, t.stored, invocation, input.Messages, input.Tools, t.storedResponseTimeout)
	} else if t.streaming != nil && t.textDeltaSink != nil {
		message, status, err = t.streaming.ChatStreamDurable(ctx, input.Messages, input.Tools, func(delta string) error {
			return t.textDeltaSink(ctx, TextDelta{
				JobID: invocation.JobID, StepID: invocation.StepID, AttemptID: invocation.AttemptID, Delta: delta,
			})
		})
	} else {
		message, status, err = t.client.Chat(ctx, input.Messages, input.Tools)
	}
	if err != nil {
		return nil, durableModelError(err)
	}
	message.Role = "assistant"
	return json.Marshal(modelResult{Message: message, Status: status})
}

func (t *modelTool) Reconcile(ctx context.Context, invocation durable.Invocation) (durable.ReconcileResult, error) {
	if t.stored == nil {
		return durable.ReconcileResult{}, errors.New("模型请求是 opaque effect,失联后不能自动重放")
	}
	message, status, state, err := reconcileStoredChat(ctx, t.stored, invocation, t.storedResponseTimeout)
	if err != nil {
		return durable.ReconcileResult{}, err
	}
	if state == durable.ReconcileUnknown {
		return durable.ReconcileResult{State: state, Detail: storedCheckpointDetail(invocation)}, nil
	}
	message.Role = "assistant"
	result, err := json.Marshal(modelResult{Message: message, Status: status})
	if err != nil {
		return durable.ReconcileResult{}, err
	}
	return durable.ReconcileResult{State: durable.ReconcileApplied, Result: result}, nil
}

func (t *modelTool) validate(input modelInput) error {
	if input.Version != protocolVersion {
		return fmt.Errorf("%w: durable agent 协议版本为 %d,当前需要 %d", durable.ErrConflict, input.Version, protocolVersion)
	}
	if input.Workspace != t.workspace {
		return fmt.Errorf("%w: durable agent workspace 从 %q 变为 %q", durable.ErrConflict, input.Workspace, t.workspace)
	}
	if normalizeProvider(input.Provider) != t.provider {
		return fmt.Errorf("%w: durable agent provider 配置已改变", durable.ErrConflict)
	}
	if input.Policy != t.policy {
		return fmt.Errorf("%w: durable agent 运行策略已改变", durable.ErrConflict)
	}
	if input.EditMode != "" && input.EditMode != t.editMode {
		return fmt.Errorf("%w: durable agent 编辑策略从 %q 变为 %q", durable.ErrConflict, input.EditMode, t.editMode)
	}
	digest, err := catalogDigest(input.Tools)
	if err != nil {
		return err
	}
	if digest != t.catalogSHA256 {
		return fmt.Errorf("%w: durable agent 工具目录已改变", durable.ErrConflict)
	}
	if input.ToolContractSHA256 == "" {
		if !t.legacyToolCompatible {
			return fmt.Errorf(
				"%w: durable agent 旧任务缺少工具 effect 快照,当前目录包含新副作用",
				durable.ErrConflict,
			)
		}
	} else if input.ToolContractSHA256 != t.toolContractSHA256 {
		return fmt.Errorf("%w: durable agent 工具执行契约已改变", durable.ErrConflict)
	}
	if len(input.Messages) == 0 {
		return errors.New("durable agent 模型输入没有消息")
	}
	tokens := contextmgr.TotalTokens(input.Messages) + contextmgr.EstimateToolsTokens(input.Tools)
	if tokens > input.Policy.ModelContextWindow {
		return fmt.Errorf("%w: 估算 %d tokens,窗口 %d", ErrContextBudget, tokens, input.Policy.ModelContextWindow)
	}
	return nil
}

func decodeModelInput(raw json.RawMessage) (modelInput, error) {
	var input modelInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, fmt.Errorf("durable agent 模型输入: %w", err)
	}
	input.Policy = normalizeStoredPolicy(input.Policy)
	return input, nil
}

func decodeModelResult(raw json.RawMessage) (modelResult, error) {
	var result modelResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("durable agent 模型结果: %w", err)
	}
	if result.Message.Role != "assistant" {
		return result, errors.New("durable agent 模型结果缺少 assistant message")
	}
	return result, nil
}

func catalogDigest(catalog []llm.Tool) (string, error) {
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return "", fmt.Errorf("编码 durable agent 工具目录: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
