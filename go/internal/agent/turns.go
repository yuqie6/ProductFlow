package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type turnCursor struct {
	V              int    `json:"v"`
	ConversationID string `json:"conversation_id"`
	CreatedAt      string `json:"created_at"`
	ID             string `json:"id"`
}

type startTurnInput struct {
	InputText      string
	AssetIDs       []string
	IdempotencyKey string
	TaskID         *string
	PageContext    map[string]any
}

// ListTurns 按时间正序分页列出 Conversation 的 Turn 投影。limit 或 cursor 无效返回 Validation。conversation 不存在返回 NotFound；Task 不匹配返回 Conflict。
func (s Service) ListTurns(ctx context.Context, productID *string, conversationID, taskID, after string, limit int) (TurnPageResponse, error) {
	if limit < 1 || limit > turnMaxPageSize {
		return TurnPageResponse{}, apperr.Validationf("Agent Turn 分页 limit 必须在 1 到 %d 之间", turnMaxPageSize)
	}
	var cursor *turnCursor
	if after != "" {
		var decoded turnCursor
		if err := decodeCursor(after, &decoded, "Agent Turn 分页 cursor 无效"); err != nil {
			return TurnPageResponse{}, err
		}
		if decoded.V != turnCursorVersion || decoded.ConversationID != conversationID {
			return TurnPageResponse{}, apperr.Validation("Agent Turn 分页 cursor 与当前 conversation 不匹配")
		}
		cursor = &decoded
	}
	var out TurnPageResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadConversation(ctx, pgxTx, productID, conversationID); err != nil {
			return err
		}
		if taskID != "" {
			var task schema.AgentTasks
			err := pgxTx.Where("id = ?", taskID).Take(&task).Error
			if errors.Is(err, gorm.ErrRecordNotFound) || task.ConversationID == nil || *task.ConversationID != conversationID {
				return apperr.Conflict("Agent Task 与当前 conversation 不匹配")
			}
			if err != nil {
				return err
			}
		}
		q := pgxTx.Model(&schema.AgentTurnProjections{}).Where("conversation_id = ?", conversationID)
		if taskID != "" {
			q = q.Where("task_id = ?", taskID)
		}
		if cursor != nil {
			created, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
			if err != nil {
				created, err = time.Parse(time.RFC3339, cursor.CreatedAt)
				if err != nil {
					return apperr.Validation("Agent Turn 分页 cursor 无效")
				}
			}
			q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))", created, created, cursor.ID)
		}
		var ids []string
		if err := q.Order("created_at DESC, id DESC").Limit(limit+1).Pluck("id", &ids).Error; err != nil {
			return err
		}
		hasMore := len(ids) > limit
		if hasMore {
			ids = ids[:limit]
		}
		// 列表按时间正序返回（Python reversed selected）。
		for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
			ids[i], ids[j] = ids[j], ids[i]
		}
		turns := make([]turnRow, 0, len(ids))
		for _, id := range ids {
			row, err := loadTurn(ctx, pgxTx, productID, conversationID, id)
			if err != nil {
				return err
			}
			turns = append(turns, row)
		}
		focus, err := canvasFocusForTurns(ctx, pgxTx, turns)
		if err != nil {
			return err
		}
		items := make([]TurnResponse, 0, len(turns))
		for _, row := range turns {
			items = append(items, serializeTurn(row, focus[row.ID]))
		}
		out.Items = items
		if hasMore && len(turns) > 0 {
			oldest := turns[0]
			if len(ids) > 0 {
				oldest = turns[0]
			}
			// next_cursor 用查询顺序中最旧（DESC 列表的最后一项 = 正序第一项）
			oldest = turns[0]
			encoded, err := encodeCursor(turnCursor{
				V: turnCursorVersion, ConversationID: conversationID,
				CreatedAt: oldest.CreatedAt.UTC().Format(time.RFC3339Nano), ID: oldest.ID,
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

// SubmitTurn 按幂等键预留 projection 并交给 Gateway；HTTP 只写 PENDING dispatch。Gateway 未配置或同步入队失败返回 Unavailable。输入非法返回 Validation；conversation 不存在返回 NotFound；状态不允许或幂等键冲突返回 Conflict。
func (s Service) SubmitTurn(ctx context.Context, productID *string, conversationID string, in startTurnInput) (SubmitTurnResponse, error) {
	if s.Gateway == nil {
		return SubmitTurnResponse{}, apperr.Unavailable("Agent 服务尚未配置或暂时不可用")
	}
	var created bool
	var projectionID string
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, wasCreated, err := reserveTurn(ctx, pgxTx, productID, conversationID, in.InputText, in.AssetIDs, in.IdempotencyKey, in.TaskID, "", in.PageContext)
		if err != nil {
			return err
		}
		created = wasCreated
		projectionID = row.ID
		return nil
	})
	if err != nil {
		return SubmitTurnResponse{}, err
	}
	bound, err := s.bindGatewayTurn(ctx, productID, conversationID, projectionID, true)
	if err != nil {
		return SubmitTurnResponse{}, err
	}
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, projectionID, 0); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = s.recordStartError(ctx, productID, conversationID, projectionID, "Agent Turn 已创建，但后台同步任务暂时无法入队")
		return SubmitTurnResponse{}, apperr.Unavailable("Agent Turn 已创建，但状态同步暂时不可用；请重试当前请求")
	}
	return SubmitTurnResponse{Created: created, Turn: bound}, nil
}

