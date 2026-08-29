package imagesession

import (
	"context"
	"encoding/json"
	"time"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

func (s Service) serializeSummary(ctx context.Context, tx *gorm.DB, sess sessionRow) (SummaryResponse, error) {
	var rounds int
	_ = pfdb.QueryRow(ctx, tx, `SELECT COUNT(*) FROM image_session_rounds WHERE session_id = $1`, sess.ID).Scan(&rounds)
	var latest *AssetResponse
	var asset assetRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT a.id, a.session_id, a.kind, a.original_filename, a.mime_type, m.storage_path, a.media_object_id, a.created_at
		FROM image_session_rounds r
		JOIN image_session_assets a ON a.id = r.generated_asset_id
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE r.session_id = $1
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT 1
	`, sess.ID).Scan(&asset.ID, &asset.SessionID, &asset.Kind, &asset.OriginalFilename, &asset.MIMEType, &asset.StoragePath, &asset.MediaObjectID, &asset.CreatedAt)
	if err == nil {
		resp := serializeAsset(asset)
		latest = &resp
	}
	return SummaryResponse{
		ID: sess.ID, Title: sess.Title, RoundsCount: rounds,
		LatestGeneratedAsset: latest, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}, nil
}

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
	roundRows, err := pfdb.Query(ctx, tx, `
		SELECT id, prompt, assistant_message, size, model_name, provider_name, prompt_version,
		       provider_response_id, previous_response_id, image_generation_call_id, generation_group_id,
		       candidate_index, candidate_count, base_asset_id, selected_reference_asset_ids,
		       provider_output_json, generated_asset_id, created_at
		FROM image_session_rounds WHERE session_id = $1
		ORDER BY created_at ASC, candidate_index ASC, id ASC
	`, sessionID)
	if err != nil {
		return DetailResponse{}, err
	}
	type roundScan struct {
		id, prompt, assistant, size, model, provider, version, generatedID string
		respID, prevID, callID, groupID, baseID                            *string
		candIndex, candCount                                               int
		refsJSON, outputJSON                                               []byte
		createdAt                                                          time.Time
	}
	var scannedRounds []roundScan
	for roundRows.Next() {
		var item roundScan
		if err := roundRows.Scan(
			&item.id, &item.prompt, &item.assistant, &item.size, &item.model, &item.provider, &item.version,
			&item.respID, &item.prevID, &item.callID, &item.groupID, &item.candIndex, &item.candCount, &item.baseID, &item.refsJSON,
			&item.outputJSON, &item.generatedID, &item.createdAt,
		); err != nil {
			roundRows.Close()
			return DetailResponse{}, err
		}
		scannedRounds = append(scannedRounds, item)
	}
	roundRows.Close()
	if err := roundRows.Err(); err != nil {
		return DetailResponse{}, err
	}
	rounds := make([]RoundResponse, 0, len(scannedRounds))
	notesByGroup := map[string][]string{}
	for _, item := range scannedRounds {
		gen, ok := assetByID[item.generatedID]
		if !ok {
			loaded, err := loadAsset(ctx, tx, sessionID, item.generatedID)
			if err != nil {
				return DetailResponse{}, err
			}
			gen = loaded
			assetByID[item.generatedID] = gen
		}
		notes := extractNotes(item.outputJSON)
		if item.groupID != nil && len(notes) > 0 {
			notesByGroup[*item.groupID] = notes
		}
		actual := extractActualSize(item.outputJSON)
		rounds = append(rounds, RoundResponse{
			ID: item.id, Prompt: item.prompt, AssistantMessage: item.assistant, Size: item.size,
			ModelName: item.model, ProviderName: item.provider, PromptVersion: item.version,
			ProviderResponseID: item.respID, PreviousResponseID: item.prevID, ImageGenerationCallID: item.callID,
			GenerationGroupID: item.groupID, CandidateIndex: item.candIndex, CandidateCount: item.candCount,
			BaseAssetID: item.baseID, SelectedReferenceAssetIDs: decodeStringSlice(item.refsJSON),
			ActualSize: actual, ProviderNotes: notes, GeneratedAsset: serializeAsset(gen), CreatedAt: item.createdAt,
		})
	}

	overview := s.queueOverview(ctx, tx)
	positions := queuedPositions(ctx, tx)
	taskRows, err := pfdb.Query(ctx, tx, `
		SELECT id, session_id, status, prompt, size, base_asset_id, selected_reference_asset_ids, tool_options,
		       generation_count, completed_candidates, active_candidate_index, progress_phase, progress_updated_at,
		       provider_response_id, provider_response_status, progress_metadata, failure_reason, result_generation_group_id,
		       created_at, started_at, finished_at, attempts, active_attempt_id, is_retryable
		FROM image_session_generation_tasks WHERE session_id = $1
		ORDER BY created_at DESC, id DESC
	`, sessionID)
	if err != nil {
		return DetailResponse{}, err
	}
	var scannedTasks []taskRow
	for taskRows.Next() {
		var row taskRow
		if err := taskRows.Scan(
			&row.ID, &row.SessionID, &row.Status, &row.Prompt, &row.Size, &row.BaseAssetID, &row.SelectedRefs, &row.ToolOptions,
			&row.GenerationCount, &row.CompletedCandidates, &row.ActiveCandidateIndex, &row.ProgressPhase, &row.ProgressUpdatedAt,
			&row.ProviderResponseID, &row.ProviderResponseStatus, &row.ProgressMetadata, &row.FailureReason, &row.ResultGenerationGroupID,
			&row.CreatedAt, &row.StartedAt, &row.FinishedAt, &row.Attempts, &row.ActiveAttemptID, &row.IsRetryable,
		); err != nil {
			taskRows.Close()
			return DetailResponse{}, err
		}
		scannedTasks = append(scannedTasks, row)
	}
	taskRows.Close()
	if err := taskRows.Err(); err != nil {
		return DetailResponse{}, err
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
	rows, err := pfdb.Query(ctx, tx, `
		SELECT id, generation_task_id, candidate_start_index, candidate_count, operation_key, effect_kind,
		       request_hash, provider_name, effect_result, reconciliation_state, provider_response_id, provider_status,
		       detail, created_at, updated_at
		FROM image_session_provider_effects
		WHERE generation_task_id = $1
		ORDER BY candidate_start_index ASC, id ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffectResponse
	for rows.Next() {
		var item EffectResponse
		if err := rows.Scan(
			&item.ID, &item.GenerationTaskID, &item.CandidateStartIndex, &item.CandidateCount, &item.OperationKey, &item.EffectKind,
			&item.RequestHash, &item.ProviderName, &item.EffectResult, &item.ReconciliationState, &item.ProviderResponseID, &item.ProviderStatus,
			&item.Detail, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if out == nil {
		out = []EffectResponse{}
	}
	return out, rows.Err()
}

type queueOverview struct {
	Active, Running, Queued, Max int
}

func (s Service) queueOverview(ctx context.Context, tx *gorm.DB) queueOverview {
	max := 3
	if s.Settings != nil {
		if runtime, err := s.Settings.Runtime(ctx); err == nil && runtime.ImageGenerationMaxDimension > 0 {
			// 容量上限来自 app_settings generation_max_concurrent_tasks，这里读 graph 共用函数的默认。
		}
	}
	_ = pfdb.QueryRow(ctx, tx, `SELECT COALESCE((SELECT value FROM app_settings WHERE key = 'generation_max_concurrent_tasks'), '3')`).Scan(new(string))
	var raw *string
	_ = pfdb.QueryRow(ctx, tx, `SELECT value FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`).Scan(&raw)
	if raw != nil && *raw != "" {
		n := 0
		for _, ch := range *raw {
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
	var sessionRunning, sessionQueued int
	_ = pfdb.QueryRow(ctx, tx, `SELECT COUNT(*) FROM image_session_generation_tasks WHERE status = 'running'`).Scan(&sessionRunning)
	_ = pfdb.QueryRow(ctx, tx, `SELECT COUNT(*) FROM image_session_generation_tasks WHERE status = 'queued'`).Scan(&sessionQueued)
	var graphRunning, graphQueued int
	_ = pfdb.QueryRow(ctx, tx, `
		SELECT COUNT(*) FROM workflow_graph_runs r
		WHERE r.status = 'running'
		  AND EXISTS (SELECT 1 FROM workflow_graph_node_runs n WHERE n.graph_run_id = r.id AND n.status = 'running')
	`).Scan(&graphRunning)
	_ = pfdb.QueryRow(ctx, tx, `
		SELECT COUNT(*) FROM workflow_graph_runs r
		WHERE r.status = 'running'
		  AND NOT EXISTS (SELECT 1 FROM workflow_graph_node_runs n WHERE n.graph_run_id = r.id AND n.status = 'running')
		  AND EXISTS (SELECT 1 FROM workflow_graph_node_runs n WHERE n.graph_run_id = r.id AND n.status = 'queued')
	`).Scan(&graphQueued)
	running := sessionRunning + graphRunning
	queued := sessionQueued + graphQueued
	return queueOverview{Active: running + queued, Running: running, Queued: queued, Max: max}
}

func queuedPositions(ctx context.Context, tx *gorm.DB) map[string]int {
	rows, err := pfdb.Query(ctx, tx, `
		SELECT id FROM image_session_generation_tasks WHERE status = 'queued' ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return map[string]int{}
	}
	defer rows.Close()
	out := map[string]int{}
	i := 1
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out
		}
		out[id] = i
		i++
	}
	return out
}

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
