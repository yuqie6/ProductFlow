package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// ConfirmEvents compares a local journal suffix with PostgreSQL without
// extending a lease or changing the persisted Turn.
func (s Service) ConfirmEvents(ctx context.Context, conversationID, executionID string, inputs []EventAppendInput) (EventConfirmationResponse, error) {
	if len(inputs) > 250 {
		return EventConfirmationResponse{}, apperr.Validation("Agent event confirmation batch 大小无效")
	}
	for index, input := range inputs {
		if err := validateEventInput(input); err != nil {
			return EventConfirmationResponse{}, err
		}
		if index > 0 && input.Sequence != inputs[index-1].Sequence+1 {
			return EventConfirmationResponse{}, apperr.Validation("Agent event confirmation sequence 必须连续")
		}
	}

	var out EventConfirmationResponse
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		if err := lockProjectionForExecution(ctx, gdb, conversationID, executionID); err != nil {
			return err
		}
		var execution schema.AgentTurnExecutions
		if err := gdb.WithContext(ctx).Where("id = ?", executionID).Take(&execution).Error; err != nil {
			return err
		}
		turn, err := scanTurn(ctx, gdb, execution.TurnProjectionID, &conversationID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Agent execution 不存在")
			}
			return err
		}
		if turn.HarnessTurnID == nil || *turn.HarnessTurnID != execution.HarnessTurnID {
			return apperr.Conflict("Agent execution 与 Turn projection 不匹配")
		}
		expectedRunID := leaseHarnessRunID(turn)
		for _, input := range inputs {
			if stringsTrim(input.TurnID) != execution.HarnessTurnID {
				return apperr.Conflict("Agent event turn ID 与 projection 不匹配")
			}
			if stringsTrim(input.RunID) != expectedRunID {
				return apperr.Conflict("Agent event run ID 与 Turn runtime 不匹配")
			}
		}

		out = EventConfirmationResponse{
			Status: "confirmed", Items: make([]EventReceipt, 0, len(inputs)),
		}
		if len(inputs) > 0 {
			out.ConfirmedThrough = inputs[0].Sequence - 1
		}
		if err := gdb.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", execution.TurnProjectionID).
			Select("COALESCE(MAX(sequence), 0)").Scan(&out.PersistedThrough).Error; err != nil {
			return err
		}
		var terminal schema.AgentTurnEvents
		terminalErr := gdb.Where("turn_projection_id = ? AND kind = ?", execution.TurnProjectionID, "turn/end").
			Order("sequence DESC").Take(&terminal).Error
		if terminalErr == nil {
			finishedAt := terminal.CreatedAt
			if turn.FinishedAt != nil {
				finishedAt = *turn.FinishedAt
			}
			out.Terminal = &ConfirmedTerminalEvent{
				EventReceipt: EventReceipt{
					ID: terminal.ID, ProjectionID: terminal.TurnProjectionID, ExecutionID: executionID,
					Sequence: terminal.Sequence, SchemaVersion: terminal.SchemaVersion, Kind: terminal.Kind,
					Ignorable: terminal.Ignorable, CreatedAt: terminal.CreatedAt,
				},
				Payload: json.RawMessage(terminal.PayloadJSON), ProjectionStatus: turn.Status,
				Output: stringValueOrEmpty(turn.OutputText), Thinking: stringValueOrEmpty(turn.ThinkingText),
				Error: stringValueOrEmpty(turn.ErrorText), FinishedAt: finishedAt,
			}
		} else if !errors.Is(terminalErr, gorm.ErrRecordNotFound) {
			return terminalErr
		}

		sequences := make([]int, 0, len(inputs))
		for _, input := range inputs {
			sequences = append(sequences, input.Sequence)
		}
		var persisted []schema.AgentTurnEvents
		if len(sequences) > 0 {
			if err := gdb.WithContext(ctx).
				Where("turn_projection_id = ? AND sequence IN ?", execution.TurnProjectionID, sequences).
				Find(&persisted).Error; err != nil {
				return err
			}
		}
		bySequence := make(map[int]schema.AgentTurnEvents, len(persisted))
		for _, event := range persisted {
			bySequence[event.Sequence] = event
		}

		for _, input := range inputs {
			event, found := bySequence[input.Sequence]
			if !found {
				out.Status = "missing"
				return nil
			}
			kind := stringsTrim(input.Kind)
			if event.SchemaVersion != input.SchemaVersion || event.RunID != stringsTrim(input.RunID) || event.TurnID != stringsTrim(input.TurnID) || event.Kind != kind || event.Ignorable != input.Ignorable || !sameJSON(event.PayloadJSON, input.Payload) {
				metrics.AgentEventSequenceConflicts.Add(1)
				return apperr.ConflictCode(apperr.CodeEventSequenceConflict, "Agent event sequence 已绑定不同内容")
			}
			out.Items = append(out.Items, EventReceipt{
				ID: event.ID, ProjectionID: event.TurnProjectionID, ExecutionID: executionID,
				Sequence: event.Sequence, SchemaVersion: event.SchemaVersion, Kind: event.Kind,
				Ignorable: event.Ignorable, CreatedAt: event.CreatedAt,
			})
			out.ConfirmedThrough = event.Sequence
		}
		return nil
	})
	return out, err
}
