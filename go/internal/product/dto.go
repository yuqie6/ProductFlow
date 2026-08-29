package product

import (
	"encoding/json"
	"time"
)

type Product struct {
	ID                string
	Name              string
	Category          *string
	Price             *string
	SourceNote        *string
	CoverImageAssetID *string
	IntakeVersion     *int
	IntakeJSON        json.RawMessage
	FactSetVersionID  *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ImageAsset struct {
	ID                      string
	ProductID               string
	MediaObjectID           string
	OriginType              string
	DisplayName             string
	OriginalFilename        string
	ImageTypeKey            *string
	UserFolderID            *string
	ParentAssetID           *string
	SourceImageSessionAsset *string
	SourceLibraryAsset      *string
	MIMEType                string
	ByteSize                *int
	Width                   *int
	Height                  *int
	VerificationStatus      string
	StoragePath             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type Summary struct {
	ID                     string
	Name                   string
	Category               *string
	Price                  *string
	CoverImageAssetID      *string
	CoverImageFilename     *string
	CoverImageDownloadURL  *string
	CoverImagePreviewURL   *string
	CoverImageThumbnailURL *string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type Detail struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Category          *string         `json:"category"`
	Price             *string         `json:"price"`
	SourceNote        *string         `json:"source_note"`
	CoverImageAssetID *string         `json:"cover_image_asset_id"`
	Intake            json.RawMessage `json:"intake"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type AssetResponse struct {
	ID                        string    `json:"id"`
	ProductID                 string    `json:"product_id"`
	MediaObjectID             string    `json:"media_object_id"`
	OriginType                string    `json:"origin_type"`
	DisplayName               string    `json:"display_name"`
	OriginalFilename          string    `json:"original_filename"`
	ImageTypeKey              *string   `json:"image_type_key"`
	UserFolderID              *string   `json:"user_folder_id"`
	ParentAssetID             *string   `json:"parent_asset_id"`
	SourceImageSessionAssetID *string   `json:"source_image_session_asset_id"`
	SourceLibraryAssetID      *string   `json:"source_library_asset_id"`
	MIMEType                  string    `json:"mime_type"`
	ByteSize                  *int      `json:"byte_size"`
	Width                     *int      `json:"width"`
	Height                    *int      `json:"height"`
	VerificationStatus        string    `json:"verification_status"`
	DownloadURL               string    `json:"download_url"`
	PreviewURL                string    `json:"preview_url"`
	ThumbnailURL              string    `json:"thumbnail_url"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type CreateResponse struct {
	Product       Detail          `json:"product"`
	CreatedAssets []AssetResponse `json:"created_assets"`
}

type ListResponse struct {
	Items    []Summary `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

type Conversation struct {
	ID           string    `json:"id"`
	ScopeType    string    `json:"scope_type"`
	SessionID    *string   `json:"session_id"`
	ProductID    *string   `json:"product_id"`
	HarnessRunID string    `json:"harness_run_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type WorkspaceCreateResponse struct {
	TaskID        *string         `json:"task_id"`
	Product       Detail          `json:"product"`
	CreatedAssets []AssetResponse `json:"created_assets"`
	Conversation  Conversation    `json:"conversation"`
}

type WorkspaceSnapshotResponse struct {
	TaskID          *string         `json:"task_id"`
	Created         bool            `json:"created"`
	IntakeFinalized bool            `json:"intake_finalized"`
	Product         Detail          `json:"product"`
	CreatedAssets   []AssetResponse `json:"created_assets"`
	Conversation    Conversation    `json:"conversation"`
}

type DirectCreateResponse struct {
	Product       Detail          `json:"product"`
	CreatedAssets []AssetResponse `json:"created_assets"`
	Graph         map[string]any  `json:"graph"`
}

type Upload struct {
	Content  []byte
	Filename string
	MIMEType string
}
