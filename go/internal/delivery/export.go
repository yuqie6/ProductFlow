package delivery

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
)

type ExportArchive struct {
	Path     string
	Filename string
}

func (s Service) Export(ctx context.Context, productID string, jobIDs []string, allowPartial bool) (ExportArchive, error) {
	if len(jobIDs) < 1 || len(jobIDs) > exportMaxJobs {
		return ExportArchive{}, apperr.Validationf("一次最多导出 %d 个交付图任务", exportMaxJobs)
	}
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(jobIDs))
	for _, id := range jobIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return ExportArchive{}, apperr.Validation("交付图任务 ID 不能为空")
		}
		if _, ok := seen[id]; ok {
			return ExportArchive{}, apperr.Validation("交付导出不能包含重复任务 ID")
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}

	type fileItem struct {
		name string
		data []byte
	}
	var files []fileItem
	var filename string
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		var productName string
		if err := pgxTx.QueryRow(ctx, `SELECT name FROM products WHERE id = $1`, productID).Scan(&productName); err != nil {
			return apperr.NotFound("商品不存在")
		}
		var missing, success int
		var total int
		used := map[string]int{}
		for _, jobID := range cleaned {
			row, err := loadJob(ctx, pgxTx, jobID)
			if err != nil {
				return apperr.NotFound("交付图任务不存在")
			}
			if row.ProductID != productID {
				return apperr.Conflict("交付图资产不属于当前商品")
			}
			if row.Status != "succeeded" || row.ResultAssetID == nil {
				missing++
				if !allowPartial {
					return apperr.Conflict("存在未成功或缺少结果的交付图任务")
				}
				continue
			}
			asset, err := product.LoadAssetRow(ctx, pgxTx, *row.ResultAssetID)
			if err != nil {
				return apperr.Conflict("交付图结果媒体不可用")
			}
			if asset.ProductID != productID {
				return apperr.Conflict("交付图结果不属于当前商品")
			}
			data, err := readStorage(s.Media.Files, asset.StoragePath)
			if err != nil {
				return apperr.Conflict("交付图结果文件不可用")
			}
			meta, err := media.Inspect(data, asset.MIMEType)
			if err != nil {
				return apperr.Conflict("交付图结果核验元数据已变化")
			}
			if asset.ByteSize != nil && meta.ByteSize != *asset.ByteSize {
				return apperr.Conflict("交付图结果文件大小已变化")
			}
			total += meta.ByteSize
			if total > exportMaxBytes {
				return apperr.Validation("交付导出图片总大小不能超过 512 MiB")
			}
			ext := filepath.Ext(asset.OriginalFilename)
			if ext == "" {
				ext = media.ExtensionForMIME(asset.MIMEType)
			}
			base := safeName(strings.TrimSuffix(asset.OriginalFilename, filepath.Ext(asset.OriginalFilename)), "rendition")
			name := base + ext
			if n := used[strings.ToLower(name)]; n > 0 {
				name = base + "-" + itoaCount(n+1) + ext
			}
			used[strings.ToLower(name)]++
			if used[strings.ToLower(name)] == 1 {
				used[strings.ToLower(name)] = 1
			}
			files = append(files, fileItem{name: name, data: data})
			success++
		}
		if success == 0 {
			return apperr.Conflict("没有可导出的成功交付图")
		}
		filename = safeName(productName, "product") + "-delivery.zip"
		return nil
	})
	if err != nil {
		return ExportArchive{}, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	epoch := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, f := range files {
		header := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		header.SetModTime(epoch)
		w, err := zw.CreateHeader(header)
		if err != nil {
			return ExportArchive{}, err
		}
		if _, err := w.Write(f.data); err != nil {
			return ExportArchive{}, err
		}
	}
	manifest, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"product_id":     productID,
		"files":          len(files),
	})
	mh := &zip.FileHeader{Name: "manifest.json", Method: zip.Deflate}
	mh.SetModTime(epoch)
	mw, err := zw.CreateHeader(mh)
	if err != nil {
		return ExportArchive{}, err
	}
	if _, err := mw.Write(manifest); err != nil {
		return ExportArchive{}, err
	}
	if err := zw.Close(); err != nil {
		return ExportArchive{}, err
	}
	tmp, err := os.CreateTemp("", "delivery-export-*.zip")
	if err != nil {
		return ExportArchive{}, err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return ExportArchive{}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return ExportArchive{}, err
	}
	return ExportArchive{Path: tmp.Name(), Filename: filename}, nil
}

func safeName(value, fallback string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	runes := []rune(out)
	if len(runes) > 80 {
		out = string(runes[:80])
	}
	return out
}

func itoaCount(n int) string {
	if n <= 1 {
		return "1"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
