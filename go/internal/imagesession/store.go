package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"gorm.io/gorm"
)

func loadSession(ctx context.Context, q *gorm.DB, id string) (sessionRow, error) {
	var row sessionRow
	err := pfdb.QueryRow(ctx, q, `SELECT id, title, created_at, updated_at FROM image_sessions WHERE id = $1`, id).Scan(
		&row.ID, &row.Title, &row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return sessionRow{}, apperr.NotFound("连续生图会话不存在")
	}
	return row, err
}

func loadAsset(ctx context.Context, q *gorm.DB, sessionID, assetID string) (assetRow, error) {
	var row assetRow
	err := pfdb.QueryRow(ctx, q, `
		SELECT a.id, a.session_id, a.kind, a.original_filename, a.mime_type, m.storage_path, a.media_object_id, a.created_at
		FROM image_session_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.id = $1 AND a.session_id = $2
	`, assetID, sessionID).Scan(&row.ID, &row.SessionID, &row.Kind, &row.OriginalFilename, &row.MIMEType, &row.StoragePath, &row.MediaObjectID, &row.CreatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return assetRow{}, apperr.NotFound("会话图片不存在")
	}
	return row, err
}

func listAssets(ctx context.Context, tx *gorm.DB, sessionID string) ([]assetRow, error) {
	if _, err := loadSession(ctx, tx, sessionID); err != nil {
		return nil, err
	}
	rows, err := pfdb.Query(ctx, tx, `
		SELECT a.id, a.session_id, a.kind, a.original_filename, a.mime_type, m.storage_path, a.media_object_id, a.created_at
		FROM image_session_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.session_id = $1
		ORDER BY a.created_at DESC, a.id DESC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []assetRow
	for rows.Next() {
		var row assetRow
		if err := rows.Scan(&row.ID, &row.SessionID, &row.Kind, &row.OriginalFilename, &row.MIMEType, &row.StoragePath, &row.MediaObjectID, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func loadTask(ctx context.Context, q *gorm.DB, sessionID, taskID string) (taskRow, error) {
	var row taskRow
	err := pfdb.QueryRow(ctx, q, `
		SELECT id, session_id, status, prompt, size, base_asset_id, selected_reference_asset_ids, tool_options,
		       generation_count, completed_candidates, active_candidate_index, progress_phase, progress_updated_at,
		       provider_response_id, provider_response_status, progress_metadata, failure_reason, result_generation_group_id,
		       created_at, started_at, finished_at, attempts, active_attempt_id, is_retryable
		FROM image_session_generation_tasks WHERE id = $1 AND session_id = $2
	`, taskID, sessionID).Scan(
		&row.ID, &row.SessionID, &row.Status, &row.Prompt, &row.Size, &row.BaseAssetID, &row.SelectedRefs, &row.ToolOptions,
		&row.GenerationCount, &row.CompletedCandidates, &row.ActiveCandidateIndex, &row.ProgressPhase, &row.ProgressUpdatedAt,
		&row.ProviderResponseID, &row.ProviderResponseStatus, &row.ProgressMetadata, &row.FailureReason, &row.ResultGenerationGroupID,
		&row.CreatedAt, &row.StartedAt, &row.FinishedAt, &row.Attempts, &row.ActiveAttemptID, &row.IsRetryable,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return taskRow{}, apperr.NotFound("生成任务不存在")
	}
	return row, err
}

func loadEffect(ctx context.Context, q *gorm.DB, taskID string, start int) (EffectResponse, error) {
	var out EffectResponse
	err := pfdb.QueryRow(ctx, q, `
		SELECT id, generation_task_id, candidate_start_index, candidate_count, operation_key, effect_kind,
		       request_hash, provider_name, effect_result, reconciliation_state, provider_response_id, provider_status,
		       detail, created_at, updated_at
		FROM image_session_provider_effects
		WHERE generation_task_id = $1 AND candidate_start_index = $2
	`, taskID, start).Scan(
		&out.ID, &out.GenerationTaskID, &out.CandidateStartIndex, &out.CandidateCount, &out.OperationKey, &out.EffectKind,
		&out.RequestHash, &out.ProviderName, &out.EffectResult, &out.ReconciliationState, &out.ProviderResponseID, &out.ProviderStatus,
		&out.Detail, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return EffectResponse{}, apperr.Conflict("找不到连续生图 provider effect ledger")
	}
	return out, err
}

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
	var roundCount int
	if err := pfdb.QueryRow(ctx, tx, `SELECT COUNT(*) FROM image_session_rounds WHERE session_id = $1`, sessionID).Scan(&roundCount); err != nil {
		return false, err
	}
	if roundCount > 0 {
		return true, nil
	}
	var active int
	if err := pfdb.QueryRow(ctx, tx, `
		SELECT COUNT(*) FROM image_session_generation_tasks
		WHERE session_id = $1 AND status IN ('queued', 'running') AND id <> $2
	`, sessionID, excludeTaskID).Scan(&active); err != nil {
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
