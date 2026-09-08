package agent

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/agentsession"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
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

// ListTasks 分页列出 AgentTask（用户显式 Goal），给 Dock 任务列表。
//
// sessionID 为 nil 跨 Session 列。after 是 opaque cursor，必须与当前 session_id / include_terminal 一致，否则 Validation。不是页码。
// includeTerminal=false 去掉 succeeded/failed/canceled/unknown。每条会 sync 关联 GraphRun，但 waiting_reason=goal_loop 不得被覆盖成 succeeded。
// limit 须在 1–100。不写 lease、不追加 journal、不因列表刷新完成 Goal。
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
		q := auth.ScopeMerchant(ctx, pgxTx.Model(&schema.AgentTasks{}), "merchant_id")
		if sessionID != nil {
			q = q.Where("session_id = ?", *sessionID)
		}
		if !includeTerminal {
			q = q.Where("status NOT IN ('succeeded','failed','canceled','unknown')")
		}
		if cursor != nil {
			updated, err := time.Parse(time.RFC3339Nano, cursor.UpdatedAt)
			if err != nil {
				updated, err = time.Parse(time.RFC3339, cursor.UpdatedAt)
				if err != nil {
					return apperr.Validation("Agent Task 分页 cursor 无效")
				}
			}
			q = q.Where("(updated_at < ? OR (updated_at = ? AND id < ?))", updated, updated, cursor.ID)
		}
		var ids []string
		if err := q.Order("updated_at DESC, id DESC").Limit(limit+1).Pluck("id", &ids).Error; err != nil {
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

// CreateTask 创建用户显式 Goal；不会因后续 Turn 成功自动完成。标题或目标无效返回 Validation。Session/conversation 不存在返回 NotFound；conversation 不属于当前 Session 返回 Conflict。
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
		var productID *string
		if conversationID != nil {
			var conv schema.AgentConversations
			err := auth.ScopeMerchant(ctx, pgxTx.Where("id = ?", *conversationID), "merchant_id").Take(&conv).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return auth.NotFoundCrossMerchant()
			}
			if err != nil {
				return err
			}
			if conv.SessionID == nil || *conv.SessionID != sessionID {
				return apperr.Conflict("Agent conversation 不属于当前 Session")
			}
			productID = conv.ProductID
		}
		id := newID()
		summary := boundedSummary(normalizedGoal)
		now := time.Now().UTC()
	merchantID := auth.ResolveMerchantID(ctx)
		row := schema.AgentTasks{
			ID:             id,
			MerchantID:     merchantID,
			SessionID:      sessionID,
			ConversationID: conversationID,
			ProductID:      productID,
			HarnessRunID:   id,
			Title:          normalizedTitle,
			Goal:           normalizedGoal,
			Summary:        &summary,
			Status:         "queued",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := pgxTx.Create(&row).Error; err != nil {
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
	if err != nil {
		return TaskResponse{}, err
	}
	publishTaskChanged(s.DB, out.ID, out.SessionID)
	return out, nil
}

// GetTask 读取 Task；关联 GraphRun 同步不得覆盖 waiting_reason=goal_loop。Task 不存在返回 NotFound。数据库失败返回 error。
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

// RenameTask 只改 Task.title，给 PATCH /api/v2/agent-tasks/:task_id。
//
// 锁住任务行。找不到 NotFound。空或过长 Validation。不改 status、Goal 正文、waiting_reason，也不取消进行中的 Turn。
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
		if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"title":      normalized,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
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
	if err != nil {
		return TaskResponse{}, err
	}
	publishTaskChanged(s.DB, out.ID, out.SessionID)
	return out, nil
}

// CompleteTask 由用户把 Goal 标为 succeeded；Turn 成功不能代替本调用。Task 不存在返回 NotFound。已终态或仍有忙碌 Turn 返回 Conflict。
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
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":         "succeeded",
			"waiting_reason": nil,
			"failure_reason": nil,
			"finished_at":    now,
			"updated_at":     now,
			"summary":        summary,
		}).Error; err != nil {
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
	if err != nil {
		return TaskResponse{}, err
	}
	publishTaskChanged(s.DB, out.ID, out.SessionID)
	return out, nil
}

