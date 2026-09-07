package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/mediaarchive"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

// CreateAdoption 持久化新的不可变交付采用版本，并把商品当前指针切到该版本。
// 文稿 O1–O7 候选采用不走此路径。quality_status=fail 的图位不得进入合格集，整次创建拒绝。
func (s Service) CreateAdoption(ctx context.Context, productID string, req CreateAdoptionRequest) (AdoptionVersionResponse, error) {
	if len(req.Slots) < 1 {
		return AdoptionVersionResponse{}, apperr.Validation("交付采用至少需要一个图位")
	}
	if len(req.Slots) > exportMaxJobs {
		return AdoptionVersionResponse{}, apperr.Validationf("一次最多采用 %d 个图位", exportMaxJobs)
	}

	prepared, err := prepareAdoptionSlots(req.Slots)
	if err != nil {
		return AdoptionVersionResponse{}, err
	}

	var versionID string
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		prod, err := product.Lock(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		assetIDs := make([]string, 0, len(prepared))
		for _, slot := range prepared {
			assetIDs = append(assetIDs, slot.sourceAssetID)
		}
		if err := product.AssetIDsExist(ctx, pgxTx, productID, assetIDs); err != nil {
			return err
		}
		if err := validateOptionalSourceVersions(ctx, pgxTx, productID, req); err != nil {
			return err
		}

		var maxVersion int
		if err := pgxTx.WithContext(ctx).Model(&schema.DeliveryAdoptionVersions{}).
			Select("COALESCE(MAX(version), 0)").
			Where("product_id = ?", productID).
			Scan(&maxVersion).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		versionID = clockid.New()
		row := schema.DeliveryAdoptionVersions{
			ID: versionID, ProductID: productID, Version: maxVersion + 1,
			GraphID: req.GraphID, GraphRevision: req.GraphRevision,
			FactSetVersionID: req.FactSetVersionID, VisualSystemVersionID: req.VisualSystemVersionID,
			Notes: trimPtr(req.Notes), CreatedAt: now,
		}
		if err := pgxTx.Create(&row).Error; err != nil {
			return err
		}
		for _, slot := range prepared {
			slotRow := schema.DeliveryAdoptionSlots{
				ID: clockid.New(), VersionID: versionID, SlotKey: slot.slotKey,
				SortOrder: slot.sortOrder, ImageTypeKey: slot.imageTypeKey,
				SourceAssetID: slot.sourceAssetID, SourceNodeID: slot.sourceNodeID,
				DeliverySpecJSON: slot.specJSON, DeliverySpecHash: slot.specHash,
				QualityStatus: slot.qualityStatus, QualityDetail: slot.qualityDetail,
				TextOverflow: slot.textOverflow, CreatedAt: now,
			}
			if err := pgxTx.Create(&slotRow).Error; err != nil {
				return err
			}
		}
		if err := pgxTx.Model(&schema.Products{}).Where("id = ?", prod.ID).Updates(map[string]any{
			"current_delivery_adoption_version_id": versionID,
			"updated_at":                           now,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return AdoptionVersionResponse{}, err
	}
	return s.GetAdoption(ctx, productID, versionID)
}

// ListAdoptions 按版本倒序列出交付采用摘要。
func (s Service) ListAdoptions(ctx context.Context, productID string) (AdoptionListResponse, error) {
	var out AdoptionListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		productRow, err := requireProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		out.CurrentVersionID = productRow.CurrentDeliveryAdoptionVersionID
		var versions []schema.DeliveryAdoptionVersions
		if err := pgxTx.Where("product_id = ?", productID).
			Order("version DESC, id DESC").Find(&versions).Error; err != nil {
			return err
		}
		items := make([]AdoptionVersionSummary, 0, len(versions))
		for _, v := range versions {
			var count int64
			if err := pgxTx.Model(&schema.DeliveryAdoptionSlots{}).
				Where("version_id = ?", v.ID).Count(&count).Error; err != nil {
				return err
			}
			items = append(items, AdoptionVersionSummary{
				ID: v.ID, ProductID: v.ProductID, Version: v.Version,
				IsCurrent: out.CurrentVersionID != nil && *out.CurrentVersionID == v.ID,
				SlotCount: int(count), CreatedAt: v.CreatedAt,
			})
		}
		out.Items = items
		return nil
	})
	return out, err
}

