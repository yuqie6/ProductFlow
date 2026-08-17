package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const HarnessCommit = "8c1d951ba9b7227e9bf16826eb50a5c801638727"

type Config struct {
	ListenAddress             string
	DataRoot                  string
	ProductFlowBaseURL        string
	InternalToken             string
	ProductFlowRequestTimeout time.Duration
	EventPollInterval         time.Duration
	HeartbeatInterval         time.Duration
	MaxBodyBytes              int64
	MaxIterations             int
	ModelContextWindow        int
	AutoCompactTokenLimit     int
	MaxConcurrentTurns        int
}

func Load() (Config, error) {
	dataRoot, err := filepath.Abs(envOr("AGENT_DATA_ROOT", "./data"))
	if err != nil {
		return Config{}, fmt.Errorf("resolve AGENT_DATA_ROOT: %w", err)
	}
	requestTimeout, err := durationEnv("PRODUCTFLOW_REQUEST_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	eventPoll, err := durationEnv("AGENT_EVENT_POLL_INTERVAL", 100*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	heartbeat, err := durationEnv("AGENT_HEARTBEAT_INTERVAL", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	maxBodyBytes, err := int64Env("AGENT_MAX_BODY_BYTES", 96<<20)
	if err != nil {
		return Config{}, err
	}
	maxIterations, err := intEnv("AGENT_MAX_ITERATIONS", 40)
	if err != nil {
		return Config{}, err
	}
	contextWindow, err := intEnv("AGENT_MODEL_CONTEXT_WINDOW", 128_000)
	if err != nil {
		return Config{}, err
	}
	compactLimit, err := intEnv("AGENT_AUTO_COMPACT_TOKEN_LIMIT", 96_000)
	if err != nil {
		return Config{}, err
	}
	maxConcurrentTurns, err := intEnv("AGENT_MAX_CONCURRENT_TURNS", 3)
	if err != nil {
		return Config{}, err
	}
	result := Config{
		ListenAddress:             envOr("AGENT_LISTEN_ADDRESS", ":29284"),
		DataRoot:                  dataRoot,
		ProductFlowBaseURL:        strings.TrimRight(envOr("PRODUCTFLOW_INTERNAL_BASE_URL", "http://productflow-backend:29280"), "/"),
		InternalToken:             strings.TrimSpace(os.Getenv("AGENT_SERVICE_INTERNAL_TOKEN")),
		ProductFlowRequestTimeout: requestTimeout,
		EventPollInterval:         eventPoll,
		HeartbeatInterval:         heartbeat,
		MaxBodyBytes:              maxBodyBytes,
		MaxIterations:             maxIterations,
		ModelContextWindow:        contextWindow,
		AutoCompactTokenLimit:     compactLimit,
		MaxConcurrentTurns:        maxConcurrentTurns,
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.ListenAddress) == "" {
		return errors.New("AGENT_LISTEN_ADDRESS is required")
	}
	if strings.TrimSpace(config.DataRoot) == "" {
		return errors.New("AGENT_DATA_ROOT is required")
	}
	if !strings.HasPrefix(config.ProductFlowBaseURL, "http://") && !strings.HasPrefix(config.ProductFlowBaseURL, "https://") {
		return errors.New("PRODUCTFLOW_INTERNAL_BASE_URL must be an absolute HTTP(S) URL")
	}
	if len(config.InternalToken) < 32 {
		return errors.New("AGENT_SERVICE_INTERNAL_TOKEN must contain at least 32 characters")
	}
	if config.ProductFlowRequestTimeout <= 0 || config.EventPollInterval < time.Millisecond || config.HeartbeatInterval < config.EventPollInterval {
		return errors.New("agent service duration settings are invalid")
	}
	if config.MaxBodyBytes <= 0 || config.MaxIterations <= 0 || config.ModelContextWindow <= 0 {
		return errors.New("agent service numeric limits must be positive")
	}
	if config.AutoCompactTokenLimit <= 0 || config.AutoCompactTokenLimit >= config.ModelContextWindow {
		return errors.New("AGENT_AUTO_COMPACT_TOKEN_LIMIT must be positive and below AGENT_MODEL_CONTEXT_WINDOW")
	}
	if config.MaxConcurrentTurns <= 0 {
		return errors.New("AGENT_MAX_CONCURRENT_TURNS must be positive")
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func intEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
