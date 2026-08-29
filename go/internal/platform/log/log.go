package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	ProcessAPI        = "api"
	ProcessWorker     = "worker"
	ProcessDispatcher = "dispatcher"
)

// Options 配置 JSON 日志：stderr 给终端/Compose，滚动文件给排障。
type Options struct {
	Level         string
	Dir           string
	Process       string
	MaxBytes      int
	BackupCount   int
	RetentionDays int
}

func New(opts Options) (*zap.Logger, error) {
	process, err := sanitizeProcess(opts.Process)
	if err != nil {
		return nil, err
	}
	level := zapcore.InfoLevel
	if parsedErr := level.UnmarshalText([]byte(strings.ToLower(opts.Level))); parsedErr != nil {
		level = zapcore.InfoLevel
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoder := zapcore.NewJSONEncoder(encoderCfg)

	cores := []zapcore.Core{
		zapcore.NewCore(encoder, zapcore.Lock(os.Stderr), level),
	}

	logFile := ""
	if dir := strings.TrimSpace(opts.Dir); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create log dir: %w", err)
		}
		_, _ = CleanupOldLogs(dir, opts.RetentionDays)
		logFile = FilePath(dir, process)
		maxSizeMB := opts.MaxBytes / (1024 * 1024)
		if maxSizeMB < 1 {
			maxSizeMB = 1
		}
		fileSync := zapcore.AddSync(&lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    maxSizeMB,
			MaxBackups: opts.BackupCount,
			MaxAge:     opts.RetentionDays,
			LocalTime:  false,
			Compress:   false,
		})
		cores = append(cores, zapcore.NewCore(encoder, fileSync, level))
	}

	logger := zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).
		With(zap.String("process", process))
	if logFile != "" {
		logger.Info("file log enabled", zap.String("file", logFile))
	}
	return logger, nil
}

func FilePath(dir, process string) string {
	return filepath.Join(dir, "productflow-"+process+".log")
}

// CleanupOldLogs 删除目录里超过保留天数的 productflow-*.log* 滚动文件。
func CleanupOldLogs(dir string, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	entries, err := filepath.Glob(filepath.Join(dir, "productflow-*.log*"))
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	deleted := 0
	for _, path := range entries {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() || !info.ModTime().UTC().Before(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil {
			continue
		}
		deleted++
	}
	return deleted, nil
}

func sanitizeProcess(process string) (string, error) {
	p := strings.TrimSpace(process)
	if p == "" {
		return "", fmt.Errorf("log process name is required")
	}
	for _, r := range p {
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '-' {
			continue
		}
		return "", fmt.Errorf("invalid log process name %q", process)
	}
	return p, nil
}
