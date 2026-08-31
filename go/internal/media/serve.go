package media

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"gorm.io/gorm"
)

// ServeVariant 按 variant 查询参数写出文件。未知变体 400；路径或文件缺失 404 且文案为 missingDetail。
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

// OriginalMissing 报告原图相对路径是否解析失败或不存在于磁盘。
// true 表示该当 404 并把 MediaObject 标 missing。路径越界或空串也当 missing。
// 不要用它判断 preview/thumbnail 变体是否生成过。
func OriginalMissing(files storage.Local, storagePath string) bool {
	abs, err := files.Resolve(storagePath)
	if err != nil {
		return true
	}
	_, err = os.Stat(abs)
	return errors.Is(err, os.ErrNotExist)
}

// ServeExistingVariant 在原图缺失时把 MediaObject 标 missing 再 404，否则调用 [ServeVariant]。
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
