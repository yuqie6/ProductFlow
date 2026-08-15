// Package agenttask exposes the recoverable model/tool task driver.
//
// Unlike the session-oriented root harness package, every model request and
// tool effect is represented by a durable journal step. Ambiguous effects stop
// in unknown or requires_action and require explicit reconciliation.
package agenttask

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/durableagent"
	"github.com/yuqie6/agent-harness/internal/llm"
	internaltools "github.com/yuqie6/agent-harness/internal/tools"
)

const (
	DefaultSystemPrompt = `You are executing a recoverable coding task. Use only the tools provided.
Inspect files and search results before making claims. If edit_file is available, read the file with metadata before editing it. Ask the user when requirements or boundaries are ambiguous. Report what you changed and how it was verified.`
	EditReviewToolName           = durableagent.EditReviewToolName
	RunCheckToolName             = internaltools.RunCheckToolName
	ResponseModeStoredBackground = ResponseMode("stored_background")
	ResponseModeOpaque           = ResponseMode("opaque")
	EditModeReadOnly             = EditMode("read_only")
	EditModeReview               = EditMode("review_edit")
	EditModeAuto                 = EditMode("auto_edit")
)

var (
	ErrMaxIterations    = errors.New("agenttask maximum iterations reached")
	ErrContextBudget    = errors.New("agenttask context budget exceeded")
	ErrVerificationGate = errors.New("agenttask verification gate failed")
)

// ResponseMode declares the provider's Responses persistence capability.
// Opaque is the default and never retries an ambiguous response.create.
type ResponseMode string

// EditMode is the immutable workspace-write policy recorded with a task.
type EditMode string

type ProviderConfig struct {
	APIKey                string
	BaseURL               string
	Model                 string
	ResponseMode          ResponseMode
	ReasoningEffort       string
	ReasoningSummary      string
	TextVerbosity         string
	ServiceTier           string
	HTTPClient            *http.Client
	StoredResponseTimeout time.Duration
}

type Policy struct {
	MaxIterations             int
	ModelContextWindow        int
	AutoCompactTokenLimit     int
	CompactionSummaryMaxChars int
	SkillCatalogMaxBytes      int
}

// ModelUsage is the provider's terminal token accounting for one Responses
// object. ResponseID is stable and can be used as an idempotency key.
type ModelUsage struct {
	ResponseID      string
	InputTokens     int64
	OutputTokens    int64
	TotalTokens     int64
	CachedTokens    int64
	ReasoningTokens int64
}

// UsageHooks let an operator-owned control plane reject response.create before
// network I/O and durably account provider-reported usage afterwards.
type UsageHooks struct {
	BeforeCreate func(ctx context.Context, requestKey string) error
	Record       func(ctx context.Context, usage ModelUsage) error
}

// Check is an operator-owned diagnostic command. The model can select Name
// only; Command always runs offline in a read-only bwrap sandbox. Required
// checks must pass after the final edit before a task can complete.
type Check struct {
	Name           string
	Description    string
	Command        string
	TimeoutSeconds int
	Required       bool
}

type Config struct {
	Database     string
	Workspace    string
	Provider     ProviderConfig
	SystemPrompt string
	Instructions []string
	// SkillUserHome overrides user-level skill discovery. It is mainly useful
	// for isolated applications and tests.
	SkillUserHome string
	Policy        Policy
	AllowEdit     bool
	ReviewEdit    bool
	Checks        []Check
	Tools         []Tool
	DurableTools  []DurableTool
	// RequiredArtifact gates successful completion on one strict tool call.
	RequiredArtifact *RequiredArtifact
	Usage            UsageHooks
	EngineOptions    durable.Options
}

type Result struct {
	Job             durable.Job
	Output          string
	ModelCalls      int
	CompactionCalls int
	ApprovalCalls   int
	ToolCalls       int
}

type AdvanceResult struct {
	Result
	Progressed bool
	Terminal   bool
}

type Runner struct {
	inner *durableagent.Runner
}

func Open(config Config) (*Runner, error) {
	return open(config, nil)
}

