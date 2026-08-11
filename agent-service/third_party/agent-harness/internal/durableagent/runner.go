package durableagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/internal/skills"
	"github.com/yuqie6/agent-harness/internal/tools"
)

type Runner struct {
	engine             *durable.Engine
	model              *modelTool
	compact            *compactionTool
	workspace          string
	system             string
	provider           ProviderSnapshot
	policy             Policy
	catalog            []llm.Tool
	allowed            map[string]bool
	editMode           string
	reviewEdit         bool
	toolContractSHA256 string
	resultCodecs       map[string]toolResultCodec
	checks             *tools.NamedCheckSet
	requiredChecks     []string
	requiredArtifact   string
}

func Open(config Config) (*Runner, error) {
	if config.Client == nil {
		return nil, errors.New("durable agent 缺少 Responses client")
	}
	if strings.TrimSpace(config.Database) == "" {
		return nil, errors.New("durable agent 缺少 journal 路径")
	}
	workspace, err := resolveWorkspace(config.Workspace)
	if err != nil {
		return nil, err
	}
	provider := normalizeProvider(config.Provider)
	if provider.WireAPI != "responses" || provider.BaseURL == "" || provider.Model == "" {
		return nil, errors.New("durable agent 需要完整的 Responses provider 配置")
	}
	policy := config.Policy
	if policy.MaxIterations <= 0 {
		return nil, errors.New("durable agent MaxIterations 必须显式设置为正数")
	}
	if policy.ModelContextWindow <= 0 {
		return nil, errors.New("durable agent ModelContextWindow 必须显式设置为正数")
	}
	if policy.AutoCompactTokenLimit < 0 || policy.AutoCompactTokenLimit > policy.ModelContextWindow {
		return nil, errors.New("durable agent AutoCompactTokenLimit 超出模型上下文")
	}
	if policy.CompactionSummaryMaxChars < 0 ||
		policy.AutoCompactTokenLimit > 0 && policy.CompactionSummaryMaxChars == 0 {
		return nil, errors.New("durable agent 启用自动压缩时 CompactionSummaryMaxChars 必须为正数")
	}
	if policy.AutoCompactTokenLimit == 0 {
		policy.CompactionSummaryMaxChars = 0
	}
	if policy.SkillCatalogMaxBytes < 0 {
		return nil, errors.New("durable agent SkillCatalogMaxBytes 不能为负数")
	}
	if config.AllowEdit && config.ReviewEdit {
		return nil, errors.New("durable agent 的 AllowEdit 与 ReviewEdit 不能同时启用")
	}
	editMode := editModeReadOnly
	if config.ReviewEdit {
		editMode = editModeReview
	} else if config.AllowEdit {
		editMode = editModeAuto
	}
	if _, stored := config.Client.(StoredChatClient); stored && config.StoredResponseTimeout <= 0 {
		return nil, errors.New("durable agent stored response timeout 必须显式设置为正数")
	}

	builtinSet := tools.BuiltinTools(workspace)
	defer builtinSet.Close()
	builtins := tools.NewRegistry(builtinSet.Tools...)
	readSource, ok := builtins.Lookup("read_file")
	if !ok {
		return nil, errors.New("内置 read_file 不存在")
	}
	read, err := newDurableReadTool(workspace, readSource)
	if err != nil {
		return nil, err
	}
	skillCatalog, err := skills.Discover(skills.Options{WorkingDir: workspace, UserHome: config.SkillUserHome})
	if err != nil {
		return nil, err
	}
	skillSource := skillCatalog.Tool()
	skillTool, err := newDurablePureTool(skillSource, nil)
	if err != nil {
		return nil, err
	}
	visible := tools.NewRegistry(readSource, skillSource, questionSchema())
	registered := []durable.Tool{read, skillTool, durableQuestionTool{}, rejectionTool()}
	allowed := map[string]bool{"read_file": true, skills.LoadToolName: true, (durableQuestionTool{}).Name(): true}
	rawToolResults := make(map[string]bool, len(config.ExternalTools))
	for _, name := range []string{"list_dir", "find_files", "search_text"} {
		source, ok := builtins.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("内置 %s 不存在", name)
		}
		adapter, err := newDurablePureTool(source, nil)
		if err != nil {
			return nil, err
		}
		visible.Register(source)
		registered = append(registered, adapter)
		allowed[name] = true
	}
	for _, source := range config.ReadTools {
		if _, duplicate := visible.Lookup(source.Name); duplicate || source.Name == modelToolName ||
			source.Name == compactionToolName || source.Name == editReviewToolName || source.Name == rejectionToolName {
			return nil, fmt.Errorf("durable agent 只读工具重名: %s", source.Name)
		}
		if source.Capabilities != tools.CapabilityRead || source.Effect != tools.EffectPure || !source.HasHandler() || source.AutoApprove == nil {
			return nil, fmt.Errorf("durable agent 扩展工具 %s 必须是可自动批准的 pure read 工具", source.Name)
		}
		adapter, err := newDurablePureTool(source, nil)
		if err != nil {
			return nil, err
		}
		visible.Register(source)
		registered = append(registered, adapter)
		allowed[source.Name] = true
	}
	for _, external := range config.ExternalTools {
		source := external.Schema
		if external.Tool == nil || source.Name == "" || source.Name != external.Tool.Name() {
			return nil, errors.New("durable agent external tool schema and implementation must have the same name")
		}
		if _, duplicate := visible.Lookup(source.Name); duplicate || source.Name == modelToolName ||
			source.Name == compactionToolName || source.Name == editReviewToolName || source.Name == rejectionToolName {
			return nil, fmt.Errorf("durable agent external tool name conflicts with %s", source.Name)
		}
		switch external.Tool.Effect() {
		case durable.EffectIdempotent, durable.EffectReconcilable, durable.EffectOpaque:
		default:
			return nil, fmt.Errorf("durable agent external tool %s must declare a side-effect recovery contract", source.Name)
		}
		visible.Register(source)
		registered = append(registered, external.Tool)
		allowed[source.Name] = true
		rawToolResults[source.Name] = true
	}
	var checkSet *tools.NamedCheckSet
	if len(config.Checks) > 0 {
		checkSet, _, err = tools.NewNamedCheckSet(workspace, config.Checks)
		if err != nil {
			return nil, err
		}
		checkSource := checkSet.Tool()
		visible.Register(checkSource)
		checkTool, err := newDurablePureTool(checkSource, checkToolSnapshot(checkSet))
		if err != nil {
			return nil, err
		}
		registered = append(registered, checkTool)
		allowed[tools.RunCheckToolName] = true
	}
	if config.AllowEdit || config.ReviewEdit {
		editSource, ok := builtins.Lookup(durable.FileEditToolName)
		if !ok {
			return nil, errors.New("内置 edit_file 不存在")
		}
		edit, err := durable.NewFileEditTool(workspace)
		if err != nil {
			return nil, err
		}
		if config.ReviewEdit {
			editSource.Description += editReviewDescription
			registered = append(registered, newEditReviewTool(edit))
		}
		visible.Register(editSource)
		registered = append(registered, edit)
		allowed[durable.FileEditToolName] = true
	}
	catalog := visible.Schemas()
	checkContractSHA256, err := checkSet.ContractSHA256()
	if err != nil {
		return nil, err
	}
	legacyToolCompatible, err := legacyToolEffectsCompatible(catalog, registered)
	if err != nil {
		return nil, err
	}
	toolContractSHA256, resultCodecs, err := buildToolExecutionContract(
		catalog, registered, rawToolResults, config.ReviewEdit, checkContractSHA256,
	)
	if err != nil {
		return nil, err
	}
	model, err := newModelTool(
		config.Client, workspace, provider, policy, catalog, toolContractSHA256,
		legacyToolCompatible, editMode, config.StoredResponseTimeout, config.TextDeltaSink,
	)
	if err != nil {
		return nil, err
	}
	compact, err := newCompactionTool(
		config.Client, workspace, provider, policy, catalog, toolContractSHA256, config.StoredResponseTimeout,
	)
	if err != nil {
		return nil, err
	}
	registered = append(registered, model, compact)
	engine, err := durable.Open(config.Database, config.EngineOptions, registered...)
	if err != nil {
		return nil, err
	}
	system := strings.TrimSpace(config.System)
	for _, instruction := range config.Instructions {
		if instruction = strings.TrimSpace(instruction); instruction != "" {
			system = strings.TrimSpace(system + "\n\n" + instruction)
		}
	}
	if checkSet != nil {
		system = strings.TrimSpace(system + "\n\n" + checkInstruction(checkSet))
	}
	if instruction := skillCatalog.Instruction(policy.SkillCatalogMaxBytes); instruction != "" {
		system = strings.TrimSpace(system + "\n\n" + instruction)
	}
	return &Runner{
		engine: engine, model: model, compact: compact, workspace: workspace, system: system,
		provider: provider, policy: policy, catalog: catalog, allowed: allowed, reviewEdit: config.ReviewEdit,
		editMode:           editMode,
		toolContractSHA256: toolContractSHA256, resultCodecs: resultCodecs,
		checks: checkSet, requiredChecks: checkSet.RequiredNames(), requiredArtifact: strings.TrimSpace(config.RequiredArtifact),
	}, nil
}

