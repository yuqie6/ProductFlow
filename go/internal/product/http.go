package product

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

type HTTP struct {
	Service  Service
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
}

// Register 挂上商品出生、facts、封面、图库与删除路由，全部走管理员 session。
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
	api.GET("/v3/products/:product_id/image-assets/:asset_id/fidelity-checks", h.listFidelityChecks)
	api.POST("/v3/products/:product_id/image-assets/:asset_id/fidelity-checks", h.createFidelityCheck)
	api.GET("/v2/product-image-assets/:asset_id/download", h.download)
	api.DELETE("/v2/product-image-assets/:asset_id", h.requireDeletion, h.deleteAsset)
	api.POST("/v3/products", h.createV3)
	api.GET("/v2/agent-product-workspaces/options", h.workspaceOptions)
	api.POST("/v2/agent-product-workspaces/drafts", h.createDraftWorkspace)
	api.POST("/v2/agent-product-workspaces", h.createWorkspace)
	api.GET("/v2/agent-product-workspaces/:conversation_id", h.getWorkspace)
	api.POST("/v2/agent-product-workspaces/:conversation_id/intake", h.finalizeWorkspaceIntake)
}

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
	deliverySpec, err := deliveryPresetSpec(c.PostForm("delivery_preset_key"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	created, err := h.Service.CreateDirect(c.Request.Context(), CreateInput{
		Name:       c.PostForm("name"),
		Category:   c.PostForm("category"),
		Price:      c.PostForm("price"),
		SourceNote: c.PostForm("source_note"),
		Uploads:    uploads,
	}, imageTypes, generationSpec, deliverySpec)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h HTTP) list(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	out, err := h.Service.List(c.Request.Context(), page, pageSize, c.Query("q"), c.DefaultQuery("sort", "updated_desc"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) get(c *gin.Context) {
	detail, err := h.Service.Get(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

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
	media.ServeVariant(
		c,
		h.Service.Media.Files,
		asset.StoragePath,
		asset.OriginalFilename,
		asset.MIMEType,
		c.DefaultQuery("variant", "original"),
		"商品图片文件不存在",
	)
}

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

func (h HTTP) getWorkspace(c *gin.Context) {
	out, err := h.Service.GetAgentWorkspace(c.Request.Context(), c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) workspaceOptions(c *gin.Context) {
	c.JSON(http.StatusOK, workspaceOptionsJSON())
}

func (h HTTP) getFacts(c *gin.Context) {
	out, err := h.Service.GetFacts(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) clearCover(c *gin.Context) {
	detail, err := h.Service.ClearCover(c.Request.Context(), c.Param("product_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

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

func (h HTTP) galleryBootstrap(c *gin.Context) {
	out, err := h.Service.GalleryBootstrap(c.Request.Context(), c.Param("product_id"))
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

func (h HTTP) deleteFolder(c *gin.Context) {
	out, err := h.Service.DeleteGalleryFolder(c.Request.Context(), c.Param("product_id"), c.Param("folder_id"), c.Query("expected_name"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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
	c.Header("Content-Disposition", "attachment; filename="+strconv.Quote(archive.Filename))
	c.Data(http.StatusOK, "application/zip", archive.Bytes)
}

func (h HTTP) listGalleryAssets(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	out, err := h.Service.ListGalleryAssets(c.Request.Context(), c.Param("product_id"), GalleryListInput{
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

func (h HTTP) getGalleryAsset(c *gin.Context) {
	out, err := h.Service.GetGalleryAsset(c.Request.Context(), c.Param("product_id"), c.Param("asset_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) deleteAsset(c *gin.Context) {
	if err := h.Service.DeleteAsset(c.Request.Context(), c.Param("asset_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h HTTP) deleteProduct(c *gin.Context) {
	if err := h.Service.DeleteProduct(c.Request.Context(), c.Param("product_id")); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h HTTP) listFidelityChecks(c *gin.Context) {
	limit := fidelityCheckMaxLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
			return
		}
		limit = n
	}
	out, err := h.Service.ListFidelityChecks(c.Request.Context(), c.Param("product_id"), c.Param("asset_id"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) createFidelityCheck(c *gin.Context) {
	var payload struct {
		ExpectedLatestVersion *int    `json:"expected_latest_version"`
		IdempotencyKey        *string `json:"idempotency_key"`
		ShapeFidelity         *string `json:"shape_fidelity"`
		ColorMaterialFidelity *string `json:"color_material_fidelity"`
		LogoTextLegibility    *string `json:"logo_text_legibility"`
		TextPolicyCompliance  *string `json:"text_policy_compliance"`
		Notes                 *string `json:"notes"`
	}
	if err := bindJSON(c, &payload); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if payload.ExpectedLatestVersion == nil || payload.IdempotencyKey == nil || payload.ShapeFidelity == nil ||
		payload.ColorMaterialFidelity == nil || payload.LogoTextLegibility == nil || payload.TextPolicyCompliance == nil {
		httpx.AbortDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	out, err := h.Service.CreateFidelityCheck(c.Request.Context(), c.Param("product_id"), c.Param("asset_id"), CreateFidelityInput{
		ExpectedLatestVersion: *payload.ExpectedLatestVersion,
		IdempotencyKey:        *payload.IdempotencyKey,
		ShapeFidelity:         *payload.ShapeFidelity,
		ColorMaterialFidelity: *payload.ColorMaterialFidelity,
		LogoTextLegibility:    *payload.LogoTextLegibility,
		TextPolicyCompliance:  *payload.TextPolicyCompliance,
		Notes:                 payload.Notes,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

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

func parseUpdateFacts(raw []byte) (UpdateFactsInput, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return UpdateFactsInput{}, apperr.Validation("请求体无效")
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
		in.ExpectedFactSetVersionID = &id
	}
	if rawVersion, ok := fields["expected_fact_version"]; ok && string(rawVersion) != "null" {
		var version int
		if err := json.Unmarshal(rawVersion, &version); err != nil {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		in.ExpectedFactVersion = &version
	}
	if rawName, ok := fields["name"]; ok {
		var name string
		if err := json.Unmarshal(rawName, &name); err != nil && string(rawName) != "null" {
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
		}
		in.Category = &category
	}
	if rawPrice, ok := fields["price"]; ok {
		var price string
		if string(rawPrice) != "null" {
			if err := json.Unmarshal(rawPrice, &price); err != nil {
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
		}
		in.SourceNote = &note
	}
	if rawFacts, ok := fields["facts"]; ok && string(rawFacts) != "null" {
		var facts []map[string]any
		if err := json.Unmarshal(rawFacts, &facts); err != nil {
			return UpdateFactsInput{}, apperr.Validation("请求体无效")
		}
		in.Facts = &facts
	}
	return in, nil
}