func open(config Config, textDeltaSink durableagent.TextDeltaSink) (*Runner, error) {
	provider := config.Provider
	provider.APIKey = strings.TrimSpace(provider.APIKey)
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	provider.Model = strings.TrimSpace(provider.Model)
	provider.ResponseMode = ResponseMode(strings.ToLower(strings.TrimSpace(string(provider.ResponseMode))))
	if provider.ResponseMode == "" {
		provider.ResponseMode = ResponseModeOpaque
	}
	if provider.APIKey == "" {
		return nil, errors.New("agenttask provider API key is required")
	}
	if provider.BaseURL == "" {
		provider.BaseURL = "https://api.openai.com/v1"
	}
	if provider.Model == "" {
		return nil, errors.New("agenttask provider model is required")
	}
	if provider.ResponseMode != ResponseModeStoredBackground && provider.ResponseMode != ResponseModeOpaque {
		return nil, fmt.Errorf("agenttask provider response mode %q is invalid", provider.ResponseMode)
	}
	if provider.ResponseMode == ResponseModeStoredBackground && provider.StoredResponseTimeout <= 0 {
		return nil, errors.New("agenttask stored background response timeout must be positive")
	}
	client := llm.New(provider.APIKey, provider.BaseURL, provider.Model)
	client.ReasoningEffort = strings.TrimSpace(provider.ReasoningEffort)
	client.ReasoningSummary = strings.TrimSpace(provider.ReasoningSummary)
	client.TextVerbosity = strings.TrimSpace(provider.TextVerbosity)
	client.ServiceTier = strings.TrimSpace(provider.ServiceTier)
	client.BeforeCreate = config.Usage.BeforeCreate
	if config.Usage.Record != nil {
		client.RecordUsage = func(ctx context.Context, usage llm.Usage) error {
			return config.Usage.Record(ctx, ModelUsage{
				ResponseID: usage.ResponseID, InputTokens: usage.InputTokens,
				OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens,
				CachedTokens: usage.CachedTokens, ReasoningTokens: usage.ReasoningTokens,
			})
		}
	}
	if provider.HTTPClient != nil {
		client.HTTP = provider.HTTPClient
	}
	var chatClient durableagent.ChatClient = client
	if provider.ResponseMode == ResponseModeOpaque {
		chatClient = ordinaryResponsesClient{client: client}
	}
	system := strings.TrimSpace(config.SystemPrompt)
	if system == "" {
		system = DefaultSystemPrompt
	}
	checks := make([]internaltools.NamedCheck, len(config.Checks))
	for index, check := range config.Checks {
		checks[index] = internaltools.NamedCheck{
			Name: check.Name, Description: check.Description, Command: check.Command,
			TimeoutSeconds: check.TimeoutSeconds, Required: check.Required,
		}
	}
	artifactTool, requiredArtifact, err := requiredArtifactTool(config.RequiredArtifact)
	if err != nil {
		return nil, err
	}
	publicTools := append([]Tool(nil), config.Tools...)
	if requiredArtifact != "" {
		publicTools = append(publicTools, artifactTool)
		artifactInstruction := "You must successfully call the strict " + requiredArtifact + " tool with the complete terminal artifact before giving a final answer. A prose-only answer is incomplete."
		if config.RequiredArtifact.AllowPriorTranscriptArtifact {
			artifactInstruction = "You must successfully call the strict " + requiredArtifact + " tool before the first terminal draft answer and whenever the user requests a draft change. If the trusted transcript already contains an accepted artifact, a prose-only answer to a question about that draft is allowed."
		}
		system = strings.TrimSpace(system + "\n\n" + artifactInstruction)
	}
	readTools, err := internalReadTools(publicTools)
	if err != nil {
		return nil, err
	}
	durableTools, err := internalDurableTools(config.DurableTools)
	if err != nil {
		return nil, err
	}
	inner, err := durableagent.Open(durableagent.Config{
		Database: config.Database, Workspace: config.Workspace, Client: chatClient,
		StoredResponseTimeout: provider.StoredResponseTimeout,
		Provider: durableagent.ProviderSnapshot{
			WireAPI: "responses", BaseURL: provider.BaseURL, Model: provider.Model,
			ResponseMode:     string(provider.ResponseMode),
			ReasoningEffort:  strings.TrimSpace(provider.ReasoningEffort),
			ReasoningSummary: strings.TrimSpace(provider.ReasoningSummary),
			TextVerbosity:    strings.TrimSpace(provider.TextVerbosity),
			ServiceTier:      strings.TrimSpace(provider.ServiceTier),
		},
		System: system, Instructions: append([]string(nil), config.Instructions...),
		SkillUserHome: config.SkillUserHome,
		Policy: durableagent.Policy{
			MaxIterations: config.Policy.MaxIterations, ModelContextWindow: config.Policy.ModelContextWindow,
			AutoCompactTokenLimit:     config.Policy.AutoCompactTokenLimit,
			CompactionSummaryMaxChars: config.Policy.CompactionSummaryMaxChars,
			SkillCatalogMaxBytes:      config.Policy.SkillCatalogMaxBytes,
		},
		AllowEdit: config.AllowEdit, ReviewEdit: config.ReviewEdit, EngineOptions: config.EngineOptions,
		Checks: checks, ReadTools: readTools, ExternalTools: durableTools,
		RequiredArtifact: requiredArtifact,
		AllowPriorTranscriptArtifact: config.RequiredArtifact != nil &&
			config.RequiredArtifact.AllowPriorTranscriptArtifact,
		TextDeltaSink: textDeltaSink,
	})
	if err != nil {
		return nil, err
	}
	return &Runner{inner: inner}, nil
}

// ordinaryResponsesClient intentionally exposes Chat only. This prevents the
// durable driver from assuming provider-side storage in explicit opaque mode.
type ordinaryResponsesClient struct {
	client *llm.Client
}

func (client ordinaryResponsesClient) Chat(
	ctx context.Context,
	messages []llm.Message,
	tools []llm.Tool,
) (llm.Message, string, error) {
	return client.client.Chat(ctx, messages, tools)
}