// GetAdoption 读取指定版本；versionID 为 "current" 时读商品当前指针。
func (s Service) GetAdoption(ctx context.Context, productID, versionID string) (AdoptionVersionResponse, error) {
	var out AdoptionVersionResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		version, currentID, err := loadAdoptionVersion(ctx, pgxTx, productID, versionID)
		if err != nil {
			return err
		}
		slots, err := loadAdoptionSlots(ctx, pgxTx, version.ID)
		if err != nil {
			return err
		}
		out, err = serializeAdoption(version, slots, currentID)
		return err
	})
	return out, err
}

// EnsureAdoptionRenditions 按快照中的源图与 DeliverySpec 提交/复用既有派生任务，不调用图片模型。
func (s Service) EnsureAdoptionRenditions(ctx context.Context, productID, versionID string, qualifiedOnly bool) (AdoptionRenditionsResponse, error) {
	version, err := s.GetAdoption(ctx, productID, versionID)
	if err != nil {
		return AdoptionRenditionsResponse{}, err
	}
	for _, slot := range version.Slots {
		if qualifiedOnly && !slot.Qualified {
			continue
		}
		payload := specPayload(slot.DeliverySpec)
		if _, err := s.Submit(ctx, slot.SourceAssetID, payload); err != nil {
			return AdoptionRenditionsResponse{}, err
		}
	}
	preview, err := s.PreviewAdoption(ctx, productID, version.ID, AdoptionExportRequest{
		AllowPartial: true, QualifiedOnly: qualifiedOnly,
	})
	if err != nil {
		return AdoptionRenditionsResponse{}, err
	}
	return AdoptionRenditionsResponse{Preview: preview}, nil
}

// PreviewAdoption 用与导出相同的快照与命名规则生成预览；不写 ZIP。
func (s Service) PreviewAdoption(ctx context.Context, productID, versionID string, req AdoptionExportRequest) (AdoptionPreviewResponse, error) {
	var out AdoptionPreviewResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		version, _, err := loadAdoptionVersion(ctx, pgxTx, productID, versionID)
		if err != nil {
			return err
		}
		slots, err := loadAdoptionSlots(ctx, pgxTx, version.ID)
		if err != nil {
			return err
		}
		productName, err := loadProductName(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		out = buildAdoptionPreview(ctx, pgxTx, productID, productName, version.ID, slots, req)
		return nil
	})
	return out, err
}

