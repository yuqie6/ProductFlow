package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ConfigItem 是一条运行时配置的设置页 HTTP 投影。
// Source 为 env_default 或 database。Secret=true 时 Value 永远是空串，HasValue 表示库里有没有值。
// 不要把本类型当 app_settings 原实行。
type ConfigItem struct {
	Key         string         `json:"key"`         // catalog 闭集键，不是任意字符串
	Label       string         `json:"label"`       // 设置页展示名，来自 catalog
	Category    string         `json:"category"`    // 图片工具参数 | 图片生成 | 提示词 | 图片与上传 | 生成队列 | 安全与运维
	InputType   string         `json:"input_type"`  // text | textarea | select | multi_select | number | boolean
	Description string         `json:"description"` // catalog 说明，可空
	Value       any            `json:"value"`       // Secret=true 时永远是空串
	Source      string         `json:"source"`      // env_default | database
	Secret      bool           `json:"secret"`      // true 时 Value 永远空串，用 HasValue 判断已配置
	HasValue    bool           `json:"has_value"`   // 库里或默认是否有值；Secret 项靠它判断已配置
	Options     []configOption `json:"options"`     // select 闭集；其它类型为 []
	Minimum     *int           `json:"minimum"`     // nil 表示无下限
	Maximum     *int           `json:"maximum"`     // nil 表示无上限
	UpdatedAt   *string        `json:"updated_at"`
}

// ConfigResponse 是可编辑运行时配置列表的 HTTP 体，只含 configDefinitions 闭集。
type ConfigResponse struct {
	Items []ConfigItem `json:"items"` // 只含 configDefinitions 闭集
}

type configRow struct {
	value     string
	updatedAt time.Time
}

// ConfigView 读取运行时配置 HTTP 投影，无写入。
// 调用时机：GET /api/settings。Secret 项不回明文。缺 app_settings 行时用环境默认，Source=env_default。
// 读库失败或 ctx 取消时返回 error。
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
	var rows []schema.AppSettings
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]configRow{}
	for _, row := range rows {
		out[row.Key] = configRow{value: row.Value, updatedAt: row.UpdatedAt}
	}
	return out, nil
}

func upsertAppSetting(dbTx *gorm.DB, key, value string) error {
	now := time.Now().UTC()
	row := schema.AppSettings{Key: key, Value: value, CreatedAt: now, UpdatedAt: now}
	return dbTx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"value": value, "updated_at": now}),
	}).Create(&row).Error
}

// publicValue 把库里的字符串收成设置页类型。Secret 永远回空串，不要把 API Key 投影出去。
func publicValue(def configDefinition, raw string) any {
	if def.Secret {
		return ""
	}
	switch def.InputType {
	case "boolean":
		return parseBool(raw, false)
	case "multi_select":
		fields, _ := parseImageToolAllowedFields(raw)
		return fields
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

// UpdateConfig 写入或重置 app_settings 键，然后回读 ConfigView。
// 调用时机：PATCH /api/settings。同一 key 不能同时出现在 values 和 reset_keys（Validation）。
// 未知键 Validation。不改供应商档案。DATABASE_URL 等 env-only 键不在闭集里，改了也无效。
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
			if err := dbTx.Where("key = ?", key).Delete(&schema.AppSettings{}).Error; err != nil {
				return err
			}
		}
		for key, value := range normalized {
			if err := upsertAppSetting(dbTx, key, value); err != nil {
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

// normalizeConfigValue 按 InputType 把设置页值收成入库字符串。未知键 400；布尔只认常见真假写法。
func normalizeConfigValue(key string, value any) (string, error) {
	def, ok := definitionByKey(key)
	if !ok {
		return "", apperr.Validation("未知配置项: " + key)
	}
	if isSMTPConfigKey(key) {
		return normalizeSMTPConfigValue(def, value)
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
		fields, err := parseAnyStringList(value)
		if err != nil {
			return "", err
		}
		return strings.Join(fields, ","), nil
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
	if err := validateMergedSMTP(merged); err != nil {
		return err
	}
	return nil
}

func parseAnyStringList(value any) ([]string, error) {
	if value == nil {
		return parseImageToolAllowedFields("")
	}
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
