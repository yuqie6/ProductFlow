package settings

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/config"
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

type Store struct {
	pool *pgxpool.Pool
	env  config.Config
}

func NewStore(pool *pgxpool.Pool, env config.Config) *Store {
	return &Store{pool: pool, env: env}
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

func (s *Store) overrides(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM app_settings`)
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
