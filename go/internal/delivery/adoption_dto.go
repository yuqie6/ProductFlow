package delivery

import "time"

// AdoptionSlotInput 是创建交付采用版本时的一个图位。
// QualityStatus 闭集 pass|fail|unchecked；fail 不得进入已采用交付合格集。
type AdoptionSlotInput struct {
	SlotKey       string         `json:"slot_key"`
	SortOrder     int            `json:"sort_order"`
	ImageTypeKey  *string        `json:"image_type_key"`
	SourceAssetID string         `json:"source_asset_id"`
	SourceNodeID  *string        `json:"source_node_id"`
	DeliverySpec  map[string]any `json:"delivery_spec"`
	QualityStatus string         `json:"quality_status"`
	QualityDetail *string        `json:"quality_detail"`
	TextOverflow  bool           `json:"text_overflow"`
}

// CreateAdoptionRequest 显式创建新的不可变交付采用版本；不修改历史版本行。
type CreateAdoptionRequest struct {
	AcknowledgeQualityWarnings bool                `json:"acknowledge_quality_warnings"`
	Slots                      []AdoptionSlotInput `json:"slots"`
	GraphID                    *string             `json:"graph_id"`
	GraphRevision              *int                `json:"graph_revision"`
	FactSetVersionID           *string             `json:"fact_set_version_id"`
	VisualSystemVersionID      *string             `json:"visual_system_version_id"`
	Notes                      *string             `json:"notes"`
}

// AdoptionSlotResponse 是已持久化图位投影。
type AdoptionSlotResponse struct {
	ID               string  `json:"id"`
	SlotKey          string  `json:"slot_key"`
	SortOrder        int     `json:"sort_order"`
	ImageTypeKey     *string `json:"image_type_key"`
	SourceAssetID    string  `json:"source_asset_id"`
	SourceNodeID     *string `json:"source_node_id"`
	DeliverySpec     Spec    `json:"delivery_spec"`
	DeliverySpecHash string  `json:"delivery_spec_hash"`
	QualityStatus    string  `json:"quality_status"`
	QualityDetail    *string `json:"quality_detail"`
	TextOverflow     bool    `json:"text_overflow"`
	Qualified        bool    `json:"qualified"` // IQ-CF-08：仅 pass 计入合格集
}

// AdoptionVersionResponse 是一次交付采用版本的 HTTP 投影。
type AdoptionVersionResponse struct {
	ID                    string                 `json:"id"`
	ProductID             string                 `json:"product_id"`
	Version               int                    `json:"version"`
	IsCurrent             bool                   `json:"is_current"`
	GraphID               *string                `json:"graph_id"`
	GraphRevision         *int                   `json:"graph_revision"`
	FactSetVersionID      *string                `json:"fact_set_version_id"`
	VisualSystemVersionID *string                `json:"visual_system_version_id"`
	Notes                 *string                `json:"notes"`
	Slots                 []AdoptionSlotResponse `json:"slots"`
	CreatedAt             time.Time              `json:"created_at"`
}

// AdoptionVersionSummary 是列表项，不含完整图位明细。
type AdoptionVersionSummary struct {
	ID        string    `json:"id"`
	ProductID string    `json:"product_id"`
	Version   int       `json:"version"`
	IsCurrent bool      `json:"is_current"`
	SlotCount int       `json:"slot_count"`
	CreatedAt time.Time `json:"created_at"`
}

// AdoptionListResponse 列出商品的交付采用版本，新版本在前。
type AdoptionListResponse struct {
	CurrentVersionID *string                  `json:"current_version_id"`
	Items            []AdoptionVersionSummary `json:"items"`
}

// AdoptionIssue 是导出/预览前可定位的问题。
type AdoptionIssue struct {
	Code    string  `json:"code"` // missing_asset|duplicate_slot|unsupported_format|invalid_crop|rendition_pending|rendition_failed
	SlotKey string  `json:"slot_key"`
	Message string  `json:"message"`
	JobID   *string `json:"job_id,omitempty"`
}

// AdoptionPreviewItem 描述预览与导出将使用的同一文件计划。
type AdoptionPreviewItem struct {
	SlotKey          string  `json:"slot_key"`
	SortOrder        int     `json:"sort_order"`
	Filename         string  `json:"filename"`
	SourceAssetID    string  `json:"source_asset_id"`
	DeliverySpecHash string  `json:"delivery_spec_hash"`
	Qualified        bool    `json:"qualified"`
	RenditionJobID   *string `json:"rendition_job_id"`
	RenditionStatus  *string `json:"rendition_status"`
	ResultAssetID    *string `json:"result_asset_id"`
}

// AdoptionPreviewResponse 与导出使用同一快照与文件命名；issues 非空时导出可能被拒绝。
type AdoptionPreviewResponse struct {
	VersionID    string                `json:"version_id"`
	ProductID    string                `json:"product_id"`
	Complete     bool                  `json:"complete"`
	Issues       []AdoptionIssue       `json:"issues"`
	Items        []AdoptionPreviewItem `json:"items"`
	ExportReady  bool                  `json:"export_ready"`
	AllowPartial bool                  `json:"allow_partial"`
}

// AdoptionExportRequest 控制是否允许跳过未就绪图位。
type AdoptionExportRequest struct {
	AllowPartial  bool `json:"allow_partial"`
	QualifiedOnly bool `json:"qualified_only"` // true 时只导出 quality_status=pass
}

// AdoptionRenditionsResponse 是确保派生任务后的预览投影。
type AdoptionRenditionsResponse struct {
	Preview AdoptionPreviewResponse `json:"preview"`
}