// GetTurn 只读取 PostgreSQL Turn 投影；活动状态和摘要由 journal fold，不读取 Node 文件快照。
func (s Service) GetTurn(ctx context.Context, productID *string, conversationID, projectionID string, _ bool) (TurnResponse, error) {
	var out TurnResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		focus, err := canvasFocusForTurns(ctx, pgxTx, []turnRow{loaded})
		if err != nil {
			return err
		}
		out = serializeTurn(loaded, focus[loaded.ID])
		return nil
	})
	return out, err
}

// ControlTurn 执行 cancel 或 resume。尚未绑定 harness Turn 的 cancel 只改 PostgreSQL 投影。Turn 不存在返回 NotFound。Gateway 未配置返回 Unavailable；终态不能 resume 返回 Conflict。
func (s Service) ControlTurn(ctx context.Context, productID *string, conversationID, projectionID, command string) (TurnResponse, error) {
	var out TurnResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := controlTurnTx(ctx, pgxTx, s, productID, conversationID, projectionID, command)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

// controlTurnTx 执行 cancel / resume。尚未绑定 harness 的 cancel 只改 PostgreSQL 投影与 Task（商品 Goal 仍走 updateTaskFromTurn 的 goal_loop）。
//
// 已绑定则调 Gateway，再 applyTurnState 并 stage sync。终态 Turn 不能 resume。Gateway 未配置返回 Unavailable。
func controlTurnTx(ctx context.Context, pgxTx *gorm.DB, s Service, productID *string, conversationID, projectionID, command string) (TurnResponse, error) {
	row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
	if err != nil {
		return TurnResponse{}, err
	}
	if row.HarnessTurnID == nil {
		if command == "cancel" {
			if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
				"status":      "canceled",
				"finished_at": time.Now().UTC(),
				"updated_at":  time.Now().UTC(),
			}).Error; err != nil {
				return TurnResponse{}, err
			}
			if err := applyConversationStatus(ctx, pgxTx, conversationID, "canceled"); err != nil {
				return TurnResponse{}, err
			}
			if row.TaskID != nil {
				if err := updateTaskFromTurn(ctx, pgxTx, *row.TaskID, "canceled", "", ""); err != nil {
					return TurnResponse{}, err
				}
			}
		}
		loaded, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return TurnResponse{}, err
		}
		return serializeTurn(loaded, nil), nil
	}
	if s.Gateway == nil {
		return TurnResponse{}, apperr.Unavailable("Agent 服务尚未配置或暂时不可用")
	}
	if command == "resume" && inSet(terminalTurn, row.Status) {
		return TurnResponse{}, apperr.Conflict("当前 Agent Turn 状态不允许恢复")
	}
	var state TurnState
	var ge error
	if command == "cancel" {
		state, ge = s.Gateway.CancelTurn(conversationID, *row.HarnessTurnID, row.TaskID)
	} else {
		state, ge = s.Gateway.ResumeTurn(conversationID, *row.HarnessTurnID, row.TaskID)
	}
	if ge != nil {
		return TurnResponse{}, mapGateway(ge)
	}
	if err := s.applyTurnState(ctx, pgxTx, productID, conversationID, projectionID, state); err != nil {
		return TurnResponse{}, err
	}
	if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, projectionID, 0); err != nil {
		return TurnResponse{}, err
	}
	loaded, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
	if err != nil {
		return TurnResponse{}, err
	}
	return serializeTurn(loaded, nil), nil
}

