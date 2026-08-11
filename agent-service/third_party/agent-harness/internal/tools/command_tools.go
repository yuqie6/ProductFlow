package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	defaultExecCommandTimeout = 30 * time.Second
	maxBashOutputBytes        = 200 << 10
	bashTruncatedNote         = "\n... (输出过长已截断)"
)

type commandResult struct {
	Command         string `json:"command"`
	ExitCode        int    `json:"exit_code"`
	Signal          string `json:"signal,omitempty"`
	TimedOut        bool   `json:"timed_out"`
	DurationMillis  int64  `json:"duration_ms"`
	Stdout          string `json:"stdout,omitempty"`
	Stderr          string `json:"stderr,omitempty"`
	StdoutSHA256    string `json:"stdout_sha256"`
	StderrSHA256    string `json:"stderr_sha256"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
}

func execCommandTool(executor bashExecutor, timeout func() time.Duration) Tool {
	return Tool{
		Name:         "exec_command",
		Description:  "同步执行命令并返回结构化 exit_code/stdout/stderr/timeout。超时由用户配置决定；长任务使用 start_command。",
		Capabilities: CapabilityRead | CapabilityWrite | CapabilityExecute,
		Effect:       EffectOpaque,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "要执行的 shell 命令"},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			command, err := parseCommand(raw)
			if err != nil {
				return "", err
			}
			result, runErr := executeStructuredCommand(ctx, executor, command, timeout())
			encoded, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				return "", marshalErr
			}
			return string(encoded), runErr
		},
	}
}

func executeStructuredCommand(parent context.Context, executor bashExecutor, command string, timeout time.Duration) (commandResult, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := executor.Command(ctx, command, executionBoundary(parent))
	stdout := newCappedWriter(maxBashOutputBytes)
	stderr := newCappedWriter(maxBashOutputBytes)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	err := cmd.Run()
	result := commandResult{
		Command: command, ExitCode: 0, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		DurationMillis: time.Since(started).Milliseconds(), Stdout: stdout.String(), Stderr: stderr.String(),
		StdoutSHA256: stdout.SHA256(), StderrSHA256: stderr.SHA256(),
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
	if err == nil {
		return result, nil
	}
	result.ExitCode = -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			result.Signal = status.Signal().String()
		}
	}
	if result.TimedOut {
		return result, context.DeadlineExceeded
	}
	return result, fmt.Errorf("命令退出码 %d", result.ExitCode)
}

func parseCommand(raw json.RawMessage) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := ParseArguments(raw, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return "", errors.New("缺少 command")
	}
	return args.Command, nil
}

type cappedWriter struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
	digest    hash.Hash
}

func newCappedWriter(limit int) cappedWriter {
	return cappedWriter{limit: max(0, limit), digest: sha256.New()}
}

func (w *cappedWriter) Write(data []byte) (int, error) {
	originalLen := len(data)
	_, _ = w.digest.Write(data)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		_, _ = w.buffer.Write(data[:min(remaining, len(data))])
	}
	if originalLen > max(0, remaining) {
		w.truncated = true
	}
	return originalLen, nil
}

func (w *cappedWriter) SHA256() string {
	return fmt.Sprintf("%x", w.digest.Sum(nil))
}

func (w *cappedWriter) String() string {
	if w.truncated {
		return w.buffer.String() + bashTruncatedNote
	}
	return w.buffer.String()
}
