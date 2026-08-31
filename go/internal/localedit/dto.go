package localedit

import (
	"context"
	"time"

	"github.com/yuqie6/productflow/internal/product"
)

const (
	modeMasked    = "masked_edit"
	maxReferences = 6
	staleAfter    = 10 * time.Minute
	maskFilename  = "local-edit-mask.png"
	opRemove      = "remove"
	opReplaceText = "replace_text"
	opInpaint     = "inpaint"
	unknownDetail = "局部编辑供应商请求结果未知，系统未自动重试。"
	cancelReason  = "用户取消局部编辑任务"
)

// CapabilityResponse 是局部编辑供应商能力的 HTTP 投影。
// Supported=false 时 Mode 为 nil、Reason 有文案；Supported=true 时相反。
// 不要和 settings 里档案 capabilities 字符串列表搞混——这是当前绑定解析后的结论。
type CapabilityResponse struct {
	ProviderName       string   `json:"provider_name"`        // 当前 image 绑定解析后的供应商标识
	Supported          bool     `json:"supported"`            // false 时 Mode 为 nil、Reason 有文案
	Mode               *string  `json:"mode"`                 // Supported=false 时为 nil；true 时为 masked_edit
	Operations         []string `json:"operations"`           // remove | replace_text | inpaint；不支持时为 []
	RequiresMask       bool     `json:"requires_mask"`        // 当前支持面必须为 true
	MaxReferenceImages int      `json:"max_reference_images"` // 不支持时为 0
	Reason             *string  `json:"reason"`               // Supported=true 时为 nil
}

// MaskGeometry 记录画布视口到源图像素的仿射变换，供 mask 对齐。
type MaskGeometry struct {
	SourceWidth        int        `json:"source_width"`        // 源图像素宽
	SourceHeight       int        `json:"source_height"`       // 源图像素高
	ViewportWidth      float64    `json:"viewport_width"`      // 画布视口宽，不是源图像素
	ViewportHeight     float64    `json:"viewport_height"`     // 画布视口高，不是源图像素
	ViewportToSource   [6]float64 `json:"viewport_to_source"`  // 视口→源图仿射矩阵 6 元
	TransformDirection string     `json:"transform_direction"` // 仅 viewport_to_source；空则按该值
}

// Draft 是局部编辑草稿参数，内部入参，不含 mask 字节。
// Operation 闭集：remove / replace_text / inpaint。mask 由 Create/Update 另传 PNG。
type Draft struct {
	Operation         string       // remove | replace_text | inpaint
	Instruction       *string      // nil 表示没有自然语言指令
	SourceText        *string      // replace_text 原字；其它操作为 nil
	ReplacementText   *string      // replace_text 新字；其它操作为 nil
	MaskGeometry      MaskGeometry // 视口到源图像素的对齐；mask PNG 另传
	ReferenceAssetIDs []string     // 商品图 id，最多 6 张
}

