// Package settings 保存 PostgreSQL 中的运行时供应商与模型配置，并与 env 启动 overlay 分工。
//
// 职责：app_settings、provider_profiles、provider_bindings。进程启动值来自 config.Load；
// 本包在 API 起来后覆盖「可以进库的键」。DATABASE_URL / SESSION_SECRET / ADMIN_ACCESS_KEY 永远只认 env。
// 调用时机：设置页 HTTP、providers.Live* 每次 Resolve、httpx 管理口令门闩读 Runtime。
// 副作用：只写上述三张设置表。导出/导入不要带出密钥明文（has_api_key 布尔即可）。
// 错误：档案仍被绑定就 Archive 会 Validation（不是 Conflict）。缺绑定对 mock 是合法的。
package settings

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

var defaultImageToolAllowedFields = []string{
	"model", "quality", "output_format", "output_compression",
	"moderation", "action", "input_fidelity", "partial_images",
}

// Runtime 是 API 启动后仍可被 app_settings 覆盖的运行时开关。
type Runtime struct {
	ImageGenerationMaxDimension int      `json:"image_generation_max_dimension"` // 生图最大单边像素
	ImageToolAllowedFields      []string `json:"image_tool_allowed_fields"`      // image tool 字段白名单
	AdminAccessRequired         bool     `json:"admin_access_required"`          // true 时工作台需要登录
	DeletionEnabled             bool     `json:"deletion_enabled"`               // false 时禁止删商品/连续生图会话
}

// RuntimeReader 读取合并 env 与 DB overlay 后的运行时开关。
type RuntimeReader interface {
	// Runtime 返回当前进程应遵守的运行时开关。
	Runtime(ctx context.Context) (Runtime, error)
}

// LimitsReader 读取上传限制；DB overlay 可覆盖 env 启动值。
type LimitsReader interface {
	// UploadLimits 返回当前上传字节、像素与 MIME 限制。
	UploadLimits(ctx context.Context) (media.Limits, error)
}

// Store 读写 PostgreSQL app_settings、供应商档案与绑定。
type Store struct {
	db  *gorm.DB
	env config.Config
}

// NewStore 打开 GORM 连接；失败时 panic，供进程启动使用。
func NewStore(pool *pgxpool.Pool, env config.Config) *Store {
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		panic(err)
	}
	return &Store{db: gdb, env: env}
}

// Runtime 实现 RuntimeReader：env 提供启动默认值，app_settings 覆盖所选键。
func (s *Store) Runtime(ctx context.Context) (Runtime, error) {
	overrides, err := s.overrides(ctx)
	if err != nil {
		return Runtime{}, err
	}
	runtime := Runtime{
		ImageGenerationMaxDimension: 3840,
		ImageToolAllowedFields:      append([]string{}, defaultImageToolAllowedFields...),
		AdminAccessRequired:         s.env.AdminAccessRequired,
		DeletionEnabled:             s.env.DeletionEnabled,
	}
	if raw, ok := overrides["admin_access_required"]; ok {
		runtime.AdminAccessRequired = parseBool(raw, runtime.AdminAccessRequired)
	}
	if raw, ok := overrides["deletion_enabled"]; ok {
		runtime.DeletionEnabled = parseBool(raw, runtime.DeletionEnabled)
	}
	if raw, ok := overrides["image_generation_max_dimension"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			runtime.ImageGenerationMaxDimension = n
		}
	}
	if raw, ok := overrides["image_tool_allowed_fields"]; ok && strings.TrimSpace(raw) != "" {
		if fields := storedImageToolAllowedFields(raw); len(fields) > 0 {
			runtime.ImageToolAllowedFields = fields
		}
	}
	return runtime, nil
}

// ImageToolRuntime 是连续生图 / images tool 允许字段与 DB 中的默认选项。
type ImageToolRuntime struct {
	Options map[string]any // 设置页默认 tool 选项
	Allowed []string       // 允许发送给 Responses image tool 的字段
}

