// Package config 是进程启动时从环境变量读出的 overlay。
// 供应商、模型、运行时门禁等仍以 PostgreSQL app_settings / provider_* 为准，启动后再由 settings.Store 覆盖。
// DATABASE_URL、REDIS_URL、SESSION_SECRET、ADMIN_ACCESS_KEY 只认 env，禁止写进数据库或配置导出。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config 是进程启动 overlay。改供应商/模型不要改这里，去 PostgreSQL；密钥字段永远只来自 env。
type Config struct {
	AppHost                           string  // env APP_HOST
	AppPort                           int     // env APP_PORT
	DatabaseURL                       string  // env DATABASE_URL，启动后不再读库
	RedisURL                          string  // env REDIS_URL，asynq 用；Load 不强制非空
	LogLevel                          string  // env LOG_LEVEL
	LogFormat                         string  // env LOG_FORMAT，console 或 json
	LogDir                            string  // env LOG_DIR；空则 STORAGE_ROOT/logs
	LogMaxBytes                       int     // 字节；交给 lumberjack 前会换成 MiB
	LogBackupCount                    int     // env LOG_BACKUP_COUNT，滚动份数
	LogRetentionDays                  int     // env LOG_RETENTION_DAYS，天
	StorageRoot                       string  // env STORAGE_ROOT，相对仓库根
	SessionSecret                     string  // env SESSION_SECRET，空则 Load 失败
	SessionCookieSecure               bool    // env SESSION_COOKIE_SECURE
	AdminAccessKey                    string  // env ADMIN_ACCESS_KEY，登录时恒定时间比较
	SettingsAccessToken               string  // env SETTINGS_ACCESS_TOKEN，设置页解锁密钥，env-only
	AdminAccessRequired               bool    // env 默认值；运行时 app_settings 可覆盖
	DeletionEnabled                   bool    // env DELETION_ENABLED；运行时 app_settings 可覆盖
	UploadMaxImageBytes               int     // env UPLOAD_MAX_IMAGE_BYTES，字节；运行时可被 app_settings 覆盖
	UploadMaxBatchBytes               int     // env UPLOAD_MAX_BATCH_BYTES，字节；运行时可被 app_settings 覆盖
	UploadMaxBatchFiles               int     // env UPLOAD_MAX_BATCH_FILES；运行时可被 app_settings 覆盖
	UploadMaxReferenceImages          int     // env UPLOAD_MAX_REFERENCE_IMAGES；运行时可被 app_settings 覆盖
	UploadMaxPixels                   int     // env UPLOAD_MAX_PIXELS；运行时可被 app_settings 覆盖
	UploadAllowedMIMETypes            string  // env UPLOAD_ALLOWED_IMAGE_MIME_TYPES，逗号分隔
	AgentServiceBaseURL               string  // env AGENT_SERVICE_BASE_URL
	AgentServiceInternalToken         string  // env AGENT_SERVICE_INTERNAL_TOKEN，env-only 密钥
	AgentServiceConnectTimeoutSeconds float64 // env AGENT_SERVICE_CONNECT_TIMEOUT_SECONDS
	AgentServiceReadTimeoutSeconds    float64 // env AGENT_SERVICE_READ_TIMEOUT_SECONDS
	AgentTurnSyncPollSeconds          float64 // env AGENT_TURN_SYNC_POLL_SECONDS
	MetricsBearerToken                string  // 空则不注册 GET /metrics
	DispatcherMetricsAddr             string  // env DISPATCHER_METRICS_ADDR；空则不启动 dispatcher metrics server
	WorkerMetricsAddr                 string  // env WORKER_METRICS_ADDR；空则不启动 worker metrics server
	QuotaTrialUnits                   int64   // env QUOTA_TRIAL_UNITS；新商家首次建账可用额度；默认 100
}

// Load 用 viper AutomaticEnv 读进程环境，并填开发默认值。
// DATABASE_URL 与 SESSION_SECRET 为空会失败；REDIS_URL 空留给 worker/dispatcher 再报。
// 相对 STORAGE_ROOT / LOG_DIR 相对仓库根解析，不跟 cwd。
func Load() (Config, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("APP_HOST", "0.0.0.0")
	v.SetDefault("APP_PORT", 29280)
	v.SetDefault("LOG_LEVEL", "INFO")
	v.SetDefault("LOG_FORMAT", "console")
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
	v.SetDefault("QUOTA_TRIAL_UNITS", 100)

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
		LogFormat:                         v.GetString("LOG_FORMAT"),
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
		MetricsBearerToken:                strings.TrimSpace(v.GetString("METRICS_BEARER_TOKEN")),
		DispatcherMetricsAddr:             strings.TrimSpace(v.GetString("DISPATCHER_METRICS_ADDR")),
		WorkerMetricsAddr:                 strings.TrimSpace(v.GetString("WORKER_METRICS_ADDR")),
		QuotaTrialUnits:                   v.GetInt64("QUOTA_TRIAL_UNITS"),
	}
	if cfg.QuotaTrialUnits < 0 {
		cfg.QuotaTrialUnits = 100
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("SESSION_SECRET is required")
	}
	return cfg, nil
}

// NormalizePostgresURL 把 SQLAlchemy 的 postgresql+psycopg(2):// 收成 pgx 认识的 postgres://。
// 只替换前缀一次；其它 scheme 原样返回。测试库与 migrate 也走这里，避免两套 URL。
func NormalizePostgresURL(raw string) string {
	replaced := strings.Replace(raw, "postgresql+psycopg://", "postgres://", 1)
	replaced = strings.Replace(replaced, "postgresql+psycopg2://", "postgres://", 1)
	return replaced
}

// Addr 拼出 http.Server 监听地址 APP_HOST:APP_PORT。不要自己再拼一次以免漏默认端口。
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.AppHost, c.AppPort)
}

// ResolveStorageRoot 把 STORAGE_ROOT 收到仓库根（含 go/go.mod 时取其父目录），不跟进程 cwd。
// 空串当作 ./storage-dev。绝对路径只做 Clean。
// 读 cwd 失败或无法拼出绝对路径时返回 error。
func ResolveStorageRoot(raw string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		cleaned = "./storage-dev"
	}
	return resolveRepoRelative(cleaned)
}

// ResolveLogDir 解析滚动 JSON 日志目录。LOG_DIR 为空则用 STORAGE_ROOT/logs；两者都空返回 error。
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

// moduleAwareRepoRoot 从 cwd 向上找 go.mod：目录名是 go 则仓库根是其父目录，否则把该目录当根。
// 找不到 go.mod 时退回 cwd，避免在容器里因工作目录不同把 storage 写飞。
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
