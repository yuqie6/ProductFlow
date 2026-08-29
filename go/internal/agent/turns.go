package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
			var conv *string
			err := pfdb.QueryRow(ctx, pgxTx, `SELECT conversation_id FROM agent_tasks WHERE id = $1`, taskID).Scan(&conv)
			if errors.Is(err, sqldb.ErrNoRows) || conv == nil || *conv != conversationID {
				return apperr.Conflict("Agent Task 与当前 conversation 不匹配")
			}
			if err != nil {
				return err
			}
		}
		q := `
			SELECT t.id FROM agent_turn_projections t
			WHERE t.conversation_id = $1
		`
		args := []any{conversationID}
		n := 2
		if taskID != "" {
			q += ` AND t.task_id = $2`
			args = append(args, taskID)
			n = 3
		}
		if cursor != nil {
			created, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
			if err != nil {
				created, err = time.Parse(time.RFC3339, cursor.CreatedAt)
				if err != nil {
					return apperr.Validation("Agent Turn 分页 cursor 无效")
				}
			}
			q += ` AND (t.created_at < $` + strconv.Itoa(n) + ` OR (t.created_at = $` + strconv.Itoa(n) + ` AND t.id < $` + strconv.Itoa(n+1) + `))`
			args = append(args, created, cursor.ID)
			n += 2
		}
		q += ` ORDER BY t.created_at DESC, t.id DESC LIMIT $` + strconv.Itoa(n)
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

func itoaN(n int) string { return strconv.Itoa(n) }

func (s Service) SubmitTurn(ctx context.Context, productID *string, conversationID string, in startTurnInput) (SubmitTurnResponse, error) {
	if s.Gateway == nil {
		return SubmitTurnResponse{}, apperr.Unavailable("Agent 服务尚未配置或暂时不可用")
	}
	var created bool
	var projectionID string
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, wasCreated, err := reserveTurn(ctx, pgxTx, productID, conversationID, in.InputText, in.AssetIDs, in.IdempotencyKey, in.TaskID, in.PageContext)
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
			if _, err := pfdb.Exec(ctx, pgxTx, `
				UPDATE agent_turn_projections SET status = 'canceled', finished_at = NOW(), updated_at = NOW() WHERE id = $1
			`, projectionID); err != nil {
				return TurnResponse{}, err
			}
			_ = applyConversationStatus(ctx, pgxTx, conversationID, "canceled")
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
	var out QuestionAnswerResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		if row.Status != "requires_input" {
			return apperr.Conflict("当前 Agent Turn 不在等待回答状态")
		}
		var question map[string]any
		if len(row.QuestionJSON) > 0 {
			_ = json.Unmarshal(row.QuestionJSON, &question)
		}
		if question == nil || question["id"] != questionID {
			return apperr.Conflict("当前 Agent 问题不存在或已经过期")
		}
		if _, hasOption := answer["option"]; hasOption {
			option, ok := answer["option"].(float64)
			options, _ := question["options"].([]any)
			if !ok || int(option) < 0 || int(option) >= len(options) {
				return apperr.Validation("Agent 问题选项无效")
			}
		} else {
			text, _ := answer["text"].(string)
			if stringsTrim(text) == "" {
				return apperr.Validation("Agent 问题文本回答不能为空")
			}
		}
		answerJSON, _ := json.Marshal(answer)
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_turn_projections SET question_answer_json = $2, updated_at = NOW() WHERE id = $1
		`, projectionID, answerJSON); err != nil {
			return err
		}
		key := "question-continuation:" + projectionID + ":" + sha256hex(questionID)
		input := continuationInput(question, answer)
		cont, _, err := reserveTurn(ctx, pgxTx, productID, conversationID, input, nil, key, row.TaskID, nil)
		if err != nil {
			return err
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_turn_projections SET continuation_turn_id = $2, updated_at = NOW() WHERE id = $1
		`, projectionID, cont.ID); err != nil {
			return err
		}
		if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, cont.ID, 0); err != nil {
			return err
		}
		answered, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		continuation, err := loadTurn(ctx, pgxTx, productID, conversationID, cont.ID)
		if err != nil {
			return err
		}
		answeredResp := serializeTurn(answered, nil)
		contResp := serializeTurn(continuation, nil)
		out = QuestionAnswerResponse{
			TurnResponse:     contResp,
			AnsweredTurn:     answeredResp,
			ContinuationTurn: contResp,
		}
		return nil
	})
	if s.Gateway != nil {
		row := out.AnsweredTurn
		if row.HarnessTurnID != nil {
			_, _ = s.Gateway.AnswerQuestion(conversationID, *row.HarnessTurnID, questionID, answer, row.TaskID)
		}
		if out.ContinuationTurn.HarnessTurnID == nil {
			bound, err := s.bindGatewayTurn(ctx, productID, conversationID, out.ContinuationTurn.ID, true)
			if err == nil {
				out.ContinuationTurn = bound
				out.TurnResponse = bound
			}
		}
	}
	return out, err
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

