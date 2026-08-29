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
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Category               *string   `json:"category"`
	Price                  *string   `json:"price"`
	CoverImageAssetID      *string   `json:"cover_image_asset_id"`
	CoverImageFilename     *string   `json:"cover_image_filename"`
	CoverImageDownloadURL  *string   `json:"cover_image_download_url"`
	CoverImagePreviewURL   *string   `json:"cover_image_preview_url"`
	CoverImageThumbnailURL *string   `json:"cover_image_thumbnail_url"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
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

// Fact 是商品资料里的一条不可变事实。
type Fact struct {
	Key                  string `json:"key"`
	Value                any    `json:"value"`
	SourceType           string `json:"source_type"`
	Status               string `json:"status"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	EvidenceAssetIDs     []any  `json:"evidence_asset_ids"`
	Conflicts            []any  `json:"conflicts"`
}

type FactSet struct {
	ID        string    `json:"id"`
	ProductID string    `json:"product_id"`
	Version   int       `json:"version"`
	Facts     []Fact    `json:"facts"`
	CreatedAt time.Time `json:"created_at"`
}

type FactsResponse struct {
	Product                 Detail   `json:"product"`
	CurrentFactSetVersionID *string  `json:"current_fact_set_version_id"`
	CurrentFactVersion      *int     `json:"current_fact_version"`
	FactSet                 *FactSet `json:"fact_set"`
	Facts                   []Fact   `json:"facts"`
}

type AssetListResponse struct {
	Items []AssetResponse `json:"items"`
}

type GalleryBootstrap struct {
	ProductID         string                   `json:"product_id"`
	CoverImageAssetID *string                  `json:"cover_image_asset_id"`
	SystemDirectories []GallerySystemDirectory `json:"system_directories"`
	ImageTypes        []GalleryImageType       `json:"image_types"`
	Origins           []GalleryOrigin          `json:"origins"`
	UserFolders       []GalleryFolder          `json:"user_folders"`
	UnorganizedCount  int                      `json:"unorganized_count"`
}

type GallerySystemDirectory struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type GalleryImageType struct {
	DirectoryKey string  `json:"directory_key"`
	ImageTypeKey *string `json:"image_type_key"`
	Title        string  `json:"title"`
	Count        int     `json:"count"`
}

type GalleryOrigin struct {
	OriginType string `json:"origin_type"`
	Count      int    `json:"count"`
}

type GalleryFolder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Count     int    `json:"count"`
}

type GalleryFolderMutation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

type DeleteGalleryFolderResponse struct {
	FolderID                string `json:"folder_id"`
	MovedToUnorganizedCount int    `json:"moved_to_unorganized_count"`
}

type GalleryAssetResponse struct {
	AssetResponse
	UserFolderName *string                   `json:"user_folder_name"`
	ImageTypeTitle *string                   `json:"image_type_title"`
	Generation     *GalleryGenerationSummary `json:"generation"`
	Rendition      *GalleryRenditionSummary  `json:"rendition"`
}

type GalleryGenerationSummary struct {
	WorkflowID              string  `json:"workflow_id"`
	NodeID                  string  `json:"node_id"`
	NodeRunID               string  `json:"node_run_id"`
	PromptArtifactVersionID *string `json:"prompt_artifact_version_id"`
	VisualSystemVersionID   *string `json:"visual_system_version_id"`
}

type GalleryRenditionSummary struct {
	JobID         string         `json:"job_id"`
	SourceAssetID string         `json:"source_asset_id"`
	DeliverySpec  map[string]any `json:"delivery_spec"`
	Status        string         `json:"status"`
}

type GalleryAssetPage struct {
	Items      []GalleryAssetResponse `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}
