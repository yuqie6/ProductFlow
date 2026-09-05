package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"gorm.io/gorm"
)

// serializeSessionSummaries 批量组装会话列表项，避免每个 session 单独 Count/Take/loadAsset。
// 最新轮按 created_at、id 倒序取一条；缺失媒体行时与旧摘要一样省略 latest asset。
func serializeSessionSummaries(ctx context.Context, tx *gorm.DB, sessions []schema.ImageSessions) ([]SummaryResponse, error) {
	if len(sessions) == 0 {
		return []SummaryResponse{}, nil
	}
	tx = tx.WithContext(ctx)
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}

	type roundCount struct {
		SessionID string `gorm:"column:session_id"`
		Count     int64  `gorm:"column:count"`
	}
	var counts []roundCount
	if err := tx.Model(&schema.ImageSessionRounds{}).
		Select("session_id, COUNT(*) AS count").
		Where("session_id IN ?", ids).
		Group("session_id").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countBySession := make(map[string]int64, len(counts))
	for _, item := range counts {
		countBySession[item.SessionID] = item.Count
	}

	type latestAsset struct {
		SessionID        string    `gorm:"column:session_id"`
		ID               string    `gorm:"column:id"`
		Kind             string    `gorm:"column:kind"`
		OriginalFilename string    `gorm:"column:original_filename"`
		MIMEType         string    `gorm:"column:mime_type"`
		CreatedAt        time.Time `gorm:"column:created_at"`
	}
	var latest []latestAsset
	if err := tx.Table("image_session_rounds AS r").
		Select(`DISTINCT ON (r.session_id)
			r.session_id, a.id, a.kind, a.original_filename, a.mime_type, a.created_at`).
		Joins("JOIN image_session_assets a ON a.id = r.generated_asset_id AND a.session_id = r.session_id").
		Joins("JOIN media_objects m ON m.id = a.media_object_id").
		Where("r.session_id IN ?", ids).
		Order("r.session_id, r.created_at DESC, r.id DESC").
		Scan(&latest).Error; err != nil {
		return nil, err
	}
	latestBySession := make(map[string]AssetResponse, len(latest))
	for _, item := range latest {
		latestBySession[item.SessionID] = serializeAsset(assetRow{
			ID: item.ID, SessionID: item.SessionID, Kind: item.Kind,
			OriginalFilename: item.OriginalFilename, MIMEType: item.MIMEType, CreatedAt: item.CreatedAt,
		})
	}

	items := make([]SummaryResponse, 0, len(sessions))
	for _, session := range sessions {
		item := SummaryResponse{
			ID: session.ID, Title: session.Title, RoundsCount: int(countBySession[session.ID]),
			CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt,
		}
		if asset, ok := latestBySession[session.ID]; ok {
			item.LatestGeneratedAsset = &asset
		}
		items = append(items, item)
	}
	return items, nil
}

// loadDetail 组装会话首屏：参考图、最新一页轮次、活动任务与队列位置。会话不存在返回 404。
func (s Service) loadDetail(ctx context.Context, tx *gorm.DB, sessionID string) (DetailResponse, error) {
	sess, err := loadSession(ctx, tx, sessionID)
	if err != nil {
		return DetailResponse{}, err
	}
	refs, err := listReferenceAssets(ctx, tx, sessionID, imageSessionReferenceAssetLimit)
	if err != nil {
		return DetailResponse{}, err
	}
	assetResp := make([]AssetResponse, 0, len(refs))
	for _, a := range refs {
		assetResp = append(assetResp, serializeAsset(a))
	}

	var roundsCount int64
	if err := tx.WithContext(ctx).Model(&schema.ImageSessionRounds{}).Where("session_id = ?", sessionID).Count(&roundsCount).Error; err != nil {
		return DetailResponse{}, err
	}
	roundRows, hasMore, err := listRoundPage(ctx, tx, sessionID, nil, time.Time{}, imageSessionDetailRoundLimit)
	if err != nil {
		return DetailResponse{}, err
	}
	rounds, notesByGroup, err := serializeRoundPage(ctx, tx, sessionID, roundRows)
	if err != nil {
		return DetailResponse{}, err
	}
	var historyNextAfter *string
	if hasMore {
		last := roundRows[len(roundRows)-1]
		next, err := encodeImageSessionHistoryCursor(last.CreatedAt, last.ID)
		if err != nil {
			return DetailResponse{}, err
		}
		historyNextAfter = &next
	}

	tasks, err := s.serializeDetailTasks(ctx, tx, sessionID, roundRows, notesByGroup)
	if err != nil {
		return DetailResponse{}, err
	}
	return DetailResponse{
		ID: sess.ID, Title: sess.Title, Assets: assetResp, Rounds: rounds, GenerationTasks: tasks,
		RoundsCount: int(roundsCount), HistoryNextAfter: historyNextAfter,
		CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}, nil
}

