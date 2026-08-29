package product

import (
	"context"
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
	api.GET("/v2/products/:product_id", h.get)
	api.GET("/v2/product-image-assets/:asset_id/download", h.download)
	api.POST("/v3/products", h.createV3)
	api.GET("/v2/agent-product-workspaces/options", h.workspaceOptions)
	api.POST("/v2/agent-product-workspaces/drafts", h.createDraftWorkspace)
	api.POST("/v2/agent-product-workspaces", h.createWorkspace)
}

func (h HTTP) createV2(c *gin.Context) {
	uploads, err := h.readImages(c, "images", "reference.bin")
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
	uploads, err := h.readImages(c, "images", "reference.bin")
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
	created, err := h.Service.CreateDirect(c.Request.Context(), CreateInput{
		Name:       c.PostForm("name"),
		Category:   c.PostForm("category"),
		Price:      c.PostForm("price"),
		SourceNote: c.PostForm("source_note"),
		Uploads:    uploads,
	}, imageTypes, generationSpec, nil)
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
	uploads, err := h.readImages(c, "images", "reference.bin")
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

func (h HTTP) workspaceOptions(c *gin.Context) {
	c.JSON(http.StatusOK, workspaceOptionsJSON())
}

func (h HTTP) readImages(c *gin.Context, field, fallback string) ([]Upload, error) {
	form, err := c.MultipartForm()
	if err != nil || form == nil {
		return nil, apperr.Validation("至少上传一张商品参考图")
	}
	files := form.File[field]
	limits := h.limits(c.Request.Context())
	if err := limits.RejectReferenceCount(len(files)); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, apperr.Validation("至少上传一张商品参考图")
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
