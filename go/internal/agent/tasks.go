package agent

import (
	"context"
	"errors"
	"strconv"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type taskCursor struct {
	V               int     `json:"v"`
	SessionID       *string `json:"session_id"`
	IncludeTerminal bool    `json:"include_terminal"`
	UpdatedAt       string  `json:"updated_at"`
	ID              string  `json:"id"`
}

func (s Service) ListTasks(ctx context.Context, sessionID *string, includeTerminal bool, after string, limit int) (TaskListResponse, error) {
	if limit < 1 || limit > taskListMaxLimit {
		return TaskListResponse{}, apperr.Validationf("Agent Task 列表 limit 必须在 1 到 %d 之间", taskListMaxLimit)
	}
	var cursor *taskCursor
	if after != "" {
		var decoded taskCursor
		if err := decodeCursor(after, &decoded, "Agent Task 分页 cursor 无效"); err != nil {
			return TaskListResponse{}, err
		}
		if decoded.V != taskCursorVersion {
			return TaskListResponse{}, apperr.Validation("Agent Task 分页 cursor 无效")
		}
		sidEqual := (decoded.SessionID == nil && sessionID == nil) || (decoded.SessionID != nil && sessionID != nil && *decoded.SessionID == *sessionID)
		if !sidEqual || decoded.IncludeTerminal != includeTerminal {
			return TaskListResponse{}, apperr.Validation("Agent Task 分页 cursor 与当前查询条件不匹配")
		}
		cursor = &decoded
	}
	var out TaskListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		q := `
			SELECT t.id FROM agent_tasks t WHERE 1=1
		`
		args := []any{}
		n := 1
		if sessionID != nil {
			q += ` AND t.session_id = $` + strconv.Itoa(n)
			args = append(args, *sessionID)
			n++
		}
		if !includeTerminal {
			q += ` AND t.status NOT IN ('succeeded','failed','canceled','unknown')`
		}
		if cursor != nil {
			updated, err := time.Parse(time.RFC3339Nano, cursor.UpdatedAt)
			if err != nil {
				updated, err = time.Parse(time.RFC3339, cursor.UpdatedAt)
				if err != nil {
					return apperr.Validation("Agent Task 分页 cursor 无效")
				}
			}
			q += ` AND (t.updated_at < $` + strconv.Itoa(n) + ` OR (t.updated_at = $` + strconv.Itoa(n) + ` AND t.id < $` + strconv.Itoa(n+1) + `))`
			args = append(args, updated, cursor.ID)
			n += 2
		}
		q += ` ORDER BY t.updated_at DESC, t.id DESC LIMIT $` + strconv.Itoa(n)
		args = append(args, limit+1)
		rows, err := pfdb.Query(ctx, pgxTx, q, args...)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		hasMore := len(ids) > limit
		if hasMore {
			ids = ids[:limit]
		}
		items := make([]TaskResponse, 0, len(ids))
		for _, id := range ids {
			item, err := loadTaskAfterGraphRunSync(ctx, pgxTx, id)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		out.Items = items
		if hasMore && len(items) > 0 {
			oldest := items[len(items)-1]
			encoded, err := encodeCursor(taskCursor{
				V: taskCursorVersion, SessionID: sessionID, IncludeTerminal: includeTerminal,
				UpdatedAt: oldest.UpdatedAt.UTC().Format(time.RFC3339Nano), ID: oldest.ID,
			})
			if err != nil {
				return err
			}
			out.NextCursor = &encoded
		}
		return nil
	})
	return out, err
}

