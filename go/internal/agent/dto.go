package agent

import (
	"encoding/json"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

const (
	sessionTitleMax         = 160
	sessionListDefaultLimit = 20
	sessionListMax          = 100
	sessionCursorVersion    = 1
	sessionDefaultTitle     = "新会话"
	taskTitleMax            = 160
	taskGoalMax             = 20_000
	taskSummaryMax          = 2_000
	taskListDefaultLimit    = 50
	taskListMaxLimit        = 100
	taskCursorVersion       = 1
	turnDefaultPageSize     = 20
	turnMaxPageSize         = 50
	turnCursorVersion       = 1
	maxInputText            = 20_000
	maxInputAssets          = 6
	// maxIdempotencyBytes 限制幂等键长度。
	maxIdempotencyBytes   = 200
	maxEventSequence      = 10_000
	maxCheckpointSequence = 10_000
	maxEventPayloadBytes  = 128 * 1024
	maxCheckpointPayload  = 64 * 1024
	maxTurnsPerSession    = 1_000
	maxSSEConnections     = 100
	// leaseSeconds 是 execution lease 秒数；过期后 dispatcher 回收并可能把 Turn 标 unknown。
	leaseSeconds          = 60
	toolContractVersion   = ToolManifestVersion
	assetListDefaultLimit = 50
	assetListMaxLimit     = 100
	globalProductListMax  = 100
)

// SessionConversation 是 Session 下列出的一条 Conversation 摘要。
type SessionConversation struct {
	ConversationID     string    `json:"conversation_id"`
	ScopeType          string    `json:"scope_type"` // product_workflow 或 global
	ProductID          *string   `json:"product_id"`
	ProductName        string    `json:"product_name"`        // 商品名快照；全局 Dock 可为空串
	ConversationStatus string    `json:"conversation_status"` // conversation.status，不要和 Turn.status 混用
	UpdatedAt          time.Time `json:"updated_at"`
}

// SessionResponse 是 Agent Session 给 Web 的投影，含下属 Conversation 摘要。
// ProductID 非空表示商品画布会话；空表示全局 Dock。不要把 Pi 目录路径放进这里。
type SessionResponse struct {
	ID                string                `json:"id"`
	ProductID         *string               `json:"product_id"`
	Title             string                `json:"title"`
	Summary           *string               `json:"summary"` // Session 有界 operational summary，不是 Goal
	Status            string                `json:"status"`
	ArchivedAt        *time.Time            `json:"archived_at"`
	ConversationCount int                   `json:"conversation_count"` // 下属 Conversation 条数
	Conversations     []SessionConversation `json:"conversations"`      // 摘要列表，不是完整 Turn
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

// SessionListResponse 给浏览器 Dock / 会话列表用的分页壳，不是内部工具面合同。
//
// Items 没有结果时是空切片不是 nil。NextCursor 为 nil 表示没有下一页；query after 吃的是这个 opaque cursor（rank_at + id），不是页码。
// 对应 GET /api/v2/agent-sessions。拉列表不得改 Goal、lease 或 journal。
type SessionListResponse struct {
	Items      []SessionResponse `json:"items"`       // 本页；空切片不是 nil
	NextCursor *string           `json:"next_cursor"` // opaque 游标（rank_at+id），不是页码；nil 表示没有下一页
}

// TaskResponse 是一条 AgentTask（产品 Goal）的投影。
//
// 终态只由用户 complete/cancel 写入。Turn 成功、GraphRun 成功、失败、取消、unknown 都不得把
// waiting_reason=goal_loop 的任务改成 succeeded。读路径同步 GraphRun 时也要守这条。
type TaskResponse struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	ConversationID *string    `json:"conversation_id"`
	ProductID      *string    `json:"product_id"`
	WorkflowID     *string    `json:"workflow_id"`
	Title          string     `json:"title"`
	Goal           string     `json:"goal"`    // 用户显式业务目标；Turn/GraphRun 成功不会改写
	Summary        *string    `json:"summary"` // 有界 operational summary，不是 transcript
	Status         string     `json:"status"`
	WaitingReason  *string    `json:"waiting_reason"` // goal_loop 时读路径不得覆盖用户拥有的 Goal
	FailureReason  *string    `json:"failure_reason"` // 失败说明；商品 Goal 不因 GraphRun 失败进入终态
	CurrentTurnID  *string    `json:"current_turn_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	CanceledAt     *time.Time `json:"canceled_at"`
}

// TaskListResponse 给 Dock 任务列表用的分页壳，条目是用户显式 Goal，不是 Turn 也不是 GraphRun。
//
// NextCursor 为 nil 表示没有下一页；after 必须与当前 session_id / include_terminal 匹配，否则 400。不是页码。
// 对应 GET /api/v2/agent-tasks。列表里的 sync 不得把 waiting_reason=goal_loop 改成 succeeded。
type TaskListResponse struct {
	Items      []TaskResponse `json:"items"`       // 本页 Goal；空切片不是 nil
	NextCursor *string        `json:"next_cursor"` // opaque 游标，不是页码；nil 表示没有下一页
}

// ConversationResponse 给 Web 的对话身份投影，挂在 Session 下承载 Turn。
//
// ProductID 非空是商品画布对话；nil 是全局 Dock。ScopeType 为 product_workflow 或 global。
// HarnessRunID 是 Pi run 身份，不是 PostgreSQL journal，也不是 Goal。Status 如 collecting，不要和 Turn.status 搞混。
type ConversationResponse struct {
	ID           string    `json:"id"`
	ScopeType    string    `json:"scope_type"` // product_workflow 或 global
	SessionID    *string   `json:"session_id"`
	ProductID    *string   `json:"product_id"`
	HarnessRunID string    `json:"harness_run_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CanvasFocus 记录本 Turn 请求前端聚焦的节点 / 边 / 分组，不改图、不写 Graph Command。
//
// TurnResponse.CanvasFocus 为 nil 表示这次没请求聚焦。RequestID 给前端对齐同一条请求。IDs 是画布身份，不是媒体 id。
type CanvasFocus struct {
	RequestID string   `json:"request_id"`
	NodeIDs   []string `json:"node_ids"`  // 画布节点身份，不是媒体 id
	EdgeIDs   []string `json:"edge_ids"`  // 画布边身份
	GroupIDs  []string `json:"group_ids"` // 画布分组身份
}

// TurnResponse 是 AgentTurnProjection 的 Web 投影。
//
// output_text / thinking_text / tool_steps 是列表摘要和空日志回退，不是第二份模型 transcript。
// 完整对话以 PostgreSQL agent_turn_events 为准，SSE 按游标回放。
type TurnResponse struct {
	ID                                 string           `json:"id"`
	ConversationID                     string           `json:"conversation_id"`
	TaskID                             *string          `json:"task_id"`
	HarnessRunID                       string           `json:"harness_run_id"`
	HarnessTurnID                      *string          `json:"harness_turn_id"`
	IdempotencyKey                     string           `json:"idempotency_key"` // 同键回放同一 projection
	InputText                          string           `json:"input_text"`      // 用户本轮输入
	InputAssetIDs                      []string         `json:"input_asset_ids"` // 本轮引用的图片身份，最多 6
	Status                             string           `json:"status"`
	TerminalReasonCode                 *string          `json:"terminal_reason_code"` // 终态原因码（如 execution_interrupted），不是 Goal 失败
	ResumeRequired                     bool             `json:"resume_required"`      // true 时 SyncTurn 不催 Gateway
	OutputText                         *string          `json:"output_text"`          // 列表摘要/空日志回退，不是第二份 transcript
	ThinkingText                       *string          `json:"thinking_text"`        // 列表摘要/空日志回退，不是第二份 transcript
	ErrorText                          *string          `json:"error_text"`           // 投影错误摘要，不是 journal
	Question                           json.RawMessage  `json:"question"`             // 待回答问题 JSON；完整对话以 journal 为准
	QuestionAnswer                     json.RawMessage  `json:"question_answer"`      // 用户答案 JSON
	ContinuationTurnID                 *string          `json:"continuation_turn_id"`
	ToolSteps                          []map[string]any `json:"tool_steps"`    // 列表摘要，不是第二份 transcript
	ArtifactName                       *string          `json:"artifact_name"` // 待确认工件名；权威在 Pi 侧
	ArtifactStepID                     *string          `json:"artifact_step_id"`
	LibraryOrganizationDraftRevisionID *string          `json:"library_organization_draft_revision_id"`
	WorkflowRunRequestID               *string          `json:"workflow_run_request_id"`
	PageContextSnapshotID              *string          `json:"page_context_snapshot_id"`
	SyncError                          *string          `json:"sync_error"`   // 同步/网关失败摘要，不证明 Goal 失败
	CanvasFocus                        *CanvasFocus     `json:"canvas_focus"` // nil 表示本次未请求聚焦
	FinishedAt                         *time.Time       `json:"finished_at"`
	CreatedAt                          time.Time        `json:"created_at"`
	UpdatedAt                          time.Time        `json:"updated_at"`
}

// TurnPageResponse 给对话历史列表用的 Turn 投影分页，权威仍是 PostgreSQL journal 而不是本页摘要。
//
// NextCursor 为 nil 表示没有下一页；after 绑定 conversation_id，不是页码。output_text / thinking_text 只是列表回退，不要当第二份 transcript。
type TurnPageResponse struct {
	Items      []TurnResponse `json:"items"`       // 本页 Turn 投影；空切片不是 nil
	NextCursor *string        `json:"next_cursor"` // opaque 游标，不是页码；nil 表示没有下一页
}

// SubmitTurnResponse 是 POST .../turns 的 202 体。Created=false 表示相同幂等键已有 projection，本次没有新建。
//
// Turn 是 Web 投影。提交成功不完成 Goal，也不等于 lease 已 claim；模型 writer 还要走内部 claim。
type SubmitTurnResponse struct {
	Created bool         `json:"created"` // false 表示同幂等键已有 projection，本次没有新建
	Turn    TurnResponse `json:"turn"`    // Web 投影；不表示 lease 已 claim
}

// QuestionAnswerResponse 包含已回答的父 Turn 与续写 Turn。
type QuestionAnswerResponse struct {
	TurnResponse
	AnsweredTurn     TurnResponse `json:"answered_turn"`     // 已写入答案的父 Turn
	ContinuationTurn TurnResponse `json:"continuation_turn"` // 续写 Turn；父 Turn 恢复后可能被取消
}

// WorkbenchResponse 是商品工作台所需的 product、conversation 与 live 图。
type WorkbenchResponse struct {
	Mode                   string               `json:"mode"`                     // 固定 "agent"
	Product                product.Detail       `json:"product"`                  // 商品 Detail，不是 Session
	Conversation           ConversationResponse `json:"conversation"`             // product_workflow 对话
	Graph                  *graph.Projection    `json:"graph"`                    // live 图投影；nil 表示尚无 active 图
	LatestWorkflowRevision int                  `json:"latest_workflow_revision"` // live 图 revision；无图为 0
}

// WorkflowRunRequestResponse 是 Agent 代为请求的 WorkflowGraphRun 确认单。
type WorkflowRunRequestResponse struct {
	ID                       string     `json:"id"`
	ConversationID           string     `json:"conversation_id"`
	TaskID                   *string    `json:"task_id"`
	ProductID                string     `json:"product_id"`
	ProductName              string     `json:"product_name"` // 请求时商品名快照
	WorkflowID               string     `json:"workflow_id"`
	WorkflowTitle            string     `json:"workflow_title"`             // 图标题快照
	ExpectedWorkflowRevision int        `json:"expected_workflow_revision"` // 确认时要对上的图 revision
	Status                   string     `json:"status"`
	SourceRunID              *string    `json:"source_run_id"`
	WorkflowRunID            *string    `json:"workflow_run_id"`
	WorkflowRunStatus        *string    `json:"workflow_run_status"` // 已提交 GraphRun 的状态；未提交为 nil
	SourceStepID             string     `json:"source_step_id"`
	FailureReason            *string    `json:"failure_reason"` // 确认单/跑图失败说明，不完成 Goal
	ConfirmedAt              *time.Time `json:"confirmed_at"`
	FinishedAt               *time.Time `json:"finished_at"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	RunScope                 string     `json:"run_scope"` // graph/node/to_node/selection
	TargetNodeID             *string    `json:"target_node_id"`
	TargetNodeIDs            []string   `json:"target_node_ids"` // selection 等范围的目标节点
	Force                    bool       `json:"force"`           // 仅 node|to_node|selection 合法
	DocumentAction           *string    `json:"document_action"` // 内容节点 fill 策略；nil 表示未指定
}

// ReconcileResponse 给 agent-service 崩溃恢复用的工具账本对账结论，不是 Goal 状态，也不是 Turn.status。
//
// State 只取 applied（账本已提交，Result 可回放）、not_applied（尚未落地）、conflict（同键不同目标）、unknown（证据不足）。
// 对账是只读查询，禁止在这里重放副作用、延长 lease 或把 Task 标完成。
type ReconcileResponse struct {
	State  string          `json:"state"`  // applied / not_applied / conflict / unknown；只读对账
	Result json.RawMessage `json:"result"` // 可回放的账本结果；State≠applied 时可能为空
	Detail *string         `json:"detail"`
}

// EffectReconciliationResponse 是 unknown Turn 上一次 tool_effect 对账行。
type EffectReconciliationResponse struct {
	SchemaVersion       int             `json:"schema_version"` // 对账行 schema
	ID                  string          `json:"id"`
	ProjectionID        string          `json:"projection_id"`
	ToolCallID          string          `json:"tool_call_id"`
	ToolName            string          `json:"tool_name"`            // 对账的工具名
	IdempotencyKey      string          `json:"idempotency_key"`      // 副作用幂等键
	EffectResult        string          `json:"effect_result"`        // applied / failed / unknown
	ReconciliationState string          `json:"reconciliation_state"` // 本次对账结论，与 EffectResult 分开保存
	Result              json.RawMessage `json:"result"`               // 对账结果 JSON
	Detail              *string         `json:"detail"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// ContractResponse 是发给 Pi 的 system prompt、工具合同与 Draft schema。
type ContractResponse struct {
	SchemaVersion       int            `json:"schema_version"` // 合同 schema
	ScopeType           string         `json:"scope_type"`     // 决定 system prompt 与 Draft
	ConversationID      string         `json:"conversation_id"`
	TaskID              *string        `json:"task_id"`
	TaskGoal            *string        `json:"task_goal"` // 绑定 Task 时的 Goal；未绑为 nil
	ProductID           *string        `json:"product_id"`
	HarnessRunID        string         `json:"harness_run_id"`
	CurrentDraftVersion int            `json:"current_draft_version"` // 全局图库 draft 当前 version；无 draft 为 0
	SystemPrompt        string         `json:"system_prompt"`         // 发给 Pi 的系统提示
	ToolContractVersion string         `json:"tool_contract_version"` // 对齐 ToolManifestVersion
	DraftKind           *string        `json:"draft_kind"`            // global 或 workflow
	DraftSchema         map[string]any `json:"draft_schema"`          // Draft JSON Schema；无则空对象
	HasLiveGraph        bool           `json:"has_live_graph"`        // 商品是否已有 active 图；全局恒 false
}

// RuntimeContextResponse 是 Session / Task 的有界 operational summary。
type RuntimeContextResponse struct {
	SchemaVersion  int     `json:"schema_version"` // 运行时上下文 schema
	SessionID      string  `json:"session_id"`
	ConversationID string  `json:"conversation_id"`
	TaskID         *string `json:"task_id"`
	SessionSummary *string `json:"session_summary"` // Dock 用的有界 summary
	TaskSummary    *string `json:"task_summary"`    // Task 有界 summary；未绑 Task 为 nil
}

// ExecutionLeaseResponse 给 agent-service 的 claim / heartbeat 回执，证明谁可以往 journal 追加事件。
//
// 浏览器不要消费本结构。LeaseToken 错配或过期视为失效；FencingToken 在他人成功重领后递增，旧 writer 再 AppendEvents 会 409。
// 本回执不改 Goal。不要把它当成 TurnResponse 或 Pi TurnState。
type ExecutionLeaseResponse struct {
	ExecutionID    string    `json:"execution_id"`
	ProjectionID   string    `json:"projection_id"`
	HarnessTurnID  string    `json:"harness_turn_id"`
	OwnerID        string    `json:"owner_id"`
	LeaseToken     string    `json:"lease_token"`   // 当前 owner 持有 lease 的证明；错配或过期视为失效
	Attempt        int       `json:"attempt"`       // 成功重新认领时递增；同一 owner 回放有效 lease 不递增
	FencingToken   int       `json:"fencing_token"` // 他人成功重领后递增；旧 writer AppendEvents 会 409
	Phase          string    `json:"phase"`         // claimed / waiting_input / terminal；过期回收会推进围栏
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

// CheckpointResponse 是内部 POST .../checkpoints 写入一条 execution checkpoint 后的回执。
//
// Sequence 必须单调。写 checkpoint 不延长 lease、不追加 agent_turn_events、不改 Goal。lease/fencing 错配返回 409。
type CheckpointResponse struct {
	ID           string    `json:"id"`
	ProjectionID string    `json:"projection_id"`
	ExecutionID  string    `json:"execution_id"`
	Attempt      int       `json:"attempt"`       // 写入时钉住的 execution attempt
	FencingToken int       `json:"fencing_token"` // 写入时钉住的围栏；错配 409
	Sequence     int       `json:"sequence"`      // checkpoint 单调序号，不是 journal sequence
	Kind         string    `json:"kind"`          // 如 tool_effect_intent
	CreatedAt    time.Time `json:"created_at"`
}

// EventReceipt 是写入 PostgreSQL journal 后的一条事件回执。
type EventReceipt struct {
	ID            string    `json:"id"`
	ProjectionID  string    `json:"projection_id"`
	ExecutionID   string    `json:"execution_id"`
	Sequence      int       `json:"sequence"`       // journal 序号，从 1 连续
	SchemaVersion int       `json:"schema_version"` // 必须为 1
	Kind          string    `json:"kind"`           // agent-service 原始词表
	Ignorable     bool      `json:"ignorable"`      // true 时未知 kind 可忽略，不进 UI 词表
	CreatedAt     time.Time `json:"created_at"`
}

// EventConfirmationResponse 比较本地 journal 后缀与 PostgreSQL，不延长 lease。
type EventConfirmationResponse struct {
	Status           string                  `json:"status"`
	ConfirmedThrough int                     `json:"confirmed_through"`  // 双方一致的最大 sequence
	PersistedThrough int                     `json:"persisted_through"`  // PostgreSQL 已持久化的最大 sequence
	Items            []EventReceipt          `json:"items"`              // 本次确认的回执
	Terminal         *ConfirmedTerminalEvent `json:"terminal,omitempty"` // 已见 turn/end 时附投影终态
}

// ConfirmedTerminalEvent 出现在 ConfirmEvents 里：双方 journal 已看到 turn/end，并带上 PostgreSQL 投影终态。
//
// ProjectionStatus 以 PG 为准，不要用 Pi TurnState.Status 覆盖。Output/Thinking/Error 来自该终态事件，不是第二份 transcript。
// 确认路径不延长 lease、不改 Goal。
type ConfirmedTerminalEvent struct {
	EventReceipt
	Payload          json.RawMessage `json:"payload"`           // turn/end 事件体
	ProjectionStatus string          `json:"projection_status"` // 以 PG 为准，不要用 Pi TurnState 覆盖
	Output           string          `json:"output"`            // 终态输出摘要，不是第二份 transcript
	Thinking         string          `json:"thinking"`          // 终态思考摘要，不是第二份 transcript
	Error            string          `json:"error"`
	FinishedAt       time.Time       `json:"finished_at"`
}

// PreparedWorkflowRunRequest 给 agent-service 在创建确认单之前看「这张图能不能跑」，不创建 GraphRun，也不写 Goal。
//
// TaskID / SourceRunID 为 nil 表示未绑定。RunnableNodeCount=0 仍可能 200，由模型决定要不要继续 create。不要把本结构当已提交的 WorkflowRunRequestResponse。
type PreparedWorkflowRunRequest struct {
	ProductID         string  `json:"product_id"`
	WorkflowID        string  `json:"workflow_id"`
	WorkflowTitle     string  `json:"workflow_title"`      // 当前图标题
	WorkflowRevision  int     `json:"workflow_revision"`   // 当前 live revision
	RunnableNodeCount int     `json:"runnable_node_count"` // 0 仍可能 200，由模型决定是否 create
	TaskID            *string `json:"task_id"`
	SourceRunID       *string `json:"source_run_id"`
}

// WorkspaceLaunchResponse 是全局 Agent 创建商品工作区的内部回执，给 agent-service 不是浏览器 Dock。
//
// Created=false 表示相同幂等键已创建过。TaskID 为 nil 表示没挂 Goal。IntakeFinalized 只报告出生图是否已展开，不要据此把 Goal 标完成。
// NavigationPath 给前端跳转。同键不同 name 会 409。
type WorkspaceLaunchResponse struct {
	SchemaVersion         int     `json:"schema_version"` // 工作区启动回执 schema
	Created               bool    `json:"created"`        // false 表示同幂等键已创建
	SessionID             string  `json:"session_id"`
	GlobalConversationID  string  `json:"global_conversation_id"`
	ProductConversationID string  `json:"product_conversation_id"`
	ProductID             string  `json:"product_id"`
	ProductName           string  `json:"product_name"` // 新建商品名
	TaskID                *string `json:"task_id"`
	IntakeFinalized       bool    `json:"intake_finalized"` // 出生图是否已展开；不要据此完成 Goal
	NavigationPath        string  `json:"navigation_path"`  // 前端跳转路径
}

// AssetMetadata 是 Agent 工具面返回的有界图片元数据，不含 bytes。
type AssetMetadata struct {
	ID                 string            `json:"id"`
	DisplayName        string            `json:"display_name"`      // 展示名，不是存储路径
	OriginalFilename   string            `json:"original_filename"` // 上传时原始文件名
	OriginType         string            `json:"origin_type"`       // 商品图片来源类型
	ImageTypeKey       *string           `json:"image_type_key"`    // 图类 key；未分类为 nil
	ImageTypeTitle     *string           `json:"image_type_title"`  // 图类标题；未分类为 nil
	UserFolderID       *string           `json:"user_folder_id"`
	UserFolderName     *string           `json:"user_folder_name"` // 用户文件夹名；未整理为 nil
	MIMEType           string            `json:"mime_type"`
	ByteSize           *int              `json:"byte_size"` // 已核验字节数
	Width              *int              `json:"width"`
	Height             *int              `json:"height"`
	VerificationStatus string            `json:"verification_status"` // 媒体核验状态
	ParentAssetID      *string           `json:"parent_asset_id"`
	Generation         map[string]string `json:"generation"` // 生成元数据短键值，不是完整 prompt
	CreatedAt          string            `json:"created_at"`
}

// AssetListResponse 给内部工具面的图片元数据分页，不含 bytes。
//
// NextCursor 为 nil 表示没有下一页；after/cursor 是 opaque 游标不是页码。商品图库与全局图库共用本壳，条目都是 AssetMetadata。
// 不要把本列表整包塞进模型 Turn；inspect 才按明确 id 取。
type AssetListResponse struct {
	Items      []AssetMetadata `json:"items"`       // 本页元数据；不含 bytes
	NextCursor *string         `json:"next_cursor"` // opaque 游标，不是页码；nil 表示没有下一页
}

// GlobalProductResponse 给全局 Agent 的商品短摘要，不是 product.Detail，也不是工作台 WorkbenchResponse。
//
// ActiveWorkflow 为 nil 表示该商品还没有 active 图。只含 id/name/category/时间，没有 facts 或图节点。
type GlobalProductResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Category       *string         `json:"category"` // 商品类目短摘要，不是 FactSet
	UpdatedAt      string          `json:"updated_at"`
	ActiveWorkflow *map[string]any `json:"active_workflow"` // nil 表示尚无 active 图；不是 Graph 投影
}

// GlobalProductListResponse 给全局 Agent 搜索商品的分页壳。
//
// NextCursor 绑定当前 query；换搜索词却复用旧 cursor 会 400。不是页码。Items 空切片不是 nil。
type GlobalProductListResponse struct {
	Items      []GlobalProductResponse `json:"items"`       // 本页短摘要；空切片不是 nil
	NextCursor *string                 `json:"next_cursor"` // 绑定当前 query 的 opaque 游标，不是页码
}

// TurnState 是 agent-service 返回的模型 Turn 状态，不是 PostgreSQL 投影权威。
type TurnState struct {
	APIVersion       string           `json:"api_version"` // agent-service 合同版本，不是 journal schema
	RunID            string           `json:"run_id"`
	TurnID           string           `json:"turn_id"`
	Status           string           `json:"status"`
	ExecutionAttempt *int             `json:"execution_attempt"`       // 模型侧看到的 attempt；权威在 PG lease
	ExecutionFence   *int             `json:"execution_fencing_token"` // 模型侧 fencing_token；过期 writer 不得覆盖投影
	Question         json.RawMessage  `json:"question"`                // Pi 侧待回答问题
	Artifact         *TurnArtifact    `json:"artifact"`                // Pi 侧待确认工件；不要写入 journal
	ToolSteps        []map[string]any `json:"tool_steps"`              // 列表摘要，不是第二份 transcript
	Output           string           `json:"output"`                  // 列表摘要，不是第二份 transcript
	Thinking         string           `json:"thinking"`                // 列表摘要，不是第二份 transcript
	Error            string           `json:"error"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	StartedAt        *time.Time       `json:"started_at"`
	FinishedAt       *time.Time       `json:"finished_at"`
}

// TurnArtifact 是 agent-service 返回的模型 Turn 待确认工件，权威不在 PostgreSQL journal。
//
// Name/Value/StepID 来自 Pi 侧。浏览器确认走对应 Draft / 执行请求路由，不要把本结构写进 agent_turn_events。TurnState.Artifact 为 nil 表示没有工件。
type TurnArtifact struct {
	Name   string          `json:"name"`
	Value  json.RawMessage `json:"value"` // Pi 侧工件 JSON，权威不在 journal
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
	TerminalReasonCode        *string
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

// GatewayError 表示调用 Node.js/Pi agent-service 失败，还不是 HTTP 层的 apperr。
//
// Status 可能为 0（未配置或拨号失败）。Code 来自上游 error.code 或本地 not_configured / unavailable / invalid_response。
// 本值不是 journal 失败，也不证明 Goal 失败。handler 路径必须经 mapGateway 收成 503/400/409，不要直接 Abort 本结构。
type GatewayError struct {
	Status int
	Code   string // 上游 error.code 或本地 not_configured / unavailable / invalid_response
	Detail string
}

// Error 返回 Detail 给日志和 errors.As 链。HTTP 状态读 Status 字段，机器码读 Code。
func (e GatewayError) Error() string { return e.Detail }

// Gateway 把 Turn 控制转发给 agent-service；Pi session files 不是业务权威。
type Gateway interface {
	// StartTurn 向 agent-service 提交一轮模型 Turn。
	StartTurn(conversationID string, taskID *string, inputText string, assetIDs []string, idempotencyKey string, pageContext any) (TurnState, error)
	// GetTurn 读取 agent-service 当前 Turn 状态。
	GetTurn(conversationID, turnID string, taskID *string) (TurnState, error)
	// CancelTurn 请求 agent-service 取消当前 Turn。
	CancelTurn(conversationID, turnID string, taskID *string) (TurnState, error)
	// ResumeTurn 请求 agent-service 恢复当前 Turn。
	ResumeTurn(conversationID, turnID string, taskID *string) (TurnState, error)
	// AnswerQuestion 把用户答案交给 agent-service 中仍活着的问题 waiter。
	AnswerQuestion(conversationID, turnID, questionID string, answer map[string]any, taskID *string) (TurnState, error)
}
