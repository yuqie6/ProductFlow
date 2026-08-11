package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxHostCommandOutput = 1 << 20

type HostCommandInput struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type HostCommandResult struct {
	Command         string `json:"command"`
	ExitCode        int    `json:"exit_code"`
	Signal          string `json:"signal,omitempty"`
	TimedOut        bool   `json:"timed_out"`
	DurationMillis  int64  `json:"duration_ms"`
	Stdout          string `json:"stdout,omitempty"`
	Stderr          string `json:"stderr,omitempty"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
}

type HostCommandTool struct {
	workingDir string
}

func NewHostCommandTool(workingDir string) (*HostCommandTool, error) {
	absolute, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("host command working directory 不是目录: %s", resolved)
	}
	return &HostCommandTool{workingDir: resolved}, nil
}

func (t *HostCommandTool) Name() string        { return "host_command" }
func (t *HostCommandTool) Effect() EffectClass { return EffectOpaque }

func (t *HostCommandTool) Prepare(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input HostCommandInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	input.Command = strings.TrimSpace(input.Command)
	if input.Command == "" {
		return nil, errors.New("host_command 缺少 command")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 600
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 3600 {
		return nil, errors.New("host_command timeout_seconds 必须在 1 到 3600 之间")
	}
	return json.Marshal(input)
}

func (t *HostCommandTool) Execute(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
	var input HostCommandInput
	if err := decodeStrict(invocation.Prepared, &input); err != nil {
		return nil, fmt.Errorf("prepared host_command: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "bash", "--noprofile", "--norc", "-c", input.Command)
	cmd.Dir = t.workingDir
	cmd.Env = durableCommandEnvironment()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	stdout := &limitedBuffer{limit: maxHostCommandOutput}
	stderr := &limitedBuffer{limit: maxHostCommandOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	started := time.Now()
	runErr := cmd.Run()
	result := HostCommandResult{
		Command: input.Command, ExitCode: 0, TimedOut: errors.Is(runCtx.Err(), context.DeadlineExceeded),
		DurationMillis: time.Since(started).Milliseconds(), Stdout: stdout.String(), Stderr: stderr.String(),
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
	if runErr != nil {
		result.ExitCode = -1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				result.Signal = status.Signal().String()
			}
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if result.TimedOut {
		return encoded, context.DeadlineExceeded
	}
	if runErr != nil {
		return encoded, fmt.Errorf("host_command 退出码 %d", result.ExitCode)
	}
	return encoded, nil
}

func (t *HostCommandTool) Reconcile(context.Context, Invocation) (ReconcileResult, error) {
	return ReconcileResult{}, errors.New("host_command 是 opaque effect,不能自动对账")
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(data[:min(remaining, len(data))])
	}
	if original > max(0, remaining) {
		b.truncated = true
	}
	return original, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

func durableCommandEnvironment() []string {
	allowed := []string{
		"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE",
		"TERM", "NO_COLOR", "TZ", "GOPATH", "GOTOOLCHAIN", "GOFLAGS", "CGO_ENABLED", "CC", "CXX",
	}
	environment := make([]string, 0, len(allowed)+2)
	for _, name := range allowed {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return append(environment, "TMPDIR=/tmp", "GOCACHE=/tmp/harness-go-build")
}
