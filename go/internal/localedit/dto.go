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

type CapabilityResponse struct {
	ProviderName       string   `json:"provider_name"`
	Supported          bool     `json:"supported"`
	Mode               *string  `json:"mode"`
	Operations         []string `json:"operations"`
	RequiresMask       bool     `json:"requires_mask"`
	MaxReferenceImages int      `json:"max_reference_images"`
	Reason             *string  `json:"reason"`
}

type MaskGeometry struct {
	SourceWidth        int        `json:"source_width"`
	SourceHeight       int        `json:"source_height"`
	ViewportWidth      float64    `json:"viewport_width"`
	ViewportHeight     float64    `json:"viewport_height"`
	ViewportToSource   [6]float64 `json:"viewport_to_source"`
	TransformDirection string     `json:"transform_direction"`
}

type Draft struct {
	Operation         string
	Instruction       *string
	SourceText        *string
	ReplacementText   *string
	MaskGeometry      MaskGeometry
	ReferenceAssetIDs []string
}

type AttemptResponse struct {
	ID                 string                 `json:"id"`
	AttemptID          string                 `json:"attempt_id"`
	AttemptNumber      int                    `json:"attempt_number"`
	OperationKey       string                 `json:"operation_key"`
	Phase              string                 `json:"phase"`
	EffectResult       string                 `json:"effect_result"`
	ProviderName       string                 `json:"provider_name"`
	ProviderModel      *string                `json:"provider_model"`
	ProviderResponseID *string                `json:"provider_response_id"`
	ProviderStatus     *string                `json:"provider_status"`
	LateResultAsset    *product.AssetResponse `json:"late_result_asset"`
	Detail             *string                `json:"detail"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

type AdoptionEventResponse struct {
	ID             string    `json:"id"`
	TaskID         string    `json:"task_id"`
	GraphID        string    `json:"graph_id"`
	NodeID         string    `json:"node_id"`
	EventType      string    `json:"event_type"`
	FromArtifactID string    `json:"from_artifact_id"`
	ToArtifactID   string    `json:"to_artifact_id"`
	RelatedEventID *string   `json:"related_event_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type TaskResponse struct {
	ID                        string                  `json:"id"`
	ProductID                 string                  `json:"product_id"`
	Status                    string                  `json:"status"`
	Revision                  int                     `json:"revision"`
	Operation                 string                  `json:"operation"`
	Instruction               *string                 `json:"instruction"`
	SourceText                *string                 `json:"source_text"`
	ReplacementText           *string                 `json:"replacement_text"`
	MaskGeometry              MaskGeometry            `json:"mask_geometry"`
	SourceMediaSHA256         string                  `json:"source_media_sha256"`
	SourceAsset               product.AssetResponse   `json:"source_asset"`
	ResultAsset               *product.AssetResponse  `json:"result_asset"`
	References                []product.AssetResponse `json:"references"`
	ReferenceAssetIDs         []string                `json:"reference_asset_ids"`
	TargetGraphID             *string                 `json:"target_graph_id"`
	TargetNodeID              *string                 `json:"target_node_id"`
	TargetGraphRevision       *int                    `json:"target_graph_revision"`
	SourceArtifactID          *string                 `json:"source_artifact_id"`
	SourceArtifactAssetID     *string                 `json:"source_artifact_asset_id"`
	SourceArtifactInputDigest *string                 `json:"source_artifact_input_digest"`
	IdempotencyKey            *string                 `json:"idempotency_key"`
	RequestHash               *string                 `json:"request_hash"`
	RequestedProviderName     *string                 `json:"requested_provider_name"`
	RequestedLocalEditMode    *string                 `json:"requested_local_edit_mode"`
	Attempts                  int                     `json:"attempts"`
	ActiveAttemptID           *string                 `json:"active_attempt_id"`
	ProgressPhase             *string                 `json:"progress_phase"`
	FailureReason             *string                 `json:"failure_reason"`
	IsRetryable               bool                    `json:"is_retryable"`
	IsCancelable              bool                    `json:"is_cancelable"`
	ProviderName              *string                 `json:"provider_name"`
	ProviderModel             *string                 `json:"provider_model"`
	ProviderResponseID        *string                 `json:"provider_response_id"`
	ProviderStatus            *string                 `json:"provider_status"`
	ProviderAttempts          []AttemptResponse       `json:"provider_attempts"`
	AdoptionEvents            []AdoptionEventResponse `json:"adoption_events"`
	CreatedAt                 time.Time               `json:"created_at"`
	UpdatedAt                 time.Time               `json:"updated_at"`
	QueuedAt                  *time.Time              `json:"queued_at"`
	StartedAt                 *time.Time              `json:"started_at"`
	FinishedAt                *time.Time              `json:"finished_at"`
}

type TaskListResponse struct {
	Items []TaskResponse `json:"items"`
}

type SubmitRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
}

type RevisionRequest struct {
	ExpectedRevision *int `json:"expected_revision"`
}

type AdoptRequest struct {
	ExpectedCurrentArtifactID string `json:"expected_current_artifact_id"`
}

type Capability struct {
	ProviderName       string
	Supported          bool
	Mode               string
	Operations         []string
	RequiresMask       bool
	MaxReferenceImages int
	Reason             string
}

type Provider interface {
	Capability() Capability
	Edit(ctx context.Context, req EditRequest) (EditResult, error)
}

type EditRequest struct {
	SourceBytes    []byte
	SourceMIME     string
	MaskPNG        []byte
	ReferenceBytes [][]byte
	Instruction    string
	Operation      string
	Size           string
}

type EditResult struct {
	Bytes          []byte
	MIME           string
	Model          string
	ResponseID     string
	ProviderStatus string
}

type MockProvider struct {
	Cap Capability
	Err error
	PNG []byte
}

func (m MockProvider) Capability() Capability {
	if m.Cap.ProviderName != "" {
		return m.Cap
	}
	return UnsupportedCapability("mock")
}

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

func UnsupportedCapability(name string) Capability {
	return Capability{
		ProviderName: name, Supported: false, MaxReferenceImages: 0,
		Reason: "图片 provider 未显式声明 masked local edit 能力",
	}
}

func SupportedCapability(name string) Capability {
	mode := modeMasked
	return Capability{
		ProviderName: name, Supported: true, Mode: mode,
		Operations:   []string{opRemove, opReplaceText, opInpaint},
		RequiresMask: true, MaxReferenceImages: maxReferences,
	}
}

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