// loadStatus 用计数、最新轮次和活动任务组装轻量状态，不加载完整历史。
func (s Service) loadStatus(ctx context.Context, tx *gorm.DB, sessionID string) (StatusResponse, error) {
	sess, err := loadSession(ctx, tx, sessionID)
	if err != nil {
		return StatusResponse{}, err
	}
	var roundsCount int64
	if err := tx.WithContext(ctx).Model(&schema.ImageSessionRounds{}).Where("session_id = ?", sessionID).Count(&roundsCount).Error; err != nil {
		return StatusResponse{}, err
	}
	var latest schema.ImageSessionRounds
	err = tx.WithContext(ctx).Select("id", "generation_group_id").
		Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").Take(&latest).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return StatusResponse{}, err
	}
	var latestRoundID, latestGroup *string
	if err == nil {
		latestRoundID = &latest.ID
		latestGroup = latest.GenerationGroupID
	}

	var activeCount int64
	if err := tx.WithContext(ctx).Model(&schema.ImageSessionGenerationTasks{}).
		Where("session_id = ? AND status IN ?", sessionID, imageSessionActiveTaskStatuses).
		Count(&activeCount).Error; err != nil {
		return StatusResponse{}, err
	}
	var scannedTaskModels []schema.ImageSessionGenerationTasks
	if err := tx.WithContext(ctx).Where("session_id = ? AND status IN ?", sessionID, imageSessionActiveTaskStatuses).
		Order("created_at DESC, id DESC").Find(&scannedTaskModels).Error; err != nil {
		return StatusResponse{}, err
	}
	taskIDs := make([]string, 0, len(scannedTaskModels))
	scannedTasks := make([]taskRow, 0, len(scannedTaskModels))
	groupIDs := make([]string, 0, len(scannedTaskModels))
	for _, m := range scannedTaskModels {
		row := taskFromModel(m)
		scannedTasks = append(scannedTasks, row)
		taskIDs = append(taskIDs, row.ID)
		if row.ResultGenerationGroupID != nil && *row.ResultGenerationGroupID != "" {
			groupIDs = append(groupIDs, *row.ResultGenerationGroupID)
		}
	}
	notesByGroup, err := loadNotesByGroupIDs(ctx, tx, sessionID, groupIDs)
	if err != nil {
		return StatusResponse{}, err
	}
	effectsByTask, err := listEffectsByTaskIDs(ctx, tx, taskIDs)
	if err != nil {
		return StatusResponse{}, err
	}
	overview := s.queueOverview(ctx, tx)
	positions := queuedPositions(ctx, tx, taskIDs)
	tasks := make([]TaskResponse, 0, len(scannedTasks))
	for _, row := range scannedTasks {
		notes := []string{}
		if row.ResultGenerationGroupID != nil {
			notes = notesByGroup[*row.ResultGenerationGroupID]
			if notes == nil {
				notes = []string{}
			}
		}
		tasks = append(tasks, serializeTask(row, effectsByTask[row.ID], notes, overview, positions))
	}
	return StatusResponse{
		ID: sess.ID, Title: sess.Title, RoundsCount: int(roundsCount),
		LatestRoundID: latestRoundID, LatestGenerationGroupID: latestGroup,
		HasActiveGenerationTask: activeCount > 0, GenerationTasks: tasks,
		CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}, nil
}

func (s Service) loadHistory(ctx context.Context, tx *gorm.DB, sessionID string, cursor imageSessionHistoryCursor, cursorAt time.Time, hasCursor bool, limit int) (HistoryResponse, error) {
	if _, err := loadSession(ctx, tx, sessionID); err != nil {
		return HistoryResponse{}, err
	}
	var decoded *imageSessionHistoryCursor
	if hasCursor {
		decoded = &cursor
	}
	roundRows, hasMore, err := listRoundPage(ctx, tx, sessionID, decoded, cursorAt, limit)
	if err != nil {
		return HistoryResponse{}, err
	}
	items, _, err := serializeRoundPage(ctx, tx, sessionID, roundRows)
	if err != nil {
		return HistoryResponse{}, err
	}
	out := HistoryResponse{Items: items}
	if hasMore {
		last := roundRows[len(roundRows)-1]
		next, err := encodeImageSessionHistoryCursor(last.CreatedAt, last.ID)
		if err != nil {
			return HistoryResponse{}, err
		}
		out.NextAfter = &next
	}
	return out, nil
}