func reserveTurn(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID, inputText string, assetIDs []string, idempotencyKey string, taskID *string, pageContext map[string]any) (turnRow, bool, error) {
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
	var existingID string
	var existingHash string
	err = pfdb.QueryRow(ctx, pgxTx, `
		SELECT id, request_hash FROM agent_turn_projections WHERE conversation_id = $1 AND idempotency_key = $2
	`, conversationID, key).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != hash {
			return turnRow{}, false, apperr.Conflict("同一 idempotency key 不能提交不同的 Agent Turn 请求")
		}
		row, err := loadTurnByID(ctx, pgxTx, existingID)
		return row, false, err
	}
	if !errors.Is(err, sqldb.ErrNoRows) {
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
		if task.CurrentTurnID != nil {
			var st string
			_ = pfdb.QueryRow(ctx, pgxTx, `SELECT status FROM agent_turn_projections WHERE id = $1`, *task.CurrentTurnID).Scan(&st)
			if inSet(blockingTurn, st) {
				return turnRow{}, false, apperr.Conflict("当前 Agent Task 仍有未结束的 Turn")
			}
		}
	}
	if conv.ScopeType == "product_workflow" && len(assets) > 0 && conv.ProductID != nil {
		for _, assetID := range assets {
			var pid string
			var status string
			err := pfdb.QueryRow(ctx, pgxTx, `SELECT product_id, verification_status FROM product_image_assets WHERE id = $1`, assetID).Scan(&pid, &status)
			if errors.Is(err, sqldb.ErrNoRows) || pid != *conv.ProductID {
				return turnRow{}, false, apperr.Validation("参考图片不属于当前商品")
			}
			if err != nil {
				return turnRow{}, false, err
			}
			if status != "verified" {
				return turnRow{}, false, apperr.Validation("参考图片尚未通过核验")
			}
		}
	}
	id := newID()
	assetJSON, _ := json.Marshal(assets)
	if _, err := pfdb.Exec(ctx, pgxTx, `
		INSERT INTO agent_turn_projections (
			id, conversation_id, task_id, idempotency_key, request_hash, input_text, input_asset_ids_json,
			status, resume_required, tool_steps_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'queued', FALSE, '[]'::jsonb, NOW(), NOW())
	`, id, conversationID, taskID, key, hash, text, assetJSON); err != nil {
		if uniqueViolation(err) {
			var replayID, replayHash string
			if scanErr := pfdb.QueryRow(ctx, pgxTx, `
				SELECT id, request_hash FROM agent_turn_projections WHERE conversation_id = $1 AND idempotency_key = $2
			`, conversationID, key).Scan(&replayID, &replayHash); scanErr == nil {
				if replayHash != hash {
					return turnRow{}, false, apperr.Conflict("同一 idempotency key 不能提交不同的 Agent Turn 请求")
				}
				row, err := loadTurnByID(ctx, pgxTx, replayID)
				return row, false, err
			}
		}
		return turnRow{}, false, err
	}
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_conversations SET status = 'collecting', updated_at = NOW() WHERE id = $1
	`, conversationID); err != nil {
		return turnRow{}, false, err
	}
	if taskID != nil {
		if _, err := pfdb.Exec(ctx, pgxTx, `UPDATE agent_tasks SET current_turn_id = $2, updated_at = NOW() WHERE id = $1`, *taskID, id); err != nil {
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

func insertPageContext(ctx context.Context, pgxTx *gorm.DB, taskID *string, turnID string, pageContext map[string]any) error {
	route, _ := pageContext["route"].(string)
	pageType, _ := pageContext["page_type"].(string)
	if stringsTrim(route) == "" || stringsTrim(pageType) == "" {
		return apperr.Validation("页面上下文缺少 route 或 page_type")
	}
	digest, err := turnRequestHash("page", nil, route+"|"+pageType, nil)
	if err != nil {
		return err
	}
	snapshotID := newID()
	selected, _ := json.Marshal(pageContext["selected_asset_ids"])
	visible, _ := json.Marshal(pageContext["visible_asset_ids"])
	filters, _ := json.Marshal(pageContext["filters"])
	if selected == nil {
		selected = []byte("[]")
	}
	if visible == nil {
		visible = []byte("[]")
	}
	if filters == nil {
		filters = []byte("{}")
	}
	if _, err := pfdb.Exec(ctx, pgxTx, `
		INSERT INTO agent_page_context_snapshots (
			id, task_id, turn_id, route, page_type, product_id, workflow_id,
			selected_asset_ids_json, visible_asset_ids_json, filters_json, digest, captured_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
	`, snapshotID, taskID, turnID, route, pageType, pageContext["product_id"], pageContext["workflow_id"], selected, visible, filters, digest); err != nil {
		return err
	}
	_, err = pfdb.Exec(ctx, pgxTx, `UPDATE agent_turn_projections SET page_context_snapshot_id = $2 WHERE id = $1`, turnID, snapshotID)
	return err
}

func (s Service) recordStartError(ctx context.Context, productID *string, conversationID, projectionID, message string) error {
	return tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID); err != nil {
			return err
		}
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_turn_projections SET sync_error = $2, updated_at = NOW() WHERE id = $1
		`, projectionID, message)
		return err
	})
}

func (s Service) bindGatewayTurn(ctx context.Context, productID *string, conversationID, projectionID string, deferIfUnavailable bool) (TurnResponse, error) {
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
	state, ge := s.Gateway.StartTurn(conversationID, row.TaskID, row.InputText, assets, row.IdempotencyKey, nil)
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
