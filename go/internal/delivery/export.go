package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/mediaarchive"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

// ExportArchive 指向已写好的临时 ZIP，内部返回值，不是 HTTP JSON。
// 调用方（HTTP exportZip）必须 FileAttachment 之后删 Path。Filename 给 Content-Disposition。
type ExportArchive struct {
	Path     string
	Filename string
}

// Export 把已成功的交付结果打成 ZIP；allowPartial 为 false 时任一任务未成功即失败。
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
		mediaObjectID    string
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
		productRow, err := requireProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
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
				if apperr.IsNotFound(err) && err.Error() == auth.CrossMerchantDetail {
					return err
				}
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
			obj, err := s.Media.Get(ctx, pgxTx, asset.MediaObjectID)
			if err != nil {
				return mapExportRead(err)
			}
			if obj.VerificationStatus != media.StatusVerified {
				return apperr.Conflict("交付图结果媒体不可用")
			}
			if obj.ByteSize <= 0 || obj.Width <= 0 || obj.Height <= 0 || obj.SHA256 == "" || strings.TrimSpace(obj.MIMEType) == "" {
				return apperr.Conflict("交付图结果缺少核验元数据")
			}
			resultSHA := obj.SHA256
			total += obj.ByteSize
			if total > exportMaxBytes {
				return apperr.Validation("交付导出图片总大小不能超过 512 MiB")
			}
			imageType := "image"
			if source.ImageTypeKey != nil && strings.TrimSpace(*source.ImageTypeKey) != "" {
				imageType = safeName(*source.ImageTypeKey, "image")
			}
			ext := media.ExtensionForMIME(obj.MIMEType)
			name := deduplicateFilename(exportImageFilename(safeProduct, imageType, index+1, obj.Width, obj.Height, ext), used)
			sourceObj, err := s.Media.Get(ctx, pgxTx, source.MediaObjectID)
			if err != nil {
				return err
			}
			files = append(files, fileItem{
				name: name, mediaObjectID: asset.MediaObjectID, expectedByteSize: obj.ByteSize,
				expectedMIME: obj.MIMEType, expectedWidth: obj.Width, expectedHeight: obj.Height,
				expectedSHA: obj.SHA256,
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
				"source_asset": assetMetadata(source, sourceObj.SHA256),
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
					"mime_type": obj.MIMEType, "width": obj.Width, "height": obj.Height,
					"byte_size": obj.ByteSize, "sha256": obj.SHA256,
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

	if manifest == nil {
		manifest = map[string]any{}
	}
	manifestBytes, err := canonjson.Compact(manifest)
	if err != nil {
		return ExportArchive{}, err
	}
	manifestBytes = append(manifestBytes, '\n')

	w, err := mediaarchive.Begin(ctx, mediaarchive.Options{
		Filename: filename,
		Pattern:  "delivery-export-*.zip",
	})
	if err != nil {
		return ExportArchive{}, err
	}
	defer w.Abort()
	if err := w.Add(ctx, mediaarchive.File{Name: "manifest.json", Data: manifestBytes}); err != nil {
		return ExportArchive{}, err
	}
	for _, f := range files {
		content, err := s.Media.ReadVerified(ctx, s.DB, f.mediaObjectID)
		if err != nil {
			return ExportArchive{}, mapExportRead(err)
		}
		meta := content.Verified
		if meta.MIMEType != f.expectedMIME || meta.ByteSize != f.expectedByteSize || meta.Width != f.expectedWidth || meta.Height != f.expectedHeight || meta.SHA256 != f.expectedSHA {
			return ExportArchive{}, apperr.Conflict("交付图结果在打包时发生变化")
		}
		if err := w.Add(ctx, mediaarchive.File{Name: f.name, Data: content.Bytes}); err != nil {
			return ExportArchive{}, err
		}
	}
	out, err := w.Finish()
	if err != nil {
		return ExportArchive{}, err
	}
	return ExportArchive{Path: out.Path, Filename: out.Filename}, nil
}

func exportImageFilename(productName, imageType string, index, width, height int, ext string) string {
	return fmt.Sprintf("%s-%s-%02d-%dx%d%s", productName, imageType, index, width, height, ext)
}

// sourceLineage 查源图最近一条 image artifact 的 graph/run。查不到时 graph/run/node_run_id 均为 nil，不返回 error。
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

// assetMetadata 组装导出 sidecar。sha256 为空时写入 JSON null，避免空串冒充摘要。
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

func mapExportRead(err error) error {
	if apperr.IsNotFound(err) {
		return apperr.Conflict("交付图结果媒体不可用")
	}
	if re, ok := media.AsReadError(err); ok {
		switch re.Kind {
		case media.ReadNotVerified:
			if re.Field == media.FieldMetadata {
				return apperr.Conflict("交付图结果缺少核验元数据")
			}
			return apperr.Conflict("交付图结果媒体不可用")
		case media.ReadNotFound:
			return apperr.Conflict("交付图结果媒体不可用")
		case media.ReadMissingFile, media.ReadCorrupt, media.ReadIO:
			return apperr.Conflict("交付图结果文件不可用")
		case media.ReadIdentity:
			if re.Field == media.FieldByteSize {
				return apperr.Conflict("交付图结果文件大小已变化")
			}
			return apperr.Conflict("交付图结果核验元数据已变化")
		}
	}
	return err
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