func checkInstruction(checks *tools.NamedCheckSet) string {
	required := checks.RequiredNames()
	if len(required) == 0 {
		return "run_check 可运行操作者声明的隔离诊断；只能选择工具 schema 中的名称，不能自行构造命令。"
	}
	return "完成任务前必须在最后一次 edit_file 之后成功调用这些 run_check 验证门: " + strings.Join(required, ", ") + "。验证失败时修复后重新运行。"
}

func (r *Runner) Close() error {
	if r == nil || r.engine == nil {
		return nil
	}
	return r.engine.Close()
}

func (r *Runner) Start(ctx context.Context, prompt string) (Result, error) {
	job, err := r.Submit(ctx, prompt)
	if err != nil {
		return Result{}, err
	}
	return r.continueJob(ctx, job.ID)
}

func (r *Runner) Resume(ctx context.Context, jobID string) (Result, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return Result{}, errors.New("durable agent resume 缺少 task ID")
	}
	return r.continueJob(ctx, jobID)
}

// Submit persists the first model request without executing it. Call Resume
// with the returned ID to drive the task to its next terminal state.
func (r *Runner) Submit(ctx context.Context, prompt string) (durable.Job, error) {
	jobID, err := newTaskID()
	if err != nil {
		return durable.Job{}, err
	}
	return r.SubmitWithID(ctx, jobID, prompt)
}