// ExportAdoption 按已采用快照导出 ZIP；预览与下载共用同一文件计划。复用既有派生结果，不另建执行器。
func (s Service) ExportAdoption(ctx context.Context, productID, versionID string, req AdoptionExportRequest) (ExportArchive, error) {
	preview, err := s.PreviewAdoption(ctx, productID, versionID, req)
	if err != nil {
		return ExportArchive{}, err
	}
	blocking := make([]AdoptionIssue, 0)
	for _, issue := range preview.Issues {
		if issue.Code == "rendition_pending" || issue.Code == "rendition_failed" {
			if !req.AllowPartial {
				blocking = append(blocking, issue)
			}
			continue
		}
		blocking = append(blocking, issue)
	}
	if len(blocking) > 0 && !req.AllowPartial {
		return ExportArchive{}, apperr.Conflict(formatAdoptionIssues(blocking))
	}

	type fileItem struct {
		name             string
		mediaObjectID    string
		expectedByteSize int
		expectedMIME     string
		expectedWidth    int
		expectedHeight   int
		expectedSHA      string
		jobID            string
		slot             AdoptionPreviewItem
	}
	var files []fileItem
	var filename string
	var manifest map[string]any
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		productRow, err := requireProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		productName := productRow.Name
		successItems := []map[string]any{}
		missingItems := []map[string]any{}
		var finishedAt []time.Time
		var total int
		for _, item := range preview.Items {
			if item.RenditionJobID == nil || item.RenditionStatus == nil || *item.RenditionStatus != "succeeded" {
				missingItems = append(missingItems, map[string]any{
					"slot_key": item.SlotKey, "status": item.RenditionStatus,
				})
				continue
			}
			row, err := loadJob(ctx, pgxTx, *item.RenditionJobID)
			if err != nil {
				if apperr.IsNotFound(err) && err.Error() == auth.CrossMerchantDetail {
					return err
				}
				return apperr.NotFound("交付图任务不存在")
			}
			if row.ProductID != productID || row.Status != "succeeded" || row.ResultAssetID == nil || row.FinishedAt == nil {
				missingItems = append(missingItems, map[string]any{"slot_key": item.SlotKey, "job_id": row.ID, "status": row.Status})
				continue
			}
			asset, err := product.LoadAssetRow(ctx, pgxTx, *row.ResultAssetID)
			if err != nil || asset.ProductID != productID {
				return apperr.Conflict("交付图结果媒体不可用")
			}
			source, err := product.LoadAssetRow(ctx, pgxTx, row.SourceAssetID)
			if err != nil || source.ProductID != productID {
				return apperr.Conflict("交付图原图不属于当前商品")
			}
			if source.ID != item.SourceAssetID {
				return apperr.Conflict("交付派生结果与采用快照源图不一致")
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
			total += obj.ByteSize
			if total > exportMaxBytes {
				return apperr.Validation("交付导出图片总大小不能超过 512 MiB")
			}
			sourceObj, err := s.Media.Get(ctx, pgxTx, source.MediaObjectID)
			if err != nil {
				return err
			}
			files = append(files, fileItem{
				name: item.Filename, mediaObjectID: asset.MediaObjectID, expectedByteSize: obj.ByteSize,
				expectedMIME: obj.MIMEType, expectedWidth: obj.Width, expectedHeight: obj.Height,
				expectedSHA: obj.SHA256, jobID: row.ID, slot: item,
			})
			finishedAt = append(finishedAt, *row.FinishedAt)
			var spec any
			_ = json.Unmarshal(row.SpecJSON, &spec)
			lineage := sourceLineage(ctx, pgxTx, source.ID, productID)
			successItems = append(successItems, map[string]any{
				"filename": item.Filename,
				"slot_key": item.SlotKey,
				"product":  map[string]any{"id": productID, "name": productName},
				"graph":    lineage["graph"], "run": lineage["run"], "node_run_id": lineage["node_run_id"],
				"source_asset": assetMetadata(source, sourceObj.SHA256),
				"rendition_job": map[string]any{
					"id": row.ID, "status": row.Status, "spec_schema_version": row.SpecSchemaVersion,
					"spec_hash": row.SpecHash, "created_at": row.CreatedAt, "started_at": row.StartedAt,
					"finished_at": row.FinishedAt, "updated_at": row.UpdatedAt,
				},
				"result_asset":  assetMetadata(asset, obj.SHA256),
				"delivery_spec": spec,
				"measured": map[string]any{
					"mime_type": obj.MIMEType, "width": obj.Width, "height": obj.Height,
					"byte_size": obj.ByteSize, "sha256": obj.SHA256,
				},
				"generated_at": row.FinishedAt,
				"adoption": map[string]any{
					"version_id": versionID, "qualified": item.Qualified,
				},
			})
		}
		if len(missingItems) > 0 && !req.AllowPartial {
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
			"schema_version":      1,
			"kind":                "productflow.delivery_adoption_export",
			"complete":            len(missingItems) == 0,
			"allow_partial":       req.AllowPartial,
			"adoption_version_id": versionID,
			"product":             map[string]any{"id": productID, "name": productName},
			"generated_at":        generated,
			"items":               successItems,
			"missing_items":       missingItems,
		}
		filename = safeName(productName, "product") + "-delivery-adoption.zip"
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
		Pattern:  "delivery-adoption-*.zip",
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

type preparedSlot struct {
	slotKey       string
	sortOrder     int
	imageTypeKey  *string
	sourceAssetID string
	sourceNodeID  *string
	specJSON      string
	specHash      string
	spec          Spec
	qualityStatus string
	qualityDetail *string
	textOverflow  bool
}

func prepareAdoptionSlots(inputs []AdoptionSlotInput) ([]preparedSlot, error) {
	seenKeys := map[string]struct{}{}
	seenOrders := map[int]struct{}{}
	out := make([]preparedSlot, 0, len(inputs))
	for _, in := range inputs {
		key := strings.TrimSpace(in.SlotKey)
		if key == "" {
			return nil, apperr.Validation("图位 key 不能为空")
		}
		if _, ok := seenKeys[key]; ok {
			return nil, apperr.Validationf("图位 key 重复：%s", key)
		}
		seenKeys[key] = struct{}{}
		if in.SortOrder < 0 {
			return nil, apperr.Validation("图位排序不能为负")
		}
		if _, ok := seenOrders[in.SortOrder]; ok {
			return nil, apperr.Validationf("图位排序重复：%d", in.SortOrder)
		}
		seenOrders[in.SortOrder] = struct{}{}
		assetID := strings.TrimSpace(in.SourceAssetID)
		if assetID == "" {
			return nil, apperr.Validation("图位必须引用源图资产")
		}
		quality := strings.TrimSpace(in.QualityStatus)
		if quality == "" {
			quality = "unchecked"
		}
		if quality != "pass" && quality != "fail" && quality != "unchecked" {
			return nil, apperr.Validation("图位质量状态无效")
		}
		if quality == "fail" {
			return nil, apperr.Validation("不合格图不得进入已采用交付合格集")
		}
		if in.TextOverflow {
			return nil, apperr.Validationf("图位 %s 文字溢出，不能采用为交付", key)
		}
		normalized, err := NormalizeSpec(in.DeliverySpec)
		if err != nil {
			return nil, mapAdoptionSpecError(key, err)
		}
		specBytes, err := json.Marshal(normalized.Payload)
		if err != nil {
			return nil, err
		}
		out = append(out, preparedSlot{
			slotKey: key, sortOrder: in.SortOrder, imageTypeKey: trimPtr(derefString(in.ImageTypeKey)),
			sourceAssetID: assetID, sourceNodeID: trimPtr(derefString(in.SourceNodeID)),
			specJSON: string(specBytes), specHash: normalized.Hash, spec: normalized.Spec,
			qualityStatus: quality, qualityDetail: trimPtr(derefString(in.QualityDetail)),
			textOverflow: in.TextOverflow,
		})
	}
	return out, nil
}

func mapAdoptionSpecError(slotKey string, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "crop_anchor") {
		return apperr.Validationf("图位 %s 裁切设置无效", slotKey)
	}
	if strings.Contains(msg, "format") || strings.Contains(msg, "schema") {
		return apperr.Validationf("图位 %s 交付格式不支持或规格无效", slotKey)
	}
	return apperr.Validationf("图位 %s 交付规格无效", slotKey)
}

func validateOptionalSourceVersions(ctx context.Context, tx *gorm.DB, productID string, req CreateAdoptionRequest) error {
	if req.FactSetVersionID != nil {
		id := strings.TrimSpace(*req.FactSetVersionID)
		if id == "" {
			return apperr.Validation("事实版本 ID 无效")
		}
		var count int64
		if err := tx.WithContext(ctx).Model(&schema.ProductFactSetVersions{}).
			Where("id = ? AND product_id = ?", id, productID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return apperr.Validation("事实版本不属于当前商品")
		}
		req.FactSetVersionID = &id
	}
	if req.VisualSystemVersionID != nil {
		id := strings.TrimSpace(*req.VisualSystemVersionID)
		if id == "" {
			return apperr.Validation("视觉方案版本 ID 无效")
		}
		var count int64
		if err := tx.WithContext(ctx).Model(&schema.VisualSystemVersions{}).
			Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return apperr.NotFound("视觉方案版本不存在")
		}
		req.VisualSystemVersionID = &id
	}
	if req.GraphID != nil {
		id := strings.TrimSpace(*req.GraphID)
		if id == "" {
			return apperr.Validation("工作流图 ID 无效")
		}
		var count int64
		if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphs{}).
			Where("id = ? AND product_id = ?", id, productID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return apperr.Validation("工作流图不属于当前商品")
		}
		req.GraphID = &id
	}
	return nil
}

