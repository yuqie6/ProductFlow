package delivery

import (
	"time"

	"github.com/yuqie6/productflow/internal/product"
)

type JobResponse struct {
	ID            string                 `json:"id"`
	ProductID     string                 `json:"product_id"`
	SourceAssetID string                 `json:"source_asset_id"`
	ResultAsset   *product.AssetResponse `json:"result_asset"`
	DeliverySpec  Spec                   `json:"delivery_spec"`
	Status        string                 `json:"status"`
	Attempts      int                    `json:"attempts"`
	IsRetryable   bool                   `json:"is_retryable"`
	FailureReason *string                `json:"failure_reason"`
	CreatedAt     time.Time              `json:"created_at"`
	StartedAt     *time.Time             `json:"started_at"`
	FinishedAt    *time.Time             `json:"finished_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type JobListResponse struct {
	Items []JobResponse `json:"items"`
}

type ExportRequest struct {
	RenditionJobIDs []string `json:"rendition_job_ids"`
	AllowPartial    bool     `json:"allow_partial"`
}

type jobRow struct {
	ID            string
	ProductID     string
	SourceAssetID string
	ResultAssetID *string
	SpecJSON      []byte
	SpecHash      string
	Status        string
	Attempts      int
	IsRetryable   bool
	FailureReason *string
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time
	ActiveAttempt *string
}
