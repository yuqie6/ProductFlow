package imagesession

import (
	"time"
)

const (
	defaultTitle       = "未命名会话"
	defaultAssistant   = "已按本轮选择的图片上下文生成候选，你可以从任意候选继续。"
	maxBranchImages    = 6
	maxGenerationCount = 10
	maxAttempts        = 3
	cancelledReason    = "已取消"
	genericFailure     = "图片生成失败，请稍后重试"
	unknownDetail      = "图片供应商请求结果未知，系统未自动重试。请检查供应商记录后重新发起生成。"
	unknownPhase       = "unknown_provider_effect"
	kindReference      = "reference_upload"
	kindGenerated      = "generated_image"
	effectKind         = "image_session_generation"
)

type AssetResponse struct {
	ID               string    `json:"id"`
	Kind             string    `json:"kind"`
	OriginalFilename string    `json:"original_filename"`
	MIMEType         string    `json:"mime_type"`
	DownloadURL      string    `json:"download_url"`
	PreviewURL       string    `json:"preview_url"`
	ThumbnailURL     string    `json:"thumbnail_url"`
	CreatedAt        time.Time `json:"created_at"`
}

type RoundResponse struct {
	ID                        string        `json:"id"`
	Prompt                    string        `json:"prompt"`
	AssistantMessage          string        `json:"assistant_message"`
	Size                      string        `json:"size"`
	ModelName                 string        `json:"model_name"`
	ProviderName              string        `json:"provider_name"`
	PromptVersion             string        `json:"prompt_version"`
	ProviderResponseID        *string       `json:"provider_response_id"`
	PreviousResponseID        *string       `json:"previous_response_id"`
	ImageGenerationCallID     *string       `json:"image_generation_call_id"`
	GenerationGroupID         *string       `json:"generation_group_id"`
	CandidateIndex            int           `json:"candidate_index"`
	CandidateCount            int           `json:"candidate_count"`
	BaseAssetID               *string       `json:"base_asset_id"`
	SelectedReferenceAssetIDs []string      `json:"selected_reference_asset_ids"`
	ActualSize                *string       `json:"actual_size"`
	ProviderNotes             []string      `json:"provider_notes"`
	GeneratedAsset            AssetResponse `json:"generated_asset"`
	CreatedAt                 time.Time     `json:"created_at"`
}

type EffectResponse struct {
	ID                  string    `json:"id"`
	GenerationTaskID    string    `json:"generation_task_id"`
	CandidateStartIndex int       `json:"candidate_start_index"`
	CandidateCount      int       `json:"candidate_count"`
	OperationKey        string    `json:"operation_key"`
	EffectKind          string    `json:"effect_kind"`
	RequestHash         string    `json:"request_hash"`
	ProviderName        string    `json:"provider_name"`
	EffectResult        string    `json:"effect_result"`
	ReconciliationState string    `json:"reconciliation_state"`
	ProviderResponseID  *string   `json:"provider_response_id"`
	ProviderStatus      *string   `json:"provider_status"`
	Detail              *string   `json:"detail"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type TaskResponse struct {
	ID                        string           `json:"id"`
	SessionID                 string           `json:"session_id"`
	Status                    string           `json:"status"`
	Prompt                    string           `json:"prompt"`
	Size                      string           `json:"size"`
	BaseAssetID               *string          `json:"base_asset_id"`
	SelectedReferenceAssetIDs []string         `json:"selected_reference_asset_ids"`
	GenerationCount           int              `json:"generation_count"`
	CompletedCandidates       int              `json:"completed_candidates"`
	ActiveCandidateIndex      *int             `json:"active_candidate_index"`
	ProgressPhase             *string          `json:"progress_phase"`
	ProgressUpdatedAt         *time.Time       `json:"progress_updated_at"`
	ProviderResponseID        *string          `json:"provider_response_id"`
	ProviderResponseStatus    *string          `json:"provider_response_status"`
	ProgressMetadata          map[string]any   `json:"progress_metadata"`
	FailureReason             *string          `json:"failure_reason"`
	ResultGenerationGroupID   *string          `json:"result_generation_group_id"`
	ToolOptions               map[string]any   `json:"tool_options"`
	ProviderNotes             []string         `json:"provider_notes"`
	ProviderEffects           []EffectResponse `json:"provider_effects"`
	Attempts                  int              `json:"attempts"`
	IsRetryable               bool             `json:"is_retryable"`
	IsCancelable              bool             `json:"is_cancelable"`
	CreatedAt                 time.Time        `json:"created_at"`
	StartedAt                 *time.Time       `json:"started_at"`
	FinishedAt                *time.Time       `json:"finished_at"`
	QueueActiveCount          int              `json:"queue_active_count"`
	QueueRunningCount         int              `json:"queue_running_count"`
	QueueQueuedCount          int              `json:"queue_queued_count"`
	QueueMaxConcurrentTasks   int              `json:"queue_max_concurrent_tasks"`
	QueuedAheadCount          *int             `json:"queued_ahead_count"`
	QueuePosition             *int             `json:"queue_position"`
}

type SummaryResponse struct {
	ID                   string         `json:"id"`
	Title                string         `json:"title"`
	RoundsCount          int            `json:"rounds_count"`
	LatestGeneratedAsset *AssetResponse `json:"latest_generated_asset"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type ListResponse struct {
	Items []SummaryResponse `json:"items"`
}

type DetailResponse struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	Assets          []AssetResponse `json:"assets"`
	Rounds          []RoundResponse `json:"rounds"`
	GenerationTasks []TaskResponse  `json:"generation_tasks"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type StatusResponse struct {
	ID                      string         `json:"id"`
	Title                   string         `json:"title"`
	RoundsCount             int            `json:"rounds_count"`
	LatestRoundID           *string        `json:"latest_round_id"`
	LatestGenerationGroupID *string        `json:"latest_generation_group_id"`
	HasActiveGenerationTask bool           `json:"has_active_generation_task"`
	GenerationTasks         []TaskResponse `json:"generation_tasks"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

type CreateRequest struct {
	Title *string `json:"title"`
}

type UpdateRequest struct {
	Title string `json:"title"`
}

type GenerateRequest struct {
	Prompt                    string         `json:"prompt"`
	Size                      string         `json:"size"`
	BaseAssetID               *string        `json:"base_asset_id"`
	SelectedReferenceAssetIDs []string       `json:"selected_reference_asset_ids"`
	GenerationCount           *int           `json:"generation_count"`
	ToolOptions               map[string]any `json:"tool_options"`
}

type AttachRequest struct {
	ProductID string `json:"product_id"`
}

type sessionRow struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type assetRow struct {
	ID                 string
	SessionID          string
	Kind               string
	OriginalFilename   string
	MIMEType           string
	StoragePath        string
	MediaObjectID      string
	VerificationStatus string
	CreatedAt          time.Time
}

type taskRow struct {
	ID                      string
	SessionID               string
	Status                  string
	Prompt                  string
	Size                    string
	BaseAssetID             *string
	SelectedRefs            []byte
	ToolOptions             []byte
	GenerationCount         int
	CompletedCandidates     int
	ActiveCandidateIndex    *int
	ProgressPhase           *string
	ProgressUpdatedAt       *time.Time
	ProviderResponseID      *string
	ProviderResponseStatus  *string
	ProgressMetadata        []byte
	FailureReason           *string
	ResultGenerationGroupID *string
	CreatedAt               time.Time
	StartedAt               *time.Time
	FinishedAt              *time.Time
	Attempts                int
	ActiveAttemptID         *string
	IsRetryable             bool
}
