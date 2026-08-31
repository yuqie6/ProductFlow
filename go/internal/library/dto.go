package library

import (
	"time"

	"github.com/yuqie6/productflow/internal/media"
)

const (
	// SourceSession 表示素材来自连续生图会话。
	SourceSession = "image_session_generated"
	// SourceProduct 表示素材来自商品图片。
	SourceProduct = "product_asset"
	// SourceUpload 表示素材来自直接上传。
	SourceUpload = "direct_upload"

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
	SourceType             string // image_session_generated | product_asset | direct_upload
	SourceID               string
	DisplayName            string // 图库展示名，可改且不改 OriginalFilename
	FolderID               *string
	FolderName             *string // nil 表示未整理
	Tags                   []Tag   // 未归档素材上的标签
	OriginalFilename       string  // 来源文件名，归档/恢复不改
	Revision               int     // 乐观并发；组织操作对不上则 Conflict
	IsArchived             bool    // true 时默认列表隐藏，不是 verification missing
	ArchivedAt             *time.Time
	ProvenanceHash         string // provenance JSON 的 canonjson SHA256
	MIMEType               string
	ByteSize               *int // nil 表示 media 行尚未核验出体积
	Width                  *int
	Height                 *int
	VerificationStatus     string // verified | missing
	StoragePath            string // STORAGE_ROOT 相对路径，不进 HTTP
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ProvenanceJSON         map[string]any // 来源哈希与核验元数据，不含 bytes
	SourceProductAssetID   *string
	SourceSessionAssetID   *string
	SHA256                 string // 媒体内容摘要；未核验可为空
	SourceProductMediaID   *string
	SourceSessionMediaID   *string
	SourceProductOrigin    *string // 源商品图 origin_type；nil 表示不是从商品图收录
	SourceProductProductID *string
}

// Tag 是全局图库标签的 HTTP 投影，Bootstrap/List 也用同一形状。
// Count 只统计未归档素材。删标签只解关联，不删 MediaLibraryAsset。
// 不要和商品图库标签、Agent 工具面 Asset 搞混。
type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"` // 只统计未归档素材
}

// Folder 是全局图库一层用户文件夹的 HTTP 投影。
// Count 只统计未归档素材。删文件夹只把素材 folder_id 置空，不删素材、不打断节点引用。
// 不要和画布视觉分组、商品图库文件夹搞混。
type Folder struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"` // 只统计未归档素材
}

// AssetResponse 是全局素材的 HTTP 投影，不含 provenance 正文和磁盘 bytes。
// FolderID 为 nil 表示未整理。VerificationStatus 是 media 核验态（verified/missing），不是归档位。
// 内部命令用 Asset；不要把本类型当 ProductImageAsset 或会话图。
type AssetResponse struct {
	ID                 string     `json:"id"`
	MediaObjectID      string     `json:"media_object_id"`
	SourceType         string     `json:"source_type"` // image_session_generated | product_asset | direct_upload
	SourceID           string     `json:"source_id"`
	DisplayName        string     `json:"display_name"` // 图库展示名，可改且不改 original_filename
	FolderID           *string    `json:"folder_id"`
	FolderName         *string    `json:"folder_name"`       // nil 表示未整理
	Tags               []Tag      `json:"tags"`              // 未归档素材上的标签
	OriginalFilename   string     `json:"original_filename"` // 来源文件名，归档/恢复不改
	Revision           int        `json:"revision"`          // 乐观并发
	IsArchived         bool       `json:"is_archived"`       // true 时默认列表隐藏，不是 verification missing
	ArchivedAt         *time.Time `json:"archived_at"`
	ProvenanceHash     string     `json:"provenance_hash"` // provenance JSON 的 canonjson SHA256
	MIMEType           string     `json:"mime_type"`
	ByteSize           *int       `json:"byte_size"` // nil 表示 media 行尚未核验出体积
	Width              *int       `json:"width"`
	Height             *int       `json:"height"`
	VerificationStatus string     `json:"verification_status"` // verified | missing
	DownloadURL        string     `json:"download_url"`        // 原图
	PreviewURL         string     `json:"preview_url"`         // 预览变体
	ThumbnailURL       string     `json:"thumbnail_url"`       // 缩略图变体
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// ListResponse 是全局素材分页的 HTTP 体。
// NextCursor 为 nil 表示没有下一页，不要用空串或 404 表达「到头了」。
type ListResponse struct {
	Items      []AssetResponse `json:"items"`       // 当前页；空是 []
	NextCursor *string         `json:"next_cursor"` // nil 表示没有下一页，不要用空串
}

// Bootstrap 是图库首页的 HTTP 投影：计数、一层文件夹、标签。
// UnorganizedCount 是未归档且 folder_id 为空的素材数，不是「没有标签」。
type Bootstrap struct {
	TotalCount       int      `json:"total_count"`       // 含归档
	ActiveCount      int      `json:"active_count"`      // 未归档
	ArchivedCount    int      `json:"archived_count"`    // 已归档素材数
	UnorganizedCount int      `json:"unorganized_count"` // 未归档且 folder_id 为空，不是「没有标签」
	Folders          []Folder `json:"folders"`           // 一层用户文件夹
	Tags             []Tag    `json:"tags"`              // Count 只统计未归档
}

// FolderMutation 是创建文件夹的内部结果，不是单独的 HTTP 体。
// Created 带 json:"-"：HTTP 用 201/200 表达新建或命中同名；不要把 Created 写进 JSON。
type FolderMutation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"-"` // false 表示同名文件夹已存在，本次没有新建
}

// TagMutation 是创建标签的内部结果，不是单独的 HTTP 体。
// Created 带 json:"-"：HTTP 用 201/200 表达新建或命中同名。
type TagMutation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"-"` // false 表示同名标签已存在，本次没有新建
}