// SubmitMessage persists a validated user message without executing it.
func (r *Runner) SubmitMessage(ctx context.Context, label string, message llm.Message) (durable.Job, error) {
	jobID, err := newTaskID()
	if err != nil {
		return durable.Job{}, err
	}
	return r.SubmitMessageWithID(ctx, jobID, label, message)
}

// SubmitWithID persists a task under a caller-owned stable ID. This lets an
// outer durable control plane bind the ID before task submission.
func (r *Runner) SubmitWithID(ctx context.Context, jobID, prompt string) (durable.Job, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return durable.Job{}, errors.New("durable agent task ID 不能为空")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return durable.Job{}, errors.New("durable agent 任务不能为空")
	}
	return r.SubmitMessageWithID(ctx, jobID, prompt, llm.Message{Role: "user", Content: &prompt})
}

func (r *Runner) SubmitMessageWithID(ctx context.Context, jobID, label string, message llm.Message) (durable.Job, error) {
	if message.Role != "user" || message.Content == nil && len(message.ContentParts) == 0 {
		return durable.Job{}, errors.New("durable agent 任务消息必须是非空 user message")
	}
	return r.SubmitMessagesWithID(ctx, jobID, label, []llm.Message{message})
}

// SubmitMessagesWithID persists a new task with an application-owned
// conversation transcript. The runner prepends its trusted system prompt; the
// supplied transcript must end with the new user message.
func (r *Runner) SubmitMessagesWithID(
	ctx context.Context,
	jobID, label string,
	messages []llm.Message,
) (durable.Job, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return durable.Job{}, errors.New("durable agent task ID 不能为空")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return durable.Job{}, errors.New("durable agent 任务标签不能为空")
	}
	if err := validateConversationMessages(messages); err != nil {
		return durable.Job{}, err
	}
	conversation := llm.CloneMessages(messages)
	seed := make([]llm.Message, 0, len(conversation)+1)
	if r.system != "" {
		system := r.system
		seed = append(seed, llm.Message{Role: "system", Content: &system})
	}
	seed = append(seed, conversation...)
	step, err := r.modelStep(jobID, 1, seed)
	if err != nil {
		return durable.Job{}, err
	}
	return r.engine.Submit(ctx, durable.JobSpec{
		ID: jobID, Kind: durable.JobKindAgentTurn, Name: taskName(label), Steps: []durable.StepSpec{step},
	})
}