func (s Service) CreateTask(ctx context.Context, sessionID, title, goal string, conversationID *string) (TaskResponse, error) {
	normalizedTitle, err := normalizeTaskTitle(title)
	if err != nil {
		return TaskResponse{}, err
	}
	normalizedGoal, err := normalizeTaskGoal(goal)
	if err != nil {
		return TaskResponse{}, err
	}
	var out TaskResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		if conversationID != nil {
			var convSession *string
			err := pfdb.QueryRow(ctx, pgxTx, `SELECT session_id FROM agent_conversations WHERE id = $1`, *conversationID).Scan(&convSession)
			if errors.Is(err, sqldb.ErrNoRows) {
				return apperr.NotFound("Agent conversation 不存在")
			}
			if err != nil {
				return err
			}
			if convSession == nil || *convSession != sessionID {
				return apperr.Conflict("Agent conversation 不属于当前 Session")
			}
		}
		id := newID()
		summary := boundedSummary(normalizedGoal)
		var productID *string
		if conversationID != nil {
			_ = pfdb.QueryRow(ctx, pgxTx, `SELECT product_id FROM agent_conversations WHERE id = $1`, *conversationID).Scan(&productID)
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO agent_tasks (
				id, session_id, conversation_id, product_id, harness_run_id, title, goal, summary, status, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $1, $5, $6, $7, 'queued', NOW(), NOW())
		`, id, sessionID, conversationID, productID, normalizedTitle, normalizedGoal, summary); err != nil {
			return err
		}
		if err := refreshSessionSummary(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		item, err := loadTask(ctx, pgxTx, id)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) GetTask(ctx context.Context, taskID string) (TaskResponse, error) {
	var out TaskResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := loadTaskAfterGraphRunSync(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) RenameTask(ctx context.Context, taskID, title string) (TaskResponse, error) {
	normalized, err := normalizeTaskTitle(title)
	if err != nil {
		return TaskResponse{}, err
	}
	var out TaskResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := lockTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `UPDATE agent_tasks SET title = $2, updated_at = NOW() WHERE id = $1`, taskID, normalized); err != nil {
			return err
		}
		if err := refreshSessionSummary(ctx, pgxTx, task.SessionID); err != nil {
			return err
		}
		item, err := loadTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) CompleteTask(ctx context.Context, taskID string) (TaskResponse, error) {
	var out TaskResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := lockTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		if inSet(terminalTask, task.Status) {
			return apperr.Conflict("已结束的 Agent Task 不能再标记完成")
		}
		if err := requireNoBusyTurn(ctx, pgxTx, task); err != nil {
			return apperr.Conflict("运行中的 Agent Task 需要先等待结束或取消，才能标记 Goal 完成")
		}
		summary := boundedSummary(task.Goal)
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_tasks SET status = 'succeeded', waiting_reason = NULL, failure_reason = NULL,
				finished_at = NOW(), updated_at = NOW(), summary = $2 WHERE id = $1
		`, taskID, summary); err != nil {
			return err
		}
		if err := refreshSessionSummary(ctx, pgxTx, task.SessionID); err != nil {
			return err
		}
		item, err := loadTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) PauseTask(ctx context.Context, taskID string) (TaskResponse, error) {
	var out TaskResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := lockTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		if inSet(terminalTask, task.Status) || task.Status == "paused" {
			item, err := loadTask(ctx, pgxTx, taskID)
			if err != nil {
				return err
			}
			out = item
			return nil
		}
		if err := requireNoBusyTurn(ctx, pgxTx, task); err != nil {
			return apperr.Conflict("运行中的 Agent Task 需要先取消，当前 harness 不支持中断后保持任务暂停")
		}
		if task.Status != "queued" && task.Status != "waiting_user" && task.Status != "awaiting_confirmation" {
			return apperr.Conflict("当前 Agent Task 状态不允许暂停")
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_tasks SET status = 'paused', waiting_reason = 'user_paused', updated_at = NOW() WHERE id = $1
		`, taskID); err != nil {
			return err
		}
		if err := refreshSessionSummary(ctx, pgxTx, task.SessionID); err != nil {
			return err
		}
		item, err := loadTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) ResumeTask(ctx context.Context, taskID string) (TaskResponse, error) {
	var out TaskResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := lockTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		if task.Status != "paused" {
			item, err := loadTask(ctx, pgxTx, taskID)
			if err != nil {
				return err
			}
			out = item
			return nil
		}
		if task.CurrentTurnID != nil {
			var status string
			if err := pfdb.QueryRow(ctx, pgxTx, `SELECT status FROM agent_turn_projections WHERE id = $1`, *task.CurrentTurnID).Scan(&status); err != nil && !errors.Is(err, sqldb.ErrNoRows) {
				return err
			}
			nextStatus := ""
			reason := ""
			switch status {
			case "requires_input":
				nextStatus, reason = "waiting_user", "requires_input"
			case "awaiting_confirmation":
				nextStatus, reason = "awaiting_confirmation", "awaiting_confirmation"
			default:
				return apperr.Conflict("暂停的 Agent Task 当前 Turn 状态已变化，请刷新后处理")
			}
			if _, err := pfdb.Exec(ctx, pgxTx, `
				UPDATE agent_tasks SET status = $2, waiting_reason = $3, updated_at = NOW() WHERE id = $1
			`, taskID, nextStatus, reason); err != nil {
				return err
			}
		} else {
			if task.ConversationID == nil {
				return apperr.Conflict("没有绑定 conversation 的 Agent Task 不能恢复执行")
			}
			conv, err := loadConversationByID(ctx, pgxTx, *task.ConversationID)
			if err != nil {
				return apperr.Conflict("Agent Task 绑定的 conversation 不存在")
			}
			if _, err := pfdb.Exec(ctx, pgxTx, `
				UPDATE agent_tasks SET status = 'queued', waiting_reason = NULL, failure_reason = NULL,
					finished_at = NULL, canceled_at = NULL, updated_at = NOW() WHERE id = $1
			`, taskID); err != nil {
				return err
			}
			var productID *string
			if conv.ScopeType == "product_workflow" {
				productID = conv.ProductID
			}
			key := "initial:" + conv.ID + ":" + taskID
			reservation, _, err := reserveTurn(ctx, pgxTx, productID, conv.ID, task.Goal, nil, key, &taskID, nil)
			if err != nil {
				return err
			}
			if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, reservation.ID, 0); err != nil {
				return err
			}
		}
		if err := refreshSessionSummary(ctx, pgxTx, task.SessionID); err != nil {
			return err
		}
		item, err := loadTask(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) CancelTask(ctx context.Context, taskID string) (TaskResponse, error) {
	var out TaskResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := cancelTaskRun(ctx, pgxTx, s, taskID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func cancelTaskRun(ctx context.Context, pgxTx *gorm.DB, s Service, taskID string) (TaskResponse, error) {
	task, err := loadTask(ctx, pgxTx, taskID)
	if err != nil {
		return TaskResponse{}, err
	}
	if inSet(terminalTask, task.Status) {
		return task, nil
	}
	if task.CurrentTurnID != nil && task.ConversationID != nil {
		turn, err := loadTurn(ctx, pgxTx, task.ProductID, *task.ConversationID, *task.CurrentTurnID)
		if err == nil && turn.WorkflowRunRequestID != nil {
			if _, err := cancelWorkflowRunRequest(ctx, pgxTx, s, task.ProductID, *task.ConversationID, *turn.WorkflowRunRequestID); err != nil {
				return TaskResponse{}, err
			}
			return loadTask(ctx, pgxTx, taskID)
		}
		if err == nil && turn.HarnessTurnID != nil && inSet(blockingTurn, turn.Status) {
			if _, err := controlTurnTx(ctx, pgxTx, s, task.ProductID, *task.ConversationID, turn.ID, "cancel"); err != nil {
				return TaskResponse{}, err
			}
			return loadTask(ctx, pgxTx, taskID)
		}
	}
	return cancelTaskLocal(ctx, pgxTx, taskID)
}

func cancelTaskLocal(ctx context.Context, pgxTx *gorm.DB, taskID string) (TaskResponse, error) {
	task, err := lockTask(ctx, pgxTx, taskID)
	if err != nil {
		return TaskResponse{}, err
	}
	if !inSet(terminalTask, task.Status) {
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_tasks SET status = 'canceled', waiting_reason = NULL, canceled_at = NOW(), finished_at = NOW(), updated_at = NOW()
			WHERE id = $1
		`, taskID); err != nil {
			return TaskResponse{}, err
		}
		if task.CurrentTurnID != nil {
			var status string
			_ = pfdb.QueryRow(ctx, pgxTx, `SELECT status FROM agent_turn_projections WHERE id = $1`, *task.CurrentTurnID).Scan(&status)
			if inSet(blockingTurn, status) {
				if _, err := pfdb.Exec(ctx, pgxTx, `
					UPDATE agent_turn_projections SET status = 'canceled', finished_at = NOW(), updated_at = NOW() WHERE id = $1
				`, *task.CurrentTurnID); err != nil {
					return TaskResponse{}, err
				}
				var convID string
				_ = pfdb.QueryRow(ctx, pgxTx, `SELECT conversation_id FROM agent_turn_projections WHERE id = $1`, *task.CurrentTurnID).Scan(&convID)
				if convID != "" {
					_ = applyConversationStatus(ctx, pgxTx, convID, "canceled")
				}
			}
		}
		if err := refreshSessionSummary(ctx, pgxTx, task.SessionID); err != nil {
			return TaskResponse{}, err
		}
	}
	return loadTask(ctx, pgxTx, taskID)
}

