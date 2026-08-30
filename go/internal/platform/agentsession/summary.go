package agentsession

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

const summaryMax = 2_000

// RefreshSummary 按当前 session 下的 Task 重写 agent_sessions.summary，对齐 Python refresh_agent_session_summary。
func RefreshSummary(ctx context.Context, pgxTx *gorm.DB, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	var total, active int
	if err := pfdb.QueryRow(ctx, pgxTx, `SELECT COUNT(*) FROM agent_tasks WHERE session_id = $1`, sessionID).Scan(&total); err != nil {
		return err
	}
	if err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT COUNT(*) FROM agent_tasks
		WHERE session_id = $1 AND status IN ('queued','running','waiting_user','awaiting_confirmation','paused')
	`, sessionID).Scan(&active); err != nil {
		return err
	}
	rows, err := pfdb.Query(ctx, pgxTx, `
		SELECT title, status FROM agent_tasks WHERE session_id = $1
		ORDER BY updated_at DESC, id DESC LIMIT 8
	`, sessionID)
	if err != nil {
		return err
	}
	type pair struct{ title, status string }
	var recent []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.title, &p.status); err != nil {
			rows.Close()
			return err
		}
		recent = append(recent, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	summary := "暂无 Agent Task"
	if len(recent) > 0 {
		parts := make([]string, 0, len(recent))
		for _, p := range recent {
			parts = append(parts, p.title+"（"+p.status+"）")
		}
		summary = "任务 " + strconv.Itoa(total) + " 个，未完成 " + strconv.Itoa(active) + " 个。最近任务：" + strings.Join(parts, "；")
	}
	summary = boundedSummary(summary)
	_, err = pfdb.Exec(ctx, pgxTx, `UPDATE agent_sessions SET summary = $2, updated_at = NOW() WHERE id = $1 AND (summary IS DISTINCT FROM $2)`, sessionID, summary)
	return err
}

func boundedSummary(value string) string {
	normalized := strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(normalized) <= summaryMax {
		return normalized
	}
	return string([]rune(normalized)[:summaryMax])
}
