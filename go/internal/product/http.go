package product

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 给管理员 session 挂商品出生、facts、封面、图库、工作区与删除路由。
// Service 与 Settings 必须注入（删除门闩读 DeletionEnabled）。路径见各处理器。不要把本类型当成 product.Service。
type HTTP struct {
	Service Service // 必须注入
	// Settings 必须注入：RequireAdmin 读 AdminAccessRequired；nil 时删除门闩视为关闭（403）。
	// UploadLimits 失败则回落 media 默认上限。
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
}

// Register 挂上商品出生、facts、封面、图库与删除路由，全部走管理员 session。
// 路径见各处理器注释。删除受 runtime.DeletionEnabled 门闩，关闭时 403。
func (h HTTP) Register(engine *gin.Engine) {
	admin := httpx.RequireAdmin(func(c *gin.Context) (bool, error) {
		runtime, err := h.Settings.Runtime(c.Request.Context())
		if err != nil {
			return false, err
		}
		return runtime.AdminAccessRequired, nil
	})
	api := engine.Group("/api", admin)
	api.POST("/v2/products", h.createV2)
	api.GET("/v2/products", h.list)
	api.GET("/v2/products/:product_id/image-library", h.galleryBootstrap)
	api.POST("/v2/products/:product_id/image-folders", h.createFolder)
	api.PATCH("/v2/products/:product_id/image-folders/:folder_id", h.renameFolder)
	api.DELETE("/v2/products/:product_id/image-folders/:folder_id", h.deleteFolder)
	api.POST("/v2/products/:product_id/image-assets/move", h.moveAssets)
	api.POST("/v2/products/:product_id/image-assets/download-archive", h.downloadArchive)
	api.GET("/v2/products/:product_id/image-assets", h.listGalleryAssets)
	api.GET("/v2/products/:product_id/image-assets/:asset_id", h.getGalleryAsset)
	api.PATCH("/v2/products/:product_id/image-assets/:asset_id", h.renameGalleryAsset)
	api.POST("/v2/products/:product_id/image-assets", h.addImages)
	api.PUT("/v2/products/:product_id/cover", h.setCover)
	api.DELETE("/v2/products/:product_id/cover", h.clearCover)
	api.GET("/v2/products/:product_id", h.get)
	api.DELETE("/v2/products/:product_id", h.requireDeletion, h.deleteProduct)
	api.GET("/v3/products/:product_id/facts", h.getFacts)
	api.PUT("/v3/products/:product_id/facts", h.updateFacts)
	api.GET("/v2/product-image-assets/:asset_id/download", h.download)
	api.DELETE("/v2/product-image-assets/:asset_id", h.requireDeletion, h.deleteAsset)
	api.POST("/v3/products", h.createV3)
	api.POST("/v3/products/from-recipe", h.createFromRecipe)
	api.GET("/v2/agent-product-workspaces/options", h.workspaceOptions)
	api.POST("/v2/agent-product-workspaces/drafts", h.createDraftWorkspace)
	api.POST("/v2/agent-product-workspaces", h.createWorkspace)
	api.GET("/v2/agent-product-workspaces/:conversation_id", h.getWorkspace)
	api.POST("/v2/agent-product-workspaces/:conversation_id/intake", h.finalizeWorkspaceIntake)
	api.POST("/v2/product-source-notes/generate", h.generateSourceNote)
}

// requireDeletion 挂在 DELETE /api/v2/products/:product_id 与 DELETE /api/v2/product-image-assets/:asset_id 前：放行不写响应（后续 204）；Settings 为 nil 或 DeletionEnabled=false 时 403；读设置失败 500。
func (h HTTP) requireDeletion(c *gin.Context) {
	if h.Settings == nil {
		httpx.AbortDetail(c, http.StatusForbidden, "删除功能已关闭，请联系管理员")
		return
	}
	runtime, err := h.Settings.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	if !runtime.DeletionEnabled {
		httpx.AbortDetail(c, http.StatusForbidden, "删除功能已关闭，请联系管理员")
		return
	}
}

