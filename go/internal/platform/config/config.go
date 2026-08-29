package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the process start-up overlay. Runtime provider settings still live in PostgreSQL.
type Config struct {
	AppHost             string
	AppPort             int
	DatabaseURL         string
	RedisURL            string
	LogLevel            string
	StorageRoot         string
	SessionSecret       string
	SessionCookieSecure bool
	AdminAccessKey      string
	SettingsAccessToken string
	AdminAccessRequired bool
	DeletionEnabled     bool
	UploadMaxImageBytes      int
	UploadMaxBatchBytes      int
	UploadMaxBatchFiles      int
	UploadMaxReferenceImages int
	UploadMaxPixels          int
	UploadAllowedMIMETypes   string
}

func Load() (Config, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("APP_HOST", "0.0.0.0")
	v.SetDefault("APP_PORT", 29280)
	v.SetDefault("LOG_LEVEL", "INFO")
	v.SetDefault("STORAGE_ROOT", "./backend/storage")
	v.SetDefault("ADMIN_ACCESS_REQUIRED", true)
	v.SetDefault("DELETION_ENABLED", false)
	v.SetDefault("UPLOAD_MAX_IMAGE_BYTES", 10*1024*1024)
	v.SetDefault("UPLOAD_MAX_BATCH_BYTES", 50*1024*1024)
	v.SetDefault("UPLOAD_MAX_BATCH_FILES", 20)
	v.SetDefault("UPLOAD_MAX_REFERENCE_IMAGES", 6)
	v.SetDefault("UPLOAD_MAX_PIXELS", 16_000_000)
	v.SetDefault("UPLOAD_ALLOWED_IMAGE_MIME_TYPES", "image/png,image/jpeg,image/webp")

	cfg := Config{
		AppHost:             v.GetString("APP_HOST"),
		AppPort:             v.GetInt("APP_PORT"),
		DatabaseURL:         NormalizePostgresURL(v.GetString("DATABASE_URL")),
		RedisURL:            v.GetString("REDIS_URL"),
		LogLevel:            v.GetString("LOG_LEVEL"),
		StorageRoot:         v.GetString("STORAGE_ROOT"),
		SessionSecret:       v.GetString("SESSION_SECRET"),
		SessionCookieSecure: v.GetBool("SESSION_COOKIE_SECURE"),
		AdminAccessKey:      v.GetString("ADMIN_ACCESS_KEY"),
		SettingsAccessToken: strings.TrimSpace(v.GetString("SETTINGS_ACCESS_TOKEN")),
		AdminAccessRequired:      v.GetBool("ADMIN_ACCESS_REQUIRED"),
		DeletionEnabled:          v.GetBool("DELETION_ENABLED"),
		UploadMaxImageBytes:      v.GetInt("UPLOAD_MAX_IMAGE_BYTES"),
		UploadMaxBatchBytes:      v.GetInt("UPLOAD_MAX_BATCH_BYTES"),
		UploadMaxBatchFiles:      v.GetInt("UPLOAD_MAX_BATCH_FILES"),
		UploadMaxReferenceImages: v.GetInt("UPLOAD_MAX_REFERENCE_IMAGES"),
		UploadMaxPixels:          v.GetInt("UPLOAD_MAX_PIXELS"),
		UploadAllowedMIMETypes:   v.GetString("UPLOAD_ALLOWED_IMAGE_MIME_TYPES"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("SESSION_SECRET is required")
	}
	return cfg, nil
}

func NormalizePostgresURL(raw string) string {
	replaced := strings.Replace(raw, "postgresql+psycopg://", "postgres://", 1)
	replaced = strings.Replace(replaced, "postgresql+psycopg2://", "postgres://", 1)
	return replaced
}

func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.AppHost, c.AppPort)
}
