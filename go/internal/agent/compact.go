package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
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
			Where(`event.kind = 'thinking.chunk' OR (
				event.kind = 'text.chunk' AND (
					SELECT jsonb_typeof(message.payload_json::jsonb->'text')
					FROM agent_turn_events AS message
					WHERE message.turn_projection_id = agent_turn_projections.id AND message.kind = 'assistant/message'
					ORDER BY message.sequence DESC LIMIT 1
				) = 'string'
			)`).
			Where("event.payload_json::jsonb <> ?::jsonb", `{"compacted":true}`)
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
			return compacted, err
		}
		if changed {
			compacted++
		}
	}
	return compacted, nil
}

func compactTurnJournal(ctx context.Context, s Service, projectionID string) (bool, error) {
	changed := false
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var rows []schema.AgentTurnEvents
		if err := gdb.Where("turn_projection_id = ? AND kind IN ?", projectionID, []string{"text.chunk", "thinking.chunk"}).
			Order("sequence").Find(&rows).Error; err != nil {
			return err
		}
		var messages []schema.AgentTurnEvents
		if err := gdb.Where("turn_projection_id = ? AND kind = ?", projectionID, "assistant/message").
			Order("sequence").Find(&messages).Error; err != nil {
			return err
		}
		hasTextSnapshot := false
		if len(messages) > 0 {
			var payload map[string]any
			if json.Unmarshal([]byte(messages[len(messages)-1].PayloadJSON), &payload) == nil {
				_, hasTextSnapshot = payload["text"].(string)
			}
		}
		seqs := make([]int, 0, len(rows))
		for _, row := range rows {
			if row.Kind == "text.chunk" && !hasTextSnapshot {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(row.PayloadJSON), &payload) == nil {
				if compacted, _ := payload["compacted"].(bool); compacted {
					continue
				}
			}
			seqs = append(seqs, row.Sequence)
			if err := gdb.Model(&schema.AgentTurnEvents{}).Where("id = ?", row.ID).
				Update("payload_json", `{"compacted":true}`).Error; err != nil {
				return err
			}
			changed = true
		}
		if len(seqs) == 0 {
			return nil
		}
		if len(messages) == 0 {
			return nil
		}
		message := messages[len(messages)-1]
		var payload map[string]any
		if err := json.Unmarshal([]byte(message.PayloadJSON), &payload); err != nil || payload == nil {
			payload = map[string]any{}
		}
		payload["sourceEventSeqs"] = seqs
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return gdb.Model(&schema.AgentTurnEvents{}).Where("id = ?", message.ID).
			Update("payload_json", string(raw)).Error
	})
	return changed, err
}
