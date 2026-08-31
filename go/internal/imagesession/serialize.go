package imagesession

import (
	"context"
	"encoding/json"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// serializeSummary 投影会话列表项：轮次数与是否仍有 queued/running 任务。
func (s Service) serializeSummary(ctx context.Context, tx *gorm.DB, sess sessionRow) (SummaryResponse, error) {
	tx = tx.WithContext(ctx)
	var rounds int64
	if err := tx.Model(&schema.ImageSessionRounds{}).Where("session_id = ?", sess.ID).Count(&rounds).Error; err != nil {
		return SummaryResponse{}, err
	}
	var latest *AssetResponse
	var round schema.ImageSessionRounds
	err := tx.Where("session_id = ?", sess.ID).Order("created_at DESC, id DESC").Take(&round).Error
	if err == nil {
		asset, loadErr := loadAsset(ctx, tx, sess.ID, round.GeneratedAssetID)
		if loadErr == nil {
			resp := serializeAsset(asset)
			latest = &resp
		}
	}
	return SummaryResponse{
		ID: sess.ID, Title: sess.Title, RoundsCount: int(rounds),
		LatestGeneratedAsset: latest, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}, nil
}

// loadDetail 组装会话详情：素材、轮次、任务与队列位置。会话不存在返回 404。
func (s Service) loadDetail(ctx context.Context, tx *gorm.DB, sessionID string) (DetailResponse, error) {
	sess, err := loadSession(ctx, tx, sessionID)
	if err != nil {
		return DetailResponse{}, err
	}
	assets, err := listAssets(ctx, tx, sessionID)
	if err != nil {
		return DetailResponse{}, err
	}
	assetByID := map[string]assetRow{}
	assetResp := make([]AssetResponse, 0, len(assets))
	for _, a := range assets {
		assetByID[a.ID] = a
		assetResp = append(assetResp, serializeAsset(a))
	}
	var scannedRounds []schema.ImageSessionRounds
	if err := tx.Where("session_id = ?", sessionID).Order("created_at ASC, candidate_index ASC, id ASC").Find(&scannedRounds).Error; err != nil {
		return DetailResponse{}, err
	}
	rounds := make([]RoundResponse, 0, len(scannedRounds))
	notesByGroup := map[string][]string{}
	for _, item := range scannedRounds {
		gen, ok := assetByID[item.GeneratedAssetID]
		if !ok {
			loaded, err := loadAsset(ctx, tx, sessionID, item.GeneratedAssetID)
			if err != nil {
				return DetailResponse{}, err
			}
			gen = loaded
			assetByID[item.GeneratedAssetID] = gen
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

	overview := s.queueOverview(ctx, tx)
	positions := queuedPositions(ctx, tx)
	var scannedTaskModels []schema.ImageSessionGenerationTasks
	if err := tx.Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").Find(&scannedTaskModels).Error; err != nil {
		return DetailResponse{}, err
	}
	scannedTasks := make([]taskRow, 0, len(scannedTaskModels))
	for _, m := range scannedTaskModels {
		scannedTasks = append(scannedTasks, taskFromModel(m))
	}
	tasks := make([]TaskResponse, 0, len(scannedTasks))
	for _, row := range scannedTasks {
		effects, err := listEffects(ctx, tx, row.ID)
		if err != nil {
			return DetailResponse{}, err
		}
		notes := []string{}
		if row.ResultGenerationGroupID != nil {
			notes = notesByGroup[*row.ResultGenerationGroupID]
			if notes == nil {
				notes = []string{}
			}
		}
		tasks = append(tasks, serializeTask(row, effects, notes, overview, positions))
	}
	if rounds == nil {
		rounds = []RoundResponse{}
	}
	if tasks == nil {
		tasks = []TaskResponse{}
	}
	return DetailResponse{
		ID: sess.ID, Title: sess.Title, Assets: assetResp, Rounds: rounds, GenerationTasks: tasks,
		CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}, nil
}

func listEffects(ctx context.Context, tx *gorm.DB, taskID string) ([]EffectResponse, error) {
	var rows []schema.ImageSessionProviderEffects
	if err := tx.WithContext(ctx).Where("generation_task_id = ?", taskID).Order("candidate_start_index ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]EffectResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, effectFromModel(row))
	}
	if out == nil {
		out = []EffectResponse{}
	}
	return out, nil
}

type queueOverview struct {
	Active, Running, Queued, Max int
}

// queueOverview 扫当前 queued/running 任务算占用。查询失败返回零值，详情页仍能渲染，只是队列位可能过时。
func (s Service) queueOverview(ctx context.Context, tx *gorm.DB) queueOverview {
	max := 3
	if s.Settings != nil {
		if runtime, err := s.Settings.Runtime(ctx); err == nil && runtime.ImageGenerationMaxDimension > 0 {
			// 容量上限来自 app_settings generation_max_concurrent_tasks，这里读 graph 共用函数的默认。
		}
	}
	var setting schema.AppSettings
	if err := tx.Where("key = ?", "generation_max_concurrent_tasks").Take(&setting).Error; err == nil && setting.Value != "" {
		n := 0
		for _, ch := range setting.Value {
			if ch < '0' || ch > '9' {
				n = 0
				break
			}
			n = n*10 + int(ch-'0')
		}
		if n > 0 {
			max = n
		}
	}
	var sessionRunning, sessionQueued int64
	_ = tx.Model(&schema.ImageSessionGenerationTasks{}).Where("status = ?", "running").Count(&sessionRunning).Error
	_ = tx.Model(&schema.ImageSessionGenerationTasks{}).Where("status = ?", "queued").Count(&sessionQueued).Error
	var runs []schema.WorkflowGraphRuns
	_ = tx.Where("status = ?", "running").Find(&runs).Error
	runIDs := make([]string, 0, len(runs))
	for _, r := range runs {
		runIDs = append(runIDs, r.ID)
	}
	byRun := map[string][]string{}
	if len(runIDs) > 0 {
		var nodes []schema.WorkflowGraphNodeRuns
		_ = tx.Where("graph_run_id IN ?", runIDs).Find(&nodes).Error
		for _, n := range nodes {
			byRun[n.GraphRunID] = append(byRun[n.GraphRunID], n.Status)
		}
	}
	graphRunning, graphQueued := 0, 0
	for _, r := range runs {
		hasRunning, hasQueued := false, false
		for _, st := range byRun[r.ID] {
			if st == "running" {
				hasRunning = true
			}
			if st == "queued" {
				hasQueued = true
			}
		}
		if hasRunning {
			graphRunning++
		} else if hasQueued {
			graphQueued++
		}
	}
	running := int(sessionRunning) + graphRunning
	queued := int(sessionQueued) + graphQueued
	return queueOverview{Active: running + queued, Running: running, Queued: queued, Max: max}
}

func queuedPositions(ctx context.Context, tx *gorm.DB) map[string]int {
	var tasks []schema.ImageSessionGenerationTasks
	if err := tx.WithContext(ctx).Where("status = ?", "queued").Order("created_at ASC, id ASC").Find(&tasks).Error; err != nil {
		return map[string]int{}
	}
	out := map[string]int{}
	for i, task := range tasks {
		out[task.ID] = i + 1
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
