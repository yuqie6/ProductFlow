package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/productflow-agent-service/internal/config"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const (
	productFlowToolContractVersion = 8
	scopeTypeProductWorkflow       = "product_workflow"
	scopeTypeGlobal                = "global"
	globalDraftArtifactName        = "propose_global_draft"
)

type ManagerConfig struct {
	DataRoot    string
	Provider    agenttask.ProviderConfig
	Policy      agenttask.Policy
	HTTPOptions agenttask.HTTPOptions
	ProductFlow *productflow.Client
	Admission   agenttask.Admission
}

type ConversationService struct {
	Scope   Scope
	Service *agenttask.Service
	Handler http.Handler
}

type Manager struct {
	config ManagerConfig

	mu      sync.Mutex
	entries map[string]*ConversationService
	closed  bool
}

func NewManager(managerConfig ManagerConfig) (*Manager, error) {
	if strings.TrimSpace(managerConfig.DataRoot) == "" || managerConfig.ProductFlow == nil {
		return nil, errors.New("conversation manager requires data root and ProductFlow client")
	}
	return &Manager{config: managerConfig, entries: make(map[string]*ConversationService)}, nil
}

func ManagerConfigFrom(configValue config.Config, client *productflow.Client) ManagerConfig {
	return ManagerConfig{
		DataRoot: configValue.DataRoot,
		Policy: agenttask.Policy{
			MaxIterations: configValue.MaxIterations, ModelContextWindow: configValue.ModelContextWindow,
			AutoCompactTokenLimit: configValue.AutoCompactTokenLimit, CompactionSummaryMaxChars: 12_000,
		},
		HTTPOptions: agenttask.HTTPOptions{
			EventPollInterval: configValue.EventPollInterval,
			HeartbeatInterval: configValue.HeartbeatInterval,
			MaxBodyBytes:      configValue.MaxBodyBytes,
		},
		ProductFlow: client,
		Admission:   agenttask.NewSemaphore(configValue.MaxConcurrentTurns),
	}
}

func (manager *Manager) Get(ctx context.Context, conversationID string) (*ConversationService, error) {
	conversationID = strings.ToLower(strings.TrimSpace(conversationID))
	if !canonicalUUID.MatchString(conversationID) {
		return nil, errors.New("conversation_id must be a canonical UUID")
	}
	return manager.getEntry(ctx, conversationID, conversationID, "", func(ctx context.Context) (productflow.Contract, error) {
		return manager.config.ProductFlow.Contract(ctx, conversationID)
	})
}

func (manager *Manager) GetTask(ctx context.Context, taskID string) (*ConversationService, error) {
	taskID = strings.ToLower(strings.TrimSpace(taskID))
	if !canonicalUUID.MatchString(taskID) {
		return nil, errors.New("task_id must be a canonical UUID")
	}
	return manager.getEntry(ctx, "task:"+taskID, "", taskID, func(ctx context.Context) (productflow.Contract, error) {
		return manager.config.ProductFlow.TaskContract(ctx, taskID)
	})
}

