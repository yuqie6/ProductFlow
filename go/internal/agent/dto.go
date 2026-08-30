package agent

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

const (
	sessionTitleMax       = 160
	sessionListMax        = 100
	sessionDefaultTitle   = "新会话"
	taskTitleMax          = 160
	taskGoalMax           = 20_000
	taskSummaryMax        = 2_000
	taskListDefaultLimit  = 50
	taskListMaxLimit      = 100
	taskCursorVersion     = 1
	turnDefaultPageSize   = 20
	turnMaxPageSize       = 50
	turnCursorVersion     = 1
	maxInputText          = 20_000
	maxInputAssets        = 6
	maxIdempotencyBytes   = 200
	maxEventSequence      = 100_000
	maxCheckpointSequence = 10_000
	maxEventPayloadBytes  = 128 * 1024
	maxCheckpointPayload  = 64 * 1024
	leaseSeconds          = 60
	toolContractVersion   = 16
	assetListDefaultLimit = 50
	assetListMaxLimit     = 100
	globalProductListMax  = 100
)

type SessionConversation struct {
	ConversationID     string    `json:"conversation_id"`
	ScopeType          string    `json:"scope_type"`
	ProductID          *string   `json:"product_id"`
	ProductName        string    `json:"product_name"`
	ConversationStatus string    `json:"conversation_status"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type SessionResponse struct {
	ID                string                `json:"id"`
	ProductID         *string               `json:"product_id"`
	Title             string                `json:"title"`
	Summary           *string               `json:"summary"`
	Status            string                `json:"status"`
	ArchivedAt        *time.Time            `json:"archived_at"`
	ConversationCount int                   `json:"conversation_count"`
	Conversations     []SessionConversation `json:"conversations"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

type SessionListResponse struct {
	Items []SessionResponse `json:"items"`
}

type TaskResponse struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	ConversationID *string    `json:"conversation_id"`
	ProductID      *string    `json:"product_id"`
	WorkflowID     *string    `json:"workflow_id"`
	Title          string     `json:"title"`
	Goal           string     `json:"goal"`
	Summary        *string    `json:"summary"`
	Status         string     `json:"status"`
	WaitingReason  *string    `json:"waiting_reason"`
	FailureReason  *string    `json:"failure_reason"`
	CurrentTurnID  *string    `json:"current_turn_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	CanceledAt     *time.Time `json:"canceled_at"`
}

type TaskListResponse struct {
	Items      []TaskResponse `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

type ConversationResponse struct {
	ID           string    `json:"id"`
	ScopeType    string    `json:"scope_type"`
	SessionID    *string   `json:"session_id"`
	ProductID    *string   `json:"product_id"`
	HarnessRunID string    `json:"harness_run_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CanvasFocus struct {
	RequestID string   `json:"request_id"`
	NodeIDs   []string `json:"node_ids"`
	EdgeIDs   []string `json:"edge_ids"`
	GroupIDs  []string `json:"group_ids"`
}

type TurnResponse struct {
	ID                                 string           `json:"id"`
	ConversationID                     string           `json:"conversation_id"`
	TaskID                             *string          `json:"task_id"`
	HarnessRunID                       string           `json:"harness_run_id"`
	HarnessTurnID                      *string          `json:"harness_turn_id"`
	IdempotencyKey                     string           `json:"idempotency_key"`
	InputText                          string           `json:"input_text"`
	InputAssetIDs                      []string         `json:"input_asset_ids"`
	Status                             string           `json:"status"`
	ResumeRequired                     bool             `json:"resume_required"`
	OutputText                         *string          `json:"output_text"`
	ThinkingText                       *string          `json:"thinking_text"`
	ErrorText                          *string          `json:"error_text"`
	Question                           json.RawMessage  `json:"question"`
	QuestionAnswer                     json.RawMessage  `json:"question_answer"`
	ContinuationTurnID                 *string          `json:"continuation_turn_id"`
	ToolSteps                          []map[string]any `json:"tool_steps"`
	ArtifactName                       *string          `json:"artifact_name"`
	ArtifactStepID                     *string          `json:"artifact_step_id"`
	LibraryOrganizationDraftRevisionID *string          `json:"library_organization_draft_revision_id"`
	WorkflowRunRequestID               *string          `json:"workflow_run_request_id"`
	PageContextSnapshotID              *string          `json:"page_context_snapshot_id"`
	SyncError                          *string          `json:"sync_error"`
	CanvasFocus                        *CanvasFocus     `json:"canvas_focus"`
	FinishedAt                         *time.Time       `json:"finished_at"`
	CreatedAt                          time.Time        `json:"created_at"`
	UpdatedAt                          time.Time        `json:"updated_at"`
}

type TurnPageResponse struct {
	Items      []TurnResponse `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

type SubmitTurnResponse struct {
	Created bool         `json:"created"`
	Turn    TurnResponse `json:"turn"`
}

type QuestionAnswerResponse struct {
	TurnResponse
	AnsweredTurn     TurnResponse `json:"answered_turn"`
	ContinuationTurn TurnResponse `json:"continuation_turn"`
}

type WorkbenchResponse struct {
	Mode                   string               `json:"mode"`
	Product                product.Detail       `json:"product"`
	Conversation           ConversationResponse `json:"conversation"`
	Graph                  *graph.Projection    `json:"graph"`
	LatestWorkflowRevision int                  `json:"latest_workflow_revision"`
}

type WorkflowRunRequestResponse struct {
	ID                       string     `json:"id"`
	ConversationID           string     `json:"conversation_id"`
	TaskID                   *string    `json:"task_id"`
	ProductID                string     `json:"product_id"`
	ProductName              string     `json:"product_name"`
	WorkflowID               string     `json:"workflow_id"`
	WorkflowTitle            string     `json:"workflow_title"`
	ExpectedWorkflowRevision int        `json:"expected_workflow_revision"`
	Status                   string     `json:"status"`
	SourceRunID              *string    `json:"source_run_id"`
	WorkflowRunID            *string    `json:"workflow_run_id"`
	WorkflowRunStatus        *string    `json:"workflow_run_status"`
	SourceStepID             string     `json:"source_step_id"`
	FailureReason            *string    `json:"failure_reason"`
	ConfirmedAt              *time.Time `json:"confirmed_at"`
	FinishedAt               *time.Time `json:"finished_at"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	RunScope                 string     `json:"run_scope"`
	TargetNodeID             *string    `json:"target_node_id"`
	TargetNodeIDs            []string   `json:"target_node_ids"`
}

type ReconcileResponse struct {
	State  string          `json:"state"`
	Result json.RawMessage `json:"result"`
	Detail *string         `json:"detail"`
}

type EffectReconciliationResponse struct {
	SchemaVersion       int             `json:"schema_version"`
	ID                  string          `json:"id"`
	ProjectionID        string          `json:"projection_id"`
	ToolCallID          string          `json:"tool_call_id"`
	ToolName            string          `json:"tool_name"`
	IdempotencyKey      string          `json:"idempotency_key"`
	EffectResult        string          `json:"effect_result"`
	ReconciliationState string          `json:"reconciliation_state"`
	Result              json.RawMessage `json:"result"`
	Detail              *string         `json:"detail"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type ContractResponse struct {
	SchemaVersion       int            `json:"schema_version"`
	ScopeType           string         `json:"scope_type"`
	ConversationID      string         `json:"conversation_id"`
	TaskID              *string        `json:"task_id"`
	TaskGoal            *string        `json:"task_goal"`
	ProductID           *string        `json:"product_id"`
	HarnessRunID        string         `json:"harness_run_id"`
	CurrentDraftVersion int            `json:"current_draft_version"`
	SystemPrompt        string         `json:"system_prompt"`
	ToolContractVersion int            `json:"tool_contract_version"`
	DraftKind           *string        `json:"draft_kind"`
	DraftSchema         map[string]any `json:"draft_schema"`
	HasLiveGraph        bool           `json:"has_live_graph"`
}

type RuntimeContextResponse struct {
	SchemaVersion  int     `json:"schema_version"`
	SessionID      string  `json:"session_id"`
	ConversationID string  `json:"conversation_id"`
	TaskID         *string `json:"task_id"`
	SessionSummary *string `json:"session_summary"`
	TaskSummary    *string `json:"task_summary"`
}

type ExecutionLeaseResponse struct {
	ExecutionID    string    `json:"execution_id"`
	ProjectionID   string    `json:"projection_id"`
	HarnessTurnID  string    `json:"harness_turn_id"`
	OwnerID        string    `json:"owner_id"`
	LeaseToken     string    `json:"lease_token"`
	Attempt        int       `json:"attempt"`
	FencingToken   int       `json:"fencing_token"`
	Phase          string    `json:"phase"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

type CheckpointResponse struct {
	ID           string    `json:"id"`
	ProjectionID string    `json:"projection_id"`
	ExecutionID  string    `json:"execution_id"`
	Attempt      int       `json:"attempt"`
	FencingToken int       `json:"fencing_token"`
	Sequence     int       `json:"sequence"`
	Kind         string    `json:"kind"`
	CreatedAt    time.Time `json:"created_at"`
}

type EventReceipt struct {
	ID            string    `json:"id"`
	ProjectionID  string    `json:"projection_id"`
	ExecutionID   string    `json:"execution_id"`
	Sequence      int       `json:"sequence"`
	SchemaVersion int       `json:"schema_version"`
	Kind          string    `json:"kind"`
	CreatedAt     time.Time `json:"created_at"`
}

type PreparedWorkflowRunRequest struct {
	ProductID         string  `json:"product_id"`
	WorkflowID        string  `json:"workflow_id"`
	WorkflowTitle     string  `json:"workflow_title"`
	WorkflowRevision  int     `json:"workflow_revision"`
	RunnableNodeCount int     `json:"runnable_node_count"`
	TaskID            *string `json:"task_id"`
	SourceRunID       *string `json:"source_run_id"`
}

type WorkspaceLaunchResponse struct {
	SchemaVersion         int     `json:"schema_version"`
	Created               bool    `json:"created"`
	SessionID             string  `json:"session_id"`
	GlobalConversationID  string  `json:"global_conversation_id"`
	ProductConversationID string  `json:"product_conversation_id"`
	ProductID             string  `json:"product_id"`
	ProductName           string  `json:"product_name"`
	TaskID                *string `json:"task_id"`
	IntakeFinalized       bool    `json:"intake_finalized"`
	NavigationPath        string  `json:"navigation_path"`
}

type AssetMetadata struct {
	ID                 string            `json:"id"`
	DisplayName        string            `json:"display_name"`
	OriginalFilename   string            `json:"original_filename"`
	OriginType         string            `json:"origin_type"`
	ImageTypeKey       *string           `json:"image_type_key"`
	ImageTypeTitle     *string           `json:"image_type_title"`
	UserFolderID       *string           `json:"user_folder_id"`
	UserFolderName     *string           `json:"user_folder_name"`
	MIMEType           string            `json:"mime_type"`
	ByteSize           *int              `json:"byte_size"`
	Width              *int              `json:"width"`
	Height             *int              `json:"height"`
	VerificationStatus string            `json:"verification_status"`
	ParentAssetID      *string           `json:"parent_asset_id"`
	Generation         map[string]string `json:"generation"`
	CreatedAt          string            `json:"created_at"`
}

type AssetListResponse struct {
	Items      []AssetMetadata `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

type GlobalProductResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Category       *string         `json:"category"`
	UpdatedAt      string          `json:"updated_at"`
	ActiveWorkflow *map[string]any `json:"active_workflow"`
}

type GlobalProductListResponse struct {
	Items      []GlobalProductResponse `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}

type TurnState struct {
	APIVersion       string           `json:"api_version"`
	RunID            string           `json:"run_id"`
	TurnID           string           `json:"turn_id"`
	Status           string           `json:"status"`
	ExecutionAttempt *int             `json:"execution_attempt"`
	ExecutionFence   *int             `json:"execution_fencing_token"`
	Question         json.RawMessage  `json:"question"`
	Artifact         *TurnArtifact    `json:"artifact"`
	ToolSteps        []map[string]any `json:"tool_steps"`
	Output           string           `json:"output"`
	Thinking         string           `json:"thinking"`
	Error            string           `json:"error"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	StartedAt        *time.Time       `json:"started_at"`
	FinishedAt       *time.Time       `json:"finished_at"`
}

type TurnArtifact struct {
	Name   string          `json:"name"`
	Value  json.RawMessage `json:"value"`
	StepID string          `json:"step_id"`
}

type conversationRow struct {
	ID           string
	ScopeType    string
	SessionID    *string
	ProductID    *string
	HarnessRunID string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type taskRow struct {
	ID             string
	SessionID      string
	ConversationID *string
	ProductID      *string
	HarnessRunID   string
	Title          string
	Goal           string
	Summary        *string
	Status         string
	WaitingReason  *string
	FailureReason  *string
	CurrentTurnID  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	CanceledAt     *time.Time
	WorkflowID     *string
}

type turnRow struct {
	ID                        string
	ConversationID            string
	TaskID                    *string
	HarnessTurnID             *string
	IdempotencyKey            string
	RequestHash               string
	InputText                 string
	InputAssetIDs             []byte
	Status                    string
	ResumeRequired            bool
	OutputText                *string
	ThinkingText              *string
	ErrorText                 *string
	QuestionJSON              []byte
	QuestionAnswerJSON        []byte
	ContinuationTurnID        *string
	ToolStepsJSON             []byte
	ArtifactName              *string
	ArtifactStepID            *string
	LibraryOrgDraftRevisionID *string
	WorkflowRunRequestID      *string
	PageContextSnapshotID     *string
	SyncError                 *string
	FinishedAt                *time.Time
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	ConversationHarnessRunID  string
	TaskHarnessRunID          *string
	ConversationScope         string
	ConversationProductID     *string
}

type GatewayError struct {
	Status int
	Code   string
	Detail string
}

func (e GatewayError) Error() string { return e.Detail }

type Gateway interface {
	StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error)
	GetTurn(conversationID, turnID string, taskID *string) (TurnState, error)
	CancelTurn(conversationID, turnID string, taskID *string) (TurnState, error)
	ResumeTurn(conversationID, turnID string, taskID *string) (TurnState, error)
	AnswerQuestion(conversationID, turnID, questionID string, answer map[string]any, taskID *string) (TurnState, error)
	StreamTurnEvents(ctx context.Context, conversationID, turnID string, taskID *string, after int, w io.Writer) error
}
