package delivery

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
		name string
		data []byte
	}
	var files []fileItem
	var filename string
	var manifest map[string]any
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var productName string
		if err := pfdb.QueryRow(ctx, pgxTx, `SELECT name FROM products WHERE id = $1`, productID).Scan(&productName); err != nil {
			return apperr.NotFound("商品不存在")
		}
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
			imageType := "image"
			if source.ImageTypeKey != nil && strings.TrimSpace(*source.ImageTypeKey) != "" {
				imageType = safeName(*source.ImageTypeKey, "image")
			}
			ext := media.ExtensionForMIME(meta.MIMEType)
			name := exportImageFilename(safeProduct, imageType, index+1, meta.Width, meta.Height, ext)
			for {
				if _, exists := used[strings.ToLower(name)]; !exists {
					break
				}
				name = strings.TrimSuffix(name, ext) + "-2" + ext
			}
			used[strings.ToLower(name)] = struct{}{}
			sum := sha256.Sum256(data)
			files = append(files, fileItem{name: name, data: data})
			finishedAt = append(finishedAt, *row.FinishedAt)
			var spec any
			_ = json.Unmarshal(row.SpecJSON, &spec)
			// Python 把 _source_lineage 的 graph/run/node_run_id 展平到 items[]，不要再套一层 graph 对象。
			lineage := sourceLineage(ctx, pgxTx, source.ID, productID)
			successItems = append(successItems, map[string]any{
				"filename":    name,
				"product":     map[string]any{"id": productID, "name": productName},
				"graph":       lineage["graph"],
				"run":         lineage["run"],
				"node_run_id": lineage["node_run_id"],
				"source_asset": map[string]any{
					"id": source.ID, "image_type_key": source.ImageTypeKey, "original_filename": source.OriginalFilename,
				},
				"rendition_job": map[string]any{
					"id": row.ID, "status": row.Status, "spec_hash": row.SpecHash,
					"created_at": row.CreatedAt, "started_at": row.StartedAt, "finished_at": row.FinishedAt, "updated_at": row.UpdatedAt,
				},
				"result_asset":  map[string]any{"id": asset.ID, "original_filename": asset.OriginalFilename},
				"delivery_spec": spec,
				"measured": map[string]any{
					"mime_type": meta.MIMEType, "width": meta.Width, "height": meta.Height,
					"byte_size": meta.ByteSize, "sha256": hex.EncodeToString(sum[:]),
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
	if manifest == nil {
		manifest = map[string]any{}
	}
	manifestBytes, _ := json.Marshal(manifest)
	mh := &zip.FileHeader{Name: "manifest.json", Method: zip.Deflate}
	mh.SetModTime(epoch)
	mw, err := zw.CreateHeader(mh)
	if err != nil {
		return ExportArchive{}, err
	}
	if _, err := mw.Write(manifestBytes); err != nil {
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

func exportImageFilename(productName, imageType string, index, width, height int, ext string) string {
	return fmt.Sprintf("%s-%s-%02d-%dx%d%s", productName, imageType, index, width, height, ext)
}

func sourceLineage(ctx context.Context, tx *gorm.DB, sourceAssetID, productID string) map[string]any {
	var graphID *string
	var graphRev *int
	var nodeRunID *string
	var runID *string
	var runRev *int
	err := pfdb.QueryRow(ctx, tx, `
		SELECT a.graph_id, a.graph_revision, a.node_run_id, r.id, r.graph_revision
		FROM workflow_graph_artifacts a
		JOIN workflow_graphs g ON g.id = a.graph_id
		LEFT JOIN workflow_graph_node_runs nr ON nr.id = a.node_run_id
		LEFT JOIN workflow_graph_runs r ON r.id = nr.graph_run_id
		WHERE a.product_image_asset_id = $1 AND a.artifact_type = 'image' AND g.product_id = $2
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT 1
	`, sourceAssetID, productID).Scan(&graphID, &graphRev, &nodeRunID, &runID, &runRev)
	if err != nil {
		return map[string]any{"graph": nil, "run": nil, "node_run_id": nil}
	}
	var graph any
	if graphID != nil {
		graph = map[string]any{"id": *graphID, "revision": graphRev}
	}
	var run any
	if runID != nil {
		run = map[string]any{"id": *runID, "revision": runRev}
	}
	return map[string]any{"graph": graph, "run": run, "node_run_id": nodeRunID}
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
