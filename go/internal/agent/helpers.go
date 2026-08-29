package agent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	sqldb "database/sql"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

var (
	startableConversation = map[string]struct{}{
		"collecting": {}, "awaiting_confirmation": {}, "failed": {},
		"canceled": {}, "unknown": {}, "completed": {},
	}
	terminalTask = map[string]struct{}{
		"succeeded": {}, "failed": {}, "canceled": {}, "unknown": {},
	}
	busyHarnessTurn = map[string]struct{}{
		"queued": {}, "running": {}, "cancel_requested": {},
	}
	blockingTurn = map[string]struct{}{
		"queued": {}, "running": {}, "cancel_requested": {},
		"requires_input": {}, "awaiting_confirmation": {},
	}
	inFlightTurn = map[string]struct{}{
		"queued": {}, "running": {}, "cancel_requested": {},
	}
	activeTurn = map[string]struct{}{
		"queued": {}, "running": {}, "cancel_requested": {}, "requires_input": {},
	}
	terminalTurn = map[string]struct{}{
		"awaiting_confirmation": {}, "succeeded": {}, "failed": {}, "canceled": {}, "unknown": {},
	}
	eventKinds = map[string]struct{}{
		"turn.queued": {}, "turn.started": {}, "text.delta": {}, "tool.step": {},
		"question.required": {}, "question.answered": {}, "turn.resume_requested": {},
		"turn.cancel_requested": {}, "turn.requires_input": {}, "artifact.proposed": {},
		"turn.awaiting_confirmation": {}, "turn.succeeded": {}, "turn.failed": {},
		"turn.canceled": {}, "turn.unknown": {},
	}
	checkpointKinds = map[string]struct{}{
		"before_model_request": {}, "tool_effect_intent": {}, "tool_effect_result": {},
		"question_required": {}, "external_job_submitted": {}, "terminal": {},
	}
	executionPhases = map[string]struct{}{
		"claimed": {}, "model": {}, "tool": {}, "waiting_input": {}, "external_job": {}, "terminal": {},
	}
)

func newID() string { return clockid.New() }

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func inSet(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

func ptr[T any](v T) *T { return &v }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func normalizeSessionTitle(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("Agent Session 名称不能为空")
	}
	if utf8.RuneCountInString(normalized) > sessionTitleMax {
		return "", apperr.Validationf("Agent Session 名称不能超过 %d 个字符", sessionTitleMax)
	}
	return normalized, nil
}

func normalizeTaskTitle(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("Agent Task 标题不能为空")
	}
	if utf8.RuneCountInString(normalized) > taskTitleMax {
		return "", apperr.Validationf("Agent Task 标题不能超过 %d 个字符", taskTitleMax)
	}
	return normalized, nil
}

func normalizeTaskGoal(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("Agent Task 目标不能为空")
	}
	if utf8.RuneCountInString(normalized) > taskGoalMax {
		return "", apperr.Validationf("Agent Task 目标不能超过 %d 个字符", taskGoalMax)
	}
	return normalized, nil
}

func boundedSummary(value string) string {
	if utf8.RuneCountInString(value) <= taskSummaryMax {
		return value
	}
	runes := []rune(value)
	return string(runes[:taskSummaryMax])
}

func normalizeIdempotency(value, field string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation(field + " 不能为空")
	}
	if len(normalized) > maxIdempotencyBytes {
		return "", apperr.Validationf("%s 不能超过 %d bytes", field, maxIdempotencyBytes)
	}
	return normalized, nil
}

func normalizeInputText(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("Agent Turn 输入不能为空")
	}
	if utf8.RuneCountInString(normalized) > maxInputText {
		return "", apperr.Validationf("Agent Turn 输入不能超过 %d 个字符", maxInputText)
	}
	return normalized, nil
}

func normalizeAssetIDs(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			return nil, apperr.Validation("参考图片 ID 不能为空")
		}
		if _, ok := seen[id]; ok {
			return nil, apperr.Validation("同一 Agent Turn 不能重复选择参考图片")
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) > maxInputAssets {
		return nil, apperr.Validationf("单个 Agent Turn 最多选择 %d 张参考图片", maxInputAssets)
	}
	return out, nil
}

func turnRequestHash(conversationID string, taskID *string, inputText string, assetIDs []string) (string, error) {
	return canonjson.SHA256Hex(map[string]any{
		"schema_version":  1,
		"conversation_id": conversationID,
		"task_id":         taskID,
		"input_text":      inputText,
		"input_asset_ids": assetIDs,
	})
}

func encodeCursor(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base64.URLEncoding.EncodeToString(raw), "="), nil
}

func decodeCursor(value string, dest any, invalid string) error {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	if len(normalized) > 4096 {
		return apperr.Validation(invalid)
	}
	padded := normalized + strings.Repeat("=", (4-len(normalized)%4)%4)
	raw, err := base64.URLEncoding.DecodeString(padded)
	if err != nil {
		return apperr.Validation(invalid)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return apperr.Validation(invalid)
	}
	return nil
}

func isNoRows(err error) bool {
	return errors.Is(err, sqldb.ErrNoRows)
}

func mapGateway(err error) error {
	var ge GatewayError
	if !errors.As(err, &ge) {
		return err
	}
	if ge.Code == "not_configured" {
		return apperr.Unavailable("Agent 服务尚未配置或暂时不可用")
	}
	switch ge.Status {
	case 400:
		return apperr.Validation("Agent 服务拒绝了当前请求")
	case 404:
		return apperr.Conflict("Agent 服务中不存在对应 Turn")
	case 409:
		return apperr.Conflict("Agent Turn 当前状态与请求冲突")
	default:
		return apperr.Unavailable("Agent 服务暂时不可用")
	}
}
