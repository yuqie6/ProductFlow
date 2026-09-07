package product

import (
	"encoding/json"
	"time"
)

// Product 是 products 行的领域快照。封面与图库引用 ProductImageAsset id，不是存储路径。
type Product struct {
	ID                string
	Name              string
	Category          *string // nil 表示未填类目
	Price             *string // nil 表示未填价格
	SourceNote        *string // nil 表示未填商品说明
	BrandID           *string // nil 表示未选定本商家 Brand
	CoverImageAssetID *string
	IntakeVersion     *int            // Agent 工作区 intake 版本；无图出生为 nil
	IntakeJSON        json.RawMessage // 图种选择与参考图 id；无值时空
	FactSetVersionID  *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ImageAsset 是一条商品图片身份，绑定 MediaObject；节点与封面引用本 id。
type ImageAsset struct {
	ID                      string
	ProductID               string
	MediaObjectID           string
	OriginType              string // upload|workflow_generation|image_session_attach|local_edit
	DisplayName             string // 图库展示名，可改；不是 OriginalFilename
	OriginalFilename        string
	ImageTypeKey            *string // 图种闭集；nil 表示未分类
	UserFolderID            *string
	ParentAssetID           *string
	SourceImageSessionAsset *string // 来自连续生图会话时的源资产 id
	SourceLibraryAsset      *string // 来自全局图库收藏时的源资产 id
	MIMEType                string
	ByteSize                *int
	Width                   *int
	Height                  *int
	VerificationStatus      string // verified|missing，来自 MediaObject
	StoragePath             string // 内部路径，HTTP 合同不得暴露
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// Summary 是商品列表项。cover_image_* 是展示元数据，改封面不改 facts 或节点绑定。
type Summary struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Category               *string   `json:"category"` // nil 表示未填
	Price                  *string   `json:"price"`    // nil 表示未填
	CoverImageAssetID      *string   `json:"cover_image_asset_id"`
	CoverImageFilename     *string   `json:"cover_image_filename"`      // 封面原文件名；无封面为 nil
	CoverImageDownloadURL  *string   `json:"cover_image_download_url"`  // 由身份推导，不含存储路径
	CoverImagePreviewURL   *string   `json:"cover_image_preview_url"`   // 预览图 URL；无封面为 nil
	CoverImageThumbnailURL *string   `json:"cover_image_thumbnail_url"` // 缩略图 URL；无封面为 nil
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// Detail 是商品详情 HTTP 合同。intake 无值时序列化为 JSON null。
type Detail struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Category          *string         `json:"category"`    // nil 表示未填
	Price             *string         `json:"price"`       // nil 表示未填
	SourceNote        *string         `json:"source_note"` // nil 表示未填
	BrandID           *string         `json:"brand_id"`    // nil 表示未选定 Brand
	CoverImageAssetID *string         `json:"cover_image_asset_id"`
	Intake            json.RawMessage `json:"intake"` // 无值时序列化为 JSON null
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// BrandSelectionView 是商品当前选定的本商家 Brand。
type BrandSelectionView struct {
	ProductID string  `json:"product_id"`
	BrandID   *string `json:"brand_id"`
}

// AssetResponse 是商品图的 Web 合同。id 是 ProductImageAsset id；URL 由身份推导，不含存储路径。
type AssetResponse struct {
	ID                        string    `json:"id"`
	ProductID                 string    `json:"product_id"`
	MediaObjectID             string    `json:"media_object_id"`
	OriginType                string    `json:"origin_type"` // upload|workflow_generation|image_session_attach|local_edit
	DisplayName               string    `json:"display_name"`
	OriginalFilename          string    `json:"original_filename"`
	ImageTypeKey              *string   `json:"image_type_key"` // 图种闭集；nil 表示未分类
	UserFolderID              *string   `json:"user_folder_id"`
	ParentAssetID             *string   `json:"parent_asset_id"`
	SourceImageSessionAssetID *string   `json:"source_image_session_asset_id"`
	SourceLibraryAssetID      *string   `json:"source_library_asset_id"`
	MIMEType                  string    `json:"mime_type"`
	ByteSize                  *int      `json:"byte_size"`
	Width                     *int      `json:"width"`
	Height                    *int      `json:"height"`
	VerificationStatus        string    `json:"verification_status"` // verified|missing
	DownloadURL               string    `json:"download_url"`        // 由身份推导，不含存储路径
	PreviewURL                string    `json:"preview_url"`         // 预览图 URL，由身份推导
	ThumbnailURL              string    `json:"thumbnail_url"`       // 缩略图 URL，由身份推导
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

// CreateResponse 是 POST /api/v2/products 201 体：无图出生的商品详情与刚写入的参考图。
// 不含 graph。不要和 DirectCreateResponse（含图）或 WorkspaceCreateResponse（含对话）搞混。
type CreateResponse struct {
	Product       Detail          `json:"product"`
	CreatedAssets []AssetResponse `json:"created_assets"` // 空列表是 [] 不是 nil
}

// ListResponse 是 GET /api/v2/products 200 分页体。Items 是 Summary（封面展示元数据），不是 Detail。
// Page 从 1 起；Total 是筛选后总数。改字段只动列表 JSON。
type ListResponse struct {
	Items    []Summary `json:"items"` // 空列表是 [] 不是 nil
	Total    int       `json:"total"` // 筛选后总数，不是本页条数
	Page     int       `json:"page"`  // 从 1 起，不是 cursor
	PageSize int       `json:"page_size"`
}

// Conversation 是商品工作区绑定的 AgentConversation 投影。
type Conversation struct {
	ID           string    `json:"id"`
	ScopeType    string    `json:"scope_type"` // product_workflow；全局 Dock 不走本结构
	SessionID    *string   `json:"session_id"`
	ProductID    *string   `json:"product_id"`
	HarnessRunID string    `json:"harness_run_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// WorkspaceCreateResponse 是 POST /api/v2/agent-product-workspaces 201 体：表单完整 Agent 出生。
// TaskID 出生时为 null（不自动开 Goal）。不要和名称-only 的 WorkspaceSnapshotResponse 或无对话的 DirectCreateResponse 搞混。
type WorkspaceCreateResponse struct {
	TaskID        *string         `json:"task_id"`
	Product       Detail          `json:"product"`
	CreatedAssets []AssetResponse `json:"created_assets"` // 空列表是 [] 不是 nil
	Conversation  Conversation    `json:"conversation"`
}

// WorkspaceSnapshotResponse 是工作区读取或 intake 落库后的快照。Created 表示本次是否新建。
type WorkspaceSnapshotResponse struct {
	TaskID          *string         `json:"task_id"`
	Created         bool            `json:"created"`          // 本次是否新建商品/对话
	IntakeFinalized bool            `json:"intake_finalized"` // 图种选择已落库
	Product         Detail          `json:"product"`
	CreatedAssets   []AssetResponse `json:"created_assets"` // 本次写入的参考图；空列表是 [] 不是 nil
	Conversation    Conversation    `json:"conversation"`
}

// DirectCreateResponse 是 v3 直连创建的 HTTP 体，含当场写出的 schema-v3 图投影。
type DirectCreateResponse struct {
	Product       Detail          `json:"product"`
	CreatedAssets []AssetResponse `json:"created_assets"` // 本次写入的参考图；空列表是 [] 不是 nil
	Graph         map[string]any  `json:"graph"`          // schema-v3 画布投影，不是 AppliedGraph
}

// Upload 是已校验的参考图像素与声明类型，尚未写成 ProductImageAsset。
type Upload struct {
	Content  []byte // 已过 media.ValidateUpload 的像素
	Filename string
	MIMEType string
}

// Fact 是 GET/PUT /api/v3/products/:id/facts 里一条不可变事实，存在 product_fact_set_versions.payload_json。
// SourceType 闭集 user|image_observation|agent_inference；Status 闭集 observed|user_declared|confirmed|conflicted。
// Layer 闭集 performance|marketing：营销口吻不得写入 performance 层；不另建事实仓库。
// 改 key 集合必须同步 normalizeFactPayload。不要和 graph.FactSet（编译器 []map）搞混。
type Fact struct {
	Key                  string `json:"key"`
	Value                any    `json:"value"`       // 标量或短文本，形状由 key 约定
	SourceType           string `json:"source_type"` // user|image_observation|agent_inference
	Status               string `json:"status"`
	Layer                string `json:"layer"`                  // performance|marketing
	RequiresConfirmation bool   `json:"requires_confirmation"` // true 时需用户确认才算正式事实
	EvidenceAssetIDs     []any  `json:"evidence_asset_ids"`    // ProductImageAsset id；空列表是 [] 不是 nil
	Conflicts            []any  `json:"conflicts"`             // 冲突事实摘要；空列表是 [] 不是 nil
}

// FactSet 是一次不可变 fact 版本的 HTTP 投影，对应 product_fact_set_versions 一行。
// 更新 facts 是插入新版本并把 products.current_fact_set_version_id 指向它，禁止 UPDATE payload。
// v2 无图出生尚未写版本时 GET 的外层 FactSet 为 null。不要和 graph.FactSet 搞混。
type FactSet struct {
	ID        string    `json:"id"`
	ProductID string    `json:"product_id"`
	Version   int       `json:"version"` // 不可变版本号；更新是插入新行
	Facts     []Fact    `json:"facts"`   // 空列表是 [] 不是 nil
	CreatedAt time.Time `json:"created_at"`
}

// FactsResponse 是 GET/PUT facts 的 HTTP 体。v2 无图出生尚未写 fact 时 id 为 null。
type FactsResponse struct {
	Product                 Detail   `json:"product"`
	CurrentFactSetVersionID *string  `json:"current_fact_set_version_id"`
	CurrentFactVersion      *int     `json:"current_fact_version"` // v2 无图尚未写 fact 时为 null
	FactSet                 *FactSet `json:"fact_set"`             // 无版本时为 null
	Facts                   []Fact   `json:"facts"`                // 空列表是 [] 不是 nil
}

// AssetListResponse 包装 POST .../image-assets 201 的新图列表，Items 是 AssetResponse 不是 GalleryAssetResponse。
// 不含分页 cursor。改字段只动该上传接口 JSON。
type AssetListResponse struct {
	Items []AssetResponse `json:"items"` // 空列表是 [] 不是 nil；不含分页 cursor
}

// GalleryBootstrap 是图库左侧目录。系统目录是查询投影；用户文件夹一层且不替代系统分类。
type GalleryBootstrap struct {
	ProductID         string                   `json:"product_id"`
	CoverImageAssetID *string                  `json:"cover_image_asset_id"`
	SystemDirectories []GallerySystemDirectory `json:"system_directories"` // 系统目录计数；空列表是 [] 不是 nil
	ImageTypes        []GalleryImageType       `json:"image_types"`        // 图种闭集目录；空列表是 [] 不是 nil
	Origins           []GalleryOrigin          `json:"origins"`            // 来源闭集目录；空列表是 [] 不是 nil
	UserFolders       []GalleryFolder          `json:"user_folders"`       // 用户一层文件夹；空列表是 [] 不是 nil
	UnorganizedCount  int                      `json:"unorganized_count"`  // folder_id 为空的未归档数
}

// GallerySystemDirectory 是图库左侧系统目录计数，查询投影，没有对应表。
// Kind 为 all|recent_generated|uploads|generated|unorganized。不要和用户文件夹 GalleryFolder（product_asset_folders）搞混。
type GallerySystemDirectory struct {
	Kind  string `json:"kind"` // all|recent_generated|uploads|generated|unorganized
	Count int    `json:"count"`
}

// GalleryImageType 按 image_type_key 聚合；未分类用内部 key __unclassified__。
type GalleryImageType struct {
	DirectoryKey string  `json:"directory_key"`  // 筛选用；未分类为 __unclassified__
	ImageTypeKey *string `json:"image_type_key"` // 未分类为 nil，目录用 __unclassified__
	Title        string  `json:"title"`
	Count        int     `json:"count"`
}

// GalleryOrigin 按 product_image_assets.origin_type 计数：upload|workflow_generation|image_session_attach|local_edit。
// 出现在 GalleryBootstrap.origins。改枚举须同步 originTypeOrder 与筛选 directory_kind=source。
type GalleryOrigin struct {
	OriginType string `json:"origin_type"` // upload|workflow_generation|image_session_attach|local_edit
	Count      int    `json:"count"`
}

// GalleryFolder 是一层用户文件夹及其资产数。删除文件夹只取消整理，不删资产。
type GalleryFolder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"` // 用户文件夹一层次序
	Count     int    `json:"count"`
}

// GalleryFolderMutation 是 POST/PATCH 用户文件夹的返回体，对应 product_asset_folders，不含 Count。
// 创建不移动资产；重命名须 expected_name。不要和带计数的 GalleryFolder 或画布 AppliedGroup 搞混。
type GalleryFolderMutation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"` // 用户文件夹一层次序
}

// DeleteGalleryFolderResponse 报告有多少资产被移到未整理。
type DeleteGalleryFolderResponse struct {
	FolderID                string `json:"folder_id"`
	MovedToUnorganizedCount int    `json:"moved_to_unorganized_count"` // 删文件夹时被取消整理的资产数
}

// GalleryAssetResponse 在 AssetResponse 上附加文件夹名与生成/交付摘要。
type GalleryAssetResponse struct {
	AssetResponse
	UserFolderName *string                   `json:"user_folder_name"` // nil 表示未整理
	ImageTypeTitle *string                   `json:"image_type_title"` // 图种标题；未分类为 nil
	Generation     *GalleryGenerationSummary `json:"generation"`       // 非工作流生成则为 nil
	Rendition      *GalleryRenditionSummary  `json:"rendition"`        // 非交付派生则为 nil
}

// GalleryGenerationSummary 追溯工作流生成结果所属的节点与 artifact。
type GalleryGenerationSummary struct {
	WorkflowID              string  `json:"workflow_id"`
	NodeID                  string  `json:"node_id"`
	NodeRunID               string  `json:"node_run_id"`
	PromptArtifactVersionID *string `json:"prompt_artifact_version_id"`
	VisualSystemVersionID   *string `json:"visual_system_version_id"`
}

// GalleryRenditionSummary 是交付派生图相对源 ProductImageAsset 的摘要。
type GalleryRenditionSummary struct {
	JobID         string         `json:"job_id"`
	SourceAssetID string         `json:"source_asset_id"`
	DeliverySpec  map[string]any `json:"delivery_spec"` // 确定性派生规格，不含像素
	Status        string         `json:"status"`
}

// GalleryAssetPage 是图库分页。cursor 绑定当前筛选，换条件必须重拉。
type GalleryAssetPage struct {
	Items      []GalleryAssetResponse `json:"items"`       // 空列表是 [] 不是 nil
	NextCursor *string                `json:"next_cursor"` // opaque，不是页码；nil=没有下一页
}
