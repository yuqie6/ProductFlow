package recipe

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

type HTTP struct {
	Service  Service
	Settings interface {
		settings.RuntimeReader
	}
}

// Register 挂上工作流配方提取 / 预览 / 应用。应用走 Graph Command，actor_type=recipe。
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
	v3 := engine.Group("/api/v3", admin)
	v3.GET("/workflow-recipes", h.list)
	v3.GET("/workflow-recipes/:recipe_id", h.get)
	v3.DELETE("/workflow-recipes/:recipe_id", h.archive)
	v3.POST("/products/:product_id/workflows/:workflow_id/recipes", h.create)
	v3.POST("/products/:product_id/workflows/:workflow_id/recipes/:recipe_id/versions", h.append)
	v3.POST("/products/:product_id/workflow-recipes/:recipe_id/preview", h.preview)
	v3.POST("/products/:product_id/workflow-recipes/:recipe_id/apply", h.apply)
}

func (h HTTP) list(c *gin.Context) {
	includeArchived, err := queryBool(c, "include_archived", false)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.List(c.Request.Context(), includeArchived)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) get(c *gin.Context) {
	out, err := h.Service.Get(c.Request.Context(), c.Param("recipe_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) create(c *gin.Context) {
	in, _, err := bindCreate(c, false)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	in.ProductID = c.Param("product_id")
	in.WorkflowID = c.Param("workflow_id")
	out, err := h.Service.Create(c.Request.Context(), in)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) append(c *gin.Context) {
	in, version, err := bindCreate(c, true)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	in.ProductID = c.Param("product_id")
	in.WorkflowID = c.Param("workflow_id")
	out, err := h.Service.Append(c.Request.Context(), AppendInput{
		CreateInput:           in,
		RecipeID:              c.Param("recipe_id"),
		ExpectedRecipeVersion: version,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h HTTP) preview(c *gin.Context) {
	var body struct {
		ExpectedRecipeVersion *int `json:"expected_recipe_version"`
	}
	if err := bindJSON(c, &body); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if body.ExpectedRecipeVersion == nil || *body.ExpectedRecipeVersion < 1 {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	out, err := h.Service.Preview(c.Request.Context(), c.Param("product_id"), c.Param("recipe_id"), *body.ExpectedRecipeVersion)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, serializePreview(out))
}

func (h HTTP) apply(c *gin.Context) {
	var body struct {
		ExpectedRecipeVersion *int    `json:"expected_recipe_version"`
		ExpectedGraphRevision *int    `json:"expected_graph_revision"`
		PreviewDigest         *string `json:"preview_digest"`
		IdempotencyKey        *string `json:"idempotency_key"`
	}
	if err := bindJSON(c, &body); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if body.ExpectedRecipeVersion == nil || *body.ExpectedRecipeVersion < 1 {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	if body.ExpectedGraphRevision == nil || *body.ExpectedGraphRevision < 0 {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	if body.PreviewDigest == nil || body.IdempotencyKey == nil {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	out, err := h.Service.Apply(c.Request.Context(), ApplyInput{
		ProductID:             c.Param("product_id"),
		RecipeID:              c.Param("recipe_id"),
		ExpectedRecipeVersion: *body.ExpectedRecipeVersion,
		ExpectedGraphRevision: *body.ExpectedGraphRevision,
		PreviewDigest:         *body.PreviewDigest,
		IdempotencyKey:        *body.IdempotencyKey,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, serializeApplication(out))
}

func (h HTTP) archive(c *gin.Context) {
	raw := strings.TrimSpace(c.Query("expected_recipe_version"))
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		httpx.AbortErr(c, apperr.Validation("请求体无效"))
		return
	}
	out, err := h.Service.Archive(c.Request.Context(), c.Param("recipe_id"), n)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

type createBody struct {
	SourceType                     string    `json:"source_type"`
	GroupID                        *string   `json:"group_id"`
	NodeIDs                        *[]string `json:"node_ids"`
	ExpectedGraphRevision          *int      `json:"expected_graph_revision"`
	Title                          string    `json:"title"`
	Description                    *string   `json:"description"`
	PreferredVisualSystemVersionID *string   `json:"preferred_visual_system_version_id"`
	ExpectedRecipeVersion          *int      `json:"expected_recipe_version"`
}

func bindCreate(c *gin.Context, appendVersion bool) (CreateInput, int, error) {
	var body createBody
	if err := bindJSON(c, &body); err != nil {
		return CreateInput{}, 0, err
	}
	expectedRecipeVersion := 0
	if appendVersion {
		if body.ExpectedRecipeVersion == nil || *body.ExpectedRecipeVersion < 1 {
			return CreateInput{}, 0, apperr.Validation("请求体无效")
		}
		expectedRecipeVersion = *body.ExpectedRecipeVersion
	} else if body.ExpectedRecipeVersion != nil {
		return CreateInput{}, 0, apperr.Validation("请求体无效")
	}
	if body.ExpectedGraphRevision == nil || *body.ExpectedGraphRevision < 0 {
		return CreateInput{}, 0, apperr.Validation("请求体无效")
	}
	if strings.TrimSpace(body.Title) == "" || runeLen(body.Title) > 255 {
		return CreateInput{}, 0, apperr.Validation("请求体无效")
	}
	if body.Description != nil && runeLen(*body.Description) > 4000 {
		return CreateInput{}, 0, apperr.Validation("请求体无效")
	}
	if body.GroupID != nil && strings.TrimSpace(*body.GroupID) == "" {
		return CreateInput{}, 0, apperr.Validation("请求体无效")
	}
	nodeIDs := []string{}
	if body.NodeIDs != nil {
		nodeIDs = *body.NodeIDs
	}
	in := CreateInput{
		SourceType:                     body.SourceType,
		GroupID:                        body.GroupID,
		NodeIDs:                        nodeIDs,
		ExpectedGraphRevision:          *body.ExpectedGraphRevision,
		Title:                          body.Title,
		Description:                    body.Description,
		PreferredVisualSystemVersionID: body.PreferredVisualSystemVersionID,
	}
	if err := validateSourceFields(ExtractInput{SourceType: in.SourceType, GroupID: in.GroupID, NodeIDs: in.NodeIDs}); err != nil {
		return CreateInput{}, 0, err
	}
	return in, expectedRecipeVersion, nil
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

func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