// AnswerQuestion 把用户答案写入投影并尝试 resume Gateway 中仍活着的问题。Gateway 未配置返回 Unavailable。Turn 不在等待回答返回 NotPending；答案校验失败返回 Validation。
func (s Service) AnswerQuestion(ctx context.Context, productID *string, conversationID, projectionID, questionID string, answer map[string]any) (QuestionAnswerResponse, error) {
	_, err := s.persistQuestionAnswer(ctx, productID, conversationID, projectionID, questionID, answer)
	if err != nil {
		return QuestionAnswerResponse{}, err
	}
	if s.Gateway == nil {
		return QuestionAnswerResponse{}, apperr.Unavailable("Agent 服务尚未配置或暂时不可用")
	}
	resumed, rerr := s.resumeLiveQuestion(ctx, productID, conversationID, projectionID, questionID, answer)
	if rerr != nil {
		return QuestionAnswerResponse{}, mapGateway(rerr)
	}
	return questionAnswerResult(resumed, resumed), nil
}

// persistQuestionAnswer 把用户答案写入 projection.question_answer_json 并 stage Turn sync。
//
// 不在 requires_input、问题 id 不对、或已有不同答案时返回 NotPending。这是答案的 PG 权威；Pi waiter 可能已死，仍以本列为准。
func (s Service) persistQuestionAnswer(ctx context.Context, productID *string, conversationID, projectionID, questionID string, answer map[string]any) (turnRow, error) {
	var row turnRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		if loaded.Status != "requires_input" {
			return apperr.NotPending("当前 Agent Turn 不在等待回答状态")
		}
		question := questionMap(loaded)
		if question == nil || question["id"] != questionID {
			return apperr.NotPending("当前 Agent 问题不存在或已经过期")
		}
		if err := validateQuestionAnswer(question, answer); err != nil {
			return err
		}
		answerJSON, err := json.Marshal(answer)
		if err != nil {
			return err
		}
		if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
			"question_answer_json": string(answerJSON),
			"resume_required":      false,
			"sync_error":           gorm.Expr("NULL"),
			"updated_at":           time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, projectionID, 0); err != nil {
			return err
		}
		row, err = loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		return err
	})
	return row, err
}

// validateQuestionAnswer 校验 skip / option / text 互斥且 option 落在问题选项内。不写表。
func validateQuestionAnswer(question, answer map[string]any) error {
	if skip, ok := answer["skip"].(bool); ok && skip {
		if _, hasOption := answer["option"]; hasOption {
			return apperr.Validation("跳过不能同时提交选项")
		}
		if text, _ := answer["text"].(string); stringsTrim(text) != "" {
			return apperr.Validation("跳过不能同时提交文本回答")
		}
		return nil
	}
	if _, hasOption := answer["option"]; hasOption {
		option, ok := intAnswerOption(answer["option"])
		options, _ := question["options"].([]any)
		if !ok || option < 0 || option >= len(options) {
			return apperr.Validation("Agent 问题选项无效")
		}
		return nil
	}
	text, _ := answer["text"].(string)
	if stringsTrim(text) == "" {
		return apperr.Validation("Agent 问题文本回答不能为空")
	}
	return nil
}

func intAnswerOption(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), float64(int(v)) == v
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func questionAnswerResult(answered, continuation TurnResponse) QuestionAnswerResponse {
	return QuestionAnswerResponse{
		TurnResponse:     continuation,
		AnsweredTurn:     answered,
		ContinuationTurn: continuation,
	}
}

