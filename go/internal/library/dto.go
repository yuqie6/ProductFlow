package library

import (
	"time"

	"github.com/yuqie6/productflow/internal/media"
)

const (
	SourceSession = "image_session_generated"
	SourceProduct = "product_asset"
	SourceUpload  = "direct_upload"

	maxFolderName   = 120
	maxTagName      = 80
	maxOrgAssets    = 100
	maxOrgBytes     = 256 * 1024
	maxCollect      = 100
	maxCollectBytes = 256 * 1024
	maxFilename     = 255
	maxWorkflow     = 100
	maxProvenance   = 32 * 1024
	maxIdempotency  = 200
)

// Asset 是全局素材身份加上媒体核验投影，不含 provenance 正文。
type Asset struct {
	ID                     string
	MediaObjectID          string
	SourceType             string
	SourceID               string
	DisplayName            string
	FolderID               *string
	FolderName             *string
	Tags                   []Tag
	OriginalFilename       string
	Revision               int
	IsArchived             bool
	ArchivedAt             *time.Time
	ProvenanceHash         string
	MIMEType               string
	ByteSize               *int
	Width                  *int
	Height                 *int
	VerificationStatus     string
	StoragePath            string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ProvenanceJSON         map[string]any
	SourceProductAssetID   *string
	SourceSessionAssetID   *string
	SHA256                 string
	SourceProductMediaID   *string
	SourceSessionMediaID   *string
	SourceProductOrigin    *string
	SourceProductProductID *string
}

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type Folder struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type AssetResponse struct {
	ID                 string     `json:"id"`
	MediaObjectID      string     `json:"media_object_id"`
	SourceType         string     `json:"source_type"`
	SourceID           string     `json:"source_id"`
	DisplayName        string     `json:"display_name"`
	FolderID           *string    `json:"folder_id"`
	FolderName         *string    `json:"folder_name"`
	Tags               []Tag      `json:"tags"`
	OriginalFilename   string     `json:"original_filename"`
	Revision           int        `json:"revision"`
	IsArchived         bool       `json:"is_archived"`
	ArchivedAt         *time.Time `json:"archived_at"`
	ProvenanceHash     string     `json:"provenance_hash"`
	MIMEType           string     `json:"mime_type"`
	ByteSize           *int       `json:"byte_size"`
	Width              *int       `json:"width"`
	Height             *int       `json:"height"`
	VerificationStatus string     `json:"verification_status"`
	DownloadURL        string     `json:"download_url"`
	PreviewURL         string     `json:"preview_url"`
	ThumbnailURL       string     `json:"thumbnail_url"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type ListResponse struct {
	Items      []AssetResponse `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

type Bootstrap struct {
	TotalCount       int      `json:"total_count"`
	ActiveCount      int      `json:"active_count"`
	ArchivedCount    int      `json:"archived_count"`
	UnorganizedCount int      `json:"unorganized_count"`
	Folders          []Folder `json:"folders"`
	Tags             []Tag    `json:"tags"`
}

type FolderMutation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"-"`
}

type TagMutation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"-"`
}

type SaveResult struct {
	Asset   Asset
	Created bool
}

type WorkflowItem struct {
	Asset               AssetResponse `json:"asset"`
	ProductImageAssetID *string       `json:"product_image_asset_id"`
	LinkedAt            time.Time     `json:"linked_at"`
}

type WorkflowList struct {
	WorkflowID string         `json:"workflow_id"`
	Items      []WorkflowItem `json:"items"`
}

type ListFilter struct {
	Limit           int
	Cursor          string
	IncludeArchived bool
	Search          string
	SourceType      string
	FolderID        string
	Tag             string
}

type UploadItem struct {
	Content  []byte
	Filename string
	MIMEType string
}

type Provenance struct {
	SchemaVersion    int
	SourceType       string
	SourceID         string
	SHA256           string
	MIMEType         string
	ByteSize         int
	Width            int
	Height           int
	OriginalFilename string
	OriginType       *string
	CapturedAt       time.Time
}

type sessionRow struct {
	ID               string
	Kind             string
	OriginalFilename string
	CreatedAt        time.Time
	Media            media.Object
}
