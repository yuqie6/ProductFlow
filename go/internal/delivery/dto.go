package delivery

import (
	"time"

	"github.com/yuqie6/productflow/internal/product"
)

// JobResponse 是一次交付派生任务的 HTTP 投影。
// Status 为 queued/running/succeeded/failed/unknown。ResultAsset 仅成功后有。
// 这是本地渲染任务，不是再跑一遍图像模型，也不是 GraphRun。
type JobResponse struct {
	ID            string                 `json:"id"`
	ProductID     string                 `json:"product_id"`
	SourceAssetID string                 `json:"source_asset_id"`
	ResultAsset   *product.AssetResponse `json:"result_asset"`  // 成功才有；nil 表示尚未出交付图
	DeliverySpec  Spec                   `json:"delivery_spec"` // 确定性渲染合同，改它不调图像模型
	Status        string                 `json:"status"`
	Attempts      int                    `json:"attempts"`       // 已占用的执行次数
	IsRetryable   bool                   `json:"is_retryable"`   // false 时 Retry 拒绝
	FailureReason *string                `json:"failure_reason"` // nil 表示未失败；交付失败不标 unknown
	CreatedAt     time.Time              `json:"created_at"`
	StartedAt     *time.Time             `json:"started_at"`
	FinishedAt    *time.Time             `json:"finished_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// JobListResponse 是同一原图（ProductImageAsset）上交付任务的 HTTP 列表。
type JobListResponse struct {
	Items []JobResponse `json:"items"` // 同一原图上的交付任务；空列表是 [] 不是 null
}

// ExportRequest 指定要打包的交付任务；AllowPartial 允许跳过尚未成功的项。
type ExportRequest struct {
	RenditionJobIDs []string `json:"rendition_job_ids"` // 要打包的交付任务 id
	AllowPartial    bool     `json:"allow_partial"`     // true 时跳过尚未成功的项，不整包失败
}

type jobRow struct {
	ID                string
	ProductID         string
	SourceAssetID     string
	ResultAssetID     *string
	SpecSchemaVersion int
	SpecJSON          []byte
	SpecHash          string
	Status            string
	Attempts          int
	IsRetryable       bool
	FailureReason     *string
	CreatedAt         time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
	UpdatedAt         time.Time
	ActiveAttempt     *string
}
