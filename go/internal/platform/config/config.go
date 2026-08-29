package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the process start-up overlay. Runtime provider settings still live in PostgreSQL.
type Config struct {
	AppHost                           string
	AppPort                           int
	DatabaseURL                       string
	RedisURL                          string
	LogLevel                          string
	LogDir                            string
	LogMaxBytes                       int
	LogBackupCount                    int
	LogRetentionDays                  int
	StorageRoot                       string
	SessionSecret                     string
	SessionCookieSecure               bool
	AdminAccessKey                    string
	SettingsAccessToken               string
	AdminAccessRequired               bool
	DeletionEnabled                   bool
	UploadMaxImageBytes               int
	UploadMaxBatchBytes               int
	UploadMaxBatchFiles               int
	UploadMaxReferenceImages          int
	UploadMaxPixels                   int
	UploadAllowedMIMETypes            string
	AgentServiceBaseURL               string
	AgentServiceInternalToken         string
	AgentServiceConnectTimeoutSeconds float64
	AgentServiceReadTimeoutSeconds    float64
	AgentTurnSyncPollSeconds          float64
}

func Load() (Config, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("APP_HOST", "0.0.0.0")
	v.SetDefault("APP_PORT", 29280)
	v.SetDefault("LOG_LEVEL", "INFO")
	v.SetDefault("LOG_MAX_BYTES", 10*1024*1024)
	v.SetDefault("LOG_BACKUP_COUNT", 5)
	v.SetDefault("LOG_RETENTION_DAYS", 14)
	v.SetDefault("STORAGE_ROOT", "./storage-dev")
	v.SetDefault("ADMIN_ACCESS_REQUIRED", true)
	v.SetDefault("DELETION_ENABLED", false)
	v.SetDefault("UPLOAD_MAX_IMAGE_BYTES", 10*1024*1024)
	v.SetDefault("UPLOAD_MAX_BATCH_BYTES", 50*1024*1024)
	v.SetDefault("UPLOAD_MAX_BATCH_FILES", 20)
	v.SetDefault("UPLOAD_MAX_REFERENCE_IMAGES", 6)
	v.SetDefault("UPLOAD_MAX_PIXELS", 16_000_000)
	v.SetDefault("UPLOAD_ALLOWED_IMAGE_MIME_TYPES", "image/png,image/jpeg,image/webp")
	v.SetDefault("AGENT_SERVICE_CONNECT_TIMEOUT_SECONDS", 5.0)
	v.SetDefault("AGENT_SERVICE_READ_TIMEOUT_SECONDS", 90.0)
	v.SetDefault("AGENT_TURN_SYNC_POLL_SECONDS", 1.0)

	storageRoot, err := ResolveStorageRoot(v.GetString("STORAGE_ROOT"))
	if err != nil {
		return Config{}, err
	}
	logDir, err := ResolveLogDir(v.GetString("LOG_DIR"), storageRoot)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		AppHost:                           v.GetString("APP_HOST"),
		AppPort:                           v.GetInt("APP_PORT"),
		DatabaseURL:                       NormalizePostgresURL(v.GetString("DATABASE_URL")),
		RedisURL:                          v.GetString("REDIS_URL"),
		LogLevel:                          v.GetString("LOG_LEVEL"),
		LogDir:                            logDir,
		LogMaxBytes:                       v.GetInt("LOG_MAX_BYTES"),
		LogBackupCount:                    v.GetInt("LOG_BACKUP_COUNT"),
		LogRetentionDays:                  v.GetInt("LOG_RETENTION_DAYS"),
		StorageRoot:                       storageRoot,
		SessionSecret:                     v.GetString("SESSION_SECRET"),
		SessionCookieSecure:               v.GetBool("SESSION_COOKIE_SECURE"),
		AdminAccessKey:                    v.GetString("ADMIN_ACCESS_KEY"),
		SettingsAccessToken:               strings.TrimSpace(v.GetString("SETTINGS_ACCESS_TOKEN")),
		AdminAccessRequired:               v.GetBool("ADMIN_ACCESS_REQUIRED"),
		DeletionEnabled:                   v.GetBool("DELETION_ENABLED"),
		UploadMaxImageBytes:               v.GetInt("UPLOAD_MAX_IMAGE_BYTES"),
		UploadMaxBatchBytes:               v.GetInt("UPLOAD_MAX_BATCH_BYTES"),
		UploadMaxBatchFiles:               v.GetInt("UPLOAD_MAX_BATCH_FILES"),
		UploadMaxReferenceImages:          v.GetInt("UPLOAD_MAX_REFERENCE_IMAGES"),
		UploadMaxPixels:                   v.GetInt("UPLOAD_MAX_PIXELS"),
		UploadAllowedMIMETypes:            v.GetString("UPLOAD_ALLOWED_IMAGE_MIME_TYPES"),
		AgentServiceBaseURL:               strings.TrimSpace(v.GetString("AGENT_SERVICE_BASE_URL")),
		AgentServiceInternalToken:         strings.TrimSpace(v.GetString("AGENT_SERVICE_INTERNAL_TOKEN")),
		AgentServiceConnectTimeoutSeconds: v.GetFloat64("AGENT_SERVICE_CONNECT_TIMEOUT_SECONDS"),
		AgentServiceReadTimeoutSeconds:    v.GetFloat64("AGENT_SERVICE_READ_TIMEOUT_SECONDS"),
		AgentTurnSyncPollSeconds:          v.GetFloat64("AGENT_TURN_SYNC_POLL_SECONDS"),
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

// ResolveStorageRoot 把相对路径收到仓库根（go.mod 所在 go/ 的上一级），不跟进程 cwd。
func ResolveStorageRoot(raw string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		cleaned = "./storage-dev"
	}
	return resolveRepoRelative(cleaned)
}

// ResolveLogDir 解析持久化日志目录。LOG_DIR 为空时用 STORAGE_ROOT/logs。
func ResolveLogDir(raw, storageRoot string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		if strings.TrimSpace(storageRoot) == "" {
			return "", fmt.Errorf("LOG_DIR is empty and storage root is missing")
		}
		return filepath.Join(filepath.Clean(storageRoot), "logs"), nil
	}
	return resolveRepoRelative(cleaned)
}

func resolveRepoRelative(cleaned string) (string, error) {
	if filepath.IsAbs(cleaned) {
		return filepath.Clean(cleaned), nil
	}
	root, err := moduleAwareRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(root, cleaned))
}

func moduleAwareRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if filepath.Base(dir) == "go" {
				return filepath.Dir(dir), nil
			}
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd, nil
		}
		dir = parent
	}
}
