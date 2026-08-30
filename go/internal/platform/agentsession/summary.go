package agentsession

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const summaryMax = 2_000

// RefreshSummary 按当前 session 下的 Task 重写 agent_sessions.summary，对齐 Python refresh_agent_session_summary。
func RefreshSummary(ctx context.Context, pgxTx *gorm.DB, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	var total int64
	if err := pgxTx.Model(&schema.AgentTasks{}).Where("session_id = ?", sessionID).Count(&total).Error; err != nil {
		return err
	}
	var active int64
	if err := pgxTx.Model(&schema.AgentTasks{}).
		Where("session_id = ? AND status IN ?", sessionID, []string{"queued", "running", "waiting_user", "awaiting_confirmation", "paused"}).
		Count(&active).Error; err != nil {
		return err
	}
	var recentRows []schema.AgentTasks
	if err := pgxTx.Where("session_id = ?", sessionID).
		Order("updated_at DESC, id DESC").
		Limit(8).
		Find(&recentRows).Error; err != nil {
		return err
	}
	summary := "暂无 Agent Task"
	if len(recentRows) > 0 {
		parts := make([]string, 0, len(recentRows))
		for _, row := range recentRows {
			parts = append(parts, row.Title+"（"+row.Status+"）")
		}
		summary = "任务 " + strconv.FormatInt(total, 10) + " 个，未完成 " + strconv.FormatInt(active, 10) + " 个。最近任务：" + strings.Join(parts, "；")
	}
	summary = boundedSummary(summary)
	return pgxTx.Model(&schema.AgentSessions{}).
		Where("id = ? AND summary IS DISTINCT FROM ?", sessionID, summary).
		Updates(map[string]any{"summary": summary, "updated_at": time.Now().UTC()}).Error
}

func boundedSummary(value string) string {
	normalized := strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(normalized) <= summaryMax {
		return normalized
	}
	return string([]rune(normalized)[:summaryMax])
}
