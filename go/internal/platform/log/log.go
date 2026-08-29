package log

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "json"
	cfg.EncoderConfig.TimeKey = "ts"
	parsed := zapcore.InfoLevel
	if err := parsed.UnmarshalText([]byte(strings.ToLower(level))); err == nil {
		cfg.Level = zap.NewAtomicLevelAt(parsed)
	}
	return cfg.Build()
}