func loadTaskAfterGraphRunSync(ctx context.Context, pgxTx *gorm.DB, taskID string) (TaskResponse, error) {
	var runID string
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT graph_run_id FROM agent_workflow_run_requests
		WHERE task_id = $1 AND graph_run_id IS NOT NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, taskID).Scan(&runID)
	if err != nil && !errors.Is(err, sqldb.ErrNoRows) {
		return TaskResponse{}, err
	}
	if runID != "" {
		if err := SyncGraphRunToTasks(ctx, pgxTx, runID); err != nil {
			return TaskResponse{}, err
		}
	}
	return loadTask(ctx, pgxTx, taskID)
}

func loadTask(ctx context.Context, pgxTx *gorm.DB, taskID string) (TaskResponse, error) {
	var row taskRow
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT t.id, t.session_id, t.conversation_id, t.product_id, t.harness_run_id, t.title, t.goal, t.summary,
			t.status, t.waiting_reason, t.failure_reason, t.current_turn_id, t.created_at, t.updated_at,
			t.started_at, t.finished_at, t.canceled_at,
			(SELECT r.graph_id FROM agent_workflow_run_requests r WHERE r.task_id = t.id ORDER BY r.created_at DESC, r.id DESC LIMIT 1)
		FROM agent_tasks t WHERE t.id = $1
	`, taskID).Scan(
		&row.ID, &row.SessionID, &row.ConversationID, &row.ProductID, &row.HarnessRunID, &row.Title, &row.Goal, &row.Summary,
		&row.Status, &row.WaitingReason, &row.FailureReason, &row.CurrentTurnID, &row.CreatedAt, &row.UpdatedAt,
		&row.StartedAt, &row.FinishedAt, &row.CanceledAt, &row.WorkflowID,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return TaskResponse{}, apperr.NotFound("Agent Task 不存在")
	}
	if err != nil {
		return TaskResponse{}, err
	}
	return TaskResponse{
		ID: row.ID, SessionID: row.SessionID, ConversationID: row.ConversationID, ProductID: row.ProductID,
		WorkflowID: row.WorkflowID, Title: row.Title, Goal: row.Goal, Summary: row.Summary, Status: row.Status,
		WaitingReason: row.WaitingReason, FailureReason: row.FailureReason, CurrentTurnID: row.CurrentTurnID,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		CanceledAt: row.CanceledAt,
	}, nil
}

func lockTask(ctx context.Context, pgxTx *gorm.DB, taskID string) (taskRow, error) {
	var row taskRow
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT id, session_id, conversation_id, product_id, harness_run_id, title, goal, summary, status,
			waiting_reason, failure_reason, current_turn_id, created_at, updated_at, started_at, finished_at, canceled_at
		FROM agent_tasks WHERE id = $1 FOR UPDATE
	`, taskID).Scan(
		&row.ID, &row.SessionID, &row.ConversationID, &row.ProductID, &row.HarnessRunID, &row.Title, &row.Goal, &row.Summary, &row.Status,
		&row.WaitingReason, &row.FailureReason, &row.CurrentTurnID, &row.CreatedAt, &row.UpdatedAt, &row.StartedAt, &row.FinishedAt, &row.CanceledAt,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return taskRow{}, apperr.NotFound("Agent Task 不存在")
	}
	return row, err
}

func requireNoBusyTurn(ctx context.Context, pgxTx *gorm.DB, task taskRow) error {
	if task.CurrentTurnID == nil {
		return nil
	}
	var status string
	err := pfdb.QueryRow(ctx, pgxTx, `SELECT status FROM agent_turn_projections WHERE id = $1`, *task.CurrentTurnID).Scan(&status)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if inSet(busyHarnessTurn, status) {
		return errors.New("busy")
	}
	return nil
}

func refreshSessionSummary(ctx context.Context, pgxTx *gorm.DB, sessionID string) error {
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
		summary = "任务 " + strconv.Itoa(total) + " 个，未完成 " + strconv.Itoa(active) + " 个。最近任务：" + joinZH(parts)
	}
	summary = boundedSummary(summary)
	_, err = pfdb.Exec(ctx, pgxTx, `UPDATE agent_sessions SET summary = $2, updated_at = NOW() WHERE id = $1 AND (summary IS DISTINCT FROM $2)`, sessionID, summary)
	return err
}

func joinZH(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "；"
		}
		out += p
	}
	return out
}
