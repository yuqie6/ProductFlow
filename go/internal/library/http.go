package library

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

type HTTP struct {
	Service  Service
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
}

// Register 挂上 /api/media-library 全局素材库路由，全部走管理员 session。
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
	out, err := h.Service.List(c.Request.Context(), ListFilter{
		Limit:           limit,
		Cursor:          c.Query("cursor"),
		IncludeArchived: includeArchived,
		Search:          c.Query("q"),
		SourceType:      sourceType,
		FolderID:        c.Query("folder_id"),
		Tag:             c.Query("tag"),
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) bootstrap(c *gin.Context) {
	out, err := h.Service.Bootstrap(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) deleteFolder(c *gin.Context) {
	moved, err := h.Service.DeleteFolder(c.Request.Context(), c.Param("folder_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"folder_id": c.Param("folder_id"), "unorganized_count": moved})
}

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

func (h HTTP) deleteTag(c *gin.Context) {
	removed, err := h.Service.DeleteTag(c.Request.Context(), c.Param("tag_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag_id": c.Param("tag_id"), "removed_assignment_count": removed})
}

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
	media.ServeVariant(
		c,
		h.Service.Media.Files,
		asset.StoragePath,
		asset.OriginalFilename,
		asset.MIMEType,
		c.DefaultQuery("variant", "original"),
		"素材库媒体文件不存在",
	)
}

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

func bindJSON(c *gin.Context, dest any) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return apperr.Validation("请求体无效")
	}
	return nil
}

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
