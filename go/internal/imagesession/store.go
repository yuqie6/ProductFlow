package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"gorm.io/gorm"
)

func loadSession(ctx context.Context, q *gorm.DB, id string) (sessionRow, error) {
	var row schema.ImageSessions
	err := auth.ScopeMerchant(ctx, q.WithContext(ctx).Where("id = ?", id), "merchant_id").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sessionRow{}, auth.NotFoundCrossMerchant()
	}
	if err != nil {
		return sessionRow{}, err
	}
	return sessionFromModel(row), nil
}

func loadAsset(ctx context.Context, q *gorm.DB, sessionID, assetID string) (assetRow, error) {
	var asset schema.ImageSessionAssets
	err := q.WithContext(ctx).Where("id = ? AND session_id = ?", assetID, sessionID).Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return assetRow{}, apperr.NotFound("会话图片不存在")
	}
	if err != nil {
		return assetRow{}, err
	}
	return assetFromModels(q, asset)
}

func listAssets(ctx context.Context, tx *gorm.DB, sessionID string) ([]assetRow, error) {
	if _, err := loadSession(ctx, tx, sessionID); err != nil {
		return nil, err
	}
	var assets []schema.ImageSessionAssets
	if err := tx.Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").Find(&assets).Error; err != nil {
		return nil, err
	}
	return assetsFromModels(tx, assets)
}

func loadTask(ctx context.Context, q *gorm.DB, sessionID, taskID string) (taskRow, error) {
	var row schema.ImageSessionGenerationTasks
	err := q.WithContext(ctx).Where("id = ? AND session_id = ?", taskID, sessionID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, apperr.NotFound("生成任务不存在")
	}
	if err != nil {
		return taskRow{}, err
	}
	return taskFromModel(row), nil
}

func loadEffect(ctx context.Context, q *gorm.DB, taskID string, start int) (EffectResponse, error) {
	var row schema.ImageSessionProviderEffects
	err := q.WithContext(ctx).Where("generation_task_id = ? AND candidate_start_index = ?", taskID, start).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return EffectResponse{}, apperr.Conflict("找不到连续生图 provider effect ledger")
	}
	if err != nil {
		return EffectResponse{}, err
	}
	return effectFromModel(row), nil
}

// validateGeneration 核对底图/参考图属于本会话、count 在 1..上限。返回规范化后的 baseID 与参考 id。
func validateGeneration(ctx context.Context, tx *gorm.DB, sessionID string, baseID *string, selected []string, count int) (*string, []string, error) {
	if count < 1 || count > maxGenerationCount {
		return nil, nil, apperr.Validationf("一次生成数量必须在 1-%d 张之间", maxGenerationCount)
	}
	assets, err := listAssets(ctx, tx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]assetRow{}
	for _, a := range assets {
		byID[a.ID] = a
	}
	refs := uniqueIDs(selected)
	if (boolToInt(baseID != nil && strings.TrimSpace(*baseID) != "") + len(refs)) > maxBranchImages {
		return nil, nil, apperr.Validation("本轮最多选择 6 张图片上下文（含分支基图）")
	}
	var normalizedBase *string
	if baseID != nil && strings.TrimSpace(*baseID) != "" {
		asset, ok := byID[*baseID]
		if !ok {
			return nil, nil, apperr.NotFound("会话图片不存在")
		}
		if asset.Kind != kindGenerated {
			return nil, nil, apperr.Validation("只能从会话生成图继续")
		}
		id := asset.ID
		normalizedBase = &id
	} else {
		prior, err := hasPriorGeneration(ctx, tx, sessionID, "")
		if err != nil {
			return nil, nil, err
		}
		if prior {
			return nil, nil, apperr.Validation("后续生图必须选择一张本会话已生成图片作为基图")
		}
	}
	var normalizedRefs []string
	for _, id := range refs {
		asset, ok := byID[id]
		if !ok {
			return nil, nil, apperr.NotFound("会话参考图不存在")
		}
		if asset.Kind != kindReference {
			return nil, nil, apperr.Validation("只能选择会话参考图参与本轮生成")
		}
		normalizedRefs = append(normalizedRefs, asset.ID)
	}
	return normalizedBase, normalizedRefs, nil
}