func loadAdoptionVersion(ctx context.Context, tx *gorm.DB, productID, versionID string) (schema.DeliveryAdoptionVersions, *string, error) {
	productRow, err := requireProduct(ctx, tx, productID)
	if err != nil {
		return schema.DeliveryAdoptionVersions{}, nil, err
	}
	currentID := productRow.CurrentDeliveryAdoptionVersionID
	target := strings.TrimSpace(versionID)
	if target == "" || target == "current" {
		if currentID == nil {
			return schema.DeliveryAdoptionVersions{}, currentID, apperr.NotFound("当前没有交付采用版本")
		}
		target = *currentID
	}
	var version schema.DeliveryAdoptionVersions
	if err := tx.WithContext(ctx).Where("id = ? AND product_id = ?", target, productID).Take(&version).Error; err != nil {
		return schema.DeliveryAdoptionVersions{}, currentID, apperr.NotFound("交付采用版本不存在")
	}
	return version, currentID, nil
}

func loadAdoptionSlots(ctx context.Context, tx *gorm.DB, versionID string) ([]schema.DeliveryAdoptionSlots, error) {
	var slots []schema.DeliveryAdoptionSlots
	err := tx.WithContext(ctx).Where("version_id = ?", versionID).
		Order("sort_order ASC, id ASC").Find(&slots).Error
	return slots, err
}

