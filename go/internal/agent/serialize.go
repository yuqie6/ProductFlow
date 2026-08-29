package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func serializeTurn(row turnRow, focus *CanvasFocus) TurnResponse {
	assetIDs := []string{}
	if len(row.InputAssetIDs) > 0 {
		_ = json.Unmarshal(row.InputAssetIDs, &assetIDs)
		if assetIDs == nil {
			assetIDs = []string{}
		}
	}
	steps := []map[string]any{}
	if len(row.ToolStepsJSON) > 0 {
		_ = json.Unmarshal(row.ToolStepsJSON, &steps)
		if steps == nil {
			steps = []map[string]any{}
		}
	}
	runID := row.ConversationHarnessRunID
	if row.TaskHarnessRunID != nil && *row.TaskHarnessRunID != "" {
		runID = *row.TaskHarnessRunID
	}
	var question json.RawMessage
	if len(row.QuestionJSON) > 0 {
		question = json.RawMessage(row.QuestionJSON)
	}
	var answer json.RawMessage
	if len(row.QuestionAnswerJSON) > 0 {
		answer = json.RawMessage(row.QuestionAnswerJSON)
	}
	return TurnResponse{
		ID: row.ID, ConversationID: row.ConversationID, TaskID: row.TaskID,
		HarnessRunID: runID, HarnessTurnID: row.HarnessTurnID, IdempotencyKey: row.IdempotencyKey,
		InputText: row.InputText, InputAssetIDs: assetIDs, Status: row.Status,
		ResumeRequired: row.ResumeRequired, OutputText: row.OutputText, ErrorText: row.ErrorText,
		Question: question, QuestionAnswer: answer, ContinuationTurnID: row.ContinuationTurnID,
		ToolSteps: steps, ArtifactName: row.ArtifactName, ArtifactStepID: row.ArtifactStepID,
		LibraryOrganizationDraftRevisionID: row.LibraryOrgDraftRevisionID,
		WorkflowRunRequestID: row.WorkflowRunRequestID, PageContextSnapshotID: row.PageContextSnapshotID,
		SyncError: row.SyncError, CanvasFocus: focus, FinishedAt: row.FinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func loadTurn(ctx context.Context, pgxTx pgx.Tx, productID *string, conversationID, projectionID string) (turnRow, error) {
	row, err := scanTurn(ctx, pgxTx, `
		SELECT t.id, t.conversation_id, t.task_id, t.harness_turn_id, t.idempotency_key, t.request_hash,
			t.input_text, t.input_asset_ids_json, t.status, t.resume_required, t.output_text, t.error_text,
			t.question_json, t.question_answer_json, t.continuation_turn_id, t.tool_steps_json,
			t.artifact_name, t.artifact_step_id, t.library_organization_draft_revision_id,
			t.workflow_run_request_id, t.page_context_snapshot_id, t.sync_error, t.finished_at,
			t.created_at, t.updated_at, c.harness_run_id, tk.harness_run_id, c.scope_type, c.product_id
		FROM agent_turn_projections t
		JOIN agent_conversations c ON c.id = t.conversation_id
		LEFT JOIN agent_tasks tk ON tk.id = t.task_id
		WHERE t.id = $1 AND t.conversation_id = $2
	`, projectionID, conversationID)
	if err != nil {
		if isNoRows(err) {
			if _, convErr := loadConversation(ctx, pgxTx, productID, conversationID); convErr != nil {
				return turnRow{}, convErr
			}
			return turnRow{}, apperr.NotFound("Agent turn 不存在")
		}
		return turnRow{}, err
	}
	if productID == nil && row.ConversationScope != "global" {
		return turnRow{}, apperr.NotFound("Agent turn 不存在")
	}
	if productID != nil && (row.ConversationScope != "product_workflow" || row.ConversationProductID == nil || *row.ConversationProductID != *productID) {
		if _, convErr := loadConversation(ctx, pgxTx, productID, conversationID); convErr != nil {
			return turnRow{}, convErr
		}
		return turnRow{}, apperr.NotFound("Agent turn 不存在")
	}
	return row, nil
}

func loadTurnByID(ctx context.Context, pgxTx pgx.Tx, projectionID string) (turnRow, error) {
	row, err := scanTurn(ctx, pgxTx, `
		SELECT t.id, t.conversation_id, t.task_id, t.harness_turn_id, t.idempotency_key, t.request_hash,
			t.input_text, t.input_asset_ids_json, t.status, t.resume_required, t.output_text, t.error_text,
			t.question_json, t.question_answer_json, t.continuation_turn_id, t.tool_steps_json,
			t.artifact_name, t.artifact_step_id, t.library_organization_draft_revision_id,
			t.workflow_run_request_id, t.page_context_snapshot_id, t.sync_error, t.finished_at,
			t.created_at, t.updated_at, c.harness_run_id, tk.harness_run_id, c.scope_type, c.product_id
		FROM agent_turn_projections t
		JOIN agent_conversations c ON c.id = t.conversation_id
		LEFT JOIN agent_tasks tk ON tk.id = t.task_id
		WHERE t.id = $1
	`, projectionID)
	if err != nil {
		if isNoRows(err) {
			return turnRow{}, apperr.NotFound("Agent turn 不存在")
		}
		return turnRow{}, err
	}
	return row, nil
}

func scanTurn(ctx context.Context, pgxTx pgx.Tx, query string, args ...any) (turnRow, error) {
	var row turnRow
	err := pgxTx.QueryRow(ctx, query, args...).Scan(
		&row.ID, &row.ConversationID, &row.TaskID, &row.HarnessTurnID, &row.IdempotencyKey, &row.RequestHash,
		&row.InputText, &row.InputAssetIDs, &row.Status, &row.ResumeRequired, &row.OutputText, &row.ErrorText,
		&row.QuestionJSON, &row.QuestionAnswerJSON, &row.ContinuationTurnID, &row.ToolStepsJSON,
		&row.ArtifactName, &row.ArtifactStepID, &row.LibraryOrgDraftRevisionID,
		&row.WorkflowRunRequestID, &row.PageContextSnapshotID, &row.SyncError, &row.FinishedAt,
		&row.CreatedAt, &row.UpdatedAt, &row.ConversationHarnessRunID, &row.TaskHarnessRunID,
		&row.ConversationScope, &row.ConversationProductID,
	)
	return row, err
}

func canvasFocusForTurns(ctx context.Context, pgxTx pgx.Tx, turns []turnRow) (map[string]*CanvasFocus, error) {
	out := map[string]*CanvasFocus{}
	if len(turns) == 0 {
		return out, nil
	}
	earliest := turns[0].CreatedAt
	conversationID := turns[0].ConversationID
	for _, turn := range turns[1:] {
		if turn.CreatedAt.Before(earliest) {
			earliest = turn.CreatedAt
		}
	}
	rows, err := pgxTx.Query(ctx, `
		SELECT id, result_json, created_at
		FROM agent_tool_mutations
		WHERE conversation_id = $1 AND tool_name = 'focus_canvas_items_v1' AND status = 'applied' AND created_at >= $2
		ORDER BY created_at DESC, id DESC
		LIMIT 200
	`, conversationID, earliest)
	if err != nil {
		return nil, err
	}
	type mut struct {
		id        string
		result    []byte
		createdAt time.Time
	}
	var mutations []mut
	for rows.Next() {
		var item mut
		if err := rows.Scan(&item.id, &item.result, &item.createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		mutations = append(mutations, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, turn := range turns {
		for _, mutation := range mutations {
			if mutation.createdAt.Before(turn.CreatedAt) {
				continue
			}
			if turn.FinishedAt != nil && mutation.createdAt.After(*turn.FinishedAt) {
				continue
			}
			var payload map[string]any
			if json.Unmarshal(mutation.result, &payload) != nil {
				break
			}
			focus := canvasFocusFromResult(payload, mutation.id)
			if focus != nil {
				out[turn.ID] = focus
			}
			break
		}
	}
	return out, nil
}

func canvasFocusFromResult(payload map[string]any, requestID string) *CanvasFocus {
	nodeIDs := stringSlice(payload["node_ids"])
	edgeIDs := stringSlice(payload["edge_ids"])
	groupIDs := stringSlice(payload["group_ids"])
	if len(nodeIDs)+len(edgeIDs)+len(groupIDs) == 0 {
		return nil
	}
	if id, ok := payload["request_id"].(string); ok && id != "" {
		requestID = id
	}
	return &CanvasFocus{RequestID: requestID, NodeIDs: nodeIDs, EdgeIDs: edgeIDs, GroupIDs: groupIDs}
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		if typed, ok := v.([]string); ok {
			return typed
		}
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
