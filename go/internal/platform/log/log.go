// Package log 把终端写成可读行，并把 JSON 滚动文件写到 STORAGE_ROOT/logs（可用 LOG_DIR 覆盖）。
package log

import (
	"fmt"
	"io"
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
	// ProcessAPI 是 productflow-api 的 process 字段，日志文件名为 productflow-api.log。
	ProcessAPI = "api"
	// ProcessWorker 是 productflow-worker 的 process 字段。
	ProcessWorker = "worker"
	// ProcessDispatcher 是 productflow-dispatcher 的 process 字段。
	ProcessDispatcher = "dispatcher"

	// FormatConsole 是 stderr 默认格式：时间、级别、进程、消息、key=value。
	FormatConsole = "console"
	// FormatJSON 让 stderr 也写 JSON（文件始终是 JSON）。
	FormatJSON = "json"
)

// Options 配置日志：stderr 给终端（默认可读行），滚动 JSON 文件给排障。
type Options struct {
	Level         string    // env LOG_LEVEL
	Format        string    // env LOG_FORMAT：console 或 json
	Dir           string    // env LOG_DIR；空则不写滚动文件
	Process       string    // log file stem: api / worker / dispatcher
	MaxBytes      int       // rotation size in bytes; converted to MiB for lumberjack
	BackupCount   int       // lumberjack MaxBackups
	RetentionDays int       // 天；≤0 不清理
	Stderr        io.Writer // 终端 Writer，nil 则 os.Stderr
	DisableColor  bool      // 关闭 ANSI
}

// New 组装 zap.Logger：stderr 按 Format 输出；Dir 非空时再挂 Debug 级滚动 JSON 文件。
// Process 必须是小写字母、数字或连字符。
// Process 非法或无法创建日志目录时返回 error。
func New(opts Options) (*zap.Logger, error) {
	process, err := sanitizeProcess(opts.Process)
	if err != nil {
		return nil, err
	}
	consoleLevel := parseLevel(opts.Level)
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	fileEncoderCfg := zap.NewProductionEncoderConfig()
	fileEncoderCfg.TimeKey = "ts"
	fileEncoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	fileEncoder := zapcore.NewJSONEncoder(fileEncoderCfg)

	var consoleEncoder zapcore.Encoder
	if resolveFormat(opts.Format) == FormatJSON {
		consoleEncoder = zapcore.NewJSONEncoder(fileEncoderCfg)
	} else {
		consoleEncoder = newPrettyEncoder(consoleColorEnabled(stderr, opts.DisableColor))
	}

	cores := []zapcore.Core{
		zapcore.NewCore(consoleEncoder, lockWriter(stderr), consoleLevel),
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
		cores = append(cores, zapcore.NewCore(fileEncoder, fileSync, zapcore.DebugLevel))
	}

	logger := zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).
		With(zap.String("process", process))
	if logFile != "" {
		logger.Info("file log enabled", zap.String("file", logFile))
	}
	return logger, nil
}

// FilePath 返回 Dir 下的 productflow-{process}.log。
func FilePath(dir, process string) string {
	return filepath.Join(dir, "productflow-"+process+".log")
}

// CleanupOldLogs 删除目录里超过保留天数的 productflow-*.log* 滚动文件。
// Glob 失败时返回 error；单文件删除失败则跳过。
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

func parseLevel(raw string) zapcore.Level {
	level := zapcore.InfoLevel
	if err := level.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(raw)))); err != nil {
		return zapcore.InfoLevel
	}
	return level
}

func resolveFormat(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case FormatJSON:
		return FormatJSON
	default:
		return FormatConsole
	}
}

func lockWriter(w io.Writer) zapcore.WriteSyncer {
	if ws, ok := w.(zapcore.WriteSyncer); ok {
		return zapcore.Lock(ws)
	}
	return zapcore.Lock(zapcore.AddSync(w))
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