func (manager *Manager) getEntry(
	ctx context.Context,
	key string,
	expectedConversationID string,
	expectedTaskID string,
	loadContract func(context.Context) (productflow.Contract, error),
) (*ConversationService, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return nil, errors.New("conversation manager is closed")
	}
	if entry := manager.entries[key]; entry != nil {
		return entry, nil
	}
	contract, err := loadContract(ctx)
	if err != nil {
		return nil, err
	}
	if contract.SchemaVersion != 1 || contract.ToolContractVersion != productFlowToolContractVersion {
		return nil, fmt.Errorf(
			"ProductFlow Agent contract mismatch: schema_version=%d tool_contract_version=%d, want 1/%d",
			contract.SchemaVersion, contract.ToolContractVersion, productFlowToolContractVersion,
		)
	}
	contractTaskID := ""
	if contract.TaskID != nil {
		contractTaskID = strings.ToLower(strings.TrimSpace(*contract.TaskID))
	}
	if expectedTaskID != contractTaskID {
		return nil, errors.New("ProductFlow returned an invalid task contract")
	}
	scopeType := strings.TrimSpace(contract.ScopeType)
	if scopeType == "" {
		// Existing ProductFlow contracts and persisted journals predate explicit scope_type.
		scopeType = scopeTypeProductWorkflow
	}
	if scopeType != scopeTypeProductWorkflow && scopeType != scopeTypeGlobal {
		return nil, fmt.Errorf("ProductFlow returned an invalid Agent scope_type %q", scopeType)
	}
	productID := optionalContractValue(contract.ProductID)
	workflowDraftID := optionalContractValue(contract.WorkflowDraftID)
	contractConversationID := strings.ToLower(strings.TrimSpace(contract.ConversationID))
	if expectedConversationID != "" && contractConversationID != expectedConversationID {
		return nil, errors.New("ProductFlow returned an invalid conversation scope")
	}
	providerConfig, err := manager.providerConfig(ctx)
	if err != nil {
		return nil, err
	}
	scope := Scope{
		SchemaVersion: scopeSchemaVersion, ConversationID: contractConversationID,
		ScopeType: scopeType, TaskID: contractTaskID,
		ProductID: productID, WorkflowDraftID: workflowDraftID, RunID: contract.HarnessRunID,
	}
	if !canonicalUUID.MatchString(strings.ToLower(scope.ConversationID)) || strings.TrimSpace(scope.RunID) == "" {
		return nil, errors.New("ProductFlow returned an invalid Agent contract")
	}
	if scope.ScopeType == scopeTypeProductWorkflow {
		if !canonicalUUID.MatchString(strings.ToLower(scope.ProductID)) ||
			!canonicalUUID.MatchString(strings.ToLower(scope.WorkflowDraftID)) {
			return nil, errors.New("ProductFlow returned an invalid product Agent contract")
		}
	} else if scope.ProductID != "" || scope.WorkflowDraftID != "" {
		return nil, errors.New("ProductFlow global Agent contract must not include product scope IDs")
	}
	if expectedTaskID != "" && scope.TaskID != expectedTaskID {
		return nil, errors.New("ProductFlow returned an invalid task scope")
	}
	database, workspace, err := ensureScope(manager.config.DataRoot, scope)
	if err != nil {
		return nil, err
	}
	readTools := scopedReadTools(manager.config.ProductFlow, scope)
	var durableTools []agenttask.DurableTool
	var requiredArtifact *agenttask.RequiredArtifact
	var optionalArtifact *agenttask.RequiredArtifact
	if scope.ScopeType == scopeTypeGlobal {
		readTools = scopedGlobalReadTools(manager.config.ProductFlow, scope)
		durableTools = scopedGlobalDurableTools(manager.config.ProductFlow, scope)
		optionalArtifact = &agenttask.RequiredArtifact{
			Name:                         globalDraftArtifactName,
			Description:                  "Submit a complete validated global ProductFlow draft for user confirmation. The draft may organize media or propose a workflow for an explicit product.",
			Schema:                       contract.DraftSchema,
			AllowPriorTranscriptArtifact: true,
			Validate: func(ctx context.Context, value json.RawMessage) error {
				return manager.config.ProductFlow.ValidateGlobalDraft(ctx, scope.ConversationID, value)
			},
		}
	} else {
		durableTools = scopedDurableTools(manager.config.ProductFlow, scope)
		if scope.TaskID == "" {
			requiredArtifact = &agenttask.RequiredArtifact{
				Name:                         agenttask.WorkflowDraftToolName,
				Description:                  "Submit the complete validated ProductFlow workflow draft for user confirmation.",
				Schema:                       contract.WorkflowDraftSchema,
				AllowPriorTranscriptArtifact: true,
				Validate: func(ctx context.Context, value json.RawMessage) error {
					return manager.config.ProductFlow.ValidateWorkflowDraft(ctx, scope.ConversationID, value)
				},
			}
		}
	}
	runnerConfig := agenttask.Config{
		Database: database, Workspace: workspace, SkillUserHome: workspace,
		Provider: providerConfig, Policy: manager.config.Policy,
		SystemPrompt: agentSystemPrompt(contract.SystemPrompt, contract.TaskGoal),
		Tools:        readTools, DurableTools: durableTools, RequiredArtifact: requiredArtifact,
		OptionalArtifact: optionalArtifact,
	}
	service, err := agenttask.OpenService(agenttask.ServiceConfig{
		Runner:        runnerConfig,
		ToolProjector: productFlowToolProjector,
		Admission:     manager.config.Admission,
	})
	if err != nil {
		return nil, fmt.Errorf("open harness service for conversation: %w", err)
	}
	handler, err := agenttask.NewHTTPHandler(service, manager.config.HTTPOptions)
	if err != nil {
		_ = service.Close()
		return nil, fmt.Errorf("open harness HTTP handler: %w", err)
	}
	entry := &ConversationService{Scope: scope, Service: service, Handler: handler}
	manager.entries[key] = entry
	return entry, nil
}

func agentSystemPrompt(base string, taskGoal *string) string {
	goal := ""
	if taskGoal != nil {
		goal = strings.TrimSpace(*taskGoal)
	}
	if goal == "" {
		return base
	}
	return base + "\n\n当前 Agent Task 的固定目标（不会随页面路由变化）：\n" + goal
}

func optionalContractValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (manager *Manager) providerConfig(ctx context.Context) (agenttask.ProviderConfig, error) {
	if strings.TrimSpace(manager.config.Provider.APIKey) != "" {
		provider := manager.config.Provider
		if provider.HTTPClient == nil {
			provider.HTTPClient = &http.Client{Timeout: 6 * time.Minute}
		}
		return provider, nil
	}
	resolved, err := manager.config.ProductFlow.AgentProviderConfig(ctx)
	if err != nil {
		return agenttask.ProviderConfig{}, err
	}
	if resolved.SchemaVersion != 1 || strings.TrimSpace(resolved.ProviderKind) != "openai" {
		return agenttask.ProviderConfig{}, fmt.Errorf(
			"ProductFlow Agent provider contract mismatch: schema_version=%d provider_kind=%q",
			resolved.SchemaVersion,
			resolved.ProviderKind,
		)
	}
	apiKey := strings.TrimSpace(resolved.APIKey)
	model := strings.TrimSpace(resolved.Model)
	if apiKey == "" || model == "" {
		return agenttask.ProviderConfig{}, errors.New("ProductFlow returned an incomplete Agent provider configuration")
	}
	return agenttask.ProviderConfig{
		APIKey:           apiKey,
		BaseURL:          optionalString(resolved.BaseURL),
		Model:            model,
		ResponseMode:     agenttask.ResponseModeOpaque,
		ReasoningEffort:  optionalString(resolved.ReasoningEffort),
		ReasoningSummary: optionalString(resolved.ReasoningSummary),
		TextVerbosity:    optionalString(resolved.TextVerbosity),
		ServiceTier:      optionalString(resolved.ServiceTier),
		HTTPClient:       &http.Client{Timeout: 6 * time.Minute},
	}, nil
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (manager *Manager) Close() error {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil
	}
	manager.closed = true
	entries := make([]*ConversationService, 0, len(manager.entries))
	for _, entry := range manager.entries {
		entries = append(entries, entry)
	}
	manager.entries = nil
	manager.mu.Unlock()
	var result error
	for _, entry := range entries {
		result = errors.Join(result, entry.Service.Close())
	}
	return result
}
