// Package schema 是 GORM 表模型与 ExtraDDL 的权威来源。productflow-migrate 只做 CreateTable/AddColumn，不用 AutoMigrate。
package schema

// 从线上 PostgreSQL head（Alembic 20260829_0095）生成。禁止手改 column tag；
// 目标 head 变了应从 schema dump 重新生成，不要在本文件补列或改类型。

import "time"

// AgentConversations 对应表 agent_conversations。
// 保存商品工作流或全局 Dock 的 Agent 对话投影；Turn journal 挂在其上。
type AgentConversations struct {
	ID                     string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID              *string   `gorm:"column:product_id;type:varchar(36)"`
	HarnessRunID           string    `gorm:"column:harness_run_id;type:varchar(120);not null"`
	Status                 string    `gorm:"column:status;type:agentconversationstatus;not null"`
	CreatedAt              time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt              time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
	CreationIdempotencyKey *string   `gorm:"column:creation_idempotency_key;type:varchar(200)"`
	CreationRequestHash    *string   `gorm:"column:creation_request_hash;type:varchar(64)"`
	IntakeIdempotencyKey   *string   `gorm:"column:intake_idempotency_key;type:varchar(200)"`
	IntakeRequestHash      *string   `gorm:"column:intake_request_hash;type:varchar(64)"`
	SessionID              *string   `gorm:"column:session_id;type:varchar(36)"`
	ScopeType              string    `gorm:"column:scope_type;type:agentconversationscope;not null"`
}

func (AgentConversations) TableName() string { return "agent_conversations" }