// resumeLiveQuestion 把已落库的答案交给 Pi 仍活着的 waiter，再 ResumeTurn 并把状态投影回 PG。
//
// AnswerQuestion 与 SyncTurn 调用。尚未绑定 harness 返回 Conflict。成功后清 resume_required 并 stage sync。Pi 侧失败由调用方 mapGateway，不改 Goal。
func (s Service) resumeLiveQuestion(ctx context.Context, productID *string, conversationID, projectionID, questionID string, answer map[string]any) (TurnResponse, error) {
	var harnessTurnID string
	var taskID *string
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		if row.HarnessTurnID == nil {
			return apperr.Conflict("Agent Turn 尚未绑定 runtime Turn")
		}
		harnessTurnID = *row.HarnessTurnID
		taskID = row.TaskID
		return nil
	})
	if err != nil {
		return TurnResponse{}, err
	}
	if s.Gateway == nil {
		return TurnResponse{}, GatewayError{Status: 503, Code: "unavailable", Detail: "Agent 服务暂时不可用"}
	}
	if _, err := s.Gateway.AnswerQuestion(conversationID, harnessTurnID, questionID, answer, taskID); err != nil {
		return TurnResponse{}, err
	}
	state, err := s.Gateway.ResumeTurn(conversationID, harnessTurnID, taskID)
	if err != nil {
		return TurnResponse{}, err
	}
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := s.applyTurnState(ctx, pgxTx, productID, conversationID, projectionID, state); err != nil {
			return err
		}
		if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
			"resume_required": false,
			"sync_error":      gorm.Expr("NULL"),
			"updated_at":      time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		_, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, projectionID, 0)
		return err
	})
	if err != nil {
		return TurnResponse{}, err
	}
	return s.serializedTurn(ctx, productID, conversationID, projectionID)
}

func (s Service) serializedTurn(ctx context.Context, productID *string, conversationID, projectionID string) (TurnResponse, error) {
	var out TurnResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		out = serializeTurn(row, nil)
		return nil
	})
	return out, err
}

func (s Service) resumeAnsweredParent(ctx context.Context, productID *string, parent turnRow) error {
	question := questionMap(parent)
	questionID, _ := question["id"].(string)
	var answer map[string]any
	if len(parent.QuestionAnswerJSON) > 0 {
		_ = json.Unmarshal(parent.QuestionAnswerJSON, &answer)
	}
	if questionID == "" || answer == nil {
		return apperr.Conflict("原问题答案不完整，无法在原 Turn 内恢复")
	}
	_, err := s.resumeLiveQuestion(ctx, productID, parent.ConversationID, parent.ID, questionID, answer)
	return err
}

