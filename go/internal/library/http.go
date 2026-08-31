package library

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 是全局图库的 Gin 处理器集合，不是素材身份本身。
// 路由前缀 /api/media-library；工作流子图库关联也挂在这里，但不复制 MediaObject bytes。
// 不要和 product 商品图库、imagesession 连续生图、recipe 配方库搞混。
type HTTP struct {
	Service  Service     // 必须注入；拥有全局素材命令
	Settings interface { // nil 时 RequireAdmin 视为不要求访问令牌
		settings.RuntimeReader
		settings.LimitsReader
	}
}

// Register 挂上 /api/media-library 全局素材库路由，全部走管理员 session。
// 路径与成功状态码见各处理器注释；HTTP 不入队 broker。
func (h HTTP) Register(engine *gin.Engine) {
	admin := httpx.RequireAdmin(func(c *gin.Context) (bool, error) {
		if h.Settings == nil {
			return true, nil
		}
		runtime, err := h.Settings.Runtime(c.Request.Context())
		if err != nil {
			return false, err
		}
		return runtime.AdminAccessRequired, nil
	})
	g := engine.Group("/api/media-library", admin)
	g.GET("", h.list)
	g.GET("/bootstrap", h.bootstrap)
	g.POST("/folders", h.createFolder)
	g.PATCH("/folders/:folder_id", h.renameFolder)
	g.DELETE("/folders/:folder_id", h.deleteFolder)
	g.POST("/tags", h.createTag)
	g.PATCH("/tags/:tag_id", h.renameTag)
	g.DELETE("/tags/:tag_id", h.deleteTag)
	g.POST("/organize/move", h.moveAssets)
	g.POST("/organize/tags", h.setTags)
	g.POST("/from-session", h.fromSession)
	g.POST("/from-product", h.fromProduct)
	g.POST("/upload", h.upload)
	g.POST("/collect", h.collect)
	g.GET("/workflows/:workflow_id/media-library", h.listWorkflow)
	g.POST("/workflows/:workflow_id/media-library/sync", h.syncWorkflow)
	g.DELETE("/workflows/:workflow_id/media-library/:media_library_asset_id", h.removeWorkflow)
	g.GET("/:asset_id", h.get)
	g.GET("/:asset_id/download", h.download)
	g.POST("/:asset_id/archive", h.archive)
	g.POST("/:asset_id/restore", h.restore)
}