func listRoundPage(ctx context.Context, tx *gorm.DB, sessionID string, cursor *imageSessionHistoryCursor, cursorAt time.Time, limit int) ([]schema.ImageSessionRounds, bool, error) {
	query := tx.WithContext(ctx).Where("session_id = ?", sessionID)
	if cursor != nil {
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", cursorAt, cursorAt, cursor.ID)
	}
	var rows []schema.ImageSessionRounds
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}

func serializeRoundPage(ctx context.Context, tx *gorm.DB, sessionID string, rows []schema.ImageSessionRounds) ([]RoundResponse, map[string][]string, error) {
	assetIDs := make([]string, 0, len(rows))
	for _, item := range rows {
		assetIDs = append(assetIDs, item.GeneratedAssetID)
	}
	assetByID, err := loadAssetsByIDs(ctx, tx, sessionID, assetIDs)
	if err != nil {
		return nil, nil, err
	}
	rounds := make([]RoundResponse, 0, len(rows))
	notesByGroup := map[string][]string{}
	for _, item := range rows {
		gen, ok := assetByID[item.GeneratedAssetID]
		if !ok {
			return nil, nil, apperr.NotFound("会话图片不存在")
		}
		outputJSON := ptrBytes(item.ProviderOutputJSON)
		notes := extractNotes(outputJSON)
		if item.GenerationGroupID != nil && len(notes) > 0 {
			notesByGroup[*item.GenerationGroupID] = notes
		}
		actual := extractActualSize(outputJSON)
		rounds = append(rounds, RoundResponse{
			ID: item.ID, Prompt: item.Prompt, AssistantMessage: item.AssistantMessage, Size: item.Size,
			ModelName: item.ModelName, ProviderName: item.ProviderName, PromptVersion: item.PromptVersion,
			ProviderResponseID: item.ProviderResponseID, PreviousResponseID: item.PreviousResponseID, ImageGenerationCallID: item.ImageGenerationCallID,
			GenerationGroupID: item.GenerationGroupID, CandidateIndex: item.CandidateIndex, CandidateCount: item.CandidateCount,
			BaseAssetID: item.BaseAssetID, SelectedReferenceAssetIDs: decodeStringSlice(ptrBytes(item.SelectedReferenceAssetIds)),
			ActualSize: actual, ProviderNotes: notes, GeneratedAsset: serializeAsset(gen), CreatedAt: item.CreatedAt,
		})
	}
	if rounds == nil {
		rounds = []RoundResponse{}
	}
	return rounds, notesByGroup, nil
}

func (s Service) serializeDetailTasks(ctx context.Context, tx *gorm.DB, sessionID string, roundRows []schema.ImageSessionRounds, notesByGroup map[string][]string) ([]TaskResponse, error) {
	groupIDs := make([]string, 0, len(roundRows))
	seenGroup := map[string]struct{}{}
	for _, item := range roundRows {
		if item.GenerationGroupID == nil || *item.GenerationGroupID == "" {
			continue
		}
		if _, ok := seenGroup[*item.GenerationGroupID]; ok {
			continue
		}
		seenGroup[*item.GenerationGroupID] = struct{}{}
		groupIDs = append(groupIDs, *item.GenerationGroupID)
	}
	terminalFilter := tx.Where("status IN ?", imageSessionDetailTerminalTaskStatuses).Or(
		"status = ? AND (result_generation_group_id IS NULL OR NOT EXISTS (SELECT 1 FROM image_session_rounds r WHERE r.session_id = image_session_generation_tasks.session_id AND r.generation_group_id = image_session_generation_tasks.result_generation_group_id))",
		"succeeded",
	)
	// 分别限量，避免近期终态挤掉旧的活动任务或首屏轮次所需任务。
	queries := []*gorm.DB{
		tx.Where("status IN ?", imageSessionActiveTaskStatuses),
		tx.Where(terminalFilter),
	}
	if len(groupIDs) > 0 {
		queries = append(queries, tx.Where("result_generation_group_id IN ?", groupIDs))
	}
	scannedTaskModels := make([]schema.ImageSessionGenerationTasks, 0, len(queries)*imageSessionDetailTaskLimit)
	for _, query := range queries {
		var page []schema.ImageSessionGenerationTasks
		if err := query.WithContext(ctx).Where("session_id = ?", sessionID).
			Order("created_at DESC, id DESC").Limit(imageSessionDetailTaskLimit).Find(&page).Error; err != nil {
			return nil, err
		}
		scannedTaskModels = append(scannedTaskModels, page...)
	}
	sort.Slice(scannedTaskModels, func(i, j int) bool {
		if scannedTaskModels[i].CreatedAt.Equal(scannedTaskModels[j].CreatedAt) {
			return scannedTaskModels[i].ID > scannedTaskModels[j].ID
		}
		return scannedTaskModels[i].CreatedAt.After(scannedTaskModels[j].CreatedAt)
	})
	scannedTasks := make([]taskRow, 0, len(scannedTaskModels))
	taskIDs := make([]string, 0, len(scannedTaskModels))
	seenTask := map[string]struct{}{}
	for _, m := range scannedTaskModels {
		if _, ok := seenTask[m.ID]; ok {
			continue
		}
		seenTask[m.ID] = struct{}{}
		row := taskFromModel(m)
		scannedTasks = append(scannedTasks, row)
		taskIDs = append(taskIDs, row.ID)
	}
	effectsByTask, err := listEffectsByTaskIDs(ctx, tx, taskIDs)
	if err != nil {
		return nil, err
	}
	overview := s.queueOverview(ctx, tx)
	positions := queuedPositions(ctx, tx, taskIDs)
	tasks := make([]TaskResponse, 0, len(scannedTasks))
	for _, row := range scannedTasks {
		notes := []string{}
		if row.ResultGenerationGroupID != nil {
			notes = notesByGroup[*row.ResultGenerationGroupID]
			if notes == nil {
				notes = []string{}
			}
		}
		tasks = append(tasks, serializeTask(row, effectsByTask[row.ID], notes, overview, positions))
	}
	if tasks == nil {
		tasks = []TaskResponse{}
	}
	return tasks, nil
}

