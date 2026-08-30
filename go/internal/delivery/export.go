package delivery

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
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
		name             string
		rel              string
		expectedByteSize int
		expectedMIME     string
		expectedWidth    int
		expectedHeight   int
		expectedSHA      string
	}
	var files []fileItem
	var filename string
	var manifest map[string]any
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var productRow schema.Products
		if err := pgxTx.Where("id = ?", productID).Take(&productRow).Error; err != nil {
			return apperr.NotFound("商品不存在")
		}
		productName := productRow.Name
		safeProduct := safeName(productName, "product")
		missingItems := []map[string]any{}
		successItems := []map[string]any{}
		var finishedAt []time.Time
		var total int
		used := map[string]struct{}{}
		for index, jobID := range cleaned {
			row, err := loadJob(ctx, pgxTx, jobID)
			if err != nil {
				return apperr.NotFound("交付图任务不存在")
			}
			if row.ProductID != productID {
				return apperr.Conflict("交付图资产不属于当前商品")
			}
			if row.Status != "succeeded" || row.ResultAssetID == nil {
				missingItems = append(missingItems, map[string]any{
					"job_id": row.ID, "status": row.Status, "failure_reason": row.FailureReason,
				})
				continue
			}
			if row.FinishedAt == nil {
				return apperr.Conflict("交付图任务缺少完成时间")
			}
			asset, err := product.LoadAssetRow(ctx, pgxTx, *row.ResultAssetID)
			if err != nil {
				return apperr.Conflict("交付图结果媒体不可用")
			}
			if asset.ProductID != productID {
				return apperr.Conflict("交付图结果不属于当前商品")
			}
			source, err := product.LoadAssetRow(ctx, pgxTx, row.SourceAssetID)
			if err != nil {
				return apperr.Conflict("交付图原图不属于当前商品")
			}
			if source.ProductID != productID {
				return apperr.Conflict("交付图原图不属于当前商品")
			}
			_, meta, resultSHA, err := readVerifiedResultMedia(ctx, pgxTx, s.Media.Files, asset)
			if err != nil {
				return err
			}
			total += meta.ByteSize
			if total > exportMaxBytes {
				return apperr.Validation("交付导出图片总大小不能超过 512 MiB")
			}
			imageType := "image"
			if source.ImageTypeKey != nil && strings.TrimSpace(*source.ImageTypeKey) != "" {
				imageType = safeName(*source.ImageTypeKey, "image")
			}
			ext := media.ExtensionForMIME(meta.MIMEType)
			name := deduplicateFilename(exportImageFilename(safeProduct, imageType, index+1, meta.Width, meta.Height, ext), used)
			sourceSHA, err := loadMediaSHA256(ctx, pgxTx, source.MediaObjectID)
			if err != nil {
				return err
			}
			files = append(files, fileItem{
				name: name, rel: asset.StoragePath, expectedByteSize: meta.ByteSize,
				expectedMIME: meta.MIMEType, expectedWidth: meta.Width, expectedHeight: meta.Height,
				expectedSHA: meta.SHA256,
			})
			finishedAt = append(finishedAt, *row.FinishedAt)
			var spec any
			_ = json.Unmarshal(row.SpecJSON, &spec)
			// Python 把 _source_lineage 的 graph/run/node_run_id 展平到 items[]，不要再套一层 graph 对象。
			lineage := sourceLineage(ctx, pgxTx, source.ID, productID)
			successItems = append(successItems, map[string]any{
				"filename":     name,
				"product":      map[string]any{"id": productID, "name": productName},
				"graph":        lineage["graph"],
				"run":          lineage["run"],
				"node_run_id":  lineage["node_run_id"],
				"source_asset": assetMetadata(source, sourceSHA),
				"rendition_job": map[string]any{
					"id":                  row.ID,
					"status":              row.Status,
					"spec_schema_version": row.SpecSchemaVersion,
					"spec_hash":           row.SpecHash,
					"created_at":          row.CreatedAt,
					"started_at":          row.StartedAt,
					"finished_at":         row.FinishedAt,
					"updated_at":          row.UpdatedAt,
				},
				"result_asset":  assetMetadata(asset, resultSHA),
				"delivery_spec": spec,
				"measured": map[string]any{
					"mime_type": meta.MIMEType, "width": meta.Width, "height": meta.Height,
					"byte_size": meta.ByteSize, "sha256": meta.SHA256,
				},
				"generated_at": row.FinishedAt,
			})
		}
		if len(missingItems) > 0 && !allowPartial {
			return apperr.Conflict("存在未成功或缺少结果的交付图任务")
		}
		if len(successItems) == 0 {
			return apperr.Conflict("没有可导出的成功交付图")
		}
		generated := finishedAt[0]
		for _, ts := range finishedAt[1:] {
			if ts.After(generated) {
				generated = ts
			}
		}
		manifest = map[string]any{
			"schema_version": 1,
			"kind":           "productflow.delivery_export",
			"complete":       len(missingItems) == 0,
			"allow_partial":  allowPartial,
			"product":        map[string]any{"id": productID, "name": productName},
			"generated_at":   generated,
			"items":          successItems,
			"missing_items":  missingItems,
		}
		filename = safeProduct + "-delivery-export.zip"
		return nil
	})
	if err != nil {
		return ExportArchive{}, err
	}

	packed := make([]struct {
		name string
		data []byte
	}, 0, len(files))
	for _, f := range files {
		data, err := readExactBoundedFile(s.Media.Files, f.rel, f.expectedByteSize)
		if err != nil {
			return ExportArchive{}, apperr.Conflict("交付图结果文件在打包时发生变化")
		}
		meta, err := media.Inspect(data, f.expectedMIME)
		if err != nil {
			return ExportArchive{}, apperr.Conflict("交付图结果文件在打包时发生变化")
		}
		if meta.MIMEType != f.expectedMIME || meta.ByteSize != f.expectedByteSize || meta.Width != f.expectedWidth || meta.Height != f.expectedHeight || meta.SHA256 != f.expectedSHA {
			return ExportArchive{}, apperr.Conflict("交付图结果在打包时发生变化")
		}
		packed = append(packed, struct {
			name string
			data []byte
		}{name: f.name, data: data})
	}

	if manifest == nil {
		manifest = map[string]any{}
	}
	manifestBytes, err := canonjson.Compact(manifest)
	if err != nil {
		return ExportArchive{}, err
	}
	manifestBytes = append(manifestBytes, '\n')

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, 9)
	})
	if err := writeZipEntry(zw, "manifest.json", manifestBytes); err != nil {
		return ExportArchive{}, err
	}
	for _, f := range packed {
		if err := writeZipEntry(zw, f.name, f.data); err != nil {
			return ExportArchive{}, err
		}
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

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	var compressed bytes.Buffer
	fw, err := flate.NewWriter(&compressed, 9)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := fw.Close(); err != nil {
		return err
	}
	header := &zip.FileHeader{
		Name:               name,
		Method:             zip.Deflate,
		CreatorVersion:     (3 << 8) | 20,
		ReaderVersion:      20,
		ExternalAttrs:      0o600 << 16,
		ModifiedTime:       0,
		ModifiedDate:       0x0021, // 1980-01-01
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(compressed.Len()),
		UncompressedSize64: uint64(len(data)),
	}
	if zipNameNeedsUTF8(name) {
		header.Flags |= 0x800
	}
	w, err := zw.CreateRaw(header)
	if err != nil {
		return err
	}
	_, err = w.Write(compressed.Bytes())
	return err
}

