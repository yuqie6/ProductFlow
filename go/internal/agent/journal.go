package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"gorm.io/gorm"
)

const eventSchemaVersion = 1

func appendUserJournalEvent(ctx context.Context, gdb *gorm.DB, projectionID, kind string, payload map[string]any, ignorable bool) error {
	if !inSet(eventKinds, kind) && !ignorable {
		return apperr.Validation("Agent event kind 不受支持")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return apperr.Validation("Agent event payload 必须是 JSON object")
	}
	if len(raw) > maxEventPayloadBytes {
		return apperr.Validation("Agent event payload 超过大小限制")
	}
	var proj schema.AgentTurnProjections
	if err := gdb.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", projectionID).Take(&proj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("Agent Turn 不存在")
		}
		return err
	}
	if proj.HarnessTurnID == nil || strings.TrimSpace(*proj.HarnessTurnID) == "" {
		return nil
	}
	row, err := loadTurnByID(ctx, gdb, projectionID)
	if err != nil {
		return err
	}
	runID := row.ConversationHarnessRunID
	if row.TaskHarnessRunID != nil && *row.TaskHarnessRunID != "" {
		runID = *row.TaskHarnessRunID
	}
	var last int
	if err := gdb.WithContext(ctx).Model(&schema.AgentTurnEvents{}).
		Where("turn_projection_id = ?", projectionID).
		Select("COALESCE(MAX(sequence), 0)").Scan(&last).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	ev := schema.AgentTurnEvents{
		ID:               newID(),
		TurnProjectionID: projectionID,
		RunID:            runID,
		TurnID:           *proj.HarnessTurnID,
		SchemaVersion:    eventSchemaVersion,
		Sequence:         last + 1,
		Kind:             kind,
		Ignorable:        ignorable,
		PayloadJSON:      string(raw),
		CreatedAt:        now,
	}
	if err := gdb.WithContext(ctx).Create(&ev).Error; err != nil {
		if uniqueViolation(err) {
			return apperr.Conflict("Agent event sequence 必须连续提交")
		}
		return err
	}
	return notify.Publish(ctx, gdb, notify.ChannelTurn, projectionID)
}

func appendApprovalResolved(ctx context.Context, gdb *gorm.DB, projectionID, approvalID, kind, decision string, extra map[string]any) error {
	payload := map[string]any{
		"approval_id":   approvalID,
		"approval_kind": kind,
		"decision":      decision,
	}
	for key, value := range extra {
		payload[key] = value
	}
	return appendUserJournalEvent(ctx, gdb, projectionID, "approval/resolved", payload, false)
}

func projectionIDForWorkflowRequest(ctx context.Context, gdb *gorm.DB, requestID string) (string, error) {
	var proj schema.AgentTurnProjections
	err := gdb.WithContext(ctx).Select("id").
		Where("workflow_run_request_id = ?", requestID).
		Order("created_at DESC, id DESC").
		Take(&proj).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return proj.ID, err
}

func projectionIDForLibraryRevision(ctx context.Context, gdb *gorm.DB, revisionID string) (string, error) {
	var proj schema.AgentTurnProjections
	err := gdb.WithContext(ctx).Select("id").
		Where("library_organization_draft_revision_id = ?", revisionID).
		Order("created_at DESC, id DESC").
		Take(&proj).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return proj.ID, err
}

func latestApprovalRequest(ctx context.Context, gdb *gorm.DB, projectionID, wantKind string) (approvalID, kind string, err error) {
	var row schema.AgentTurnEvents
	q := gdb.WithContext(ctx).Where("turn_projection_id = ? AND kind = ?", projectionID, "approval/requested")
	if err := q.Order("sequence DESC").Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", nil
		}
		return "", "", err
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(row.PayloadJSON), &payload)
	kind, _ = payload["approval_kind"].(string)
	if wantKind != "" && kind != wantKind {
		return "", "", nil
	}
	approvalID, _ = payload["approval_id"].(string)
	if approvalID == "" {
		approvalID, _ = payload["id"].(string)
	}
	return approvalID, kind, nil
}

func SyncGraphProposalDecision(ctx context.Context, gdb *gorm.DB, _, _, proposalID, decision string) error {
	proposalID = strings.TrimSpace(proposalID)
	if proposalID == "" {
		return nil
	}
	var row schema.AgentTurnEvents
	err := gdb.WithContext(ctx).
		Where("kind = ? AND payload_json::jsonb->>'approval_kind' = ? AND payload_json::jsonb->>'approval_id' = ?",
			"approval/requested", "graph_proposal", proposalID).
		Order("sequence DESC").
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := appendApprovalResolved(ctx, gdb, row.TurnProjectionID, proposalID, "graph_proposal", decision, map[string]any{
		"proposal_id": proposalID,
	}); err != nil {
		return err
	}
	var projection schema.AgentTurnProjections
	if err := gdb.WithContext(ctx).Where("id = ?", row.TurnProjectionID).Take(&projection).Error; err != nil {
		return err
	}
	if projection.Status != "awaiting_confirmation" {
		return nil
	}
	now := time.Now().UTC()
	if err := gdb.WithContext(ctx).Model(&schema.AgentTurnProjections{}).Where("id = ?", projection.ID).Updates(map[string]any{
		"status":      "succeeded",
		"finished_at": gorm.Expr("COALESCE(finished_at, ?)", now),
		"updated_at":  now,
	}).Error; err != nil {
		return err
	}
	if err := applyConversationStatus(ctx, gdb, projection.ConversationID, "succeeded"); err != nil {
		return err
	}
	if projection.TaskID != nil {
		summary := "图提案已确认"
		if decision == "discarded" {
			summary = "图提案已丢弃"
		}
		if err := updateTaskFromTurn(ctx, gdb, *projection.TaskID, "succeeded", "", summary); err != nil {
			return err
		}
	}
	return nil
}