func questionMap(row turnRow) map[string]any {
	if len(row.QuestionJSON) == 0 {
		return nil
	}
	var question map[string]any
	_ = json.Unmarshal(row.QuestionJSON, &question)
	return question
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// reserveTurn 按幂等键预占一条 queued 投影。同键同 request hash 回放；同键不同 hash 返回 Conflict。
//
// SubmitTurn 与 recoverQueuedTaskTurns 调用。写 agent_turn_projections，可选 insertPageContext（只绑本 Turn）。商品工作流参考图必须属于该商品且已 verified。
//
// 已结束或 paused 的 Task、仍有 blocking 当前 Turn、会话 Turn 数量触顶、conversation 不可 start 返回 Conflict。页面上下文不得改 Goal。
func reserveTurn(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID, inputText string, assetIDs []string, idempotencyKey string, taskID *string, ignoreTurnID string, pageContext map[string]any) (turnRow, bool, error) {
	text, err := normalizeInputText(inputText)
	if err != nil {
		return turnRow{}, false, err
	}
	assets, err := normalizeAssetIDs(assetIDs)
	if err != nil {
		return turnRow{}, false, err
	}
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return turnRow{}, false, err
	}
	hash, err := turnRequestHash(conversationID, taskID, text, assets)
	if err != nil {
		return turnRow{}, false, err
	}
	conv, err := lockConversation(ctx, pgxTx, conversationID)
	if err != nil {
		return turnRow{}, false, err
	}
	if productID == nil && conv.ScopeType != "global" {
		return turnRow{}, false, apperr.NotFound("Agent conversation 不存在")
	}
	if productID != nil && (conv.ScopeType != "product_workflow" || conv.ProductID == nil || *conv.ProductID != *productID) {
		return turnRow{}, false, apperr.NotFound("Agent conversation 不存在")
	}
	if !inSet(startableConversation, conv.Status) {
		return turnRow{}, false, apperr.Conflict("当前 Agent conversation 状态不允许创建 Turn")
	}
	var existing schema.AgentTurnProjections
	err = pgxTx.Where("conversation_id = ? AND idempotency_key = ?", conversationID, key).Take(&existing).Error
	if err == nil {
		if existing.RequestHash != hash {
			return turnRow{}, false, apperr.Conflict("同一 idempotency key 不能提交不同的 Agent Turn 请求")
		}
		row, err := loadTurnByID(ctx, pgxTx, existing.ID)
		return row, false, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return turnRow{}, false, err
	}
	var turnCount int64
	countQuery := pgxTx.Model(&schema.AgentTurnProjections{}).
		Joins("JOIN agent_conversations turn_conversations ON turn_conversations.id = agent_turn_projections.conversation_id")
	if conv.SessionID != nil {
		countQuery = countQuery.Where("turn_conversations.session_id = ?", *conv.SessionID)
	} else {
		countQuery = countQuery.Where("agent_turn_projections.conversation_id = ?", conversationID)
	}
	if err := countQuery.Count(&turnCount).Error; err != nil {
		return turnRow{}, false, err
	}
	if turnCount >= maxTurnsPerSession {
		return turnRow{}, false, apperr.Conflict("当前 Agent Session 已达到 Turn 数量上限")
	}
	if conv.ScopeType == "global" && conv.SessionID != nil {
		_ = autoNameSession(ctx, pgxTx, *conv.SessionID, text)
	}
	if taskID != nil {
		task, err := lockTask(ctx, pgxTx, *taskID)
		if err != nil {
			return turnRow{}, false, err
		}
		if conv.SessionID == nil || task.SessionID != *conv.SessionID || task.ConversationID == nil || *task.ConversationID != conv.ID {
			return turnRow{}, false, apperr.Conflict("Agent Task 与当前 Agent conversation 不匹配")
		}
		if inSet(terminalTask, task.Status) {
			return turnRow{}, false, apperr.Conflict("已结束的 Agent Task 不能继续提交 Turn")
		}
		if task.Status == "paused" {
			return turnRow{}, false, apperr.Conflict("已暂停的 Agent Task 需要恢复后才能提交 Turn")
		}
		if task.CurrentTurnID != nil && *task.CurrentTurnID != ignoreTurnID {
			var current schema.AgentTurnProjections
			if err := pgxTx.Where("id = ?", *task.CurrentTurnID).Take(&current).Error; err == nil && inSet(blockingTurn, current.Status) {
				return turnRow{}, false, apperr.Conflict("当前 Agent Task 仍有未结束的 Turn")
			}
		}
	}
	if conv.ScopeType == "product_workflow" && len(assets) > 0 && conv.ProductID != nil {
		for _, assetID := range assets {
			var asset schema.ProductImageAssets
			err := pgxTx.Where("id = ?", assetID).Take(&asset).Error
			if errors.Is(err, gorm.ErrRecordNotFound) || asset.ProductID != *conv.ProductID {
				return turnRow{}, false, apperr.Validation("参考图片不属于当前商品")
			}
			if err != nil {
				return turnRow{}, false, err
			}
			var media schema.MediaObjects
			if err := pgxTx.Where("id = ?", asset.MediaObjectID).Take(&media).Error; err != nil {
				return turnRow{}, false, err
			}
			if media.VerificationStatus != "verified" {
				return turnRow{}, false, apperr.Validation("参考图片尚未通过核验")
			}
		}
	}
	id := newID()
	assetJSON, _ := json.Marshal(assets)
	now := time.Now().UTC()
	proj := schema.AgentTurnProjections{
		ID:                id,
		ConversationID:    conversationID,
		TaskID:            taskID,
		IdempotencyKey:    key,
		RequestHash:       hash,
		InputText:         text,
		InputAssetIdsJSON: string(assetJSON),
		Status:            "queued",
		ResumeRequired:    false,
		ToolStepsJSON:     "[]",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := pgxTx.Create(&proj).Error; err != nil {
		if uniqueViolation(err) {
			var replay schema.AgentTurnProjections
			if scanErr := pgxTx.Where("conversation_id = ? AND idempotency_key = ?", conversationID, key).Take(&replay).Error; scanErr == nil {
				if replay.RequestHash != hash {
					return turnRow{}, false, apperr.Conflict("同一 idempotency key 不能提交不同的 Agent Turn 请求")
				}
				row, err := loadTurnByID(ctx, pgxTx, replay.ID)
				return row, false, err
			}
		}
		return turnRow{}, false, err
	}
	if err := pgxTx.Model(&schema.AgentConversations{}).Where("id = ?", conversationID).Updates(map[string]any{
		"status":     "collecting",
		"updated_at": now,
	}).Error; err != nil {
		return turnRow{}, false, err
	}
	if taskID != nil {
		if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", *taskID).Updates(map[string]any{
			"current_turn_id": id,
			"updated_at":      now,
		}).Error; err != nil {
			return turnRow{}, false, err
		}
	}
	if pageContext != nil {
		if err := insertPageContext(ctx, pgxTx, taskID, id, pageContext); err != nil {
			return turnRow{}, false, err
		}
	}
	row, err := loadTurnByID(ctx, pgxTx, id)
	return row, true, err
}

