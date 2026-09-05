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

// AssetResponse 是会话内一张图的 HTTP 下载/预览投影，不是 ProductImageAsset。
// Kind 为 reference_upload 或 generated_image。不要用它当全局图库素材。
type AssetResponse struct {
	ID               string    `json:"id"`
	Kind             string    `json:"kind"`              // reference_upload | generated_image
	OriginalFilename string    `json:"original_filename"` // 上传或生成时的文件名
	MIMEType         string    `json:"mime_type"`
	DownloadURL      string    `json:"download_url"`  // 原图；不是 ProductImageAsset 路径
	PreviewURL       string    `json:"preview_url"`   // 预览变体
	ThumbnailURL     string    `json:"thumbnail_url"` // 缩略图变体
	CreatedAt        time.Time `json:"created_at"`
}

// RoundResponse 是一轮连续生图结果的 HTTP 投影（提示词、模型、候选）。
// 一轮对应一次成功落盘的候选；queued 任务还没有 Round。不要和 GraphRun 节点结果搞混。
type RoundResponse struct {
	ID                        string        `json:"id"`
	Prompt                    string        `json:"prompt"`            // 本轮用户提示词
	AssistantMessage          string        `json:"assistant_message"` // 会话展示文案，不是模型 thinking
	Size                      string        `json:"size"`              // 请求尺寸，宽x高
	ModelName                 string        `json:"model_name"`        // 供应商实际模型名
	ProviderName              string        `json:"provider_name"`     // 供应商标识，如 openai-responses
	PromptVersion             string        `json:"prompt_version"`    // 连续生图模板版本，不是 graph 文稿
	ProviderResponseID        *string       `json:"provider_response_id"`
	PreviousResponseID        *string       `json:"previous_response_id"`
	ImageGenerationCallID     *string       `json:"image_generation_call_id"`
	GenerationGroupID         *string       `json:"generation_group_id"`
	CandidateIndex            int           `json:"candidate_index"` // 本组内从 1 起的候选序号
	CandidateCount            int           `json:"candidate_count"` // 本任务计划候选总数
	BaseAssetID               *string       `json:"base_asset_id"`
	SelectedReferenceAssetIDs []string      `json:"selected_reference_asset_ids"` // 参考图，不是节点绑定
	ActualSize                *string       `json:"actual_size"`                  // nil 表示供应商未回实测尺寸
	ProviderNotes             []string      `json:"provider_notes"`               // 供应商旁注；空是 []
	GeneratedAsset            AssetResponse `json:"generated_asset"`              // 本轮落盘的 generated_image
	CreatedAt                 time.Time     `json:"created_at"`
}