// PauseTask 在无忙碌 Turn 时暂停 Goal；已暂停或已终态则幂等返回。Task 不存在返回 NotFound。有忙碌 Turn 或当前状态不允许暂停返回 Conflict。
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
		if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":         "paused",
			"waiting_reason": "user_paused",
			"updated_at":     time.Now().UTC(),
		}).Error; err != nil {
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
	if err != nil {
		return TaskResponse{}, err
	}
	publishTaskChanged(s.DB, out.ID, out.SessionID)
	return out, nil
}

// ResumeTask 恢复已暂停的 Goal；非 paused 则幂等返回当前行。Task 不存在返回 NotFound。暂停 Task 的当前 Turn 状态已变、未绑定 conversation 或无法恢复返回 Conflict。
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
			var proj schema.AgentTurnProjections
			if err := pgxTx.Where("id = ?", *task.CurrentTurnID).Take(&proj).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if proj.ID != "" {
				status = proj.Status
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
			if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", taskID).Updates(map[string]any{
				"status":         nextStatus,
				"waiting_reason": reason,
				"updated_at":     time.Now().UTC(),
			}).Error; err != nil {
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
			if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", taskID).Updates(map[string]any{
				"status":         "queued",
				"waiting_reason": nil,
				"failure_reason": nil,
				"finished_at":    nil,
				"canceled_at":    nil,
				"updated_at":     time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
			var productID *string
			if conv.ScopeType == "product_workflow" {
				productID = conv.ProductID
			}
			key := "initial:" + conv.ID + ":" + taskID
			reservation, _, err := reserveTurn(ctx, pgxTx, productID, conv.ID, task.Goal, nil, key, &taskID, "", nil)
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
	if err != nil {
		return TaskResponse{}, err
	}
	publishTaskChanged(s.DB, out.ID, out.SessionID)
	return out, nil
}

// CancelTask 由用户把 Goal 标 canceled，给 POST /api/v2/agent-tasks/:task_id/cancel。
//
// 已终态幂等返回当前行。若当前 Turn 挂着执行请求则经 graph 取消 GraphRun；Turn 仍阻塞则转发 harness cancel。
// 这是用户拥有的 Goal 终态，Turn/GraphRun 成功都不能代替。读路径禁止调用。找不到 NotFound。
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
	if err != nil {
		return TaskResponse{}, err
	}
	publishTaskChanged(s.DB, out.ID, out.SessionID)
	return out, nil
}

// cancelTaskRun 落实用户取消 Goal：若当前 Turn 挂着执行请求则经 graph 包取消 GraphRun；若 Turn 仍阻塞则 controlTurnTx cancel；否则 cancelTaskLocal。
//
// 已终态幂等返回。商品 Goal 取消是用户动作，与 Turn/GraphRun 成功无关。不要在读路径调用本函数。
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