// AgentPageContextSnapshots 对应表 agent_page_context_snapshots。
// 记录 Turn 当时可见的有界页面上下文，路由切换只更新后续 Turn。
type AgentPageContextSnapshots struct {
	ID                   string    `gorm:"column:id;type:varchar(36);primaryKey"`
	TaskID               *string   `gorm:"column:task_id;type:varchar(36)"`
	TurnID               *string   `gorm:"column:turn_id;type:varchar(36)"`
	Route                string    `gorm:"column:route;type:varchar(512);not null"`
	PageType             string    `gorm:"column:page_type;type:varchar(80);not null"`
	ProductID            *string   `gorm:"column:product_id;type:varchar(36)"`
	WorkflowID           *string   `gorm:"column:workflow_id;type:varchar(36)"`
	SelectedAssetIdsJSON string    `gorm:"column:selected_asset_ids_json;type:json;not null"`
	VisibleAssetIdsJSON  string    `gorm:"column:visible_asset_ids_json;type:json;not null"`
	FiltersJSON          string    `gorm:"column:filters_json;type:json;not null"`
	WorkflowRevision     *int      `gorm:"column:workflow_revision;type:integer"`
	LibraryRevision      *int      `gorm:"column:library_revision;type:integer"`
	Digest               string    `gorm:"column:digest;type:varchar(64);not null"`
	CapturedAt           time.Time `gorm:"column:captured_at;type:timestamptz;not null"`
	CreatedAt            time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (AgentPageContextSnapshots) TableName() string { return "agent_page_context_snapshots" }

// AgentSessions 对应表 agent_sessions。
// 长期交流容器：画布会话属于一个商品，全局 Dock 会话不属于商品。
type AgentSessions struct {
	ID         string     `gorm:"column:id;type:varchar(36);primaryKey"`
	Title      string     `gorm:"column:title;type:varchar(160);not null"`
	Status     string     `gorm:"column:status;type:agentsessionstatus;not null"`
	ArchivedAt *time.Time `gorm:"column:archived_at;type:timestamptz"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	Summary    *string    `gorm:"column:summary;type:text"`
	ProductID  *string    `gorm:"column:product_id;type:varchar(36)"`
	ActivityAt time.Time  `gorm:"column:activity_at;type:timestamptz;not null;default:now()"`
}

func (AgentSessions) TableName() string { return "agent_sessions" }

// AgentTasks 对应表 agent_tasks。
// 一条业务 Goal；harness_run_id 是持久化的运行身份，完成只能走用户 complete/cancel。
type AgentTasks struct {
	ID             string     `gorm:"column:id;type:varchar(36);primaryKey"`
	SessionID      string     `gorm:"column:session_id;type:varchar(36);not null"`
	ConversationID *string    `gorm:"column:conversation_id;type:varchar(36)"`
	ProductID      *string    `gorm:"column:product_id;type:varchar(36)"`
	HarnessRunID   string     `gorm:"column:harness_run_id;type:varchar(120);not null"`
	Title          string     `gorm:"column:title;type:varchar(160);not null"`
	Goal           string     `gorm:"column:goal;type:text;not null"`
	Status         string     `gorm:"column:status;type:agenttaskstatus;not null"`
	WaitingReason  *string    `gorm:"column:waiting_reason;type:varchar(160)"`
	FailureReason  *string    `gorm:"column:failure_reason;type:text"`
	CurrentTurnID  *string    `gorm:"column:current_turn_id;type:varchar(36)"`
	StartedAt      *time.Time `gorm:"column:started_at;type:timestamptz"`
	FinishedAt     *time.Time `gorm:"column:finished_at;type:timestamptz"`
	CanceledAt     *time.Time `gorm:"column:canceled_at;type:timestamptz"`
	CreatedAt      time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	Summary        *string    `gorm:"column:summary;type:text"`
}

func (AgentTasks) TableName() string { return "agent_tasks" }

// AgentToolMutations 对应表 agent_tool_mutations。
// 按 idempotency_key 记录工具写入，避免重复副作用。
type AgentToolMutations struct {
	ID                  string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ConversationID      string    `gorm:"column:conversation_id;type:varchar(36);not null"`
	ToolName            string    `gorm:"column:tool_name;type:varchar(120);not null"`
	IdempotencyKey      string    `gorm:"column:idempotency_key;type:varchar(200);not null"`
	RequestHash         string    `gorm:"column:request_hash;type:varchar(64);not null"`
	AssetID             *string   `gorm:"column:asset_id;type:varchar(36)"`
	ExpectedDisplayName *string   `gorm:"column:expected_display_name;type:varchar(255)"`
	TargetDisplayName   *string   `gorm:"column:target_display_name;type:varchar(255)"`
	Status              string    `gorm:"column:status;type:agenttoolmutationstatus;not null"`
	ResultJSON          *string   `gorm:"column:result_json;type:json"`
	CreatedAt           time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
	PreparedJSON        string    `gorm:"column:prepared_json;type:json;not null"`
}

func (AgentToolMutations) TableName() string { return "agent_tool_mutations" }

// AgentTurnCheckpoints 对应表 agent_turn_checkpoints。
// 执行围栏检查点；fencing_token 淘汰过期 worker。
type AgentTurnCheckpoints struct {
	ID               string    `gorm:"column:id;type:varchar(36);primaryKey"`
	TurnProjectionID string    `gorm:"column:turn_projection_id;type:varchar(36);not null"`
	ExecutionID      string    `gorm:"column:execution_id;type:varchar(36);not null"`
	Attempt          int       `gorm:"column:attempt;type:integer;not null"`
	FencingToken     int       `gorm:"column:fencing_token;type:integer;not null"`
	Sequence         int       `gorm:"column:sequence;type:integer;not null"`
	Kind             string    `gorm:"column:kind;type:agentcheckpointkind;not null"`
	PayloadJSON      string    `gorm:"column:payload_json;type:json;not null"`
	CreatedAt        time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (AgentTurnCheckpoints) TableName() string { return "agent_turn_checkpoints" }

// AgentModelInvocations 对应表 agent_model_invocations。
// 一次模型调用的用量与状态记账。
type AgentModelInvocations struct {
	ID                 string     `gorm:"column:id;type:varchar(36);primaryKey"`
	TurnProjectionID   string     `gorm:"column:turn_projection_id;type:varchar(36);not null"`
	ExecutionID        string     `gorm:"column:execution_id;type:varchar(36);not null"`
	ModelRequestID     string     `gorm:"column:model_request_id;type:varchar(120);not null"`
	Attempt            int        `gorm:"column:attempt;type:integer;not null"`
	FencingToken       int        `gorm:"column:fencing_token;type:integer;not null"`
	Provider           string     `gorm:"column:provider;type:varchar(80);not null"`
	Model              string     `gorm:"column:model;type:varchar(160);not null"`
	ExecutionMode      string     `gorm:"column:execution_mode;type:varchar(24);not null"`
	HarnessHash        *string    `gorm:"column:harness_hash;type:varchar(64)"`
	ProviderResponseID *string    `gorm:"column:provider_response_id;type:varchar(200)"`
	ProviderCursor     *string    `gorm:"column:provider_cursor;type:text"`
	Status             string     `gorm:"column:status;type:varchar(24);not null"`
	DurationMS         *int64     `gorm:"column:duration_ms;type:bigint"`
	InputTokens        *int64     `gorm:"column:input_tokens;type:bigint"`
	OutputTokens       *int64     `gorm:"column:output_tokens;type:bigint"`
	TotalTokens        *int64     `gorm:"column:total_tokens;type:bigint"`
	UsageSource        string     `gorm:"column:usage_source;type:varchar(24);not null"`
	ErrorCode          *string    `gorm:"column:error_code;type:varchar(80)"`
	StartedAt          time.Time  `gorm:"column:started_at;type:timestamptz;not null"`
	FinishedAt         *time.Time `gorm:"column:finished_at;type:timestamptz"`
	CreatedAt          time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (AgentModelInvocations) TableName() string { return "agent_model_invocations" }

// AgentTurnEffectReconciliations 对应表 agent_turn_effect_reconciliations。
// 工具副作用对账；无法证明的结果保持 unknown。
type AgentTurnEffectReconciliations struct {
	ID                  string    `gorm:"column:id;type:varchar(36);primaryKey"`
	TurnProjectionID    string    `gorm:"column:turn_projection_id;type:varchar(36);not null"`
	ToolCallID          string    `gorm:"column:tool_call_id;type:varchar(120);not null"`
	ToolName            string    `gorm:"column:tool_name;type:varchar(120);not null"`
	IdempotencyKey      string    `gorm:"column:idempotency_key;type:varchar(200);not null"`
	EffectResult        string    `gorm:"column:effect_result;type:varchar(20);not null"`
	ReconciliationState string    `gorm:"column:reconciliation_state;type:varchar(20);not null"`
	ResultJSON          *string   `gorm:"column:result_json;type:json"`
	Detail              *string   `gorm:"column:detail;type:text"`
	CreatedAt           time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (AgentTurnEffectReconciliations) TableName() string { return "agent_turn_effect_reconciliations" }

// AgentTurnEvents 对应表 agent_turn_events。
// Turn journal 权威行；浏览器 SSE 只回放本表。
type AgentTurnEvents struct {
	ID               string    `gorm:"column:id;type:varchar(36);primaryKey"`
	TurnProjectionID string    `gorm:"column:turn_projection_id;type:varchar(36);not null"`
	ExecutionID      *string   `gorm:"column:execution_id;type:varchar(36)"`
	RunID            string    `gorm:"column:run_id;type:varchar(120);not null"`
	TurnID           string    `gorm:"column:turn_id;type:varchar(120);not null"`
	SchemaVersion    int       `gorm:"column:schema_version;type:integer;not null"`
	Sequence         int       `gorm:"column:sequence;type:integer;not null"`
	Attempt          *int      `gorm:"column:attempt;type:integer"`
	FencingToken     *int      `gorm:"column:fencing_token;type:integer"`
	Kind             string    `gorm:"column:kind;type:varchar(120);not null"`
	Ignorable        bool      `gorm:"column:ignorable;type:boolean;not null;default:false"`
	PayloadJSON      string    `gorm:"column:payload_json;type:json;not null"`
	CreatedAt        time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (AgentTurnEvents) TableName() string { return "agent_turn_events" }

// AgentTurnExecutions 对应表 agent_turn_executions。
// 同一 Turn 的执行 lease 与 phase；过期恢复标 unknown。
type AgentTurnExecutions struct {
	ID                     string     `gorm:"column:id;type:varchar(36);primaryKey"`
	TurnProjectionID       string     `gorm:"column:turn_projection_id;type:varchar(36);not null"`
	HarnessTurnID          string     `gorm:"column:harness_turn_id;type:varchar(120);not null"`
	OwnerID                *string    `gorm:"column:owner_id;type:varchar(120)"`
	LeaseToken             *string    `gorm:"column:lease_token;type:varchar(36)"`
	LeaseExpiresAt         *time.Time `gorm:"column:lease_expires_at;type:timestamptz"`
	Attempt                int        `gorm:"column:attempt;type:integer;not null"`
	FencingToken           int        `gorm:"column:fencing_token;type:integer;not null"`
	Phase                  string     `gorm:"column:phase;type:agentexecutionphase;not null"`
	LastHeartbeatAt        *time.Time `gorm:"column:last_heartbeat_at;type:timestamptz"`
	ReleasedAt             *time.Time `gorm:"column:released_at;type:timestamptz"`
	CreatedAt              time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt              time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	LastCheckpointSequence int        `gorm:"column:last_checkpoint_sequence;type:integer;not null;default:0"`
	LastCheckpointAt       *time.Time `gorm:"column:last_checkpoint_at;type:timestamptz"`
}

func (AgentTurnExecutions) TableName() string { return "agent_turn_executions" }

// AgentTurnProjections 对应表 agent_turn_projections。
// Web 侧 Turn 投影；output_text 等是列表摘要，不是第二份模型 transcript。
type AgentTurnProjections struct {
	ID                                 string     `gorm:"column:id;type:varchar(36);primaryKey"`
	ConversationID                     string     `gorm:"column:conversation_id;type:varchar(36);not null"`
	HarnessTurnID                      *string    `gorm:"column:harness_turn_id;type:varchar(120)"`
	IdempotencyKey                     string     `gorm:"column:idempotency_key;type:varchar(200);not null"`
	RequestHash                        string     `gorm:"column:request_hash;type:varchar(64);not null"`
	InputText                          string     `gorm:"column:input_text;type:text;not null"`
	InputAssetIdsJSON                  string     `gorm:"column:input_asset_ids_json;type:json;not null"`
	Status                             string     `gorm:"column:status;type:agentturnstatus;not null"`
	TerminalReasonCode                 *string    `gorm:"column:terminal_reason_code;type:varchar(40)"`
	ResumeRequired                     bool       `gorm:"column:resume_required;type:boolean;not null"`
	OutputText                         *string    `gorm:"column:output_text;type:text"`
	ThinkingText                       *string    `gorm:"column:thinking_text;type:text"`
	ErrorText                          *string    `gorm:"column:error_text;type:text"`
	QuestionJSON                       *string    `gorm:"column:question_json;type:json"`
	ArtifactName                       *string    `gorm:"column:artifact_name;type:varchar(120)"`
	ArtifactStepID                     *string    `gorm:"column:artifact_step_id;type:varchar(120)"`
	SyncError                          *string    `gorm:"column:sync_error;type:text"`
	FinishedAt                         *time.Time `gorm:"column:finished_at;type:timestamptz"`
	CreatedAt                          time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt                          time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	ToolStepsJSON                      string     `gorm:"column:tool_steps_json;type:json;not null"`
	TaskID                             *string    `gorm:"column:task_id;type:varchar(36)"`
	PageContextSnapshotID              *string    `gorm:"column:page_context_snapshot_id;type:varchar(36)"`
	LibraryOrganizationDraftRevisionID *string    `gorm:"column:library_organization_draft_revision_id;type:varchar(36)"`
	WorkflowRunRequestID               *string    `gorm:"column:workflow_run_request_id;type:varchar(36)"`
	QuestionAnswerJSON                 *string    `gorm:"column:question_answer_json;type:json"`
}

func (AgentTurnProjections) TableName() string { return "agent_turn_projections" }

// AgentWorkflowRunRequests 对应表 agent_workflow_run_requests。
// Agent 代为请求工作流执行，确认后复用同一套 Graph Run 约束。
type AgentWorkflowRunRequests struct {
	ID                       string     `gorm:"column:id;type:varchar(36);primaryKey"`
	ConversationID           string     `gorm:"column:conversation_id;type:varchar(36);not null"`
	TaskID                   *string    `gorm:"column:task_id;type:varchar(36)"`
	ProductID                string     `gorm:"column:product_id;type:varchar(36);not null"`
	ExpectedWorkflowRevision int        `gorm:"column:expected_workflow_revision;type:integer;not null"`
	IdempotencyKey           string     `gorm:"column:idempotency_key;type:varchar(200);not null"`
	RequestHash              string     `gorm:"column:request_hash;type:varchar(64);not null"`
	SourceStepID             string     `gorm:"column:source_step_id;type:varchar(120);not null"`
	Status                   string     `gorm:"column:status;type:agentworkflowrunrequeststatus;not null"`
	FailureReason            *string    `gorm:"column:failure_reason;type:text"`
	ConfirmedAt              *time.Time `gorm:"column:confirmed_at;type:timestamptz"`
	FinishedAt               *time.Time `gorm:"column:finished_at;type:timestamptz"`
	CreatedAt                time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt                time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	GraphID                  string     `gorm:"column:graph_id;type:varchar(36);not null"`
	GraphRunID               *string    `gorm:"column:graph_run_id;type:varchar(36)"`
	SourceGraphRunID         *string    `gorm:"column:source_graph_run_id;type:varchar(36)"`
	RunScope                 *string    `gorm:"column:run_scope;type:varchar(40)"`
	TargetNodeID             *string    `gorm:"column:target_node_id;type:varchar(36)"`
	TargetNodeIDsJSON        *string    `gorm:"column:target_node_ids_json;type:json"`
	Force                    bool       `gorm:"column:force;type:boolean;not null;default:false"`
	DocumentAction           *string    `gorm:"column:document_action;type:varchar(24)"`
}

func (AgentWorkflowRunRequests) TableName() string { return "agent_workflow_run_requests" }

// AppSettings 对应表 app_settings。
// 运行时键值覆盖（供应商与门禁等）；DATABASE_URL 一类密钥不进本表。
type AppSettings struct {
	Key       string    `gorm:"column:key;type:varchar(120);primaryKey"`
	Value     string    `gorm:"column:value;type:text;not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (AppSettings) TableName() string { return "app_settings" }

// AsyncDispatches 对应表 async_dispatches。
// 异步信封：HTTP 只写 PENDING，dispatcher 标 SENT 再入队。
type AsyncDispatches struct {
	ID             string     `gorm:"column:id;type:varchar(36);primaryKey"`
	DeliveryKey    string     `gorm:"column:delivery_key;type:varchar(255);not null"`
	ActorName      string     `gorm:"column:actor_name;type:varchar(120);not null"`
	AggregateID    string     `gorm:"column:aggregate_id;type:varchar(36);not null"`
	PayloadJSON    *string    `gorm:"column:payload_json;type:json"`
	Status         string     `gorm:"column:status;type:asyncdispatchstatus;not null"`
	AvailableAt    time.Time  `gorm:"column:available_at;type:timestamptz;not null"`
	LeaseToken     *string    `gorm:"column:lease_token;type:varchar(36)"`
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at;type:timestamptz"`
	Attempts       int        `gorm:"column:attempts;type:integer;not null"`
	LastError      *string    `gorm:"column:last_error;type:text"`
	SentAt         *time.Time `gorm:"column:sent_at;type:timestamptz"`
	ConsumedAt     *time.Time `gorm:"column:consumed_at;type:timestamptz"`
	CreatedAt      time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (AsyncDispatches) TableName() string { return "async_dispatches" }

// DeliveryRenditionJobs 对应表 delivery_rendition_jobs。
// 确定性交付转码作业，不调用图像模型。
type DeliveryRenditionJobs struct {
	ID                string     `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID         string     `gorm:"column:product_id;type:varchar(36);not null"`
	SourceAssetID     string     `gorm:"column:source_asset_id;type:varchar(36);not null"`
	ResultAssetID     *string    `gorm:"column:result_asset_id;type:varchar(36)"`
	SpecSchemaVersion int        `gorm:"column:spec_schema_version;type:integer;not null"`
	SpecJSON          string     `gorm:"column:spec_json;type:json;not null"`
	SpecHash          string     `gorm:"column:spec_hash;type:varchar(64);not null"`
	Status            string     `gorm:"column:status;type:jobstatus;not null"`
	Attempts          int        `gorm:"column:attempts;type:integer;not null"`
	ActiveAttemptID   *string    `gorm:"column:active_attempt_id;type:varchar(36)"`
	IsRetryable       bool       `gorm:"column:is_retryable;type:boolean;not null"`
	FailureReason     *string    `gorm:"column:failure_reason;type:text"`
	StartedAt         *time.Time `gorm:"column:started_at;type:timestamptz"`
	FinishedAt        *time.Time `gorm:"column:finished_at;type:timestamptz"`
	CreatedAt         time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (DeliveryRenditionJobs) TableName() string { return "delivery_rendition_jobs" }

// DeliveryAdoptionVersions 对应表 delivery_adoption_versions。
// 商品交付采用的不可变版本快照；重跑节点不改已落库版本。
type DeliveryAdoptionVersions struct {
	ID                    string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID             string    `gorm:"column:product_id;type:varchar(36);not null"`
	Version               int       `gorm:"column:version;type:integer;not null"`
	GraphID               *string   `gorm:"column:graph_id;type:varchar(36)"`
	GraphRevision         *int      `gorm:"column:graph_revision;type:integer"`
	FactSetVersionID      *string   `gorm:"column:fact_set_version_id;type:varchar(36)"`
	VisualSystemVersionID *string   `gorm:"column:visual_system_version_id;type:varchar(36)"`
	Notes                 *string   `gorm:"column:notes;type:text"`
	CreatedAt             time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (DeliveryAdoptionVersions) TableName() string { return "delivery_adoption_versions" }

// DeliveryAdoptionSlots 对应表 delivery_adoption_slots。
// 某一交付采用版本中的图位：引用资产 ID 与 DeliverySpec，不复制图片字节。
type DeliveryAdoptionSlots struct {
	ID               string    `gorm:"column:id;type:varchar(36);primaryKey"`
	VersionID        string    `gorm:"column:version_id;type:varchar(36);not null"`
	SlotKey          string    `gorm:"column:slot_key;type:varchar(120);not null"`
	SortOrder        int       `gorm:"column:sort_order;type:integer;not null"`
	ImageTypeKey     *string   `gorm:"column:image_type_key;type:varchar(80)"`
	SourceAssetID    string    `gorm:"column:source_asset_id;type:varchar(36);not null"`
	SourceNodeID     *string   `gorm:"column:source_node_id;type:varchar(36)"`
	DeliverySpecJSON string    `gorm:"column:delivery_spec_json;type:json;not null"`
	DeliverySpecHash string    `gorm:"column:delivery_spec_hash;type:varchar(64);not null"`
	QualityStatus    string    `gorm:"column:quality_status;type:varchar(16);not null"`
	QualityDetail    *string   `gorm:"column:quality_detail;type:text"`
	TextOverflow     bool      `gorm:"column:text_overflow;type:boolean;not null"`
	CreatedAt        time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (DeliveryAdoptionSlots) TableName() string { return "delivery_adoption_slots" }

// ImageSessionAssets 对应表 image_session_assets。
// 连续生图会话里的参考图或生成图，指向 MediaObject。
type ImageSessionAssets struct {
	ID               string    `gorm:"column:id;type:varchar(36);primaryKey"`
	SessionID        string    `gorm:"column:session_id;type:varchar(36);not null"`
	Kind             string    `gorm:"column:kind;type:imagesessionassetkind;not null"`
	OriginalFilename string    `gorm:"column:original_filename;type:varchar(255);not null"`
	MIMEType         string    `gorm:"column:mime_type;type:varchar(100);not null"`
	StoragePath      string    `gorm:"column:storage_path;type:varchar(500);not null"`
	CreatedAt        time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	MediaObjectID    string    `gorm:"column:media_object_id;type:varchar(36);not null"`
}

func (ImageSessionAssets) TableName() string { return "image_session_assets" }

// ImageSessionGenerationTasks 对应表 image_session_generation_tasks。
// 一轮连续生图任务及其进度。
type ImageSessionGenerationTasks struct {
	ID                        string     `gorm:"column:id;type:varchar(36);primaryKey"`
	SessionID                 string     `gorm:"column:session_id;type:varchar(36);not null"`
	Status                    string     `gorm:"column:status;type:jobstatus;not null"`
	Prompt                    string     `gorm:"column:prompt;type:text;not null"`
	Size                      string     `gorm:"column:size;type:varchar(32);not null"`
	BaseAssetID               *string    `gorm:"column:base_asset_id;type:varchar(36)"`
	SelectedReferenceAssetIds *string    `gorm:"column:selected_reference_asset_ids;type:json"`
	GenerationCount           int        `gorm:"column:generation_count;type:integer;not null"`
	FailureReason             *string    `gorm:"column:failure_reason;type:text"`
	ResultGenerationGroupID   *string    `gorm:"column:result_generation_group_id;type:varchar(36)"`
	CreatedAt                 time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	StartedAt                 *time.Time `gorm:"column:started_at;type:timestamptz"`
	FinishedAt                *time.Time `gorm:"column:finished_at;type:timestamptz"`
	Attempts                  int        `gorm:"column:attempts;type:integer;not null"`
	IsRetryable               bool       `gorm:"column:is_retryable;type:boolean;not null"`
	ToolOptions               *string    `gorm:"column:tool_options;type:json"`
	CompletedCandidates       int        `gorm:"column:completed_candidates;type:integer;not null"`
	ActiveCandidateIndex      *int       `gorm:"column:active_candidate_index;type:integer"`
	ProgressPhase             *string    `gorm:"column:progress_phase;type:varchar(64)"`
	ProgressUpdatedAt         *time.Time `gorm:"column:progress_updated_at;type:timestamptz"`
	ProviderResponseID        *string    `gorm:"column:provider_response_id;type:varchar(255)"`
	ProviderResponseStatus    *string    `gorm:"column:provider_response_status;type:varchar(64)"`
	ProgressMetadata          *string    `gorm:"column:progress_metadata;type:json"`
	ActiveAttemptID           *string    `gorm:"column:active_attempt_id;type:varchar(36)"`
}

func (ImageSessionGenerationTasks) TableName() string { return "image_session_generation_tasks" }

// ImageSessionProviderEffects 对应表 image_session_provider_effects。
// 连续生图的 provider 调用对账。
type ImageSessionProviderEffects struct {
	ID                  string    `gorm:"column:id;type:varchar(36);primaryKey"`
	GenerationTaskID    string    `gorm:"column:generation_task_id;type:varchar(36);not null"`
	CandidateStartIndex int       `gorm:"column:candidate_start_index;type:integer;not null"`
	CandidateCount      int       `gorm:"column:candidate_count;type:integer;not null"`
	OperationKey        string    `gorm:"column:operation_key;type:varchar(255);not null"`
	EffectKind          string    `gorm:"column:effect_kind;type:varchar(80);not null"`
	RequestHash         string    `gorm:"column:request_hash;type:varchar(64);not null"`
	ProviderName        string    `gorm:"column:provider_name;type:varchar(80);not null"`
	AttemptID           string    `gorm:"column:attempt_id;type:varchar(36);not null"`
	EffectResult        string    `gorm:"column:effect_result;type:varchar(20);not null"`
	ReconciliationState string    `gorm:"column:reconciliation_state;type:varchar(20);not null"`
	ProviderResponseID  *string   `gorm:"column:provider_response_id;type:varchar(255)"`
	ProviderStatus      *string   `gorm:"column:provider_status;type:varchar(80)"`
	RequestJSON         *string   `gorm:"column:request_json;type:json"`
	ResultJSON          *string   `gorm:"column:result_json;type:json"`
	Detail              *string   `gorm:"column:detail;type:text"`
	CreatedAt           time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (ImageSessionProviderEffects) TableName() string { return "image_session_provider_effects" }

// ImageSessionRounds 对应表 image_session_rounds。
// 一轮提示词、候选与生成结果。
type ImageSessionRounds struct {
	ID                        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	SessionID                 string    `gorm:"column:session_id;type:varchar(36);not null"`
	Prompt                    string    `gorm:"column:prompt;type:text;not null"`
	AssistantMessage          string    `gorm:"column:assistant_message;type:text;not null"`
	Size                      string    `gorm:"column:size;type:varchar(32);not null"`
	ModelName                 string    `gorm:"column:model_name;type:varchar(100);not null"`
	ProviderName              string    `gorm:"column:provider_name;type:varchar(50);not null"`
	PromptVersion             string    `gorm:"column:prompt_version;type:varchar(32);not null"`
	GeneratedAssetID          string    `gorm:"column:generated_asset_id;type:varchar(36);not null"`
	CreatedAt                 time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	ProviderResponseID        *string   `gorm:"column:provider_response_id;type:varchar(128)"`
	PreviousResponseID        *string   `gorm:"column:previous_response_id;type:varchar(128)"`
	ImageGenerationCallID     *string   `gorm:"column:image_generation_call_id;type:varchar(128)"`
	ProviderRequestJSON       *string   `gorm:"column:provider_request_json;type:json"`
	ProviderOutputJSON        *string   `gorm:"column:provider_output_json;type:json"`
	GenerationGroupID         *string   `gorm:"column:generation_group_id;type:varchar(36)"`
	CandidateIndex            int       `gorm:"column:candidate_index;type:integer;not null"`
	CandidateCount            int       `gorm:"column:candidate_count;type:integer;not null"`
	BaseAssetID               *string   `gorm:"column:base_asset_id;type:varchar(36)"`
	SelectedReferenceAssetIds *string   `gorm:"column:selected_reference_asset_ids;type:json"`
}

func (ImageSessionRounds) TableName() string { return "image_session_rounds" }

// ImageSessions 对应表 image_sessions。
// 连续生图会话容器，不等于 AgentSession。
type ImageSessions struct {
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Title     string    `gorm:"column:title;type:varchar(255);not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (ImageSessions) TableName() string { return "image_sessions" }

// LibraryOrganizationDraftRevisions 对应表 library_organization_draft_revisions。
// 全局图库组织草稿的不可变版本。
type LibraryOrganizationDraftRevisions struct {
	ID                   string     `gorm:"column:id;type:varchar(36);primaryKey"`
	DraftID              string     `gorm:"column:draft_id;type:varchar(36);not null"`
	Version              int        `gorm:"column:version;type:integer;not null"`
	SchemaVersion        int        `gorm:"column:schema_version;type:integer;not null"`
	PayloadJSON          string     `gorm:"column:payload_json;type:json;not null"`
	PayloadHash          string     `gorm:"column:payload_hash;type:varchar(64);not null"`
	SourceTurnID         *string    `gorm:"column:source_turn_id;type:varchar(120)"`
	SourceArtifactStepID *string    `gorm:"column:source_artifact_step_id;type:varchar(120)"`
	ConfirmedAt          *time.Time `gorm:"column:confirmed_at;type:timestamptz"`
	CreatedAt            time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
}

func (LibraryOrganizationDraftRevisions) TableName() string {
	return "library_organization_draft_revisions"
}

// LibraryOrganizationDrafts 对应表 library_organization_drafts。
// Agent 提出的图库文件夹/标签草稿，需用户确认。
type LibraryOrganizationDrafts struct {
	ID                         string     `gorm:"column:id;type:varchar(36);primaryKey"`
	ConversationID             string     `gorm:"column:conversation_id;type:varchar(36);not null"`
	Status                     string     `gorm:"column:status;type:libraryorganizationdraftstatus;not null"`
	CurrentRevisionID          *string    `gorm:"column:current_revision_id;type:varchar(36)"`
	ConfirmedRevisionID        *string    `gorm:"column:confirmed_revision_id;type:varchar(36)"`
	ConfirmationIdempotencyKey *string    `gorm:"column:confirmation_idempotency_key;type:varchar(200)"`
	ConfirmationRequestHash    *string    `gorm:"column:confirmation_request_hash;type:varchar(64)"`
	ConfirmationResultJSON     *string    `gorm:"column:confirmation_result_json;type:json"`
	ConfirmedAt                *time.Time `gorm:"column:confirmed_at;type:timestamptz"`
	CreatedAt                  time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt                  time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (LibraryOrganizationDrafts) TableName() string { return "library_organization_drafts" }

// LocalImageEditAdoptionEvents 对应表 local_image_edit_adoption_events。
// 局部编辑结果采纳或回退到图节点的事件。
type LocalImageEditAdoptionEvents struct {
	ID             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID      string    `gorm:"column:product_id;type:varchar(36);not null"`
	TaskID         string    `gorm:"column:task_id;type:varchar(36);not null"`
	GraphID        string    `gorm:"column:graph_id;type:varchar(36);not null"`
	NodeID         string    `gorm:"column:node_id;type:varchar(36);not null"`
	EventType      string    `gorm:"column:event_type;type:varchar(16);not null"`
	FromArtifactID string    `gorm:"column:from_artifact_id;type:varchar(36);not null"`
	ToArtifactID   string    `gorm:"column:to_artifact_id;type:varchar(36);not null"`
	RelatedEventID *string   `gorm:"column:related_event_id;type:varchar(36)"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (LocalImageEditAdoptionEvents) TableName() string { return "local_image_edit_adoption_events" }

// LocalImageEditProviderAttempts 对应表 local_image_edit_provider_attempts。
// 一次局部编辑的 provider 尝试与对账。
type LocalImageEditProviderAttempts struct {
	ID                      string    `gorm:"column:id;type:varchar(36);primaryKey"`
	TaskID                  string    `gorm:"column:task_id;type:varchar(36);not null"`
	AttemptID               string    `gorm:"column:attempt_id;type:varchar(36);not null"`
	AttemptNumber           int       `gorm:"column:attempt_number;type:integer;not null"`
	OperationKey            string    `gorm:"column:operation_key;type:varchar(255);not null"`
	RequestHash             string    `gorm:"column:request_hash;type:varchar(64);not null"`
	Phase                   string    `gorm:"column:phase;type:varchar(40);not null"`
	EffectResult            string    `gorm:"column:effect_result;type:varchar(20);not null"`
	ProviderName            string    `gorm:"column:provider_name;type:varchar(80);not null"`
	ProviderModel           *string   `gorm:"column:provider_model;type:varchar(255)"`
	ProviderResponseID      *string   `gorm:"column:provider_response_id;type:varchar(255)"`
	ProviderStatus          *string   `gorm:"column:provider_status;type:varchar(80)"`
	RequestJSON             *string   `gorm:"column:request_json;type:json"`
	EffectiveParametersJSON *string   `gorm:"column:effective_parameters_json;type:json"`
	ResultJSON              *string   `gorm:"column:result_json;type:json"`
	LateResultAssetID       *string   `gorm:"column:late_result_asset_id;type:varchar(36)"`
	Detail                  *string   `gorm:"column:detail;type:text"`
	CreatedAt               time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt               time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (LocalImageEditProviderAttempts) TableName() string { return "local_image_edit_provider_attempts" }

// LocalImageEditTaskReferences 对应表 local_image_edit_task_references。
// 局部编辑任务的参考图关联。
type LocalImageEditTaskReferences struct {
	TaskID    string `gorm:"column:task_id;type:varchar(36);primaryKey"`
	AssetID   string `gorm:"column:asset_id;type:varchar(36);primaryKey"`
	SortOrder int    `gorm:"column:sort_order;type:integer;not null"`
}

func (LocalImageEditTaskReferences) TableName() string { return "local_image_edit_task_references" }

// LocalImageEditTasks 对应表 local_image_edit_tasks。
// 蒙版局部编辑任务；mask 指向独立 MediaObject。
type LocalImageEditTasks struct {
	ID                        string     `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID                 string     `gorm:"column:product_id;type:varchar(36);not null"`
	SourceAssetID             string     `gorm:"column:source_asset_id;type:varchar(36);not null"`
	SourceMediaSHA256         string     `gorm:"column:source_media_sha256;type:varchar(64);not null"`
	MaskMediaObjectID         string     `gorm:"column:mask_media_object_id;type:varchar(36);not null"`
	TargetGraphID             *string    `gorm:"column:target_graph_id;type:varchar(36)"`
	TargetNodeID              *string    `gorm:"column:target_node_id;type:varchar(36)"`
	TargetGraphRevision       *int       `gorm:"column:target_graph_revision;type:integer"`
	SourceArtifactID          *string    `gorm:"column:source_artifact_id;type:varchar(36)"`
	SourceArtifactAssetID     *string    `gorm:"column:source_artifact_asset_id;type:varchar(36)"`
	SourceArtifactInputDigest *string    `gorm:"column:source_artifact_input_digest;type:varchar(64)"`
	Operation                 string     `gorm:"column:operation;type:varchar(32);not null"`
	Instruction               *string    `gorm:"column:instruction;type:text"`
	SourceText                *string    `gorm:"column:source_text;type:text"`
	ReplacementText           *string    `gorm:"column:replacement_text;type:text"`
	MaskGeometryJSON          string     `gorm:"column:mask_geometry_json;type:json;not null"`
	Status                    string     `gorm:"column:status;type:localimageedittaskstatus;not null"`
	Revision                  int        `gorm:"column:revision;type:integer;not null"`
	IdempotencyKey            *string    `gorm:"column:idempotency_key;type:varchar(120)"`
	RequestHash               *string    `gorm:"column:request_hash;type:varchar(64)"`
	Attempts                  int        `gorm:"column:attempts;type:integer;not null"`
	ActiveAttemptID           *string    `gorm:"column:active_attempt_id;type:varchar(36)"`
	ProgressPhase             *string    `gorm:"column:progress_phase;type:varchar(80)"`
	FailureReason             *string    `gorm:"column:failure_reason;type:text"`
	IsRetryable               bool       `gorm:"column:is_retryable;type:boolean;not null"`
	ProviderName              *string    `gorm:"column:provider_name;type:varchar(80)"`
	ProviderModel             *string    `gorm:"column:provider_model;type:varchar(255)"`
	ProviderResponseID        *string    `gorm:"column:provider_response_id;type:varchar(255)"`
	ProviderStatus            *string    `gorm:"column:provider_status;type:varchar(80)"`
	ResultAssetID             *string    `gorm:"column:result_asset_id;type:varchar(36)"`
	QueuedAt                  *time.Time `gorm:"column:queued_at;type:timestamptz"`
	StartedAt                 *time.Time `gorm:"column:started_at;type:timestamptz"`
	FinishedAt                *time.Time `gorm:"column:finished_at;type:timestamptz"`
	CreatedAt                 time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt                 time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	RequestedProviderName     *string    `gorm:"column:requested_provider_name;type:varchar(80)"`
	RequestedLocalEditMode    *string    `gorm:"column:requested_local_edit_mode;type:varchar(32)"`
}

func (LocalImageEditTasks) TableName() string { return "local_image_edit_tasks" }

// MediaLibraryAssetTags 对应表 media_library_asset_tags。
// 全局素材与标签的多对多。
type MediaLibraryAssetTags struct {
	AssetID   string    `gorm:"column:asset_id;type:varchar(36);primaryKey"`
	TagID     string    `gorm:"column:tag_id;type:varchar(36);primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (MediaLibraryAssetTags) TableName() string { return "media_library_asset_tags" }

// MediaLibraryAssets 对应表 media_library_assets。
// 跨会话可归档的全局图库素材身份，不拥有第二份 bytes。
type MediaLibraryAssets struct {
	ID                        string     `gorm:"column:id;type:varchar(36);primaryKey"`
	MediaObjectID             string     `gorm:"column:media_object_id;type:varchar(36);not null"`
	SourceType                string     `gorm:"column:source_type;type:varchar(40);not null"`
	SourceID                  string     `gorm:"column:source_id;type:varchar(36);not null"`
	SourceImageSessionAssetID *string    `gorm:"column:source_image_session_asset_id;type:varchar(36)"`
	SourceProductAssetID      *string    `gorm:"column:source_product_asset_id;type:varchar(36)"`
	ProvenanceJSON            string     `gorm:"column:provenance_json;type:json;not null"`
	ProvenanceHash            string     `gorm:"column:provenance_hash;type:varchar(64);not null"`
	Revision                  int        `gorm:"column:revision;type:integer;not null"`
	DisplayName               string     `gorm:"column:display_name;type:varchar(255);not null"`
	OriginalFilename          string     `gorm:"column:original_filename;type:varchar(255);not null"`
	IsArchived                bool       `gorm:"column:is_archived;type:boolean;not null"`
	ArchivedAt                *time.Time `gorm:"column:archived_at;type:timestamptz"`
	CreatedAt                 time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt                 time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	FolderID                  *string    `gorm:"column:folder_id;type:varchar(36)"`
}

func (MediaLibraryAssets) TableName() string { return "media_library_assets" }

// MediaLibraryCollectionKeys 对应表 media_library_collection_keys。
// 从商品收藏到全局图库的幂等键。
type MediaLibraryCollectionKeys struct {
	ID             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID      string    `gorm:"column:product_id;type:varchar(36);not null"`
	IdempotencyKey string    `gorm:"column:idempotency_key;type:varchar(200);not null"`
	RequestHash    string    `gorm:"column:request_hash;type:varchar(64);not null"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (MediaLibraryCollectionKeys) TableName() string { return "media_library_collection_keys" }

// MediaLibraryFolders 对应表 media_library_folders。
// 全局图库一层用户文件夹。
type MediaLibraryFolders struct {
	ID             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Name           string    `gorm:"column:name;type:varchar(120);not null"`
	NormalizedName string    `gorm:"column:normalized_name;type:varchar(120);not null"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (MediaLibraryFolders) TableName() string { return "media_library_folders" }

// MediaLibraryTags 对应表 media_library_tags。
// 全局图库标签。
type MediaLibraryTags struct {
	ID             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Name           string    `gorm:"column:name;type:varchar(80);not null"`
	NormalizedName string    `gorm:"column:normalized_name;type:varchar(80);not null"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (MediaLibraryTags) TableName() string { return "media_library_tags" }

// MediaLibraryUploadKeys 对应表 media_library_upload_keys。
// 直接上传到全局图库的幂等键。
type MediaLibraryUploadKeys struct {
	ID             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	IdempotencyKey string    `gorm:"column:idempotency_key;type:varchar(200);not null"`
	RequestHash    string    `gorm:"column:request_hash;type:varchar(64);not null"`
	AssetIdsJSON   string    `gorm:"column:asset_ids_json;type:text;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (MediaLibraryUploadKeys) TableName() string { return "media_library_upload_keys" }

// MediaObjects 对应表 media_objects。
// 不可变媒体字节身份；商品图、会话图、全局素材都引用它。
type MediaObjects struct {
	ID                 string     `gorm:"column:id;type:varchar(36);primaryKey"`
	StoragePath        string     `gorm:"column:storage_path;type:varchar(500);not null"`
	MIMEType           string     `gorm:"column:mime_type;type:varchar(100);not null"`
	ByteSize           *int64     `gorm:"column:byte_size;type:bigint"`
	Width              *int       `gorm:"column:width;type:integer"`
	Height             *int       `gorm:"column:height;type:integer"`
	SHA256             *string    `gorm:"column:sha256;type:varchar(64)"`
	VerificationStatus string     `gorm:"column:verification_status;type:mediaverificationstatus;not null"`
	CreatedAt          time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	VerifiedAt         *time.Time `gorm:"column:verified_at;type:timestamptz"`
}

func (MediaObjects) TableName() string { return "media_objects" }

// ProductAssetFolders 对应表 product_asset_folders。
// 商品图库一层用户文件夹；删除只去掉组织，不删资产。
type ProductAssetFolders struct {
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID string    `gorm:"column:product_id;type:varchar(36);not null"`
	Name      string    `gorm:"column:name;type:varchar(120);not null"`
	SortOrder int       `gorm:"column:sort_order;type:integer;not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (ProductAssetFolders) TableName() string { return "product_asset_folders" }

// ProductFactSetVersions 对应表 product_fact_set_versions。
// 商品 facts 的不可变版本，供历史 Run 引用。
type ProductFactSetVersions struct {
	ID          string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID   string    `gorm:"column:product_id;type:varchar(36);not null"`
	Version     int       `gorm:"column:version;type:integer;not null"`
	PayloadJSON string    `gorm:"column:payload_json;type:json;not null"`
	PayloadHash string    `gorm:"column:payload_hash;type:varchar(64);not null"`
	CreatedAt   time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (ProductFactSetVersions) TableName() string { return "product_fact_set_versions" }

// ProductImageAssets 对应表 product_image_assets。
// 商品命名空间内的一张图；工作流节点绑定本 id，不绑存储路径。
type ProductImageAssets struct {
	ID                        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID                 string    `gorm:"column:product_id;type:varchar(36);not null"`
	MediaObjectID             string    `gorm:"column:media_object_id;type:varchar(36);not null"`
	OriginType                string    `gorm:"column:origin_type;type:productimageorigintype;not null"`
	DisplayName               string    `gorm:"column:display_name;type:varchar(255);not null"`
	OriginalFilename          string    `gorm:"column:original_filename;type:varchar(255);not null"`
	ParentAssetID             *string   `gorm:"column:parent_asset_id;type:varchar(36)"`
	SourceImageSessionAssetID *string   `gorm:"column:source_image_session_asset_id;type:varchar(36)"`
	CreatedAt                 time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt                 time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
	ImageTypeKey              *string   `gorm:"column:image_type_key;type:varchar(80)"`
	UserFolderID              *string   `gorm:"column:user_folder_id;type:varchar(36)"`
	SourceLibraryAssetID      *string   `gorm:"column:source_library_asset_id;type:varchar(36)"`
}

func (ProductImageAssets) TableName() string { return "product_image_assets" }

// Products 对应表 products。
// 商品主档：名称、intake、封面与当前 facts 版本。
type Products struct {
	CreationIdempotencyKey  *string   `gorm:"column:creation_idempotency_key;type:varchar(120)"`
	CreationRequestHash     *string   `gorm:"column:creation_request_hash;type:varchar(64)"`
	ID                      string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Name                    string    `gorm:"column:name;type:varchar(255);not null"`
	Category                *string   `gorm:"column:category;type:varchar(120)"`
	Price                   *string   `gorm:"column:price;type:numeric(10,2)"`
	SourceNote              *string   `gorm:"column:source_note;type:text"`
	CreatedAt               time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt               time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
	CoverImageAssetID                  *string   `gorm:"column:cover_image_asset_id;type:varchar(36)"`
	CurrentFactSetVersionID            *string   `gorm:"column:current_fact_set_version_id;type:varchar(36)"`
	CurrentDeliveryAdoptionVersionID   *string   `gorm:"column:current_delivery_adoption_version_id;type:varchar(36)"`
	IntakeSchemaVersion                *int      `gorm:"column:intake_schema_version;type:integer"`
	IntakeJSON                         *string   `gorm:"column:intake_json;type:json"`
}

func (Products) TableName() string { return "products" }

// ProviderBindings 对应表 provider_bindings。
// 把 prompt/image 等用途绑到一个 ProviderProfile。
type ProviderBindings struct {
	ID                string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Purpose           string    `gorm:"column:purpose;type:varchar(40);not null"`
	ProviderKind      string    `gorm:"column:provider_kind;type:varchar(40);not null"`
	ProviderProfileID *string   `gorm:"column:provider_profile_id;type:varchar(36)"`
	ModelSettingsJSON string    `gorm:"column:model_settings_json;type:json;not null"`
	ConfigJSON        string    `gorm:"column:config_json;type:json;not null"`
	CreatedAt         time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt         time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (ProviderBindings) TableName() string { return "provider_bindings" }

// ProviderProfiles 对应表 provider_profiles。
// 供应商连接与默认模型；密钥存在本表而非进程 overlay。
type ProviderProfiles struct {
	ID                string     `gorm:"column:id;type:varchar(36);primaryKey"`
	Name              string     `gorm:"column:name;type:varchar(120);not null"`
	ProviderType      string     `gorm:"column:provider_type;type:varchar(40);not null"`
	BaseURL           *string    `gorm:"column:base_url;type:text"`
	APIKey            *string    `gorm:"column:api_key;type:text"`
	CapabilitiesJSON  string     `gorm:"column:capabilities_json;type:json;not null"`
	DefaultModelsJSON string     `gorm:"column:default_models_json;type:json;not null"`
	ConfigJSON        string     `gorm:"column:config_json;type:json;not null"`
	Enabled           bool       `gorm:"column:enabled;type:boolean;not null"`
	ArchivedAt        *time.Time `gorm:"column:archived_at;type:timestamptz"`
	CreatedAt         time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (ProviderProfiles) TableName() string { return "provider_profiles" }

// VisualSystemVersionReferences 对应表 visual_system_version_references。
// 视觉系统某一版本引用的参考图。
type VisualSystemVersionReferences struct {
	ID                    string `gorm:"column:id;type:varchar(36);primaryKey"`
	VisualSystemVersionID string `gorm:"column:visual_system_version_id;type:varchar(36);not null"`
	AssetID               string `gorm:"column:asset_id;type:varchar(36);not null"`
	Role                  string `gorm:"column:role;type:varchar(120);not null"`
	Label                 string `gorm:"column:label;type:varchar(255);not null"`
	Position              int    `gorm:"column:position;type:integer;not null"`
}

func (VisualSystemVersionReferences) TableName() string { return "visual_system_version_references" }

// VisualSystemVersions 对应表 visual_system_versions。
// 视觉系统不可变版本，供历史 Run 引用。
type VisualSystemVersions struct {
	ID             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	VisualSystemID string    `gorm:"column:visual_system_id;type:varchar(36);not null"`
	Version        int       `gorm:"column:version;type:integer;not null"`
	SchemaVersion  int       `gorm:"column:schema_version;type:integer;not null"`
	PayloadJSON    string    `gorm:"column:payload_json;type:json;not null"`
	PayloadHash    string    `gorm:"column:payload_hash;type:varchar(64);not null"`
	SourceMarkdown *string   `gorm:"column:source_markdown;type:text"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (VisualSystemVersions) TableName() string { return "visual_system_versions" }

// VisualSystems 对应表 visual_systems。
// 可归档的视觉系统主档。
type VisualSystems struct {
	ID         string     `gorm:"column:id;type:varchar(36);primaryKey"`
	Name       string     `gorm:"column:name;type:varchar(255);not null"`
	ArchivedAt *time.Time `gorm:"column:archived_at;type:timestamptz"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (VisualSystems) TableName() string { return "visual_systems" }

// WorkflowGraphArtifacts 对应表 workflow_graph_artifacts。
// 节点产物与 lineage；image_generation 产物不是 compile 闸门。
type WorkflowGraphArtifacts struct {
	ID                  string    `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphID             string    `gorm:"column:graph_id;type:varchar(36);not null"`
	NodeID              *string   `gorm:"column:node_id;type:varchar(36)"`
	NodeRunID           *string   `gorm:"column:node_run_id;type:varchar(36)"`
	ArtifactType        string    `gorm:"column:artifact_type;type:varchar(40);not null"`
	SchemaVersion       int       `gorm:"column:schema_version;type:integer;not null"`
	GraphRevision       int       `gorm:"column:graph_revision;type:integer;not null"`
	PayloadJSON         string    `gorm:"column:payload_json;type:json;not null"`
	PayloadHash         string    `gorm:"column:payload_hash;type:varchar(64);not null"`
	InputDigest         string    `gorm:"column:input_digest;type:varchar(64);not null"`
	DocumentAction      *string   `gorm:"column:document_action;type:varchar(24)"`
	BaseDocumentHash    *string   `gorm:"column:base_document_hash;type:varchar(64)"`
	ProductImageAssetID *string   `gorm:"column:product_image_asset_id;type:varchar(36)"`
	ProviderName        *string   `gorm:"column:provider_name;type:varchar(80)"`
	ProviderModel       *string   `gorm:"column:provider_model;type:varchar(255)"`
	CreatedAt           time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (WorkflowGraphArtifacts) TableName() string { return "workflow_graph_artifacts" }

// WorkflowGraphEdges 对应表 workflow_graph_edges。
// schema-v3 边；role 等于 React Flow handle id。
type WorkflowGraphEdges struct {
	ID           string    `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphID      string    `gorm:"column:graph_id;type:varchar(36);not null"`
	SourceNodeID string    `gorm:"column:source_node_id;type:varchar(36);not null"`
	TargetNodeID string    `gorm:"column:target_node_id;type:varchar(36);not null"`
	DataType     string    `gorm:"column:data_type;type:varchar(40);not null"`
	Role         string    `gorm:"column:role;type:varchar(40);not null"`
	SortOrder    int       `gorm:"column:sort_order;type:integer;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (WorkflowGraphEdges) TableName() string { return "workflow_graph_edges" }

// WorkflowGraphGroups 对应表 workflow_graph_groups。
// 画布一层视觉分组，没有执行状态或端口。
type WorkflowGraphGroups struct {
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphID   string    `gorm:"column:graph_id;type:varchar(36);not null"`
	Title     string    `gorm:"column:title;type:varchar(255);not null"`
	SortOrder int       `gorm:"column:sort_order;type:integer;not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (WorkflowGraphGroups) TableName() string { return "workflow_graph_groups" }

// WorkflowGraphNodeRuns 对应表 workflow_graph_node_runs。
// 一次 Graph Run 里单个节点的 queued|running|succeeded|failed|unknown|skipped|cancelled。
type WorkflowGraphNodeRuns struct {
	ID                  string     `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphRunID          string     `gorm:"column:graph_run_id;type:varchar(36);not null"`
	NodeID              *string    `gorm:"column:node_id;type:varchar(36)"`
	Status              string     `gorm:"column:status;type:varchar(40);not null"`
	SortOrder           int        `gorm:"column:sort_order;type:integer;not null"`
	CompiledContextJSON *string    `gorm:"column:compiled_context_json;type:json"`
	OutputJSON          *string    `gorm:"column:output_json;type:json"`
	FailureReason       *string    `gorm:"column:failure_reason;type:text"`
	StartedAt           time.Time  `gorm:"column:started_at;type:timestamptz;not null"`
	FinishedAt          *time.Time `gorm:"column:finished_at;type:timestamptz"`
	ActiveAttemptID     *string    `gorm:"column:active_attempt_id;type:varchar(36)"`
	AttemptCount        int        `gorm:"column:attempt_count;type:integer;not null;default:0"`
	ProgressPhase       *string    `gorm:"column:progress_phase;type:varchar(80)"`
	ProgressUpdatedAt   *time.Time `gorm:"column:progress_updated_at;type:timestamptz"`
	PlannedAction       *string    `gorm:"column:planned_action;type:varchar(40)"`
}

func (WorkflowGraphNodeRuns) TableName() string { return "workflow_graph_node_runs" }

// WorkflowGraphRunEvents 对应表 workflow_graph_run_events。
// Graph Run 的有序事件。
type WorkflowGraphRunEvents struct {
	ID          string    `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphRunID  string    `gorm:"column:graph_run_id;type:varchar(36);not null"`
	Sequence    int       `gorm:"column:sequence;type:integer;not null"`
	Kind        string    `gorm:"column:kind;type:varchar(64);not null"`
	NodeRunID   *string   `gorm:"column:node_run_id;type:varchar(36)"`
	PayloadJSON string    `gorm:"column:payload_json;type:json;not null"`
	CreatedAt   time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (WorkflowGraphRunEvents) TableName() string { return "workflow_graph_run_events" }

// WorkflowGraphNodes 对应表 workflow_graph_nodes。
// schema-v3 节点；document_origin 为 seed|generated|authored。
type WorkflowGraphNodes struct {
	ID                         string    `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphID                    string    `gorm:"column:graph_id;type:varchar(36);not null"`
	NodeType                   string    `gorm:"column:node_type;type:varchar(40);not null"`
	Title                      string    `gorm:"column:title;type:varchar(255);not null"`
	PositionX                  int       `gorm:"column:position_x;type:integer;not null"`
	PositionY                  int       `gorm:"column:position_y;type:integer;not null"`
	ConfigJSON                 string    `gorm:"column:config_json;type:json;not null"`
	BoundImageAssetID          *string   `gorm:"column:bound_image_asset_id;type:varchar(36)"`
	GroupID                    *string   `gorm:"column:group_id;type:varchar(36)"`
	CreatedAt                  time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt                  time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
	CurrentArtifactID          *string   `gorm:"column:current_artifact_id;type:varchar(36)"`
	PendingCandidateArtifactID *string   `gorm:"column:pending_candidate_artifact_id;type:varchar(36)"`
	DocumentOrigin             *string   `gorm:"column:document_origin;type:varchar(40)"`
}

func (WorkflowGraphNodes) TableName() string { return "workflow_graph_nodes" }

// WorkflowGraphProposals 对应表 workflow_graph_proposals。
// Agent 提出的多节点 ChangeSet，确认前不写入 live 图。
type WorkflowGraphProposals struct {
	ID                string     `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphID           string     `gorm:"column:graph_id;type:varchar(36);not null"`
	ConversationID    *string    `gorm:"column:conversation_id;type:varchar(36)"`
	Status            string     `gorm:"column:status;type:varchar(16);not null"`
	Summary           string     `gorm:"column:summary;type:varchar(500);not null"`
	BaseGraphRevision int        `gorm:"column:base_graph_revision;type:integer;not null"`
	ChangeSetJSON     string     `gorm:"column:change_set_json;type:json;not null"`
	OperationGroupID  *string    `gorm:"column:operation_group_id;type:varchar(36)"`
	CreatedAt         time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	ResolvedAt        *time.Time `gorm:"column:resolved_at;type:timestamptz"`
}

func (WorkflowGraphProposals) TableName() string { return "workflow_graph_proposals" }

// WorkflowGraphProviderEffects 对应表 workflow_graph_provider_effects。
// 图节点 provider 调用对账；无法证明则 unknown。
type WorkflowGraphProviderEffects struct {
	ID                  string    `gorm:"column:id;type:varchar(36);primaryKey"`
	NodeRunID           string    `gorm:"column:node_run_id;type:varchar(36);not null"`
	OperationKey        string    `gorm:"column:operation_key;type:varchar(255);not null"`
	EffectKind          string    `gorm:"column:effect_kind;type:varchar(80);not null"`
	RequestHash         string    `gorm:"column:request_hash;type:varchar(64);not null"`
	ProviderName        string    `gorm:"column:provider_name;type:varchar(80);not null"`
	AttemptID           string    `gorm:"column:attempt_id;type:varchar(36);not null"`
	EffectResult        string    `gorm:"column:effect_result;type:varchar(20);not null"`
	ReconciliationState string    `gorm:"column:reconciliation_state;type:varchar(20);not null"`
	ProviderResponseID  *string   `gorm:"column:provider_response_id;type:varchar(255)"`
	ProviderStatus      *string   `gorm:"column:provider_status;type:varchar(80)"`
	RequestJSON         *string   `gorm:"column:request_json;type:json"`
	ResultJSON          *string   `gorm:"column:result_json;type:json"`
	Detail              *string   `gorm:"column:detail;type:text"`
	CreatedAt           time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (WorkflowGraphProviderEffects) TableName() string { return "workflow_graph_provider_effects" }

// WorkflowGraphRuns 对应表 workflow_graph_runs。
// 独立业务执行记录；用户点运行即可创建，不必先有 Agent Session。
type WorkflowGraphRuns struct {
	ID                      string     `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphID                 string     `gorm:"column:graph_id;type:varchar(36);not null"`
	Status                  string     `gorm:"column:status;type:varchar(40);not null"`
	RunScope                string     `gorm:"column:run_scope;type:varchar(40);not null"`
	RequestedNodeID         *string    `gorm:"column:requested_node_id;type:varchar(36)"`
	GraphRevision           int        `gorm:"column:graph_revision;type:integer;not null"`
	SnapshotJSON            string     `gorm:"column:snapshot_json;type:json;not null"`
	FailureReason           *string    `gorm:"column:failure_reason;type:text"`
	IsRetryable             bool       `gorm:"column:is_retryable;type:boolean;not null"`
	ProgressMetadata        *string    `gorm:"column:progress_metadata;type:json"`
	ExecutionLeaseToken     *string    `gorm:"column:execution_lease_token;type:varchar(36)"`
	ExecutionLeaseExpiresAt *time.Time `gorm:"column:execution_lease_expires_at;type:timestamptz"`
	StartedAt               time.Time  `gorm:"column:started_at;type:timestamptz;not null"`
	FinishedAt              *time.Time `gorm:"column:finished_at;type:timestamptz"`
}

func (WorkflowGraphRuns) TableName() string { return "workflow_graph_runs" }

// WorkflowGraphs 对应表 workflow_graphs。
// 商品上唯一在线的 schema-v3 live 图。
type WorkflowGraphs struct {
	ID            string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID     string    `gorm:"column:product_id;type:varchar(36);not null"`
	Title         string    `gorm:"column:title;type:varchar(255);not null"`
	Active        bool      `gorm:"column:active;type:boolean;not null"`
	SchemaVersion int       `gorm:"column:schema_version;type:integer;not null"`
	Revision      int       `gorm:"column:revision;type:integer;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt     time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (WorkflowGraphs) TableName() string { return "workflow_graphs" }

// WorkflowMediaLibraryAssets 对应表 workflow_media_library_assets。
// 工作流与全局素材的关联，不复制媒体 bytes。
type WorkflowMediaLibraryAssets struct {
	WorkflowID          string    `gorm:"column:workflow_id;type:varchar(36);primaryKey"`
	MediaLibraryAssetID string    `gorm:"column:media_library_asset_id;type:varchar(36);primaryKey"`
	CreatedAt           time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (WorkflowMediaLibraryAssets) TableName() string { return "workflow_media_library_assets" }

// WorkflowOperationGroups 对应表 workflow_operation_groups。
// 一次可撤销的 Graph Command 操作组及其 inverse。
type WorkflowOperationGroups struct {
	ID                    string    `gorm:"column:id;type:varchar(36);primaryKey"`
	GraphID               string    `gorm:"column:graph_id;type:varchar(36);not null"`
	ActorType             string    `gorm:"column:actor_type;type:varchar(40);not null"`
	Summary               string    `gorm:"column:summary;type:varchar(500);not null"`
	BaseRevision          int       `gorm:"column:base_revision;type:integer;not null"`
	ResultRevision        int       `gorm:"column:result_revision;type:integer;not null"`
	OperationsJSON        string    `gorm:"column:operations_json;type:json;not null"`
	InverseOperationsJSON string    `gorm:"column:inverse_operations_json;type:json;not null"`
	CreatedAt             time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	HistoryKind           string    `gorm:"column:history_kind;type:varchar(16);not null"`
}

func (WorkflowOperationGroups) TableName() string { return "workflow_operation_groups" }

// WorkflowRecipeApplications 对应表 workflow_recipe_applications。
// 把配方应用到某商品 live 图的确认记录。
type WorkflowRecipeApplications struct {
	ID                   string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ProductID            string    `gorm:"column:product_id;type:varchar(36);not null"`
	RecipeVersionID      string    `gorm:"column:recipe_version_id;type:varchar(36);not null"`
	GraphID              string    `gorm:"column:graph_id;type:varchar(36);not null"`
	OperationGroupID     string    `gorm:"column:operation_group_id;type:varchar(36);not null"`
	Mode                 string    `gorm:"column:mode;type:varchar(16);not null"`
	SchemaVersion        int       `gorm:"column:schema_version;type:integer;not null"`
	IdempotencyKey       string    `gorm:"column:idempotency_key;type:varchar(120);not null"`
	RequestHash          string    `gorm:"column:request_hash;type:varchar(64);not null"`
	AddedNodeIdsJSON     string    `gorm:"column:added_node_ids_json;type:json;not null"`
	AddedEdgeIdsJSON     string    `gorm:"column:added_edge_ids_json;type:json;not null"`
	CreatedAt            time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	PreviewGraphRevision *int      `gorm:"column:preview_graph_revision;type:integer"`
	PreviewDigest        *string   `gorm:"column:preview_digest;type:varchar(64)"`
	UpdatedNodeIdsJSON   *string   `gorm:"column:updated_node_ids_json;type:json"`
	RequiredBindingsJSON *string   `gorm:"column:required_bindings_json;type:json"`
}

func (WorkflowRecipeApplications) TableName() string { return "workflow_recipe_applications" }

// WorkflowRecipeVersions 对应表 workflow_recipe_versions。
// 配方不可变版本；不含商品身份或媒体 bytes。
type WorkflowRecipeVersions struct {
	ID                             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	RecipeID                       string    `gorm:"column:recipe_id;type:varchar(36);not null"`
	Version                        int       `gorm:"column:version;type:integer;not null"`
	SchemaVersion                  int       `gorm:"column:schema_version;type:integer;not null"`
	Title                          string    `gorm:"column:title;type:varchar(255);not null"`
	Description                    *string   `gorm:"column:description;type:text"`
	PayloadJSON                    string    `gorm:"column:payload_json;type:json;not null"`
	PayloadHash                    string    `gorm:"column:payload_hash;type:varchar(64);not null"`
	PreferredVisualSystemVersionID *string   `gorm:"column:preferred_visual_system_version_id;type:varchar(36)"`
	CreatedAt                      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	CatalogVersion                 int       `gorm:"column:catalog_version;type:integer;not null"`
	CreationSource                 string    `gorm:"column:creation_source;type:workflowrecipecreationsource;not null"`
	GovernanceJSON                 *string   `gorm:"column:governance_json;type:json"`
}

func (WorkflowRecipeVersions) TableName() string { return "workflow_recipe_versions" }

// WorkflowRecipes 对应表 workflow_recipes。
// 可复用工作流结构的主档，不等于收藏画廊。
type WorkflowRecipes struct {
	ID               string     `gorm:"column:id;type:varchar(36);primaryKey"`
	Kind             string     `gorm:"column:kind;type:workflowrecipekind;not null"`
	CurrentVersionID *string    `gorm:"column:current_version_id;type:varchar(36)"`
	ArchivedAt       *time.Time `gorm:"column:archived_at;type:timestamptz"`
	CreatedAt        time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	Origin           string     `gorm:"column:origin;type:workflowrecipeorigin;not null"`
	OfficialKey      *string    `gorm:"column:official_key;type:varchar(80)"`
}

func (WorkflowRecipes) TableName() string { return "workflow_recipes" }

// AllModels 返回 migrate CreateTable/AddColumn 要注册的全部 GORM 模型，顺序即建表顺序。
func AllModels() []any {
	return []any{
		&Users{},
		&Merchants{},
		&Memberships{},
		&MerchantInvites{},
		&AuthSessions{},
		&AgentConversations{},
		&AgentPageContextSnapshots{},
		&AgentSessions{},
		&AgentTasks{},
		&AgentToolMutations{},
		&AgentTurnCheckpoints{},
		&AgentModelInvocations{},
		&AgentTurnEffectReconciliations{},
		&AgentTurnEvents{},
		&AgentTurnExecutions{},
		&AgentTurnProjections{},
		&AgentWorkflowRunRequests{},
		&AppSettings{},
		&AsyncDispatches{},
		&DeliveryRenditionJobs{},
		&DeliveryAdoptionVersions{},
		&DeliveryAdoptionSlots{},
		&ImageSessionAssets{},
		&ImageSessionGenerationTasks{},
		&ImageSessionProviderEffects{},
		&ImageSessionRounds{},
		&ImageSessions{},
		&LibraryOrganizationDraftRevisions{},
		&LibraryOrganizationDrafts{},
		&LocalImageEditAdoptionEvents{},
		&LocalImageEditProviderAttempts{},
		&LocalImageEditTaskReferences{},
		&LocalImageEditTasks{},
		&MediaLibraryAssetTags{},
		&MediaLibraryAssets{},
		&MediaLibraryCollectionKeys{},
		&MediaLibraryFolders{},
		&MediaLibraryTags{},
		&MediaLibraryUploadKeys{},
		&MediaObjects{},
		&ProductAssetFolders{},
		&ProductFactSetVersions{},
		&ProductImageAssets{},
		&Products{},
		&ProviderBindings{},
		&ProviderProfiles{},
		&VisualSystemVersionReferences{},
		&VisualSystemVersions{},
		&VisualSystems{},
		&WorkflowGraphArtifacts{},
		&WorkflowGraphEdges{},
		&WorkflowGraphGroups{},
		&WorkflowGraphNodeRuns{},
		&WorkflowGraphRunEvents{},
		&WorkflowGraphNodes{},
		&WorkflowGraphProposals{},
		&WorkflowGraphProviderEffects{},
		&WorkflowGraphRuns{},
		&WorkflowGraphs{},
		&WorkflowMediaLibraryAssets{},
		&WorkflowOperationGroups{},
		&WorkflowRecipeApplications{},
		&WorkflowRecipeVersions{},
		&WorkflowRecipes{},
	}
}
