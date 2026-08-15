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

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/productflow-agent-service/internal/config"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const productFlowToolContractVersion = 2

type ManagerConfig struct {
	DataRoot    string
	Provider    agenttask.ProviderConfig
	Policy      agenttask.Policy
	HTTPOptions agenttask.HTTPOptions
	ProductFlow *productflow.Client
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
		Provider: agenttask.ProviderConfig{
			APIKey: configValue.ProviderAPIKey, BaseURL: configValue.ProviderBaseURL,
			Model: configValue.ProviderModel, ResponseMode: agenttask.ResponseModeOpaque,
			ReasoningEffort: configValue.ProviderReasoningEffort, ReasoningSummary: configValue.ProviderReasoningSummary,
			TextVerbosity: configValue.ProviderTextVerbosity, ServiceTier: configValue.ProviderServiceTier,
		},
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
	}
}

func (manager *Manager) Get(ctx context.Context, conversationID string) (*ConversationService, error) {
	conversationID = strings.ToLower(strings.TrimSpace(conversationID))
	if !canonicalUUID.MatchString(conversationID) {
		return nil, errors.New("conversation_id must be a canonical UUID")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return nil, errors.New("conversation manager is closed")
	}
	if entry := manager.entries[conversationID]; entry != nil {
		return entry, nil
	}
	contract, err := manager.config.ProductFlow.Contract(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if contract.SchemaVersion != 1 || contract.ToolContractVersion != productFlowToolContractVersion {
		return nil, fmt.Errorf(
			"ProductFlow Agent contract mismatch: schema_version=%d tool_contract_version=%d, want 1/%d",
			contract.SchemaVersion, contract.ToolContractVersion, productFlowToolContractVersion,
		)
	}
	scope := Scope{
		SchemaVersion: scopeSchemaVersion, ConversationID: contract.ConversationID,
		ProductID: contract.ProductID, WorkflowDraftID: contract.WorkflowDraftID, RunID: contract.HarnessRunID,
	}
	if scope.ConversationID != conversationID || !canonicalUUID.MatchString(strings.ToLower(scope.ProductID)) ||
		!canonicalUUID.MatchString(strings.ToLower(scope.WorkflowDraftID)) || strings.TrimSpace(scope.RunID) == "" {
		return nil, errors.New("ProductFlow returned an invalid conversation contract")
	}
	database, workspace, err := ensureScope(manager.config.DataRoot, scope)
	if err != nil {
		return nil, err
	}
	runnerConfig := agenttask.Config{
		Database: database, Workspace: workspace, SkillUserHome: workspace,
		Provider: manager.config.Provider, Policy: manager.config.Policy,
		SystemPrompt: contract.SystemPrompt,
		Tools:        scopedReadTools(manager.config.ProductFlow, scope),
		DurableTools: scopedDurableTools(manager.config.ProductFlow, scope),
		RequiredArtifact: &agenttask.RequiredArtifact{
			Name:        agenttask.WorkflowDraftToolName,
			Description: "Submit the complete validated ProductFlow workflow draft for user confirmation.",
			Schema:      contract.WorkflowDraftSchema,
			Validate: func(ctx context.Context, value json.RawMessage) error {
				return manager.config.ProductFlow.ValidateWorkflowDraft(ctx, scope.ConversationID, value)
			},
		},
	}
	service, err := agenttask.OpenService(agenttask.ServiceConfig{Runner: runnerConfig})
	if err != nil {
		return nil, fmt.Errorf("open harness service for conversation: %w", err)
	}
	handler, err := agenttask.NewHTTPHandler(service, manager.config.HTTPOptions)
	if err != nil {
		_ = service.Close()
		return nil, fmt.Errorf("open harness HTTP handler: %w", err)
	}
	entry := &ConversationService{Scope: scope, Service: service, Handler: handler}
	manager.entries[conversationID] = entry
	return entry, nil
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