func hasPriorGeneration(ctx context.Context, tx *gorm.DB, sessionID, excludeTaskID string) (bool, error) {
	var roundCount int64
	if err := tx.WithContext(ctx).Model(&schema.ImageSessionRounds{}).Where("session_id = ?", sessionID).Count(&roundCount).Error; err != nil {
		return false, err
	}
	if roundCount > 0 {
		return true, nil
	}
	var active int64
	q := tx.Model(&schema.ImageSessionGenerationTasks{}).
		Where("session_id = ? AND status IN ? AND id <> ?", sessionID, []string{"queued", "running"}, excludeTaskID)
	if err := q.Count(&active).Error; err != nil {
		return false, err
	}
	return active > 0, nil
}

func uniqueIDs(ids []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// filterToolOptions 只保留 allowed 里的键。空输入返回 nil，与「用户没传 options」同一形状。
func filterToolOptions(in map[string]any, allowed []string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	allow := map[string]struct{}{}
	for _, k := range allowed {
		allow[k] = struct{}{}
	}
	out := map[string]any{}
	for k, v := range in {
		if _, ok := allow[k]; !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validateToolOptions 检查 quality/format 等闭集与压缩范围。未知键或非法值返回 400。
func validateToolOptions(in map[string]any) error {
	if len(in) == 0 {
		return nil
	}
	known := map[string]struct{}{
		"model": {}, "quality": {}, "output_format": {}, "output_compression": {},
		"background": {}, "moderation": {}, "action": {}, "input_fidelity": {},
		"partial_images": {}, "n": {},
	}
	for key := range in {
		if _, ok := known[key]; !ok {
			return apperr.Validation("请求体无效")
		}
	}
	if err := optionalStringEnum(in, "quality", "auto", "low", "medium", "high"); err != nil {
		return err
	}
	if err := optionalStringEnum(in, "output_format", "png", "jpeg", "webp"); err != nil {
		return err
	}
	if err := optionalStringEnum(in, "background", "auto", "opaque", "transparent"); err != nil {
		return err
	}
	if err := optionalStringEnum(in, "moderation", "auto", "low"); err != nil {
		return err
	}
	if err := optionalStringEnum(in, "action", "auto", "generate", "edit"); err != nil {
		return err
	}
	if err := optionalStringEnum(in, "input_fidelity", "low", "high"); err != nil {
		return err
	}
	if v, ok := in["model"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return apperr.Validation("请求体无效")
		}
		s = strings.TrimSpace(s)
		if s == "" || utf8.RuneCountInString(s) > 100 {
			return apperr.Validation("请求体无效")
		}
	}
	if err := optionalIntRange(in, "output_compression", 0, 100); err != nil {
		return err
	}
	if err := optionalIntRange(in, "partial_images", 0, 3); err != nil {
		return err
	}
	return optionalIntRange(in, "n", 1, 10)
}

func optionalStringEnum(in map[string]any, key string, allowed ...string) error {
	v, ok := in[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return apperr.Validation("请求体无效")
	}
	for _, item := range allowed {
		if s == item {
			return nil
		}
	}
	return apperr.Validation("请求体无效")
}

func optionalIntRange(in map[string]any, key string, min, max int) error {
	v, ok := in[key]
	if !ok || v == nil {
		return nil
	}
	n, ok := jsonInt(v)
	if !ok || n < min || n > max {
		return apperr.Validation("请求体无效")
	}
	return nil
}

// jsonInt 把 JSON 数字收成 int。float64 只接受整数值；其它类型返回 false，不要静默截断。
func jsonInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func decodeStringSlice(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func decodeMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func serializeAsset(row assetRow) AssetResponse {
	download, preview, thumb := storage.ImageURLs("/api/image-session-assets/" + row.ID + "/download")
	return AssetResponse{
		ID: row.ID, Kind: row.Kind, OriginalFilename: row.OriginalFilename, MIMEType: row.MIMEType,
		DownloadURL: download, PreviewURL: preview, ThumbnailURL: thumb, CreatedAt: row.CreatedAt,
	}
}

func sessionFromModel(m schema.ImageSessions) sessionRow {
	return sessionRow{ID: m.ID, MerchantID: m.MerchantID, Title: m.Title, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func assetFromModels(tx *gorm.DB, asset schema.ImageSessionAssets) (assetRow, error) {
	var media schema.MediaObjects
	if err := tx.Where("id = ?", asset.MediaObjectID).Take(&media).Error; err != nil {
		return assetRow{}, err
	}
	return assetRow{
		ID: asset.ID, SessionID: asset.SessionID, Kind: asset.Kind,
		OriginalFilename: asset.OriginalFilename, MIMEType: asset.MIMEType,
		StoragePath: media.StoragePath, MediaObjectID: asset.MediaObjectID,
		VerificationStatus: media.VerificationStatus, CreatedAt: asset.CreatedAt,
	}, nil
}

// assetsFromModels 联表补 MediaObject。任一素材缺媒体行返回 404，不输出半残列表。
func assetsFromModels(tx *gorm.DB, assets []schema.ImageSessionAssets) ([]assetRow, error) {
	if len(assets) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(assets))
	for _, a := range assets {
		ids = append(ids, a.MediaObjectID)
	}
	var medias []schema.MediaObjects
	if err := tx.Where("id IN ?", ids).Find(&medias).Error; err != nil {
		return nil, err
	}
	byID := map[string]schema.MediaObjects{}
	for _, m := range medias {
		byID[m.ID] = m
	}
	out := make([]assetRow, 0, len(assets))
	for _, asset := range assets {
		media, ok := byID[asset.MediaObjectID]
		if !ok {
			return nil, apperr.NotFound("会话图片不存在")
		}
		out = append(out, assetRow{
			ID: asset.ID, SessionID: asset.SessionID, Kind: asset.Kind,
			OriginalFilename: asset.OriginalFilename, MIMEType: asset.MIMEType,
			StoragePath: media.StoragePath, MediaObjectID: asset.MediaObjectID,
			VerificationStatus: media.VerificationStatus, CreatedAt: asset.CreatedAt,
		})
	}
	return out, nil
}

func taskFromModel(m schema.ImageSessionGenerationTasks) taskRow {
	return taskRow{
		ID: m.ID, SessionID: m.SessionID, Status: m.Status, Prompt: m.Prompt, Size: m.Size,
		BaseAssetID: m.BaseAssetID, SelectedRefs: ptrBytes(m.SelectedReferenceAssetIds), ToolOptions: ptrBytes(m.ToolOptions),
		GenerationCount: m.GenerationCount, CompletedCandidates: m.CompletedCandidates, ActiveCandidateIndex: m.ActiveCandidateIndex,
		ProgressPhase: m.ProgressPhase, ProgressUpdatedAt: m.ProgressUpdatedAt,
		ProviderResponseID: m.ProviderResponseID, ProviderResponseStatus: m.ProviderResponseStatus,
		ProgressMetadata: ptrBytes(m.ProgressMetadata), FailureReason: m.FailureReason, ResultGenerationGroupID: m.ResultGenerationGroupID,
		CreatedAt: m.CreatedAt, StartedAt: m.StartedAt, FinishedAt: m.FinishedAt, Attempts: m.Attempts,
		ActiveAttemptID: m.ActiveAttemptID, IsRetryable: m.IsRetryable,
	}
}

func effectFromModel(m schema.ImageSessionProviderEffects) EffectResponse {
	return EffectResponse{
		ID: m.ID, GenerationTaskID: m.GenerationTaskID, CandidateStartIndex: m.CandidateStartIndex,
		CandidateCount: m.CandidateCount, OperationKey: m.OperationKey, EffectKind: m.EffectKind,
		RequestHash: m.RequestHash, ProviderName: m.ProviderName, EffectResult: m.EffectResult,
		ReconciliationState: m.ReconciliationState, ProviderResponseID: m.ProviderResponseID,
		ProviderStatus: m.ProviderStatus, Detail: m.Detail, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func ptrBytes(s *string) []byte {
	if s == nil {
		return nil
	}
	return []byte(*s)
}