// EffectResponse 是一次供应商副作用账本行；EffectResult 为 unknown 时不自动当失败重试。
type EffectResponse struct {
	ID                  string    `json:"id"`
	GenerationTaskID    string    `json:"generation_task_id"`
	CandidateStartIndex int       `json:"candidate_start_index"` // 本批覆盖的起始候选序号
	CandidateCount      int       `json:"candidate_count"`       // 本批候选张数
	OperationKey        string    `json:"operation_key"`         // 幂等操作键，避免重复打网
	EffectKind          string    `json:"effect_kind"`           // 固定 image_session_generation
	RequestHash         string    `json:"request_hash"`          // 请求体哈希，对账用
	ProviderName        string    `json:"provider_name"`         // 实际打网的供应商标识
	EffectResult        string    `json:"effect_result"`         // pending | applied | failed | unknown
	ReconciliationState string    `json:"reconciliation_state"`  // not_requested | applied | not_applied | unknown
	ProviderResponseID  *string   `json:"provider_response_id"`
	ProviderStatus      *string   `json:"provider_status"` // nil 表示供应商未回状态；常见 completed
	Detail              *string   `json:"detail"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// TaskResponse 是一次连续生图任务的 HTTP 投影，含队列位置。
// Status 为 queued/running/succeeded/failed/unknown/cancelled。
// EffectResult=unknown 时 IsRetryable 为 false，不能走 Retry。不要当成 AgentTask。
type TaskResponse struct {
	ID                        string           `json:"id"`
	SessionID                 string           `json:"session_id"`
	Status                    string           `json:"status"`
	Prompt                    string           `json:"prompt,omitempty"` // 详情必有；Status/SSE 省略，避免活动集轮询重复下发
	Size                      string           `json:"size"`             // 规范化后的宽x高
	BaseAssetID               *string          `json:"base_asset_id"`
	SelectedReferenceAssetIDs []string         `json:"selected_reference_asset_ids"` // 参考图，不是节点绑定
	GenerationCount           int              `json:"generation_count"`             // 计划候选张数，1–10
	CompletedCandidates       int              `json:"completed_candidates"`         // 已落盘候选数
	ActiveCandidateIndex      *int             `json:"active_candidate_index"`       // nil 表示当前没有在跑的候选
	ProgressPhase             *string          `json:"progress_phase"`               // nil 表示尚未进入 worker 阶段
	ProgressUpdatedAt         *time.Time       `json:"progress_updated_at"`
	ProviderResponseID        *string          `json:"provider_response_id"`
	ProviderResponseStatus    *string          `json:"provider_response_status"` // nil 表示尚未收到供应商状态
	ProgressMetadata          map[string]any   `json:"progress_metadata"`        // worker 进度旁路；空是 {}
	FailureReason             *string          `json:"failure_reason"`           // nil 表示未失败；unknown 时有说明且不可 Retry
	ResultGenerationGroupID   *string          `json:"result_generation_group_id"`
	ToolOptions               map[string]any   `json:"tool_options"`     // 过滤后的 image tool 字段
	ProviderNotes             []string         `json:"provider_notes"`   // 空是 []
	ProviderEffects           []EffectResponse `json:"provider_effects"` // 副作用账本；unknown 不自动重试
	Attempts                  int              `json:"attempts"`         // 已占用的执行次数
	IsRetryable               bool             `json:"is_retryable"`     // unknown 时为 false，不能走 Retry
	IsCancelable              bool             `json:"is_cancelable"`    // queued 或 running 才为 true
	CreatedAt                 time.Time        `json:"created_at"`
	StartedAt                 *time.Time       `json:"started_at"`
	FinishedAt                *time.Time       `json:"finished_at"`
	QueueActiveCount          int              `json:"queue_active_count"`         // 图运行+连续生图占用
	QueueRunningCount         int              `json:"queue_running_count"`        // 当前 running 占用
	QueueQueuedCount          int              `json:"queue_queued_count"`         // 当前 queued 占用
	QueueMaxConcurrentTasks   int              `json:"queue_max_concurrent_tasks"` // app_settings 全局并发上限
	QueuedAheadCount          *int             `json:"queued_ahead_count"`         // nil 表示本任务不在 queued
	QueuePosition             *int             `json:"queue_position"`             // nil 表示本任务不在 queued；1 为队首
}

// SummaryResponse 是会话列表项的 HTTP 投影，不含全量轮次。
// LatestGeneratedAsset 为 nil 表示还没成功出图，不是「文件丢失」。
type SummaryResponse struct {
	ID                   string         `json:"id"`
	Title                string         `json:"title"`
	RoundsCount          int            `json:"rounds_count"`           // 已成功落盘的轮次数，不含 queued 任务
	LatestGeneratedAsset *AssetResponse `json:"latest_generated_asset"` // nil 表示还没成功出图，不是文件丢失
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

// ListResponse 是 GET 会话列表的 HTTP 体：按 updated_at、id 倒序返回一页。
// NextCursor 是不透明游标；轮询单会话请用 StatusResponse。
type ListResponse struct {
	Items      []SummaryResponse `json:"items"`       // 空库是 [] 不是 null
	NextCursor *string           `json:"next_cursor"` // 没有下一页时为 null
}

// DetailResponse 是会话详情的 HTTP 投影：参考图、首屏轮次、活动任务。
// 完整历史请用 HistoryResponse；轮询请用 StatusResponse，不要反复拉本类型。
type DetailResponse struct {
	ID               string          `json:"id"`
	Title            string          `json:"title"`
	Assets           []AssetResponse `json:"assets"`             // 仅 reference_upload，最多 6 张
	Rounds           []RoundResponse `json:"rounds"`             // 首屏最新一轮页；queued 任务还没有 Round
	GenerationTasks  []TaskResponse  `json:"generation_tasks"`   // 最新活动、近期终态/无轮次成功、首屏轮次任务各最多 20 条，去重后最多 60；轮询用 StatusResponse
	RoundsCount      int             `json:"rounds_count"`       // 已成功落盘的轮次总数
	HistoryNextAfter *string         `json:"history_next_after"` // 还有更早历史时的不透明游标；没有下一页为 null
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// HistoryResponse 是 GET 会话历史页的 HTTP 体：按 created_at、id 倒序返回一页已落盘轮次。
// NextAfter 是不透明游标；空页是 Items=[]、NextAfter=null。
type HistoryResponse struct {
	Items     []RoundResponse `json:"items"`      // 空页是 [] 不是 null
	NextAfter *string         `json:"next_after"` // 没有下一页时为 null
}

// StatusResponse 是会话轻量状态的 HTTP 投影，供轮询和 SSE，不带全量轮次/素材。
// HasActiveGenerationTask 来自本次 GenerationTasks 读取，任一任务 queued 或 running 时为 true。
type StatusResponse struct {
	ID                      string         `json:"id"`
	Title                   string         `json:"title"`
	RoundsCount             int            `json:"rounds_count"` // 已成功落盘的轮次数
	LatestRoundID           *string        `json:"latest_round_id"`
	LatestGenerationGroupID *string        `json:"latest_generation_group_id"`
	HasActiveGenerationTask bool           `json:"has_active_generation_task"` // 任一任务 queued 或 running
	GenerationTasks         []TaskResponse `json:"generation_tasks"`           // 该会话全部 queued/running；不含 prompt，提示词以详情为准
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

// CreateRequest 是 POST /api/image-sessions 的 JSON 体。
// Title 为 nil 或空白时用「未命名会话」；不要传空对象以外的未知字段（extra=forbid）。
type CreateRequest struct {
	Title *string `json:"title"`
}

// UpdateRequest 是 PATCH 重命名会话的 JSON 体。Title 必填且非空，最长 255 字。
type UpdateRequest struct {
	Title string `json:"title"`
}

// GenerateRequest 是提交一轮连续生图的 JSON 体，不是 GraphRun 入参。
// BaseAssetID 为从哪张已生成图继续；SelectedReferenceAssetIDs 是参考图，二者不是绑定节点。
// GenerationCount 为 nil 时默认 1。
type GenerateRequest struct {
	Prompt                    string         `json:"prompt"` // 用户提示词，空则 Validation
	Size                      string         `json:"size"`   // 宽x高；空则 1024x1024，再按最大边长规范化
	BaseAssetID               *string        `json:"base_asset_id"`
	SelectedReferenceAssetIDs []string       `json:"selected_reference_asset_ids"` // 参考图，不是节点绑定
	GenerationCount           *int           `json:"generation_count"`             // nil 时默认 1
	ToolOptions               map[string]any `json:"tool_options"`                 // 超出允许字段的键会被丢掉
}

// AttachRequest 指定把生成结果写入哪个商品图库。
// 只接受 generated_image；参考图不能 attach。复用同一 MediaObject，不复制 bytes。
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
