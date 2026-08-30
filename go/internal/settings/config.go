package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type ConfigItem struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Category    string         `json:"category"`
	InputType   string         `json:"input_type"`
	Description string         `json:"description"`
	Value       any            `json:"value"`
	Source      string         `json:"source"`
	Secret      bool           `json:"secret"`
	HasValue    bool           `json:"has_value"`
	Options     []configOption `json:"options"`
	Minimum     *int           `json:"minimum"`
	Maximum     *int           `json:"maximum"`
	UpdatedAt   *string        `json:"updated_at"`
}

type ConfigResponse struct {
	Items []ConfigItem `json:"items"`
}

type configRow struct {
	value     string
	updatedAt time.Time
}

func (s *Store) ConfigView(ctx context.Context) (ConfigResponse, error) {
	rows, err := s.configRows(ctx)
	if err != nil {
		return ConfigResponse{}, err
	}
	items := make([]ConfigItem, 0, len(configDefinitions()))
	for _, def := range configDefinitions() {
		item := ConfigItem{
			Key: def.Key, Label: def.Label, Category: def.Category, InputType: def.InputType,
			Description: def.Description, Options: def.Options, Secret: def.Secret,
			Minimum: def.Minimum, Maximum: def.Maximum, Source: "env_default",
		}
		if item.Options == nil {
			item.Options = []configOption{}
		}
		raw := envDefault(s, def.Key)
		if row, ok := rows[def.Key]; ok {
			raw = row.value
			item.Source = "database"
			stamp := row.updatedAt.UTC().Format(time.RFC3339Nano)
			item.UpdatedAt = &stamp
		}
		item.Value = publicValue(def, raw)
		item.HasValue = hasConfigValue(def, raw)
		items = append(items, item)
	}
	return ConfigResponse{Items: items}, nil
}

