package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
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

func (s Service) GetTurn(ctx context.Context, productID *string, conversationID, projectionID string, requireLibraryClear bool) (TurnResponse, error) {
	var row turnRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
	needsRefresh := inSet(inFlightTurn, row.Status)
	if row.Status == "awaiting_confirmation" && (!requireLibraryClear || row.LibraryOrgDraftRevisionID == nil) {
		needsRefresh = true
	}
	if needsRefresh && s.Gateway != nil && row.HarnessTurnID != nil {
		refreshed, err := s.refreshTurn(ctx, productID, conversationID, projectionID, true)
		if err == nil {
			return refreshed, nil
		}
	}
	var out TurnResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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

func (s Service) AnswerQuestion(ctx context.Context, productID *string, conversationID, projectionID, questionID string, answer map[string]any) (QuestionAnswerResponse, error) {
	row, err := s.persistQuestionAnswer(ctx, productID, conversationID, projectionID, questionID, answer)
	if err != nil {
		return QuestionAnswerResponse{}, err
	}
	if row.HarnessTurnID != nil && s.Gateway != nil {
		resumed, rerr := s.resumeLiveQuestion(ctx, productID, conversationID, projectionID, questionID, answer)
		if rerr == nil {
			if row.ContinuationTurnID != nil {
				_ = s.cancelUnusedContinuation(ctx, productID, turnRow{
					ID:             *row.ContinuationTurnID,
					ConversationID: conversationID,
				})
			}
			return questionAnswerResult(resumed, resumed), nil
		}
		if !gatewayQuestionNotLive(rerr) {
			return QuestionAnswerResponse{}, mapGateway(rerr)
		}
	} else if s.Gateway == nil {
		return QuestionAnswerResponse{}, apperr.Unavailable("Agent 服务尚未配置或暂时不可用")
	}
	return s.continueAfterDeadWaiter(ctx, productID, conversationID, projectionID, questionID, answer)
}

func (s Service) persistQuestionAnswer(ctx context.Context, productID *string, conversationID, projectionID, questionID string, answer map[string]any) (turnRow, error) {
	var row turnRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		if loaded.Status != "requires_input" {
			return apperr.Conflict("当前 Agent Turn 不在等待回答状态")
		}
		question := questionMap(loaded)
		if question == nil || question["id"] != questionID {
			return apperr.Conflict("当前 Agent 问题不存在或已经过期")
		}
		if err := validateQuestionAnswer(question, answer); err != nil {
			return err
		}
		if loaded.ContinuationTurnID != nil && len(loaded.QuestionAnswerJSON) > 0 && !sameQuestionAnswer(loaded.QuestionAnswerJSON, answer) {
			return apperr.Conflict("当前问题已经使用其他答案创建 continuation Turn")
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
		row, err = loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		return err
	})
	return row, err
}

func validateQuestionAnswer(question, answer map[string]any) error {
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

func sameQuestionAnswer(stored []byte, answer map[string]any) bool {
	var existing map[string]any
	if json.Unmarshal(stored, &existing) != nil {
		return false
	}
	left, err := json.Marshal(existing)
	if err != nil {
		return false
	}
	right, err := json.Marshal(answer)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

func questionAnswerResult(answered, continuation TurnResponse) QuestionAnswerResponse {
	return QuestionAnswerResponse{
		TurnResponse:     continuation,
		AnsweredTurn:     answered,
		ContinuationTurn: continuation,
	}
}

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

func (s Service) continueAfterDeadWaiter(ctx context.Context, productID *string, conversationID, projectionID, questionID string, answer map[string]any) (QuestionAnswerResponse, error) {
	var answeredID string
	var continuationID string
	var originalHarness *string
	var originalTaskID *string
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		answeredID = row.ID
		originalHarness = row.HarnessTurnID
		originalTaskID = row.TaskID
		if row.ContinuationTurnID != nil {
			continuationID = *row.ContinuationTurnID
			return nil
		}
		key := "question-continuation:" + projectionID + ":" + sha256hex(questionID)
		cont, _, err := reserveTurn(ctx, pgxTx, productID, conversationID, continuationInput(questionMap(row), answer), inputAssetIDs(row), key, row.TaskID, row.ID, nil)
		if err != nil {
			return err
		}
		continuationID = cont.ID
		if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
			"continuation_turn_id": cont.ID,
			"updated_at":           time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		_, err = queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, cont.ID, 0)
		return err
	})
	if err != nil {
		return QuestionAnswerResponse{}, err
	}
	if s.Gateway != nil && originalHarness != nil {
		state, ge := s.Gateway.CancelTurn(conversationID, *originalHarness, originalTaskID)
		if ge == nil {
			if err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
				return s.applyTurnState(ctx, pgxTx, productID, conversationID, answeredID, state)
			}); err != nil {
				return QuestionAnswerResponse{}, err
			}
		}
	}
	continuation, err := s.serializedTurn(ctx, productID, conversationID, continuationID)
	if err != nil {
		return QuestionAnswerResponse{}, err
	}
	if continuation.HarnessTurnID == nil {
		bound, bindErr := s.bindGatewayTurn(ctx, productID, conversationID, continuationID, true)
		if bindErr == nil {
			continuation = bound
		}
	}
	answered, err := s.serializedTurn(ctx, productID, conversationID, answeredID)
	if err != nil {
		return QuestionAnswerResponse{}, err
	}
	return questionAnswerResult(answered, continuation), nil
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