// createV2 是 POST /api/v2/products：201 无图出生；缺参考图 400。
func (h HTTP) createV2(c *gin.Context) {
	uploads, err := h.readImages(c, "images", "reference.bin", "至少上传一张商品参考图")
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	created, err := h.Service.CreateWithoutGraph(c.Request.Context(), CreateInput{
		Name:       c.PostForm("name"),
		Category:   c.PostForm("category"),
		Price:      c.PostForm("price"),
		SourceNote: c.PostForm("source_note"),
		Uploads:    uploads,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

// createV3 是 POST /api/v3/products：201 直连创建（商品+参考图+模板图）；校验失败 400。
func (h HTTP) createV3(c *gin.Context) {
	uploads, err := h.readImages(c, "images", "reference.bin", "至少上传一张商品参考图")
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	imageTypes, err := parseDirectImageTypes(c.PostForm("image_types"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	generationSpec, err := parseGenerationSpec(c.PostForm("generation_spec"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	textSettings, err := parseGenerationSpec(c.PostForm("text_settings"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	deliverySpec, err := deliveryPresetSpec(c.PostForm("delivery_preset_key"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	created, err := h.Service.CreateDirect(c.Request.Context(), CreateInput{
		TextSettings: textSettings,
		Name:         c.PostForm("name"),
		Category:     c.PostForm("category"),
		Price:        c.PostForm("price"),
		SourceNote:   c.PostForm("source_note"),
		Uploads:      uploads,
	}, imageTypes, generationSpec, deliverySpec)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

// list 是 GET /api/v2/products：200 分页列表；page/page_size/q/sort 非法 400。
func (h HTTP) list(c *gin.Context) {
	page, err := parseQueryInt(c, "page", 1, 1, 0)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	pageSize, err := parseQueryInt(c, "page_size", 20, 1, 100)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	q := c.Query("q")
	if utf8.RuneCountInString(q) > 100 {
		httpx.AbortErr(c, apperr.Validation("请求参数无效"))
		return
	}
	sort, err := parseProductListSort(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.List(c.Request.Context(), page, pageSize, q, sort)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// get 是 GET /api/v2/products/:product_id：200 详情；找不到 404。
func (h HTTP) get(c *gin.Context) {
	detail, err := h.Service.Get(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// download 是 GET /api/v2/product-image-assets/:asset_id/download：200 原图/变体；缺文件 404。
func (h HTTP) download(c *gin.Context) {
	asset, err := h.Service.AssetForDownload(c.Request.Context(), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if asset.VerificationStatus == media.StatusMissing {
		httpx.AbortDetail(c, http.StatusNotFound, "商品图片文件不存在")
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
		"商品图片文件不存在",
	)
}

// createDraftWorkspace 是 POST /api/v2/agent-product-workspaces/drafts：201 名称-only；体非法 400。
func (h HTTP) createDraftWorkspace(c *gin.Context) {
	var payload struct {
		Name           string  `json:"name"`
		AgentSessionID *string `json:"agent_session_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	created, err := h.Service.CreateAgentDraft(c.Request.Context(), payload.Name, c.GetHeader("Idempotency-Key"), payload.AgentSessionID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

// createWorkspace 是 POST /api/v2/agent-product-workspaces：201 表单出生；Idempotency-Key 去重。
func (h HTTP) createWorkspace(c *gin.Context) {
	uploads, err := h.readImages(c, "images", "reference.bin", "至少上传一张商品参考图")
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	sessionID := strings.TrimSpace(c.PostForm("agent_session_id"))
	var sessionPtr *string
	if sessionID != "" {
		sessionPtr = &sessionID
	}
	created, err := h.Service.CreateAgentWorkspace(
		c.Request.Context(),
		c.PostForm("name"),
		c.PostForm("selection"),
		c.GetHeader("Idempotency-Key"),
		sessionPtr,
		uploads,
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

// getWorkspace 是 GET /api/v2/agent-product-workspaces/:conversation_id：200 快照；找不到 404。
func (h HTTP) getWorkspace(c *gin.Context) {
	out, err := h.Service.GetAgentWorkspace(c.Request.Context(), c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// finalizeWorkspaceIntake 是 POST .../intake：200 快照；同 key 不同哈希 409。
func (h HTTP) finalizeWorkspaceIntake(c *gin.Context) {
	uploads, err := h.readImages(c, "images", "reference.bin", "至少上传一张商品参考图")
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	note := strings.TrimSpace(c.PostForm("source_note"))
	var notePtr *string
	if note != "" {
		notePtr = &note
	}
	taskID := strings.TrimSpace(c.PostForm("task_id"))
	var taskPtr *string
	if taskID != "" {
		taskPtr = &taskID
	}
	out, err := h.Service.FinalizeAgentIntake(
		c.Request.Context(),
		c.Param("conversation_id"),
		c.PostForm("selection"),
		c.GetHeader("Idempotency-Key"),
		notePtr,
		uploads,
		taskPtr,
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// workspaceOptions 是 GET /api/v2/agent-product-workspaces/options：200 图种目录与数量上限。
func (h HTTP) workspaceOptions(c *gin.Context) {
	c.JSON(http.StatusOK, workspaceOptionsJSON())
}

// getFacts 是 GET /api/v3/products/:product_id/facts：200；v2 未写 fact 时 id 为 null。
func (h HTTP) getFacts(c *gin.Context) {
	out, err := h.Service.GetFacts(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// updateFacts 是 PUT /api/v3/products/:product_id/facts：200 新版本；体非法 400；expected 落后 409。
func (h HTTP) updateFacts(c *gin.Context) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	in, err := parseUpdateFacts(raw)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.UpdateFacts(c.Request.Context(), c.Param("product_id"), in)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// setCover 是 PUT /api/v2/products/:product_id/cover：200 详情；asset 非法 400。
func (h HTTP) setCover(c *gin.Context) {
	var payload struct {
		AssetID string `json:"asset_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.AssetID) == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	detail, err := h.Service.SetCover(c.Request.Context(), c.Param("product_id"), payload.AssetID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// clearCover 是 DELETE /api/v2/products/:product_id/cover：200 详情；只清封面不删资产。
func (h HTTP) clearCover(c *gin.Context) {
	detail, err := h.Service.ClearCover(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// addImages 是 POST /api/v2/products/:product_id/image-assets：201 新图列表。
func (h HTTP) addImages(c *gin.Context) {
	uploads, err := h.readImages(c, "images", "image.bin", "至少上传一张商品图片")
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	assets, err := h.Service.AddImages(c.Request.Context(), c.Param("product_id"), uploads)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, AssetListResponse{Items: serializeAssets(assets)})
}

// galleryBootstrap 是 GET /api/v2/products/:product_id/image-library：200 目录与文件夹。
func (h HTTP) galleryBootstrap(c *gin.Context) {
	out, err := h.Service.GalleryBootstrap(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// createFolder 是 POST /api/v2/products/:product_id/image-folders：201；体非法 400。
func (h HTTP) createFolder(c *gin.Context) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	out, err := h.Service.CreateGalleryFolder(c.Request.Context(), c.Param("product_id"), payload.Name)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// renameFolder 是 PATCH .../image-folders/:folder_id：200；expected_name 不匹配 409。
func (h HTTP) renameFolder(c *gin.Context) {
	var payload struct {
		ExpectedName string `json:"expected_name"`
		Name         string `json:"name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	out, err := h.Service.RenameGalleryFolder(c.Request.Context(), c.Param("product_id"), c.Param("folder_id"), payload.ExpectedName, payload.Name)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// deleteFolder 是 DELETE .../image-folders/:folder_id：200；资产移回未整理，不删 MediaObject。
func (h HTTP) deleteFolder(c *gin.Context) {
	out, err := h.Service.DeleteGalleryFolder(c.Request.Context(), c.Param("product_id"), c.Param("folder_id"), c.Query("expected_name"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// moveAssets 是 POST .../image-assets/move：200；expected_folder_id 不一致整批失败 409。
func (h HTTP) moveAssets(c *gin.Context) {
	var payload struct {
		Items []struct {
			AssetID          string  `json:"asset_id"`
			ExpectedFolderID *string `json:"expected_folder_id"`
		} `json:"items"`
		FolderID *string `json:"folder_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	moves := make([]GalleryAssetMove, 0, len(payload.Items))
	for _, item := range payload.Items {
		moves = append(moves, GalleryAssetMove{AssetID: item.AssetID, ExpectedFolderID: item.ExpectedFolderID})
	}
	out, err := h.Service.MoveGalleryAssets(c.Request.Context(), c.Param("product_id"), moves, payload.FolderID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// downloadArchive 是 POST .../image-assets/download-archive：200 application/zip。
func (h HTTP) downloadArchive(c *gin.Context) {
	var payload struct {
		AssetIDs []string `json:"asset_ids"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	archive, err := h.Service.BuildGalleryArchive(c.Request.Context(), c.Param("product_id"), payload.AssetIDs)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	defer os.Remove(archive.Path)
	c.Header("Content-Type", "application/zip")
	c.FileAttachment(archive.Path, archive.Filename)
}

// listGalleryAssets 是 GET .../image-assets：200 分页；cursor 与筛选不匹配 400。
func (h HTTP) listGalleryAssets(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	out, err := h.Service.ListGalleryAssets(c.Request.Context(), c.Param("product_id"), GalleryListInput{
		NodeID:        c.Query("node_id"),
		DirectoryKind: c.DefaultQuery("directory_kind", "all"),
		DirectoryKey:  c.Query("directory_key"),
		Query:         c.Query("q"),
		Sort:          c.DefaultQuery("sort", "created_desc"),
		After:         c.Query("after"),
		Limit:         limit,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// getGalleryAsset 是 GET .../image-assets/:asset_id：200 详情；找不到 404。
func (h HTTP) getGalleryAsset(c *gin.Context) {
	out, err := h.Service.GetGalleryAsset(c.Request.Context(), c.Param("product_id"), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// renameGalleryAsset 是 PATCH .../image-assets/:asset_id：200；expected_display_name 不匹配 409。
func (h HTTP) renameGalleryAsset(c *gin.Context) {
	var payload struct {
		ExpectedDisplayName string `json:"expected_display_name"`
		DisplayName         string `json:"display_name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	out, err := h.Service.RenameGalleryAsset(c.Request.Context(), c.Param("product_id"), c.Param("asset_id"), payload.ExpectedDisplayName, payload.DisplayName)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// deleteAsset 是 DELETE /api/v2/product-image-assets/:asset_id：204；仍被引用 409。
func (h HTTP) deleteAsset(c *gin.Context) {
	if err := h.Service.DeleteAsset(c.Request.Context(), c.Param("asset_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// deleteProduct 是 DELETE /api/v2/products/:product_id：204；有 running GraphRun 或共享视觉体系 409。
func (h HTTP) deleteProduct(c *gin.Context) {
	if err := h.Service.DeleteProduct(c.Request.Context(), c.Param("product_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// bindJSON 用 DisallowUnknownFields 解码 JSON。多字段或尾随内容一律 400「请求体无效」，不要改成忽略未知键。
func bindJSON(c *gin.Context, dest any) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return apperr.Validation("请求体无效")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return apperr.Validation("请求体无效")
	}
	if dec.More() {
		return apperr.Validation("请求体无效")
	}
	return nil
}

// readImages 从 multipart 字段读参考图，供 createV2/createV3/addImages/generateSourceNote 使用，不是独立路由。
// 数量超限或缺文件返回 Validation。不要把未校验字节写进 ProductImageAsset。
func (h HTTP) readImages(c *gin.Context, field, fallback, emptyDetail string) ([]Upload, error) {
	form, err := c.MultipartForm()
	if err != nil || form == nil {
		return nil, apperr.Validation(emptyDetail)
	}
	files := form.File[field]
	limits := h.limits(c.Request.Context())
	if err := limits.RejectReferenceCount(len(files)); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, apperr.Validation(emptyDetail)
	}
	out := make([]Upload, 0, len(files))
	for _, header := range files {
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
			filename = fallback
		}
		validated, err := media.ValidateUpload(filename, header.Header.Get("Content-Type"), content, limits)
		if err != nil {
			return nil, err
		}
		out = append(out, Upload{Content: validated.Content, Filename: validated.Filename, MIMEType: validated.MIMEType})
	}
	return out, nil
}

// limits 读运行时上传上限，供 readImages 校验；Settings 为 nil 或读取失败时用 media 默认。不是路由。
func (h HTTP) limits(ctx context.Context) media.Limits {
	if h.Settings == nil {
		return media.DefaultLimits()
	}
	limits, err := h.Settings.UploadLimits(ctx)
	if err != nil || limits.MaxImageBytes <= 0 {
		return media.DefaultLimits()
	}
	return limits
}

// parseUpdateFacts 用出现过的 JSON 键区分「未传」和「显式清空」。未知字段 extra=forbid，返回 Validation。
// Fields 必须原样交给 UpdateFacts，不要自己填默认空值。
func parseUpdateFacts(raw []byte) (UpdateFactsInput, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return UpdateFactsInput{}, apperr.Validation("请求体无效")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil || fields == nil {
		return UpdateFactsInput{}, apperr.Validation("请求体无效")
	}
	if dec.More() {
		return UpdateFactsInput{}, apperr.Validation("请求体无效")
	}
	allowed := map[string]struct{}{
		"expected_fact_set_version_id": {},
		"expected_fact_version":        {},
		"name":                         {},
		"category":                     {},
		"price":                        {},
		"source_note":                  {},
		"facts":                        {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
	}
	in := UpdateFactsInput{Fields: map[string]bool{}}
	for key := range fields {
		in.Fields[key] = true
	}
	if in.Fields["expected_fact_set_version_id"] || in.Fields["expected_fact_version"] {
		in.ExpectedVersionProvided = true
	}
	if rawID, ok := fields["expected_fact_set_version_id"]; ok && string(rawID) != "null" {
		var id string
		if err := json.Unmarshal(rawID, &id); err != nil {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		if utf8.RuneCountInString(id) < 1 || utf8.RuneCountInString(id) > 36 {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		in.ExpectedFactSetVersionID = &id
	}
	if rawVersion, ok := fields["expected_fact_version"]; ok && string(rawVersion) != "null" {
		var version int
		if err := json.Unmarshal(rawVersion, &version); err != nil {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		if version < 1 {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		in.ExpectedFactVersion = &version
	}
	if rawName, ok := fields["name"]; ok {
		var name string
		if err := json.Unmarshal(rawName, &name); err != nil && string(rawName) != "null" {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		if string(rawName) != "null" && utf8.RuneCountInString(name) > 255 {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		in.Name = &name
	}
	if rawCategory, ok := fields["category"]; ok {
		var category string
		if string(rawCategory) != "null" {
			if err := json.Unmarshal(rawCategory, &category); err != nil {
				return UpdateFactsInput{}, apperr.Validation("请求体无效")
			}
			if utf8.RuneCountInString(category) > 120 {
				return UpdateFactsInput{}, apperr.Validation("请求体无效")
			}
		}
		in.Category = &category
	}
	if rawPrice, ok := fields["price"]; ok {
		var price string
		if string(rawPrice) != "null" {
			if err := json.Unmarshal(rawPrice, &price); err != nil {
				return UpdateFactsInput{}, apperr.Validation("请求体无效")
			}
			if utf8.RuneCountInString(price) > 40 {
				return UpdateFactsInput{}, apperr.Validation("请求体无效")
			}
		}
		in.Price = &price
	}
	if rawNote, ok := fields["source_note"]; ok {
		var note string
		if string(rawNote) != "null" {
			if err := json.Unmarshal(rawNote, &note); err != nil {
				return UpdateFactsInput{}, apperr.Validation("请求体无效")
			}
			if utf8.RuneCountInString(note) > 4000 {
				return UpdateFactsInput{}, apperr.Validation("请求体无效")
			}
		}
		in.SourceNote = &note
	}
	if rawFacts, ok := fields["facts"]; ok && string(rawFacts) != "null" {
		facts, err := parseFactItems(rawFacts)
		if err != nil {
			return UpdateFactsInput{}, err
		}
		in.Facts = facts
	}
	return in, nil
}

// parseFactItems 要求每条同时有 key/value；可选枚举字段禁止 JSON null。
func parseFactItems(raw json.RawMessage) (*[]map[string]any, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, apperr.Validation("请求体无效")
	}
	allowed := map[string]struct{}{
		"key": {}, "value": {}, "source_type": {}, "status": {},
		"requires_confirmation": {}, "evidence_asset_ids": {}, "conflicts": {},
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		dec := json.NewDecoder(bytes.NewReader(item))
		dec.DisallowUnknownFields()
		var wire struct {
			Key                  string           `json:"key"`
			Value                json.RawMessage  `json:"value"`
			SourceType           *string          `json:"source_type"`
			Status               *string          `json:"status"`
			RequiresConfirmation *bool            `json:"requires_confirmation"`
			EvidenceAssetIDs     []string         `json:"evidence_asset_ids"`
			Conflicts            []map[string]any `json:"conflicts"`
		}
		if err := dec.Decode(&wire); err != nil {
			return nil, apperr.Validation("请求体无效")
		}
		if dec.More() {
			return nil, apperr.Validation("请求体无效")
		}
		var presence map[string]json.RawMessage
		if err := json.Unmarshal(item, &presence); err != nil || presence == nil {
			return nil, apperr.Validation("请求体无效")
		}
		for key := range presence {
			if _, ok := allowed[key]; !ok {
				return nil, apperr.Validation("请求体无效")
			}
		}
		if _, ok := presence["key"]; !ok {
			return nil, apperr.Validation("请求体无效")
		}
		if _, ok := presence["value"]; !ok {
			return nil, apperr.Validation("请求体无效")
		}
		for _, key := range []string{"source_type", "status", "requires_confirmation", "evidence_asset_ids", "conflicts"} {
			if raw, ok := presence[key]; ok && string(raw) == "null" {
				return nil, apperr.Validation("请求体无效")
			}
		}
		row := map[string]any{"key": wire.Key}
		if string(presence["value"]) == "null" {
			row["value"] = nil
		} else {
			var value any
			if err := json.Unmarshal(presence["value"], &value); err != nil {
				return nil, apperr.Validation("请求体无效")
			}
			row["value"] = value
		}
		if wire.SourceType != nil {
			if _, ok := factSourceTypes[*wire.SourceType]; !ok {
				return nil, apperr.Validation("请求体无效")
			}
			row["source_type"] = *wire.SourceType
		}
		if wire.Status != nil {
			if _, ok := factStatuses[*wire.Status]; !ok {
				return nil, apperr.Validation("请求体无效")
			}
			row["status"] = *wire.Status
		}
		if wire.RequiresConfirmation != nil {
			row["requires_confirmation"] = *wire.RequiresConfirmation
		}
		if _, ok := presence["evidence_asset_ids"]; ok {
			ids := make([]any, 0, len(wire.EvidenceAssetIDs))
			for _, id := range wire.EvidenceAssetIDs {
				ids = append(ids, id)
			}
			row["evidence_asset_ids"] = ids
		}
		if _, ok := presence["conflicts"]; ok {
			conflicts := make([]any, 0, len(wire.Conflicts))
			for _, conflict := range wire.Conflicts {
				conflicts = append(conflicts, conflict)
			}
			row["conflicts"] = conflicts
		}
		out = append(out, row)
	}
	return &out, nil
}

// parseQueryInt 读查询整数；缺省用 def。空字符串或越界是 400，不要静默夹紧。
func parseQueryInt(c *gin.Context, key string, def, min, max int) (int, error) {
	raw, present := c.GetQuery(key)
	if !present {
		return def, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, apperr.Validation("请求参数无效")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || (max > 0 && n > max) {
		return 0, apperr.Validation("请求参数无效")
	}
	return n, nil
}

// parseProductListSort 只接受 updated_desc / created_desc / name_asc；缺省 updated_desc。其它值 400。
func parseProductListSort(c *gin.Context) (string, error) {
	raw, present := c.GetQuery("sort")
	if !present {
		return "updated_desc", nil
	}
	switch raw {
	case "updated_desc", "created_desc", "name_asc":
		return raw, nil
	default:
		return "", apperr.Validation("请求参数无效")
	}
}