// cancelTaskLocal 在没有需要转发的 GraphRun / harness Turn 时，把 Task 标 canceled，并取消仍阻塞的本地投影。
//
// 写 agent_tasks 与可能的 agent_turn_projections。已终态不改。这是用户取消，不是 goal_loop 停车。
func cancelTaskLocal(ctx context.Context, pgxTx *gorm.DB, taskID string) (TaskResponse, error) {
	task, err := lockTask(ctx, pgxTx, taskID)
	if err != nil {
		return TaskResponse{}, err
	}
	if !inSet(terminalTask, task.Status) {
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":         "canceled",
			"waiting_reason": nil,
			"canceled_at":    now,
			"finished_at":    now,
			"updated_at":     now,
		}).Error; err != nil {
			return TaskResponse{}, err
		}
		if task.CurrentTurnID != nil {
			var proj schema.AgentTurnProjections
			_ = pgxTx.Where("id = ?", *task.CurrentTurnID).Take(&proj).Error
			if inSet(blockingTurn, proj.Status) {
				if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", *task.CurrentTurnID).Updates(map[string]any{
					"status":      "canceled",
					"finished_at": now,
					"updated_at":  now,
				}).Error; err != nil {
					return TaskResponse{}, err
				}
				if proj.ConversationID != "" {
					_ = applyConversationStatus(ctx, pgxTx, proj.ConversationID, "canceled")
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
	var req schema.AgentWorkflowRunRequests
	err := pgxTx.Where("task_id = ?", taskID).
		Order("created_at DESC, id DESC").
		Take(&req).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return TaskResponse{}, err
	}
	if req.GraphRunID != nil && *req.GraphRunID != "" {
		if err := SyncGraphRunToTasks(ctx, pgxTx, *req.GraphRunID); err != nil {
			return TaskResponse{}, err
		}
	}
	return loadTask(ctx, pgxTx, taskID)
}

// loadTask 读取 AgentTask 投影及最近一条 workflow request 的 graph id。不调用 SyncGraphRunToTasks，避免读路径覆盖 waiting_reason=goal_loop。
func loadTask(ctx context.Context, pgxTx *gorm.DB, taskID string) (TaskResponse, error) {
	var t schema.AgentTasks
	err := auth.ScopeMerchant(ctx, pgxTx.WithContext(ctx).Where("id = ?", taskID), "merchant_id").Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return TaskResponse{}, auth.NotFoundCrossMerchant()
	}
	if err != nil {
		return TaskResponse{}, err
	}
	var workflowID *string
	var req schema.AgentWorkflowRunRequests
	reqErr := pgxTx.Where("task_id = ?", taskID).Order("created_at DESC, id DESC").Take(&req).Error
	if reqErr == nil {
		workflowID = &req.GraphID
	} else if !errors.Is(reqErr, gorm.ErrRecordNotFound) {
		return TaskResponse{}, reqErr
	}
	return TaskResponse{
		ID: t.ID, SessionID: t.SessionID, ConversationID: t.ConversationID, ProductID: t.ProductID,
		WorkflowID: workflowID, Title: t.Title, Goal: t.Goal, Summary: t.Summary, Status: t.Status,
		WaitingReason: t.WaitingReason, FailureReason: t.FailureReason, CurrentTurnID: t.CurrentTurnID,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt,
		CanceledAt: t.CanceledAt,
	}, nil
}

func lockTask(ctx context.Context, pgxTx *gorm.DB, taskID string) (taskRow, error) {
	var t schema.AgentTasks
	err := auth.ScopeMerchant(ctx, pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", taskID), "merchant_id").Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, auth.NotFoundCrossMerchant()
	}
	if err != nil {
		return taskRow{}, err
	}
	return taskRow{
		ID: t.ID, SessionID: t.SessionID, ConversationID: t.ConversationID, ProductID: t.ProductID,
		HarnessRunID: t.HarnessRunID, Title: t.Title, Goal: t.Goal, Summary: t.Summary, Status: t.Status,
		WaitingReason: t.WaitingReason, FailureReason: t.FailureReason, CurrentTurnID: t.CurrentTurnID,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt,
		CanceledAt: t.CanceledAt,
	}, nil
}

func requireNoBusyTurn(ctx context.Context, pgxTx *gorm.DB, task taskRow) error {
	if task.CurrentTurnID == nil {
		return nil
	}
	var proj schema.AgentTurnProjections
	err := pgxTx.WithContext(ctx).Where("id = ?", *task.CurrentTurnID).Take(&proj).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if inSet(busyHarnessTurn, proj.Status) {
		return errors.New("busy")
	}
	return nil
}

func refreshSessionSummary(ctx context.Context, pgxTx *gorm.DB, sessionID string) error {
	return agentsession.RefreshSummary(ctx, pgxTx, sessionID)
}
