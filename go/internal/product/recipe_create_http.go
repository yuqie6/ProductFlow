package product

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

// POST /api/v3/products/from-recipe confirms a read-only recipe preview and creates its product atomically.
func (h HTTP) createFromRecipe(c *gin.Context) {
	uploads, err := h.readImages(c, "images", "reference.bin", "至少上传一张商品参考图")
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	version, err := strconv.Atoi(c.PostForm("expected_recipe_version"))
	if err != nil || version < 1 {
		httpx.AbortErr(c, apperr.Validation("配方版本无效"))
		return
	}
	out, err := h.Service.CreateFromRecipe(c.Request.Context(), RecipeCreateInput{
		Product:  CreateInput{Name: c.PostForm("name"), SourceNote: c.PostForm("source_note"), Uploads: uploads},
		RecipeID: c.PostForm("recipe_id"), ExpectedRecipeVersion: version,
		PreviewDigest: c.PostForm("preview_digest"), IdempotencyKey: c.GetHeader("Idempotency-Key"),
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	status := http.StatusCreated
	if !out.Created {
		status = http.StatusOK
	}
	c.JSON(status, out)
}