// ImageToolRuntime 读取 image tool 允许字段与默认选项。
func (s *Store) ImageToolRuntime(ctx context.Context) (ImageToolRuntime, error) {
	if s == nil {
		return ImageToolRuntime{Allowed: append([]string{}, defaultImageToolAllowedFields...)}, nil
	}
	overrides, err := s.overrides(ctx)
	if err != nil {
		return ImageToolRuntime{}, err
	}
	out := ImageToolRuntime{
		Options: map[string]any{},
		Allowed: append([]string{}, defaultImageToolAllowedFields...),
	}
	if raw, ok := overrides["image_tool_allowed_fields"]; ok && strings.TrimSpace(raw) != "" {
		if fields := storedImageToolAllowedFields(raw); len(fields) > 0 {
			out.Allowed = fields
		}
	}
	for _, key := range []string{
		"model", "quality", "output_format", "output_compression",
		"background", "moderation", "action", "input_fidelity", "partial_images",
	} {
		raw, ok := overrides["image_tool_"+key]
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		if key == "output_compression" || key == "partial_images" {
			n, err := strconv.Atoi(strings.TrimSpace(raw))
			if err == nil {
				out.Options[key] = n
			}
			continue
		}
		out.Options[key] = strings.TrimSpace(raw)
	}
	return out, nil
}

// UploadLimits 实现 LimitsReader：env 启动值可被 app_settings 覆盖。
func (s *Store) UploadLimits(ctx context.Context) (media.Limits, error) {
	overrides, err := s.overrides(ctx)
	if err != nil {
		return media.Limits{}, err
	}
	limits := media.Limits{
		MaxImageBytes:      s.env.UploadMaxImageBytes,
		MaxPixels:          s.env.UploadMaxPixels,
		AllowedMIMETypes:   media.ParseMIMEList(s.env.UploadAllowedMIMETypes),
		MaxBatchFiles:      s.env.UploadMaxBatchFiles,
		MaxBatchBytes:      s.env.UploadMaxBatchBytes,
		MaxReferenceImages: s.env.UploadMaxReferenceImages,
	}
	if n, ok := parseOverrideInt(overrides, "upload_max_image_bytes"); ok {
		limits.MaxImageBytes = n
	}
	if n, ok := parseOverrideInt(overrides, "upload_max_pixels"); ok {
		limits.MaxPixels = n
	}
	if n, ok := parseOverrideInt(overrides, "upload_max_batch_files"); ok {
		limits.MaxBatchFiles = n
	}
	if n, ok := parseOverrideInt(overrides, "upload_max_batch_bytes"); ok {
		limits.MaxBatchBytes = n
	}
	if n, ok := parseOverrideInt(overrides, "upload_max_reference_images"); ok {
		limits.MaxReferenceImages = n
	}
	if raw, ok := overrides["upload_allowed_image_mime_types"]; ok && strings.TrimSpace(raw) != "" {
		limits.AllowedMIMETypes = media.ParseMIMEList(raw)
	}
	return limits, nil
}

func parseOverrideInt(overrides map[string]string, key string) (int, bool) {
	raw, ok := overrides[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return n, true
}

func (s *Store) overrides(ctx context.Context) (map[string]string, error) {
	var rows []schema.AppSettings
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}

// ImageChatPromptTemplate 读取连续生图提示词模板。
// 调用时机：imagesession 拼 prompt。app_settings 缺键或空白时回退内置 defaultPromptTemplate。
// 不要把返回值当 listing 图类 compile 模板。
func (s *Store) ImageChatPromptTemplate(ctx context.Context) (string, error) {
	overrides, err := s.overrides(ctx)
	if err != nil {
		return "", err
	}
	if raw, ok := overrides["prompt_image_chat_template"]; ok && strings.TrimSpace(raw) != "" {
		return raw, nil
	}
	return defaultPromptTemplate, nil
}

func storedImageToolAllowedFields(raw string) []string {
	fields, _ := parseImageToolAllowedFields(raw)
	return fields
}

func parseBool(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