// list 是 GET /api/media-library：200 返回 ListResponse。
func (h HTTP) list(c *gin.Context) {
	limit, err := queryLimit(c, 20, 100)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	includeArchived, err := queryBool(c, "include_archived", false)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	sourceType := strings.TrimSpace(c.Query("source_type"))
	if sourceType != "" && sourceType != SourceSession && sourceType != SourceProduct && sourceType != SourceUpload {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	q, err := queryBounded(c, "q", 255)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	folderID, err := queryBounded(c, "folder_id", 36)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	tag, err := queryBounded(c, "tag", 80)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.List(c.Request.Context(), ListFilter{
		Limit:           limit,
		Cursor:          c.Query("cursor"),
		IncludeArchived: includeArchived,
		Search:          q,
		SourceType:      sourceType,
		FolderID:        folderID,
		Tag:             tag,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// bootstrap 是 GET /api/media-library/bootstrap：200 返回 Bootstrap。
func (h HTTP) bootstrap(c *gin.Context) {
	out, err := h.Service.Bootstrap(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// createFolder 是 POST /api/media-library/folders：201 新建；同名已存在 200。
func (h HTTP) createFolder(c *gin.Context) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.CreateFolder(c.Request.Context(), payload.Name)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	c.JSON(status, Folder{ID: out.ID, Name: out.Name})
}

// renameFolder 是 PATCH /api/media-library/folders/:folder_id：200 返回 Folder。
func (h HTTP) renameFolder(c *gin.Context) {
	var payload struct {
		ExpectedName string `json:"expected_name"`
		Name         string `json:"name"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.RenameFolder(c.Request.Context(), c.Param("folder_id"), payload.ExpectedName, payload.Name)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// deleteFolder 是 DELETE /api/media-library/folders/:folder_id：200 返回 folder_id 与移出数量。
func (h HTTP) deleteFolder(c *gin.Context) {
	moved, err := h.Service.DeleteFolder(c.Request.Context(), c.Param("folder_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"folder_id": c.Param("folder_id"), "unorganized_count": moved})
}

// createTag 是 POST /api/media-library/tags：201 新建；同名已存在 200。
func (h HTTP) createTag(c *gin.Context) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.CreateTag(c.Request.Context(), payload.Name)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	c.JSON(status, Tag{ID: out.ID, Name: out.Name})
}

// renameTag 是 PATCH /api/media-library/tags/:tag_id：200 返回 Tag。
func (h HTTP) renameTag(c *gin.Context) {
	var payload struct {
		ExpectedName string `json:"expected_name"`
		Name         string `json:"name"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.RenameTag(c.Request.Context(), c.Param("tag_id"), payload.ExpectedName, payload.Name)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// deleteTag 是 DELETE /api/media-library/tags/:tag_id：200 返回 tag_id 与解除关联数。
func (h HTTP) deleteTag(c *gin.Context) {
	removed, err := h.Service.DeleteTag(c.Request.Context(), c.Param("tag_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag_id": c.Param("tag_id"), "removed_assignment_count": removed})
}

// moveAssets 是 POST /api/media-library/organize/move：200 返回素材投影列表。
func (h HTTP) moveAssets(c *gin.Context) {
	var payload struct {
		AssetIDs          []string       `json:"asset_ids"`
		FolderID          *string        `json:"folder_id"`
		ExpectedRevisions map[string]int `json:"expected_revisions"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	assets, err := h.Service.MoveAssets(c.Request.Context(), payload.AssetIDs, payload.FolderID, payload.ExpectedRevisions)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := serializeAssets(assets)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// setTags 是 POST /api/media-library/organize/tags：200 返回素材投影列表。
func (h HTTP) setTags(c *gin.Context) {
	var payload struct {
		AssetIDs          []string       `json:"asset_ids"`
		TagNames          []string       `json:"tag_names"`
		ExpectedRevisions map[string]int `json:"expected_revisions"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if payload.TagNames == nil {
		payload.TagNames = []string{}
	}
	assets, err := h.Service.SetTags(c.Request.Context(), payload.AssetIDs, payload.TagNames, payload.ExpectedRevisions)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := serializeAssets(assets)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// fromSession 是 POST /api/media-library/from-session：201 新建；已按来源命中 200。
func (h HTTP) fromSession(c *gin.Context) {
	var payload struct {
		ImageSessionAssetID string `json:"image_session_asset_id"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	result, err := h.Service.SaveFromSession(c.Request.Context(), payload.ImageSessionAssetID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	item, err := serializeAsset(result.Asset)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	c.JSON(status, item)
}

// fromProduct 是 POST /api/media-library/from-product：201 新建；已按来源命中 200。
func (h HTTP) fromProduct(c *gin.Context) {
	var payload struct {
		ProductImageAssetID string `json:"product_image_asset_id"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	result, err := h.Service.SaveFromProduct(c.Request.Context(), payload.ProductImageAssetID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	item, err := serializeAsset(result.Asset)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	c.JSON(status, item)
}

// upload 是 POST /api/media-library/upload：201 返回素材投影列表（含幂等回放）。
func (h HTTP) upload(c *gin.Context) {
	items, err := h.readFiles(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	var folderID *string
	if raw := strings.TrimSpace(c.PostForm("folder_id")); raw != "" {
		folderID = &raw
	}
	results, err := h.Service.Upload(c.Request.Context(), items, folderID, c.GetHeader("Idempotency-Key"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out := make([]AssetResponse, 0, len(results))
	for _, result := range results {
		item, err := serializeAsset(result.Asset)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		out = append(out, item)
	}
	c.JSON(http.StatusCreated, out)
}

// collect 是 POST /api/media-library/collect：200 返回商品图投影（写入商品图库，不是全局图库行）。
func (h HTTP) collect(c *gin.Context) {
	var payload struct {
		ProductID            string   `json:"product_id"`
		MediaLibraryAssetIDs []string `json:"media_library_asset_ids"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		httpx.AbortErr(c, apperr.Validation("素材收录 Idempotency-Key 不能为空"))
		return
	}
	assets, err := h.Service.Collect(c.Request.Context(), payload.ProductID, payload.MediaLibraryAssetIDs, key)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, product.SerializeAssets(assets))
}

// listWorkflow 是 GET /api/media-library/workflows/:workflow_id/media-library：200 返回 WorkflowList。
func (h HTTP) listWorkflow(c *gin.Context) {
	productID := strings.TrimSpace(c.Query("product_id"))
	if productID == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	limit, err := queryLimit(c, 100, 100)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ListWorkflow(c.Request.Context(), productID, c.Param("workflow_id"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// syncWorkflow 是 POST /api/media-library/workflows/:workflow_id/media-library/sync：200 返回 WorkflowList。
func (h HTTP) syncWorkflow(c *gin.Context) {
	productID := strings.TrimSpace(c.Query("product_id"))
	if productID == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	var payload struct {
		MediaLibraryAssetIDs []string `json:"media_library_asset_ids"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.SyncWorkflow(c.Request.Context(), productID, c.Param("workflow_id"), payload.MediaLibraryAssetIDs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// removeWorkflow 是 DELETE /api/media-library/workflows/:workflow_id/media-library/:media_library_asset_id：204。
func (h HTTP) removeWorkflow(c *gin.Context) {
	productID := strings.TrimSpace(c.Query("product_id"))
	if productID == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if err := h.Service.RemoveWorkflow(c.Request.Context(), productID, c.Param("workflow_id"), c.Param("media_library_asset_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// get 是 GET /api/media-library/:asset_id：200 返回 AssetResponse。
func (h HTTP) get(c *gin.Context) {
	asset, err := h.Service.Get(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	item, err := serializeAsset(asset)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// download 是 GET /api/media-library/:asset_id/download：200 写出文件；缺文件 404；未核验 409。
func (h HTTP) download(c *gin.Context) {
	asset, err := h.Service.Get(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if asset.MIMEType == "" || asset.StoragePath == "" {
		httpx.AbortDetail(c, http.StatusNotFound, "素材库媒体文件不存在")
		return
	}
	if asset.VerificationStatus != media.StatusVerified {
		httpx.AbortDetail(c, http.StatusConflict, "素材库媒体尚未通过核验")
		return
	}
	media.ServeExistingVariant(
		c,
		h.Service.Media,
		h.Service.DB,
		asset.StoragePath,
		asset.OriginalFilename,
		asset.MIMEType,
		c.DefaultQuery("variant", "original"),
		"素材库媒体文件不存在",
	)
}

// archive 是 POST /api/media-library/:asset_id/archive：200 返回归档后的 AssetResponse。
func (h HTTP) archive(c *gin.Context) {
	expected, err := queryOptionalRevision(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	asset, err := h.Service.Archive(c.Request.Context(), c.Param("asset_id"), expected)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	item, err := serializeAsset(asset)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// restore 是 POST /api/media-library/:asset_id/restore：200 返回取消归档后的 AssetResponse。
func (h HTTP) restore(c *gin.Context) {
	expected, err := queryOptionalRevision(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	asset, err := h.Service.Restore(c.Request.Context(), c.Param("asset_id"), expected)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	item, err := serializeAsset(asset)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// readFiles 读 multipart 字段 files。至少一张，上限走 UploadLimits。校验失败返回 Validation，不要把原始 multipart error 丢给用户。
func (h HTTP) readFiles(c *gin.Context) ([]UploadItem, error) {
	form, err := c.MultipartForm()
	if err != nil || form == nil {
		return nil, apperr.Validation("至少需要上传一张图片")
	}
	files := form.File["files"]
	limits := h.limits(c)
	if len(files) == 0 {
		return nil, apperr.Validation("至少需要上传一张图片")
	}
	if len(files) > limits.MaxBatchFiles {
		return nil, apperr.Validationf("单批次最多上传 %d 张图片，当前提交了 %d 张", limits.MaxBatchFiles, len(files))
	}
	out := make([]UploadItem, 0, len(files))
	total := 0
	for i, header := range files {
		f, err := header.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(f, int64(limits.MaxImageBytes)+1))
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		filename := header.Filename
		if filename == "" {
			filename = fmt.Sprintf("library-upload-%d.png", i+1)
		}
		validated, err := media.ValidateUpload(filename, header.Header.Get("Content-Type"), content, limits)
		if err != nil {
			return nil, err
		}
		total += len(validated.Content)
		if total > limits.MaxBatchBytes {
			return nil, apperr.TooLarge(fmt.Sprintf(
				"批量上传总大小超过限制: %s (当前已读取 %s)",
				media.FormatByteSize(limits.MaxBatchBytes),
				media.FormatByteSize(total),
			))
		}
		out = append(out, UploadItem{Content: validated.Content, Filename: validated.Filename, MIMEType: validated.MIMEType})
	}
	return out, nil
}

// limits 读 settings 上传上限；读失败或未注入 Settings 回退 DefaultLimits，避免上传接口 500。
func (h HTTP) limits(c *gin.Context) media.Limits {
	if h.Settings == nil {
		return media.DefaultLimits()
	}
	limits, err := h.Settings.UploadLimits(c.Request.Context())
	if err != nil || limits.MaxImageBytes <= 0 {
		return media.DefaultLimits()
	}
	return limits
}

// bindJSON 用 DisallowUnknownFields 解码 JSON。多字段或尾随内容一律 400「请求体无效」。
func bindJSON(c *gin.Context, dest any) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return apperr.Validation("请求体无效")
	}
	if dec.More() {
		return apperr.Validation("请求体无效")
	}
	return nil
}

// queryBounded 读查询字符串并限制 rune 数，超长 400。用于 q / 名称类参数，不要用它解析 cursor。
func queryBounded(c *gin.Context, key string, maxRunes int) (string, error) {
	raw := c.Query(key)
	if utf8.RuneCountInString(raw) > maxRunes {
		return "", apperr.Validation("请求参数无效")
	}
	return raw, nil
}

// queryLimit 读 limit；缺省 def，超过 max 截到 max。非法数字 400。
func queryLimit(c *gin.Context, def, max int) (int, error) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > max {
		return 0, apperr.Validation("请求体无效")
	}
	return n, nil
}

// queryBool 把 1/true/yes 当 true，0/false/no 当 false；缺省用 def。其它值 400，不要静默当 false。
func queryBool(c *gin.Context, key string, def bool) (bool, error) {
	raw, ok := c.GetQuery(key)
	if !ok || raw == "" {
		return def, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, apperr.Validation("请求体无效")
	}
}

// queryOptionalRevision 读 optimistic 用的 revision 查询；缺省 nil。非法整数 400。
func queryOptionalRevision(c *gin.Context) (*int, error) {
	raw, ok := c.GetQuery("expected_revision")
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return nil, apperr.Validation("请求体无效")
	}
	return &n, nil
}