func listReferenceAssets(ctx context.Context, tx *gorm.DB, sessionID string, limit int) ([]assetRow, error) {
	var assets []schema.ImageSessionAssets
	query := tx.WithContext(ctx).Where("session_id = ? AND kind = ?", sessionID, kindReference).Order("created_at ASC, id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&assets).Error; err != nil {
		return nil, err
	}
	rows, err := assetsFromModels(tx, assets)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []assetRow{}, nil
	}
	return rows, nil
}

func loadAssetsByIDs(ctx context.Context, tx *gorm.DB, sessionID string, ids []string) (map[string]assetRow, error) {
	out := map[string]assetRow{}
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var assets []schema.ImageSessionAssets
	if err := tx.WithContext(ctx).Where("session_id = ? AND id IN ?", sessionID, ids).Find(&assets).Error; err != nil {
		return nil, err
	}
	rows, err := assetsFromModels(tx, assets)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

func loadNotesByGroupIDs(ctx context.Context, tx *gorm.DB, sessionID string, groupIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	groupIDs = uniqueIDs(groupIDs)
	if len(groupIDs) == 0 {
		return out, nil
	}
	var rows []schema.ImageSessionRounds
	if err := tx.WithContext(ctx).
		Select("generation_group_id", "provider_output_json").
		Where("session_id = ? AND generation_group_id IN ?", sessionID, groupIDs).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.GenerationGroupID == nil || *row.GenerationGroupID == "" {
			continue
		}
		if _, ok := out[*row.GenerationGroupID]; ok {
			continue
		}
		out[*row.GenerationGroupID] = extractNotes(ptrBytes(row.ProviderOutputJSON))
	}
	return out, nil
}

func listEffectsByTaskIDs(ctx context.Context, tx *gorm.DB, taskIDs []string) (map[string][]EffectResponse, error) {
	out := map[string][]EffectResponse{}
	taskIDs = uniqueIDs(taskIDs)
	for _, id := range taskIDs {
		out[id] = []EffectResponse{}
	}
	if len(taskIDs) == 0 {
		return out, nil
	}
	var rows []schema.ImageSessionProviderEffects
	// These raw payloads belong to execution/reconciliation, not the task response.
	if err := tx.WithContext(ctx).Omit("request_json", "result_json").Where("generation_task_id IN ?", taskIDs).
		Order("generation_task_id ASC, candidate_start_index ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.GenerationTaskID] = append(out[row.GenerationTaskID], effectFromModel(row))
	}
	return out, nil
}

type queueOverview struct {
	Active, Running, Queued, Max int
}