func serializeAdoption(version schema.DeliveryAdoptionVersions, slots []schema.DeliveryAdoptionSlots, currentID *string) (AdoptionVersionResponse, error) {
	items := make([]AdoptionSlotResponse, 0, len(slots))
	for _, slot := range slots {
		spec, err := specFromJSON([]byte(slot.DeliverySpecJSON))
		if err != nil {
			return AdoptionVersionResponse{}, err
		}
		items = append(items, AdoptionSlotResponse{
			ID: slot.ID, SlotKey: slot.SlotKey, SortOrder: slot.SortOrder,
			ImageTypeKey: slot.ImageTypeKey, SourceAssetID: slot.SourceAssetID, SourceNodeID: slot.SourceNodeID,
			DeliverySpec: spec, DeliverySpecHash: slot.DeliverySpecHash,
			QualityStatus: slot.QualityStatus, QualityDetail: slot.QualityDetail,
			TextOverflow: slot.TextOverflow, Qualified: slot.QualityStatus == "pass",
		})
	}
	return AdoptionVersionResponse{
		ID: version.ID, ProductID: version.ProductID, Version: version.Version,
		IsCurrent: currentID != nil && *currentID == version.ID,
		GraphID:   version.GraphID, GraphRevision: version.GraphRevision,
		FactSetVersionID: version.FactSetVersionID, VisualSystemVersionID: version.VisualSystemVersionID,
		Notes: version.Notes, Slots: items, CreatedAt: version.CreatedAt,
	}, nil
}