// SaveResult 是写入全局素材身份的结果；Created 为 false 表示按来源命中已有行。
type SaveResult struct {
	Asset   Asset // 写入后的全局素材身份
	Created bool  // false 表示按来源命中已有行，没有新建
}

// WorkflowItem 是工作流子图库的一条 HTTP 关联，不拥有另一份 MediaObject。
// ProductImageAssetID 可空：已关联到商品图时才有。不要把它当成节点绑定或封面。
type WorkflowItem struct {
	Asset               AssetResponse `json:"asset"` // 全局素材投影；关联不复制 MediaObject
	ProductImageAssetID *string       `json:"product_image_asset_id"`
	LinkedAt            time.Time     `json:"linked_at"`
}

// WorkflowList 是某张 live 图上工作流子图库关联的 HTTP 集合。
// 改 Items 只动 workflow_media_library_assets，不复制、不删除全局素材。
type WorkflowList struct {
	WorkflowID string         `json:"workflow_id"`
	Items      []WorkflowItem `json:"items"` // 只动关联表，不复制、不删除全局素材
}

// ListFilter 是 List 的内部查询条件，不是 HTTP JSON 体；query 由 handler 填入。
// FolderID/Tag 空串表示不按该项筛选。SourceType 空串表示全部来源，不是 SourceUpload。
type ListFilter struct {
	Limit           int    // <1 时默认 20，上限 100
	Cursor          string // 空串表示第一页；与当前筛选签名不一致会 Validation
	IncludeArchived bool   // false 时跳过已归档
	Search          string // 空串表示不按名称筛选
	SourceType      string // 空串表示全部来源，不是 SourceUpload
	FolderID        string
	Tag             string // 空串表示不按标签筛选
}

// UploadItem 是一次直接上传的已校验文件，内部入参，不是 HTTP JSON。
// Content 是校验后的 bytes；MIMEType 必须已过 media.ValidateUpload。不要把未校验 multipart 塞进来。
type UploadItem struct {
	Content  []byte // 已过 media.ValidateUpload 的 bytes
	Filename string
	MIMEType string
}

// Provenance 记录素材来源哈希与核验元数据，不含媒体 bytes。
type Provenance struct {
	SchemaVersion    int    // 当前为 1
	SourceType       string // 与 Asset.SourceType 相同闭集
	SourceID         string
	SHA256           string // 媒体内容摘要
	MIMEType         string
	ByteSize         int // 核验时的内容长度
	Width            int
	Height           int
	OriginalFilename string  // 来源文件名
	OriginType       *string // 商品图 origin_type；nil 表示无商品来源
	CapturedAt       time.Time
}

type sessionRow struct {
	ID               string
	Kind             string
	OriginalFilename string
	CreatedAt        time.Time
	Media            media.Object
}
