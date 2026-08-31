package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const streamCompactAfter = 7 * 24 * time.Hour

// CompactExpiredTurnJournals shrinks chunk payloads on terminal Turns older
// than the retention window. Sequence numbers stay in place so replay has no
// holes; projectTurnEvent emits agent.ignored for compacted rows.
func CompactExpiredTurnJournals(ctx context.Context, s Service, now time.Time) (int, error) {
	cutoff := now.Add(-streamCompactAfter)
	var ids []string
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		uncompactedChunks := gdb.Table("agent_turn_events AS event").
			Select("1").
			Where("event.turn_projection_id = agent_turn_projections.id").
			Where("event.kind IN ?", []string{"text.chunk", "thinking.chunk"}).
			Where("event.payload_json::jsonb <> ?::jsonb", `{"compacted":true}`).
			Where(`EXISTS (
				SELECT 1 FROM agent_turn_events AS message
				WHERE message.turn_projection_id = agent_turn_projections.id
				  AND message.kind = 'assistant/message'
				  AND COALESCE(event.payload_json::jsonb->>'attempt_id', '') = COALESCE(message.payload_json::jsonb->>'attempt_id', '')
				  AND (
					(event.kind = 'text.chunk' AND jsonb_typeof(message.payload_json::jsonb->'text') = 'string')
					OR (event.kind = 'thinking.chunk' AND jsonb_typeof(message.payload_json::jsonb->'thinking') = 'string')
				  )
			)`)
		return gdb.Model(&schema.AgentTurnProjections{}).
			Where("status IN ? AND finished_at IS NOT NULL AND finished_at < ?",
				[]string{"succeeded", "failed", "canceled", "unknown", "awaiting_confirmation"}, cutoff).
			Where("EXISTS (?)", uncompactedChunks).
			Order("finished_at, id").
			Limit(50).
			Pluck("id", &ids).Error
	})
	if err != nil {
		return 0, err
	}
	compacted := 0
	for _, id := range ids {
		changed, err := compactTurnJournal(ctx, s, id)
		if err != nil {
			metrics.AgentJournalCompactErrors.Add(1)
			return compacted, err
		}
		if changed {
			compacted++
			metrics.AgentJournalCompactTurns.Add(1)
		}
	}
	return compacted, nil
}

func compactTurnJournal(ctx context.Context, s Service, projectionID string) (bool, error) {
	changed := false
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var rows []schema.AgentTurnEvents
		if err := gdb.Where("turn_projection_id = ? AND kind IN ?", projectionID, []string{"text.chunk", "thinking.chunk", "assistant/message"}).
			Order("sequence").Find(&rows).Error; err != nil {
			return err
		}
		type messageRef struct {
			id      string
			payload map[string]any
		}
		canonicalText := map[string]messageRef{}
		canonicalThinking := map[string]messageRef{}
		for _, row := range rows {
			if row.Kind != "assistant/message" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(row.PayloadJSON), &payload) != nil || payload == nil {
				continue
			}
			attemptID, _ := payload["attempt_id"].(string)
			ref := messageRef{id: row.ID, payload: payload}
			if _, ok := payload["text"].(string); ok {
				canonicalText[attemptID] = ref
			}
			if _, ok := payload["thinking"].(string); ok {
				canonicalThinking[attemptID] = ref
			}
		}
		textSeqs := map[string][]int{}
		thinkingSeqs := map[string][]int{}
		for _, row := range rows {
			if row.Kind != "text.chunk" && row.Kind != "thinking.chunk" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(row.PayloadJSON), &payload) == nil {
				if compacted, _ := payload["compacted"].(bool); compacted {
					continue
				}
			}
			attemptID := ""
			if payload != nil {
				attemptID, _ = payload["attempt_id"].(string)
			}
			switch row.Kind {
			case "text.chunk":
				if _, ok := canonicalText[attemptID]; !ok {
					continue
				}
				textSeqs[attemptID] = append(textSeqs[attemptID], row.Sequence)
			case "thinking.chunk":
				if _, ok := canonicalThinking[attemptID]; !ok {
					continue
				}
				thinkingSeqs[attemptID] = append(thinkingSeqs[attemptID], row.Sequence)
			}
			if err := gdb.Model(&schema.AgentTurnEvents{}).Where("id = ?", row.ID).
				Update("payload_json", `{"compacted":true}`).Error; err != nil {
				return err
			}
			changed = true
		}
		bind := func(refs map[string]messageRef, seqs map[string][]int) error {
			for attemptID, message := range refs {
				ids := seqs[attemptID]
				if len(ids) == 0 {
					continue
				}
				message.payload["sourceEventSeqs"] = ids
				raw, err := json.Marshal(message.payload)
				if err != nil {
					return err
				}
				if err := gdb.Model(&schema.AgentTurnEvents{}).Where("id = ?", message.id).
					Update("payload_json", string(raw)).Error; err != nil {
					return err
				}
			}
			return nil
		}
		if err := bind(canonicalText, textSeqs); err != nil {
			return err
		}
		return bind(canonicalThinking, thinkingSeqs)
	})
	return changed, err
}
