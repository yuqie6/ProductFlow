package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
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
		InputText: row.InputText, InputAssetIDs: assetIDs, Status: row.Status, TerminalReasonCode: row.TerminalReasonCode,
		ResumeRequired: row.ResumeRequired, OutputText: row.OutputText, ThinkingText: row.ThinkingText, ErrorText: row.ErrorText,
		Question: question, QuestionAnswer: answer, ContinuationTurnID: row.ContinuationTurnID,
		ToolSteps: steps, ArtifactName: row.ArtifactName, ArtifactStepID: row.ArtifactStepID,
		LibraryOrganizationDraftRevisionID: row.LibraryOrgDraftRevisionID,
		WorkflowRunRequestID:               row.WorkflowRunRequestID, PageContextSnapshotID: row.PageContextSnapshotID,
		SyncError: row.SyncError, CanvasFocus: focus, FinishedAt: row.FinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

type turnJoinDest struct {
	schema.AgentTurnProjections
	ConversationHarnessRunID string  `gorm:"column:conversation_harness_run_id"`
	TaskHarnessRunID         *string `gorm:"column:task_harness_run_id"`
	ConversationScope        string  `gorm:"column:conversation_scope"`
	ConversationProductID    *string `gorm:"column:conversation_product_id"`
}

func jsonPtrBytes(s *string) []byte {
	if s == nil {
		return nil
	}
	return []byte(*s)
}

func turnFromModels(proj schema.AgentTurnProjections, convHarness string, taskHarness *string, scope string, productID *string) turnRow {
	return turnRow{
		ID: proj.ID, ConversationID: proj.ConversationID, TaskID: proj.TaskID,
		HarnessTurnID: proj.HarnessTurnID, IdempotencyKey: proj.IdempotencyKey, RequestHash: proj.RequestHash,
		InputText: proj.InputText, InputAssetIDs: []byte(proj.InputAssetIdsJSON), Status: proj.Status, TerminalReasonCode: proj.TerminalReasonCode,
		ResumeRequired: proj.ResumeRequired, OutputText: proj.OutputText, ThinkingText: proj.ThinkingText, ErrorText: proj.ErrorText,
		QuestionJSON: jsonPtrBytes(proj.QuestionJSON), QuestionAnswerJSON: jsonPtrBytes(proj.QuestionAnswerJSON),
		ContinuationTurnID: proj.ContinuationTurnID, ToolStepsJSON: []byte(proj.ToolStepsJSON),
		ArtifactName: proj.ArtifactName, ArtifactStepID: proj.ArtifactStepID,
		LibraryOrgDraftRevisionID: proj.LibraryOrganizationDraftRevisionID,
		WorkflowRunRequestID:      proj.WorkflowRunRequestID, PageContextSnapshotID: proj.PageContextSnapshotID,
		SyncError: proj.SyncError, FinishedAt: proj.FinishedAt, CreatedAt: proj.CreatedAt, UpdatedAt: proj.UpdatedAt,
		ConversationHarnessRunID: convHarness, TaskHarnessRunID: taskHarness,
		ConversationScope: scope, ConversationProductID: productID,
	}
}

func loadTurn(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID, projectionID string) (turnRow, error) {
	row, err := scanTurn(ctx, pgxTx, projectionID, &conversationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
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

func loadTurnByID(ctx context.Context, pgxTx *gorm.DB, projectionID string) (turnRow, error) {
	row, err := scanTurn(ctx, pgxTx, projectionID, nil)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return turnRow{}, apperr.NotFound("Agent turn 不存在")
		}
		return turnRow{}, err
	}
	return row, nil
}

func scanTurn(ctx context.Context, pgxTx *gorm.DB, projectionID string, conversationID *string) (turnRow, error) {
	var dest turnJoinDest
	q := pgxTx.WithContext(ctx).Model(&schema.AgentTurnProjections{}).
		Select(`agent_turn_projections.*,
			agent_conversations.harness_run_id AS conversation_harness_run_id,
			agent_tasks.harness_run_id AS task_harness_run_id,
			agent_conversations.scope_type AS conversation_scope,
			agent_conversations.product_id AS conversation_product_id`).
		Joins("JOIN agent_conversations ON agent_conversations.id = agent_turn_projections.conversation_id").
		Joins("LEFT JOIN agent_tasks ON agent_tasks.id = agent_turn_projections.task_id").
		Where("agent_turn_projections.id = ?", projectionID)
	if conversationID != nil {
		q = q.Where("agent_turn_projections.conversation_id = ?", *conversationID)
	}
	if err := q.Take(&dest).Error; err != nil {
		return turnRow{}, err
	}
	return turnFromModels(dest.AgentTurnProjections, dest.ConversationHarnessRunID, dest.TaskHarnessRunID, dest.ConversationScope, dest.ConversationProductID), nil
}

func canvasFocusForTurns(ctx context.Context, pgxTx *gorm.DB, turns []turnRow) (map[string]*CanvasFocus, error) {
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
	var records []schema.AgentToolMutations
	err := pgxTx.WithContext(ctx).
		Where("conversation_id = ? AND tool_name = ? AND status = ? AND created_at >= ?", conversationID, "focus_canvas_items_v1", "applied", earliest).
		Order("created_at DESC, id DESC").
		Limit(200).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	type mut struct {
		id        string
		result    []byte
		createdAt time.Time
	}
	mutations := make([]mut, 0, len(records))
	for _, item := range records {
		mutations = append(mutations, mut{
			id:        item.ID,
			result:    jsonPtrBytes(item.ResultJSON),
			createdAt: item.CreatedAt,
		})
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
