package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

const toolEffectIntentSchemaVersion = 1

type toolEffectIntentV1 struct {
	SchemaVersion  int             `json:"schema_version"`
	ToolName       string          `json:"tool_name"`
	ToolCallID     string          `json:"tool_call_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	RecoveryPolicy string          `json:"recovery_policy"`
	RequestPayload json.RawMessage `json:"request_payload"`
}

func parseToolEffectIntent(payload json.RawMessage) (toolEffectIntentV1, error) {
	if len(payload) > maxCheckpointPayload {
		return toolEffectIntentV1{}, apperr.Validation("Agent checkpoint payload 超过大小限制")
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var intent toolEffectIntentV1
	if err := dec.Decode(&intent); err != nil {
		return toolEffectIntentV1{}, apperr.Validation("tool_effect_intent 必须符合 schema v1")
	}
	if dec.More() {
		return toolEffectIntentV1{}, apperr.Validation("tool_effect_intent 必须符合 schema v1")
	}
	if intent.SchemaVersion != toolEffectIntentSchemaVersion {
		return toolEffectIntentV1{}, apperr.Validation("tool_effect_intent schema_version 必须为 1")
	}
	intent.ToolName = strings.TrimSpace(intent.ToolName)
	intent.ToolCallID = strings.TrimSpace(intent.ToolCallID)
	intent.IdempotencyKey = strings.TrimSpace(intent.IdempotencyKey)
	intent.RecoveryPolicy = strings.TrimSpace(intent.RecoveryPolicy)
	if intent.ToolName == "" || len(intent.ToolName) > 120 ||
		intent.ToolCallID == "" || len(intent.ToolCallID) > 120 ||
		intent.IdempotencyKey == "" || len(intent.IdempotencyKey) > maxIdempotencyBytes {
		return toolEffectIntentV1{}, apperr.Validation("tool_effect_intent 身份字段无效")
	}
	if intent.RecoveryPolicy != "none" && intent.RecoveryPolicy != "reconcile_only" && intent.RecoveryPolicy != "reconcile_then_retry" {
		return toolEffectIntentV1{}, apperr.Validation("tool_effect_intent recovery_policy 无效")
	}
	if !jsonObjectPayload(intent.RequestPayload) {
		return toolEffectIntentV1{}, apperr.Validation("tool_effect_intent request_payload 必须是对象")
	}
	if err := rejectForbiddenIntentValue(intent.RequestPayload); err != nil {
		return toolEffectIntentV1{}, err
	}
	return intent, nil
}

func jsonObjectPayload(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func rejectForbiddenIntentValue(raw json.RawMessage) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return apperr.Validation("tool_effect_intent request_payload 必须是对象")
	}
	return walkForbiddenIntentValue(value)
}

func walkForbiddenIntentValue(value any) error {
	switch typed := value.(type) {
	case nil, bool, float64:
		return nil
	case json.Number:
		return nil
	case string:
		if strings.HasPrefix(typed, "data:image/") {
			return apperr.Validation("tool_effect_intent request_payload 不得包含密钥、图片字节或原始 HTTP 响应")
		}
		return nil
	case []any:
		for _, item := range typed {
			if err := walkForbiddenIntentValue(item); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for key, child := range typed {
			if forbiddenIntentKey(key) {
				return apperr.Validation("tool_effect_intent request_payload 不得包含密钥、图片字节或原始 HTTP 响应")
			}
			if err := walkForbiddenIntentValue(child); err != nil {
				return err
			}
		}
		return nil
	default:
		return apperr.Validation("tool_effect_intent request_payload 必须是 JSON")
	}
}

func forbiddenIntentKey(key string) bool {
	normalized := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' {
			return -1
		}
		return unicode.ToLower(r)
	}, key)
	switch normalized {
	case "authorization", "cookie", "setcookie", "apikey", "accesskey", "secret", "password", "passwd", "token", "bearer",
		"rawresponse", "httpresponse", "responsebody", "responseheaders", "rawhttp",
		"imagebytes", "imagedata", "filebytes":
		return true
	default:
		return false
	}
}
