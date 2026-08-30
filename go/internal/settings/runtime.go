package settings

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

var defaultImageToolAllowedFields = []string{
	"model", "quality", "output_format", "output_compression",
	"moderation", "action", "input_fidelity", "partial_images",
}

type Runtime struct {
	ImageGenerationMaxDimension int      `json:"image_generation_max_dimension"`
	ImageToolAllowedFields      []string `json:"image_tool_allowed_fields"`
	AdminAccessRequired         bool     `json:"admin_access_required"`
	DeletionEnabled             bool     `json:"deletion_enabled"`
}

type RuntimeReader interface {
	Runtime(ctx context.Context) (Runtime, error)
}

type LimitsReader interface {
	UploadLimits(ctx context.Context) (media.Limits, error)
}

type Store struct {
	db  *gorm.DB
	env config.Config
}

func NewStore(pool *pgxpool.Pool, env config.Config) *Store {
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		panic(err)
	}
	return &Store{db: gdb, env: env}
}

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
		parts := strings.Split(raw, ",")
		fields := make([]string, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			if item != "" {
				fields = append(fields, item)
			}
		}
		if len(fields) > 0 {
			runtime.ImageToolAllowedFields = fields
		}
	}
	return runtime, nil
}

type ImageToolRuntime struct {
	Options map[string]any
	Allowed []string
}

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
		parts := strings.Split(raw, ",")
		fields := make([]string, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			if item != "" {
				fields = append(fields, item)
			}
		}
		if len(fields) > 0 {
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
	rows, err := pfdb.Query(ctx, s.db, `SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

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