func buildAdoptionPreview(
	ctx context.Context,
	pgxTx *gorm.DB,
	productID, productName, versionID string,
	slots []schema.DeliveryAdoptionSlots,
	req AdoptionExportRequest,
) AdoptionPreviewResponse {
	safeProduct := safeName(productName, "product")
	issues := make([]AdoptionIssue, 0)
	items := make([]AdoptionPreviewItem, 0, len(slots))
	usedNames := map[string]struct{}{}
	seenKeys := map[string]struct{}{}

	for index, slot := range slots {
		if req.QualifiedOnly && slot.QualityStatus != "pass" {
			continue
		}
		if _, dup := seenKeys[slot.SlotKey]; dup {
			issues = append(issues, AdoptionIssue{
				Code: "duplicate_slot", SlotKey: slot.SlotKey, Message: "导出图位重复",
			})
			continue
		}
		seenKeys[slot.SlotKey] = struct{}{}

		if slot.QualityStatus == "fail" {
			issues = append(issues, AdoptionIssue{
				Code: "quality_failed", SlotKey: slot.SlotKey, Message: "不合格图不得进入已采用交付合格集",
			})
			continue
		}
		if slot.TextOverflow {
			issues = append(issues, AdoptionIssue{
				Code: "text_overflow", SlotKey: slot.SlotKey, Message: "图位文字溢出",
			})
			continue
		}

		asset, err := product.LoadAssetRow(ctx, pgxTx, slot.SourceAssetID)
		if err != nil || asset.ProductID != productID {
			issues = append(issues, AdoptionIssue{
				Code: "missing_asset", SlotKey: slot.SlotKey, Message: "采用快照引用的源图不可用",
			})
			continue
		}

		spec, err := specFromJSON([]byte(slot.DeliverySpecJSON))
		if err != nil {
			issues = append(issues, AdoptionIssue{
				Code: "unsupported_format", SlotKey: slot.SlotKey, Message: "交付规格无法解析",
			})
			continue
		}
		if _, err := NormalizeSpec(specPayload(spec)); err != nil {
			code := "unsupported_format"
			msg := "交付格式不支持或规格无效"
			if strings.Contains(err.Error(), "crop_anchor") {
				code = "invalid_crop"
				msg = "裁切设置超出允许范围"
			}
			issues = append(issues, AdoptionIssue{Code: code, SlotKey: slot.SlotKey, Message: msg})
			continue
		}

		imageType := "image"
		if slot.ImageTypeKey != nil && strings.TrimSpace(*slot.ImageTypeKey) != "" {
			imageType = safeName(*slot.ImageTypeKey, "image")
		} else if asset.ImageTypeKey != nil && strings.TrimSpace(*asset.ImageTypeKey) != "" {
			imageType = safeName(*asset.ImageTypeKey, "image")
		}
		ext := formatExt[spec.Format]
		if ext == "" {
			ext = ".bin"
		}
		filename := deduplicateFilename(
			exportImageFilename(safeProduct, imageType, index+1, spec.Width, spec.Height, ext),
			usedNames,
		)

		item := AdoptionPreviewItem{
			SlotKey: slot.SlotKey, SortOrder: slot.SortOrder, Filename: filename,
			SourceAssetID: slot.SourceAssetID, DeliverySpecHash: slot.DeliverySpecHash,
			Qualified: slot.QualityStatus == "pass",
		}
		job, err := loadBySourceHash(ctx, pgxTx, slot.SourceAssetID, slot.DeliverySpecHash)
		if err == nil && job != nil {
			item.RenditionJobID = &job.ID
			status := job.Status
			item.RenditionStatus = &status
			item.ResultAssetID = job.ResultAssetID
			if job.Status == "queued" || job.Status == "running" {
				issues = append(issues, AdoptionIssue{
					Code: "rendition_pending", SlotKey: slot.SlotKey,
					Message: "交付图尚未生成完成", JobID: &job.ID,
				})
			} else if job.Status != "succeeded" {
				issues = append(issues, AdoptionIssue{
					Code: "rendition_failed", SlotKey: slot.SlotKey,
					Message: "交付图任务未成功", JobID: &job.ID,
				})
			}
		} else {
			issues = append(issues, AdoptionIssue{
				Code: "rendition_pending", SlotKey: slot.SlotKey, Message: "尚未创建交付派生任务",
			})
		}
		items = append(items, item)
	}

	complete := true
	exportReady := true
	for _, issue := range issues {
		complete = false
		if issue.Code != "rendition_pending" || !req.AllowPartial {
			exportReady = false
		}
		if issue.Code == "rendition_failed" || issue.Code == "missing_asset" ||
			issue.Code == "duplicate_slot" || issue.Code == "unsupported_format" ||
			issue.Code == "invalid_crop" || issue.Code == "text_overflow" || issue.Code == "quality_failed" {
			exportReady = false
		}
	}
	if len(items) == 0 {
		exportReady = false
		complete = false
	} else {
		hasSucceeded := false
		for _, item := range items {
			if item.RenditionStatus != nil && *item.RenditionStatus == "succeeded" {
				hasSucceeded = true
				break
			}
		}
		if !hasSucceeded {
			exportReady = false
		}
	}

	return AdoptionPreviewResponse{
		VersionID: versionID, ProductID: productID, Complete: complete,
		Issues: issues, Items: items, ExportReady: exportReady, AllowPartial: req.AllowPartial,
	}
}

func loadProductName(ctx context.Context, tx *gorm.DB, productID string) (string, error) {
	row, err := requireProduct(ctx, tx, productID)
	if err != nil {
		return "", err
	}
	return row.Name, nil
}

func formatAdoptionIssues(issues []AdoptionIssue) string {
	if len(issues) == 1 {
		return issues[0].Message
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("%s(%s)", issue.SlotKey, issue.Code))
	}
	return "交付采用导出前存在问题：" + strings.Join(parts, ", ")
}

func trimPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func derefString(value *string) *string {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}