func (s *Store) configRows(ctx context.Context) (map[string]configRow, error) {
	query, err := pfdb.Query(ctx, s.db, `SELECT key, value, updated_at FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer query.Close()
	out := map[string]configRow{}
	for query.Next() {
		var key, value string
		var updated time.Time
		if err := query.Scan(&key, &value, &updated); err != nil {
			return nil, err
		}
		out[key] = configRow{value: value, updatedAt: updated}
	}
	return out, query.Err()
}

func publicValue(def configDefinition, raw string) any {
	if def.Secret {
		return ""
	}
	switch def.InputType {
	case "boolean":
		return parseBool(raw, false)
	case "multi_select":
		return parseImageToolAllowedFields(raw)
	case "number":
		if def.Optional && strings.TrimSpace(raw) == "" {
			return nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return raw
		}
		return n
	default:
		return raw
	}
}

func hasConfigValue(def configDefinition, raw string) bool {
	if def.InputType == "boolean" {
		return true
	}
	return strings.TrimSpace(raw) != ""
}

func (s *Store) UpdateConfig(ctx context.Context, values map[string]any, resetKeys []string) (ConfigResponse, error) {
	reset := map[string]struct{}{}
	for _, key := range resetKeys {
		reset[key] = struct{}{}
	}
	unknown := []string{}
	for key := range values {
		if _, ok := definitionByKey(key); !ok {
			unknown = append(unknown, key)
		}
	}
	for key := range reset {
		if _, ok := definitionByKey(key); !ok {
			unknown = append(unknown, key)
		}
		if _, exists := values[key]; exists {
			return ConfigResponse{}, apperr.Validation("同一个配置项不能同时更新和恢复默认")
		}
	}
	if len(unknown) > 0 {
		return ConfigResponse{}, apperr.Validation("未知配置项: " + strings.Join(sortedUnique(unknown), ", "))
	}
	normalized := map[string]string{}
	for key, value := range values {
		text, err := normalizeConfigValue(key, value)
		if err != nil {
			return ConfigResponse{}, err
		}
		normalized[key] = text
	}
	rows, err := s.configRows(ctx)
	if err != nil {
		return ConfigResponse{}, err
	}
	merged := map[string]string{}
	for key, row := range rows {
		if _, drop := reset[key]; drop {
			continue
		}
		merged[key] = row.value
	}
	for key, value := range normalized {
		merged[key] = value
	}
	if err := validateMerged(merged); err != nil {
		return ConfigResponse{}, err
	}
	err = tx.WithGorm(ctx, s.db, func(dbTx *gorm.DB) error {
		for key := range reset {
			if _, err := pfdb.Exec(ctx, dbTx, `DELETE FROM app_settings WHERE key = $1`, key); err != nil {
				return err
			}
		}
		for key, value := range normalized {
			if _, err := pfdb.Exec(ctx, dbTx, `
				INSERT INTO app_settings (key, value, created_at, updated_at)
				VALUES ($1, $2, NOW(), NOW())
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
			`, key, value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ConfigResponse{}, err
	}
	return s.ConfigView(ctx)
}

func normalizeConfigValue(key string, value any) (string, error) {
	def, ok := definitionByKey(key)
	if !ok {
		return "", apperr.Validation("未知配置项: " + key)
	}
	switch def.InputType {
	case "boolean":
		switch typed := value.(type) {
		case bool:
			if typed {
				return "true", nil
			}
			return "false", nil
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "1", "true", "yes", "on":
				return "true", nil
			case "0", "false", "no", "off":
				return "false", nil
			}
		}
		return "", apperr.Validation(def.Label + " 必须是布尔值")
	case "multi_select":
		return strings.Join(parseAnyStringList(value), ","), nil
	case "number":
		if def.Optional && isEmptyNumber(value) {
			return "", nil
		}
		n, err := toInt(value)
		if err != nil {
			return "", apperr.Validation(def.Label + " 必须是整数")
		}
		if def.Minimum != nil && n < *def.Minimum {
			return "", apperr.Validation(fmt.Sprintf("%s 不能小于 %d", def.Label, *def.Minimum))
		}
		if def.Maximum != nil && n > *def.Maximum {
			return "", apperr.Validation(fmt.Sprintf("%s 不能大于 %d", def.Label, *def.Maximum))
		}
		return strconv.Itoa(n), nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if value == nil {
		text = ""
	}
	if key == "prompt_image_chat_template" && text == "" {
		return "", apperr.Validation(def.Label + " 不能为空；如需回到默认值请使用恢复默认")
	}
	if def.InputType == "select" {
		allowed := map[string]struct{}{}
		for _, opt := range def.Options {
			allowed[opt.Value] = struct{}{}
		}
		if _, ok := allowed[text]; !ok {
			names := make([]string, 0, len(def.Options))
			for _, opt := range def.Options {
				names = append(names, opt.Value)
			}
			return "", apperr.Validation(fmt.Sprintf("%s 必须是以下之一: %s", def.Label, strings.Join(names, ", ")))
		}
	}
	return text, nil
}

func validateMerged(merged map[string]string) error {
	if mime, ok := merged["upload_allowed_image_mime_types"]; ok && strings.TrimSpace(mime) == "" {
		return apperr.Validation("允许图片 MIME 不能为空")
	}
	return nil
}

func parseAnyStringList(value any) []string {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, strings.TrimSpace(fmt.Sprint(item)))
		}
		return parseImageToolAllowedFields(strings.Join(parts, ","))
	case []string:
		return parseImageToolAllowedFields(strings.Join(typed, ","))
	default:
		return parseImageToolAllowedFields(fmt.Sprint(value))
	}
}

func isEmptyNumber(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}

func toInt(value any) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(typed))
	default:
		return strconv.Atoi(fmt.Sprint(value))
	}
}

func sortedUnique(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// IntSetting 读取 app_settings 中的正整数配置，缺省或非法时回退。
func (s *Store) IntSetting(ctx context.Context, key string, fallback int) int {
	if s == nil {
		return fallback
	}
	raw := envDefault(s, key)
	rows, err := s.configRows(ctx)
	if err == nil {
		if row, ok := rows[key]; ok && strings.TrimSpace(row.value) != "" {
			raw = row.value
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