func zipNameNeedsUTF8(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 {
			return true
		}
	}
	return false
}

func exportImageFilename(productName, imageType string, index, width, height int, ext string) string {
	return fmt.Sprintf("%s-%s-%02d-%dx%d%s", productName, imageType, index, width, height, ext)
}

func sourceLineage(ctx context.Context, tx *gorm.DB, sourceAssetID, productID string) map[string]any {
	var art schema.WorkflowGraphArtifacts
	err := tx.WithContext(ctx).Model(&schema.WorkflowGraphArtifacts{}).
		Select("workflow_graph_artifacts.*").
		Joins("JOIN workflow_graphs g ON g.id = workflow_graph_artifacts.graph_id").
		Where("workflow_graph_artifacts.product_image_asset_id = ? AND workflow_graph_artifacts.artifact_type = ? AND g.product_id = ?", sourceAssetID, "image", productID).
		Order("workflow_graph_artifacts.created_at DESC, workflow_graph_artifacts.id DESC").
		Take(&art).Error
	if err != nil {
		return map[string]any{"graph": nil, "run": nil, "node_run_id": nil}
	}
	var graph any
	graph = map[string]any{"id": art.GraphID, "revision": art.GraphRevision}
	var nodeRunID *string
	var run any
	if art.NodeRunID != nil {
		nodeRunID = art.NodeRunID
		var nodeRun schema.WorkflowGraphNodeRuns
		if err := tx.Where("id = ?", *art.NodeRunID).Take(&nodeRun).Error; err == nil {
			var graphRun schema.WorkflowGraphRuns
			if err := tx.Where("id = ?", nodeRun.GraphRunID).Take(&graphRun).Error; err == nil {
				run = map[string]any{"id": graphRun.ID, "revision": graphRun.GraphRevision}
			}
		}
	}
	return map[string]any{"graph": graph, "run": run, "node_run_id": nodeRunID}
}

