package media

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
)

func ServeVariant(
	c *gin.Context,
	files storage.Local,
	storagePath string,
	originalFilename string,
	mimeType string,
	variantRaw string,
	missingDetail string,
) {
	variant, err := storage.ParseVariant(variantRaw)
	if err != nil {
		httpx.AbortDetail(c, http.StatusBadRequest, err.Error())
		return
	}
	resolved, err := files.ResolveForVariant(storagePath, mimeType, variant)
	if err != nil {
		httpx.AbortDetail(c, http.StatusNotFound, missingDetail)
		return
	}
	if _, err := os.Stat(resolved.AbsPath); err != nil {
		httpx.AbortDetail(c, http.StatusNotFound, missingDetail)
		return
	}
	filename := storage.VariantDownloadName(originalFilename, variant, filepath.Ext(resolved.AbsPath))
	c.Header("Content-Type", resolved.MediaType)
	c.Header("Content-Disposition", `attachment; filename="`+url.PathEscape(filename)+`"`)
	c.File(resolved.AbsPath)
}