// queueOverview 用共享队列总览统计当前 queued/running 占用，不取 admission 节点计数。查询失败返回零值（仍尽量带上 Max），详情页仍能渲染，只是队列位可能过时。
func (s Service) queueOverview(ctx context.Context, tx *gorm.DB) queueOverview {
	snap, err := generation.LoadQueueOverview(ctx, tx)
	if err != nil {
		max, _ := generation.LoadMaxConcurrent(ctx, tx)
		return queueOverview{Max: max}
	}
	return queueOverview{
		Active:  snap.OverviewActive(),
		Running: snap.OverviewRunning,
		Queued:  snap.OverviewQueued,
		Max:     snap.Max,
	}
}

func queuedPositions(ctx context.Context, tx *gorm.DB, neededIDs []string) map[string]int {
	out := map[string]int{}
	neededIDs = uniqueIDs(neededIDs)
	if len(neededIDs) == 0 {
		return out
	}
	var positions []struct {
		ID       string
		Position int
	}
	// 先在完整队列内排名，再筛所需 ID，避免把全库 queued ID 装入 Go。
	queue := tx.Model(&schema.ImageSessionGenerationTasks{}).
		Select("id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS position").Where("status = ?", "queued")
	if err := tx.WithContext(ctx).Table("(?) AS queued", queue).
		Where("id IN ?", neededIDs).Scan(&positions).Error; err != nil {
		return out
	}
	for _, item := range positions {
		out[item.ID] = item.Position
	}
	return out
}

// serializeTask 投影任务及队列位置。effects/notes 为 nil 时写成空切片。
func serializeTask(row taskRow, effects []EffectResponse, notes []string, overview queueOverview, positions map[string]int) TaskResponse {
	if notes == nil {
		notes = []string{}
	}
	if effects == nil {
		effects = []EffectResponse{}
	}
	refs := decodeStringSlice(row.SelectedRefs)
	cancelable := row.Status == "queued" || row.Status == "running"
	var ahead, pos *int
	if row.Status == "queued" {
		if p, ok := positions[row.ID]; ok {
			pos = &p
			a := p - 1
			if a < 0 {
				a = 0
			}
			ahead = &a
		}
	}
	return TaskResponse{
		ID: row.ID, SessionID: row.SessionID, Status: row.Status, Prompt: row.Prompt, Size: row.Size,
		BaseAssetID: row.BaseAssetID, SelectedReferenceAssetIDs: refs, GenerationCount: row.GenerationCount,
		CompletedCandidates: row.CompletedCandidates, ActiveCandidateIndex: row.ActiveCandidateIndex,
		ProgressPhase: row.ProgressPhase, ProgressUpdatedAt: row.ProgressUpdatedAt,
		ProviderResponseID: row.ProviderResponseID, ProviderResponseStatus: row.ProviderResponseStatus,
		ProgressMetadata: decodeMap(row.ProgressMetadata), FailureReason: row.FailureReason,
		ResultGenerationGroupID: row.ResultGenerationGroupID, ToolOptions: decodeMap(row.ToolOptions),
		ProviderNotes: notes, ProviderEffects: effects, Attempts: row.Attempts, IsRetryable: row.IsRetryable,
		IsCancelable: cancelable, CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		QueueActiveCount: overview.Active, QueueRunningCount: overview.Running, QueueQueuedCount: overview.Queued,
		QueueMaxConcurrentTasks: overview.Max, QueuedAheadCount: ahead, QueuePosition: pos,
	}
}

func extractNotes(outputJSON []byte) []string {
	meta := extractProviderMeta(outputJSON)
	return meta.notes
}

func extractActualSize(outputJSON []byte) *string {
	meta := extractProviderMeta(outputJSON)
	return meta.actual
}

type providerMeta struct {
	actual *string
	notes  []string
}

// extractProviderMeta 从 output_json 抽 model/response_id。缺字段或非法 JSON 返回零值，不报错。
func extractProviderMeta(outputJSON []byte) providerMeta {
	out := providerMeta{notes: []string{}}
	if len(outputJSON) == 0 {
		return out
	}
	var payload map[string]any
	if err := json.Unmarshal(outputJSON, &payload); err != nil {
		return out
	}
	raw, ok := payload["_productflow"].(map[string]any)
	if !ok {
		return out
	}
	if s, ok := raw["actual_image_size"].(string); ok {
		s = stringsTrim(s)
		if s != "" {
			out.actual = &s
		}
	}
	if notes, ok := raw["notes"].([]any); ok {
		for _, item := range notes {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			msg, _ := m["message"].(string)
			msg = stringsTrim(msg)
			if msg != "" {
				out.notes = append(out.notes, msg)
				if len(out.notes) >= 3 {
					break
				}
			}
		}
	}
	return out
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