func (client ordinaryResponsesClient) ChatStreamDurable(
	ctx context.Context,
	messages []llm.Message,
	tools []llm.Tool,
	onDelta func(string) error,
) (llm.Message, string, error) {
	return client.client.ChatStreamDurable(ctx, messages, tools, onDelta)
}

func (r *Runner) Close() error {
	if r == nil || r.inner == nil {
		return nil
	}
	return r.inner.Close()
}

func (r *Runner) Start(ctx context.Context, prompt string) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	result, err := r.inner.Start(ctx, prompt)
	return publicResult(result), publicError(err)
}

func (r *Runner) Submit(ctx context.Context, prompt string) (durable.Job, error) {
	if err := r.validate(); err != nil {
		return durable.Job{}, err
	}
	job, err := r.inner.Submit(ctx, prompt)
	return job, publicError(err)
}

// SubmitWithID is the stable-ID variant for an outer durable supervisor that
// must journal the task identity before creating the task itself.
func (r *Runner) SubmitWithID(ctx context.Context, taskID, prompt string) (durable.Job, error) {
	if err := r.validate(); err != nil {
		return durable.Job{}, err
	}
	job, err := r.inner.SubmitWithID(ctx, taskID, prompt)
	return job, publicError(err)
}

func (r *Runner) Resume(ctx context.Context, jobID string) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	result, err := r.inner.Resume(ctx, jobID)
	return publicResult(result), publicError(err)
}

func (r *Runner) AdvanceOne(ctx context.Context, jobID string) (AdvanceResult, error) {
	if err := r.validate(); err != nil {
		return AdvanceResult{}, err
	}
	result, err := r.inner.AdvanceOne(ctx, jobID)
	return AdvanceResult{
		Result: publicResult(result.Result), Progressed: result.Progressed, Terminal: result.Terminal,
	}, publicError(err)
}

func (r *Runner) ActiveTasks(ctx context.Context, limit int) ([]durable.Job, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.inner.ActiveTasks(ctx, limit)
}

func (r *Runner) Task(ctx context.Context, jobID string) (durable.Job, error) {
	if err := r.validate(); err != nil {
		return durable.Job{}, err
	}
	return r.inner.Task(ctx, jobID)
}

func (r *Runner) Tasks(ctx context.Context, options durable.ListOptions) ([]durable.Job, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.inner.Tasks(ctx, options)
}

func (r *Runner) Attempts(ctx context.Context, jobID string) ([]durable.Attempt, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.inner.Attempts(ctx, jobID)
}

func (r *Runner) Events(ctx context.Context, jobID string, afterSequence int64) ([]durable.Event, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.inner.Events(ctx, jobID, afterSequence)
}

func (r *Runner) PendingResolution(ctx context.Context, jobID string) (durable.ResolutionTarget, error) {
	if err := r.validate(); err != nil {
		return durable.ResolutionTarget{}, err
	}
	return r.inner.PendingResolution(ctx, jobID)
}

func (r *Runner) ResolveUnknown(
	ctx context.Context,
	jobID string,
	resolution durable.UnknownResolution,
) (durable.Job, error) {
	if err := r.validate(); err != nil {
		return durable.Job{}, err
	}
	return r.inner.ResolveUnknown(ctx, jobID, resolution)
}

// WorkspaceFromJob returns the immutable workspace recorded in the first
// model step, allowing a control plane to scope tasks from a shared journal.
func WorkspaceFromJob(job durable.Job) (string, error) {
	return durableagent.WorkspaceFromJob(job)
}

// ToolNamesFromJob returns the unique model tool names embedded in a task.
func ToolNamesFromJob(job durable.Job) ([]string, error) {
	return durableagent.ToolNamesFromJob(job)
}

// EditModeFromJob returns the immutable edit policy recorded with a task.
// It also supports tasks created before the explicit edit_mode snapshot.
func EditModeFromJob(job durable.Job) (EditMode, error) {
	mode, err := durableagent.EditModeFromJob(job)
	return EditMode(mode), err
}

func (r *Runner) validate() error {
	if r == nil || r.inner == nil {
		return errors.New("agenttask runner is nil")
	}
	return nil
}

func publicResult(result durableagent.Result) Result {
	return Result{
		Job: result.Job, Output: result.Output, ModelCalls: result.ModelCalls,
		CompactionCalls: result.CompactionCalls, ApprovalCalls: result.ApprovalCalls,
		ToolCalls: result.ToolCalls,
	}
}

func publicError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, durableagent.ErrMaxIterations):
		return fmt.Errorf("%w: %v", ErrMaxIterations, err)
	case errors.Is(err, durableagent.ErrContextBudget):
		return fmt.Errorf("%w: %v", ErrContextBudget, err)
	case errors.Is(err, durableagent.ErrVerificationGate):
		return fmt.Errorf("%w: %v", ErrVerificationGate, err)
	case errors.Is(err, durableagent.ErrRequiredArtifactMissing):
		return fmt.Errorf("%w: %v", ErrRequiredArtifactMissing, err)
	default:
		return err
	}
}