func validateConversationMessages(messages []llm.Message) error {
	if len(messages) == 0 {
		return errors.New("durable agent 对话历史不能为空")
	}
	last := messages[len(messages)-1]
	if last.Role != "user" || last.Content == nil && len(last.ContentParts) == 0 {
		return errors.New("durable agent 对话历史必须以非空 user message 结束")
	}
	for index, message := range messages {
		switch message.Role {
		case "system", "user":
			if message.Content == nil && len(message.ContentParts) == 0 {
				return fmt.Errorf("durable agent 对话消息 %d 内容为空", index)
			}
		case "assistant":
			if message.Content == nil && len(message.ContentParts) == 0 && len(message.ToolCalls) == 0 && len(message.ResponseItems) == 0 {
				return fmt.Errorf("durable agent 对话消息 %d assistant 内容为空", index)
			}
		case "tool":
			if strings.TrimSpace(message.ToolCallID) == "" || message.Content == nil && len(message.ContentParts) == 0 {
				return fmt.Errorf("durable agent 对话消息 %d tool 结果无效", index)
			}
		default:
			return fmt.Errorf("durable agent 对话消息 %d 角色 %q 无效", index, message.Role)
		}
	}
	return nil
}

func resolveWorkspace(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		value = "."
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("durable agent workspace 不是目录: %s", resolved)
	}
	return resolved, nil
}

func normalizeProvider(provider ProviderSnapshot) ProviderSnapshot {
	provider.WireAPI = strings.ToLower(strings.TrimSpace(provider.WireAPI))
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	provider.Model = strings.TrimSpace(provider.Model)
	provider.ResponseMode = strings.ToLower(strings.TrimSpace(provider.ResponseMode))
	if provider.ResponseMode == "" {
		// Snapshots written before response_mode existed always used stored Responses.
		provider.ResponseMode = "stored_background"
	}
	provider.ReasoningEffort = strings.TrimSpace(provider.ReasoningEffort)
	provider.ReasoningSummary = strings.TrimSpace(provider.ReasoningSummary)
	provider.TextVerbosity = strings.TrimSpace(provider.TextVerbosity)
	provider.ServiceTier = strings.TrimSpace(provider.ServiceTier)
	return provider
}

func newTaskID() (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "agent-" + hex.EncodeToString(random), nil
}

func taskName(prompt string) string {
	runes := []rune(prompt)
	if len(runes) > 120 {
		runes = runes[:120]
	}
	return "agent: " + string(runes)
}
