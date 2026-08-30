package media

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"gorm.io/gorm"
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

func OriginalMissing(files storage.Local, storagePath string) bool {
	abs, err := files.Resolve(storagePath)
	if err != nil {
		return true
	}
	_, err = os.Stat(abs)
	return err != nil
}

func ServeExistingVariant(
	c *gin.Context,
	store Store,
	db *gorm.DB,
	storagePath string,
	originalFilename string,
	mimeType string,
	variantRaw string,
	missingDetail string,
) {
	if OriginalMissing(store.Files, storagePath) {
		store.MarkMissingByStoragePath(c.Request.Context(), db, storagePath)
		httpx.AbortDetail(c, http.StatusNotFound, missingDetail)
		return
	}
	ServeVariant(c, store.Files, storagePath, originalFilename, mimeType, variantRaw, missingDetail)
}