func (s Service) recordStartError(ctx context.Context, productID *string, conversationID, projectionID, message string) error {
	return tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID); err != nil {
			return err
		}
		return pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
			"sync_error": message,
			"updated_at": time.Now().UTC(),
		}).Error
	})
}

// bindGatewayTurn 把尚未绑定 harness 的 queued 投影交给 Gateway.StartTurn，再用 applyTurnState 写下 harness_turn_id。
//
// 已绑定则原样返回。Gateway 不可用且 deferIfUnavailable 时只记 sync_error，不伪造终态。幂等键沿用 projection，避免 Pi 侧重复开 Turn。
func (s Service) bindGatewayTurn(ctx context.Context, productID *string, conversationID, projectionID string, deferIfUnavailable bool) (TurnResponse, error) {
	var row turnRow
	var pageContext any
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		row = loaded
		if loaded.PageContextSnapshotID != nil && *loaded.PageContextSnapshotID != "" {
			payload, err := loadPageContextPayload(ctx, pgxTx, *loaded.PageContextSnapshotID)
			if err != nil {
				return err
			}
			pageContext = payload
		}
		return nil
	})
	if err != nil {
		return TurnResponse{}, err
	}
	if row.HarnessTurnID != nil {
		return serializeTurn(row, nil), nil
	}
	if s.Gateway == nil {
		if !deferIfUnavailable {
			return TurnResponse{}, apperr.Unavailable("Agent 服务暂时不可用")
		}
		_ = s.recordStartError(ctx, productID, conversationID, projectionID, "Agent 服务暂时不可用")
		return serializeTurn(row, nil), nil
	}
	assets := []string{}
	_ = json.Unmarshal(row.InputAssetIDs, &assets)
	state, ge := s.Gateway.StartTurn(conversationID, row.TaskID, row.InputText, assets, row.IdempotencyKey, pageContext)
	if ge != nil {
		_ = s.recordStartError(ctx, productID, conversationID, projectionID, "Agent 服务暂时不可用")
		if !deferIfUnavailable {
			return TurnResponse{}, mapGateway(ge)
		}
		return serializeTurn(row, nil), nil
	}
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := s.applyTurnState(ctx, pgxTx, productID, conversationID, projectionID, state); err != nil {
			return err
		}
		loaded, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		row = loaded
		return nil
	})
	if err != nil {
		return TurnResponse{}, err
	}
	return serializeTurn(row, nil), nil
}