// AttemptResponse 是一次供应商尝试账本的 HTTP 投影。
// EffectResult 为 unknown 时不自动当失败重试。LateResultAsset 是取消后迟到的审计图，不是当前结果。
type AttemptResponse struct {
	ID                 string                 `json:"id"`
	AttemptID          string                 `json:"attempt_id"`
	AttemptNumber      int                    `json:"attempt_number"` // 从 1 起
	OperationKey       string                 `json:"operation_key"`  // 幂等操作键
	Phase              string                 `json:"phase"`          // claimed | provider_* | 终态阶段
	EffectResult       string                 `json:"effect_result"`  // pending | applied | failed | unknown
	ProviderName       string                 `json:"provider_name"`  // 实际打网的供应商标识
	ProviderModel      *string                `json:"provider_model"` // nil 表示尚未调用或供应商未回
	ProviderResponseID *string                `json:"provider_response_id"`
	ProviderStatus     *string                `json:"provider_status"`   // nil 表示供应商未回状态
	LateResultAsset    *product.AssetResponse `json:"late_result_asset"` // 取消后迟到的审计图；nil 表示没有
	Detail             *string                `json:"detail"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

// AdoptionEventResponse 是一次 adopt 或 revert 的 HTTP 事件。
// EventType 区分 adopt/revert；未出现在本列表的结果还没写进节点当前 artifact。
type AdoptionEventResponse struct {
	ID             string    `json:"id"`
	TaskID         string    `json:"task_id"`
	GraphID        string    `json:"graph_id"`
	NodeID         string    `json:"node_id"`
	EventType      string    `json:"event_type"` // adopt | revert
	FromArtifactID string    `json:"from_artifact_id"`
	ToArtifactID   string    `json:"to_artifact_id"`
	RelatedEventID *string   `json:"related_event_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// TaskResponse 是局部编辑任务的 HTTP 投影。
// Status 为 draft/queued/running/succeeded/failed/unknown/cancelled。
// ResultAsset 在 succeeded 后才有；adopt 之前节点绑定仍是源图。不要当 GraphRun 节点投影。
type TaskResponse struct {
	ID                        string                  `json:"id"`
	ProductID                 string                  `json:"product_id"`
	Status                    string                  `json:"status"`
	Revision                  int                     `json:"revision"`            // 乐观并发；Update/Submit 对不上则 Conflict
	Operation                 string                  `json:"operation"`           // remove | replace_text | inpaint
	Instruction               *string                 `json:"instruction"`         // nil 表示没有自然语言指令
	SourceText                *string                 `json:"source_text"`         // replace_text 原字；其它为 nil
	ReplacementText           *string                 `json:"replacement_text"`    // replace_text 新字；其它为 nil
	MaskGeometry              MaskGeometry            `json:"mask_geometry"`       // 视口到源图像素的对齐
	SourceMediaSHA256         string                  `json:"source_media_sha256"` // 提交时源图指纹，adopt 时用来发现源已变
	SourceAsset               product.AssetResponse   `json:"source_asset"`        // 编辑前的商品图
	ResultAsset               *product.AssetResponse  `json:"result_asset"`        // succeeded 才有；adopt 前节点仍绑源图
	References                []product.AssetResponse `json:"references"`          // 参考商品图投影
	ReferenceAssetIDs         []string                `json:"reference_asset_ids"` // 与 References 对应的商品图 id
	TargetGraphID             *string                 `json:"target_graph_id"`
	TargetNodeID              *string                 `json:"target_node_id"`
	TargetGraphRevision       *int                    `json:"target_graph_revision"` // nil 表示未绑画布节点
	SourceArtifactID          *string                 `json:"source_artifact_id"`
	SourceArtifactAssetID     *string                 `json:"source_artifact_asset_id"`
	SourceArtifactInputDigest *string                 `json:"source_artifact_input_digest"` // nil 表示无节点 artifact 上下文
	IdempotencyKey            *string                 `json:"idempotency_key"`              // 草稿未 submit 时为 nil
	RequestHash               *string                 `json:"request_hash"`                 // 未 submit 时为 nil
	RequestedProviderName     *string                 `json:"requested_provider_name"`      // nil 表示 submit 时再解析
	RequestedLocalEditMode    *string                 `json:"requested_local_edit_mode"`    // nil 或 masked_edit
	Attempts                  int                     `json:"attempts"`                     // 已占用的执行次数
	ActiveAttemptID           *string                 `json:"active_attempt_id"`
	ProgressPhase             *string                 `json:"progress_phase"` // nil 表示尚未进入 worker 阶段
	FailureReason             *string                 `json:"failure_reason"` // nil 表示未失败；unknown 不可 Retry
	IsRetryable               bool                    `json:"is_retryable"`   // unknown 时为 false
	IsCancelable              bool                    `json:"is_cancelable"`  // queued 或 running 才为 true
	ProviderName              *string                 `json:"provider_name"`  // nil 表示尚未调用
	ProviderModel             *string                 `json:"provider_model"` // nil 表示尚未调用或供应商未回
	ProviderResponseID        *string                 `json:"provider_response_id"`
	ProviderStatus            *string                 `json:"provider_status"`   // nil 表示供应商未回状态
	ProviderAttempts          []AttemptResponse       `json:"provider_attempts"` // 列表项默认不含完整 audit
	AdoptionEvents            []AdoptionEventResponse `json:"adoption_events"`   // 已写入节点的 adopt/revert
	CreatedAt                 time.Time               `json:"created_at"`
	UpdatedAt                 time.Time               `json:"updated_at"`
	QueuedAt                  *time.Time              `json:"queued_at"`
	StartedAt                 *time.Time              `json:"started_at"`
	FinishedAt                *time.Time              `json:"finished_at"`
}

// TaskListResponse 是按商品列出局部编辑任务的 HTTP 体。列表项不含完整 audit。
type TaskListResponse struct {
	Items []TaskResponse `json:"items"` // 列表项不含完整 audit
}

// SubmitRequest 是 POST .../submit 的 JSON 体，只带幂等键。
// 草稿尚未 submit 时不要写 dispatch。键空则 Validation。
type SubmitRequest struct {
	IdempotencyKey string `json:"idempotency_key"` // 空则 Validation；草稿尚未 submit 时不要写 dispatch
}

// RevisionRequest 用 expected_revision 做乐观并发。
type RevisionRequest struct {
	ExpectedRevision *int `json:"expected_revision"` // nil 表示不校验 revision；有值对不上则 Conflict
}

// AdoptRequest 要求当前节点 artifact 仍是预期值，避免覆盖他人写入。
type AdoptRequest struct {
	ExpectedCurrentArtifactID string `json:"expected_current_artifact_id"`
}

// Capability 是供应商声明的局部编辑能力，内部类型；HTTP 用 [Capability.Response]。
// Mode 空且 Supported=false 表示未声明 masked local edit。不要和 ProviderProfile.Capabilities 混用。
type Capability struct {
	ProviderName       string   // 供应商标识，如 openai-responses
	Supported          bool     // false 表示未声明 masked local edit，Edit 应拒绝且不打网
	Mode               string   // 空且 Supported=false 表示未声明 masked local edit
	Operations         []string // remove | replace_text | inpaint
	RequiresMask       bool     // 当前支持面必须为 true
	MaxReferenceImages int      // 当前支持面为 6
	Reason             string   // Supported=true 时为空
}

// Provider 执行带 mask 的局部编辑。worker 调用；未声明能力时应拒绝且不打网。
type Provider interface {
	// Capability 声明当前供应商是否支持 masked local edit。
	Capability() Capability
	// Edit 执行一次局部编辑；未声明能力时应拒绝且不打网。
	Edit(ctx context.Context, req EditRequest) (EditResult, error)
}

// EditRequest 是一次局部编辑供应商调用的内部入参，不是 HTTP multipart。
// MaskPNG 必须是已对齐源图像素的 PNG。Operation 与 Capability.Operations 闭集一致。
type EditRequest struct {
	SourceBytes    []byte   // 源图像素
	SourceMIME     string   // 源图 MIME
	MaskPNG        []byte   // 已对齐源图像素的 PNG
	ReferenceBytes [][]byte // 参考图 bytes，不是节点绑定
	Instruction    string   // 自然语言指令；replace_text 可配合 Source/Replacement
	Operation      string   // remove | replace_text | inpaint
	Size           string   // 输出尺寸提示，宽x高
}

// EditResult 是局部编辑供应商返回的内部图片，不是商品图身份。
// 落盘与 ProductImageAsset 由 Executor 负责。不可证明失败应返回 error。
type EditResult struct {
	Bytes          []byte // 编辑后的图，不是商品图身份
	MIME           string
	Model          string // 供应商实际模型名
	ResponseID     string
	ProviderStatus string // 常见 completed
}

// MockProvider 实现 Provider，不打网；未注入 Cap 时 Capability 为 unsupported。
type MockProvider struct {
	Cap Capability // 空时 Capability() 返回 unsupported
	Err error      // 非 nil 时 Edit 直接返回，不打网
	PNG []byte     // 空时回显源图
}

// Capability 实现 Provider；空 Cap 时返回 UnsupportedCapability("mock")。
func (m MockProvider) Capability() Capability {
	if m.Cap.ProviderName != "" {
		return m.Cap
	}
	return UnsupportedCapability("mock")
}

// Edit 实现 Provider，不打网；默认回显源图或返回注入的 PNG。
func (m MockProvider) Edit(ctx context.Context, req EditRequest) (EditResult, error) {
	_ = ctx
	if m.Err != nil {
		return EditResult{}, m.Err
	}
	png := m.PNG
	if len(png) == 0 {
		png = req.SourceBytes
	}
	return EditResult{Bytes: png, MIME: "image/png", Model: "mock-local-edit", ProviderStatus: "completed"}, nil
}

// UnsupportedCapability 声明该供应商未支持 masked local edit。
func UnsupportedCapability(name string) Capability {
	return Capability{
		ProviderName: name, Supported: false, MaxReferenceImages: 0,
		Reason: "图片 provider 未显式声明 masked local edit 能力",
	}
}

// SupportedCapability 声明该供应商支持 remove / replace_text / inpaint，且必须提供 mask。
func SupportedCapability(name string) Capability {
	mode := modeMasked
	return Capability{
		ProviderName: name, Supported: true, Mode: mode,
		Operations:   []string{opRemove, opReplaceText, opInpaint},
		RequiresMask: true, MaxReferenceImages: maxReferences,
	}
}

// Response 把内部 Capability 编成 HTTP 投影。
// Supported=false 时 Mode=nil 且写 Reason；切片 nil 收成 []。调用时机：HTTP GET capability。
func (c Capability) Response() CapabilityResponse {
	out := CapabilityResponse{
		ProviderName: c.ProviderName, Supported: c.Supported,
		RequiresMask: c.RequiresMask, MaxReferenceImages: c.MaxReferenceImages,
		Operations: c.Operations,
	}
	if out.Operations == nil {
		out.Operations = []string{}
	}
	if c.Supported {
		mode := c.Mode
		out.Mode = &mode
	} else {
		reason := c.Reason
		out.Reason = &reason
	}
	return out
}