func assetMetadata(asset product.ImageAsset, sha256 string) map[string]any {
	var sha any
	if sha256 != "" {
		sha = sha256
	}
	return map[string]any{
		"id":                asset.ID,
		"origin_type":       asset.OriginType,
		"image_type_key":    asset.ImageTypeKey,
		"display_name":      asset.DisplayName,
		"original_filename": asset.OriginalFilename,
		"parent_asset_id":   asset.ParentAssetID,
		"created_at":        asset.CreatedAt,
		"mime_type":         asset.MIMEType,
		"width":             asset.Width,
		"height":            asset.Height,
		"byte_size":         asset.ByteSize,
		"sha256":            sha,
	}
}

var errFileSizeChanged = errors.New("delivery result file size changed")

func readVerifiedResultMedia(ctx context.Context, q *gorm.DB, files storage.Local, asset product.ImageAsset) ([]byte, media.Verified, string, error) {
	if asset.VerificationStatus != media.StatusVerified {
		return nil, media.Verified{}, "", apperr.Conflict("交付图结果媒体不可用")
	}
	sha, err := loadMediaSHA256(ctx, q, asset.MediaObjectID)
	if err != nil {
		return nil, media.Verified{}, "", err
	}
	if asset.ByteSize == nil || asset.Width == nil || asset.Height == nil || sha == "" || strings.TrimSpace(asset.MIMEType) == "" {
		return nil, media.Verified{}, "", apperr.Conflict("交付图结果缺少核验元数据")
	}
	data, err := readExactBoundedFile(files, asset.StoragePath, *asset.ByteSize)
	if err != nil {
		if errors.Is(err, errFileSizeChanged) {
			return nil, media.Verified{}, "", apperr.Conflict("交付图结果文件大小已变化")
		}
		return nil, media.Verified{}, "", apperr.Conflict("交付图结果文件不可用")
	}
	meta, err := media.Inspect(data, asset.MIMEType)
	if err != nil {
		return nil, media.Verified{}, "", apperr.Conflict("交付图结果文件不可用")
	}
	if meta.MIMEType != asset.MIMEType || meta.ByteSize != *asset.ByteSize || meta.Width != *asset.Width || meta.Height != *asset.Height || meta.SHA256 != sha {
		return nil, media.Verified{}, "", apperr.Conflict("交付图结果核验元数据已变化")
	}
	return data, meta, sha, nil
}

func readExactBoundedFile(files storage.Local, rel string, expectedByteSize int) ([]byte, error) {
	abs, err := files.Resolve(rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, int64(expectedByteSize)+1))
	if err != nil {
		return nil, err
	}
	if len(content) != expectedByteSize {
		return nil, errFileSizeChanged
	}
	return content, nil
}

func deduplicateFilename(filename string, used map[string]struct{}) string {
	key := strings.ToLower(filename)
	if _, exists := used[key]; !exists {
		used[key] = struct{}{}
		return filename
	}
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	for serial := 2; ; serial++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, serial, ext)
		candidateKey := strings.ToLower(candidate)
		if _, exists := used[candidateKey]; !exists {
			used[candidateKey] = struct{}{}
			return candidate
		}
	}
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