func (s Service) cancelUnusedContinuation(ctx context.Context, productID *string, child turnRow) error {
	if child.ID == "" {
		return nil
	}
	if child.HarnessTurnID == nil || child.ConversationID == "" {
		var loaded turnRow
		err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
			row, err := loadTurnByID(ctx, pgxTx, child.ID)
			loaded = row
			return err
		})
		if err != nil {
			return err
		}
		child = loaded
	}
	if child.HarnessTurnID != nil && s.Gateway != nil {
		_, _ = s.Gateway.CancelTurn(child.ConversationID, *child.HarnessTurnID, child.TaskID)
	}
	return s.abandonUnusedContinuation(ctx, productID, child.ConversationID, child.ID)
}

func (s Service) abandonUnusedContinuation(ctx context.Context, productID *string, conversationID, continuationID string) error {
	return tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if conversationID != "" {
			if _, err := loadTurn(ctx, pgxTx, productID, conversationID, continuationID); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		return pgxTx.Model(&schema.AgentTurnProjections{}).
			Where("id = ? AND status IN ?", continuationID, []string{"queued", "running", "cancel_requested"}).
			Updates(map[string]any{
				"status":      "canceled",
				"finished_at": now,
				"sync_error":  "问题已在原 Turn 内恢复，continuation 不再执行",
				"updated_at":  now,
			}).Error
	})
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

func inputAssetIDs(row turnRow) []string {
	assets := []string{}
	if len(row.InputAssetIDs) > 0 {
		_ = json.Unmarshal(row.InputAssetIDs, &assets)
	}
	if assets == nil {
		return []string{}
	}
	return assets
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

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func continuationInput(question, answer map[string]any) string {
	qtext, _ := question["question"].(string)
	answerText := ""
	if opt, ok := answer["option"]; ok {
		idx := 0
		switch v := opt.(type) {
		case float64:
			idx = int(v)
		case int:
			idx = v
		case json.Number:
			n, _ := v.Int64()
			idx = int(n)
		}
		answerText = "选择第 " + strconv.Itoa(idx+1) + " 项"
	} else {
		answerText, _ = answer["text"].(string)
	}
	return "继续当前 Agent 任务。针对问题“" + qtext + "”，用户回答：" + answerText + "。请基于这个回答继续执行，并再次通过 ProductFlow 工具确认业务事实。"
}

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

func (s Service) refreshTurn(ctx context.Context, productID *string, conversationID, projectionID string, tolerate bool) (TurnResponse, error) {
	var row turnRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
	if row.HarnessTurnID == nil || s.Gateway == nil {
		return serializeTurn(row, nil), nil
	}
	state, ge := s.Gateway.GetTurn(conversationID, *row.HarnessTurnID, row.TaskID)
	if ge != nil {
		if tolerate {
			_ = s.recordStartError(ctx, productID, conversationID, projectionID, "Agent 服务暂时不可用")
			return serializeTurn(row, nil), nil
		}
		return TurnResponse{}, mapGateway(ge)
	}
	var out TurnResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := s.applyTurnState(ctx, pgxTx, productID, conversationID, projectionID, state); err != nil {
			return err
		}
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
