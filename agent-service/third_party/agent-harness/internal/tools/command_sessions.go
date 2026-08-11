package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultCommandTimeout = 10 * time.Minute
	maxCommandTimeout     = time.Hour
	maxCommandLogBytes    = 10 << 20
	maxPollOutputBytes    = 100 << 10
	maxCommandSessions    = 64
)

type commandSessionStatus string

const (
	commandRunning    commandSessionStatus = "running"
	commandSucceeded  commandSessionStatus = "succeeded"
	commandFailed     commandSessionStatus = "failed"
	commandTimedOut   commandSessionStatus = "timed_out"
	commandTerminated commandSessionStatus = "terminated"
)

type commandLog struct {
	mu        sync.Mutex
	file      *os.File
	size      int64
	limit     int64
	truncated bool
}

func newCommandLog(kind string) (*commandLog, error) {
	file, err := os.CreateTemp("", "agent-harness-command-*."+kind)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	path := file.Name()
	// Linux/WSL 可在保持 FD 可读写的同时解除目录项,进程异常退出也不会遗留日志文件。
	if err := os.Remove(path); err != nil {
		file.Close()
		return nil, err
	}
	return &commandLog{file: file, limit: maxCommandLogBytes}, nil
}

func (l *commandLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	original := len(data)
	remaining := l.limit - l.size
	if remaining > 0 {
		write := data
		if int64(len(write)) > remaining {
			write = write[:remaining]
		}
		n, err := l.file.Write(write)
		l.size += int64(n)
		if err != nil {
			return n, err
		}
	}
	if int64(original) > max(0, remaining) {
		l.truncated = true
	}
	return original, nil
}

func (l *commandLog) read(offset int64) (content string, next int64, more, truncated bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if offset < 0 || offset > l.size {
		return "", offset, false, l.truncated, fmt.Errorf("输出 offset=%d 越界,当前大小 %d", offset, l.size)
	}
	length := min(int64(maxPollOutputBytes), l.size-offset)
	data := make([]byte, length)
	if length > 0 {
		n, readErr := l.file.ReadAt(data, offset)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", offset, false, l.truncated, readErr
		}
		data = data[:n]
	}
	next = offset + int64(len(data))
	return string(data), next, next < l.size, l.truncated, nil
}

func (l *commandLog) closeAndRemove() {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.file.Close()
}

type commandSession struct {
	mu      sync.Mutex
	stdinMu sync.Mutex

	id       string
	command  string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *commandLog
	stderr   *commandLog
	cancel   context.CancelFunc
	done     chan struct{}
	status   commandSessionStatus
	started  time.Time
	finished time.Time
	exitCode *int
	signal   string
	err      string
	stopping bool
}

type commandManager struct {
	mu       sync.RWMutex
	executor bashExecutor
	sessions map[string]*commandSession
	starting int
	closed   bool
}

func newCommandManager(executor bashExecutor) *commandManager {
	return &commandManager{executor: executor, sessions: make(map[string]*commandSession)}
}

type startCommandArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type pollCommandArgs struct {
	SessionID    string `json:"session_id"`
	StdoutOffset int64  `json:"stdout_offset,omitempty"`
	StderrOffset int64  `json:"stderr_offset,omitempty"`
	WaitMillis   int    `json:"wait_ms,omitempty"`
}

type commandSessionResult struct {
	SessionID       string               `json:"session_id"`
	Status          commandSessionStatus `json:"status"`
	Command         string               `json:"command,omitempty"`
	PID             int                  `json:"pid,omitempty"`
	StartedAt       time.Time            `json:"started_at"`
	FinishedAt      *time.Time           `json:"finished_at,omitempty"`
	DurationMillis  int64                `json:"duration_ms"`
	ExitCode        *int                 `json:"exit_code,omitempty"`
	Signal          string               `json:"signal,omitempty"`
	Error           string               `json:"error,omitempty"`
	Stdout          string               `json:"stdout,omitempty"`
	StdoutOffset    int64                `json:"stdout_offset"`
	NextStdout      int64                `json:"next_stdout_offset"`
	StdoutMore      bool                 `json:"stdout_more,omitempty"`
	StdoutTruncated bool                 `json:"stdout_truncated,omitempty"`
	Stderr          string               `json:"stderr,omitempty"`
	StderrOffset    int64                `json:"stderr_offset"`
	NextStderr      int64                `json:"next_stderr_offset"`
	StderrMore      bool                 `json:"stderr_more,omitempty"`
	StderrTruncated bool                 `json:"stderr_truncated,omitempty"`
}

func (m *commandManager) start(boundary ExecutionBoundary, args startCommandArgs) (commandSessionResult, error) {
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return commandSessionResult{}, errors.New("缺少 command")
	}
	timeout := defaultCommandTimeout
	if args.TimeoutSeconds != 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 || timeout > maxCommandTimeout {
		return commandSessionResult{}, fmt.Errorf("timeout_seconds 必须在 1 到 %d 之间", int(maxCommandTimeout/time.Second))
	}
	if err := m.reserve(); err != nil {
		return commandSessionResult{}, err
	}
	reserved := true
	defer func() {
		if reserved {
			m.cancelReservation()
		}
	}()
	id, err := newCommandSessionID()
	if err != nil {
		return commandSessionResult{}, err
	}
	stdout, err := newCommandLog("stdout")
	if err != nil {
		return commandSessionResult{}, err
	}
	stderr, err := newCommandLog("stderr")
	if err != nil {
		stdout.closeAndRemove()
		return commandSessionResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := m.executor.Command(ctx, args.Command, boundary)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		stdout.closeAndRemove()
		stderr.closeAndRemove()
		return commandSessionResult{}, err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	started := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		cancel()
		stdin.Close()
		stdout.closeAndRemove()
		stderr.closeAndRemove()
		return commandSessionResult{}, err
	}
	session := &commandSession{
		id: id, command: args.Command, cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr,
		cancel: cancel, done: make(chan struct{}), status: commandRunning, started: started,
	}
	m.commitReservation(session)
	reserved = false
	go session.wait(ctx)
	return session.result(0, 0)
}

func (m *commandManager) reserve() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("后台命令管理器已关闭")
	}
	retired := make([]*commandSession, 0)
	if len(m.sessions)+m.starting >= maxCommandSessions {
		for id, session := range m.sessions {
			session.mu.Lock()
			finished := session.status != commandRunning
			session.mu.Unlock()
			if finished {
				delete(m.sessions, id)
				retired = append(retired, session)
			}
		}
	}
	if len(m.sessions)+m.starting >= maxCommandSessions {
		m.mu.Unlock()
		closeCommandSessions(retired)
		return fmt.Errorf("后台命令会话已达到上限 %d", maxCommandSessions)
	}
	m.starting++
	m.mu.Unlock()
	closeCommandSessions(retired)
	return nil
}

func (m *commandManager) cancelReservation() {
	m.mu.Lock()
	m.starting--
	m.mu.Unlock()
}

func (m *commandManager) commitReservation(session *commandSession) {
	m.mu.Lock()
	m.starting--
	m.sessions[session.id] = session
	m.mu.Unlock()
}

func (s *commandSession) wait(ctx context.Context) {
	runErr := s.cmd.Wait()
	finished := time.Now().UTC()
	s.mu.Lock()
	s.finished = finished
	s.err = ""
	s.status = commandSucceeded
	exitCode := 0
	if s.cmd.ProcessState != nil {
		exitCode = s.cmd.ProcessState.ExitCode()
	}
	s.exitCode = &exitCode
	if s.stopping {
		s.status = commandTerminated
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		s.status = commandTimedOut
		s.err = context.DeadlineExceeded.Error()
	} else if runErr != nil {
		s.status = commandFailed
		s.err = runErr.Error()
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			s.signal = status.Signal().String()
		}
	}
	s.mu.Unlock()
	s.cancel()
	_ = s.stdin.Close()
	close(s.done)
}

func (m *commandManager) get(id string) (*commandSession, error) {
	m.mu.RLock()
	session := m.sessions[strings.TrimSpace(id)]
	m.mu.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("命令会话 %q 不存在", id)
	}
	return session, nil
}

func (m *commandManager) poll(ctx context.Context, args pollCommandArgs) (commandSessionResult, error) {
	if args.StdoutOffset < 0 || args.StderrOffset < 0 || args.WaitMillis < 0 || args.WaitMillis > 30000 {
		return commandSessionResult{}, errors.New("offset 或 wait_ms 超出允许范围")
	}
	session, err := m.get(args.SessionID)
	if err != nil {
		return commandSessionResult{}, err
	}
	if args.WaitMillis > 0 {
		timer := time.NewTimer(time.Duration(args.WaitMillis) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-session.done:
		case <-timer.C:
		case <-ctx.Done():
			result, resultErr := session.result(args.StdoutOffset, args.StderrOffset)
			if resultErr != nil {
				return result, resultErr
			}
			return result, ctx.Err()
		}
	}
	result, err := session.result(args.StdoutOffset, args.StderrOffset)
	if err != nil {
		return result, err
	}
	switch result.Status {
	case commandFailed, commandTimedOut, commandTerminated:
		return result, fmt.Errorf("命令会话状态: %s", result.Status)
	default:
		return result, nil
	}
}

func (s *commandSession) result(stdoutOffset, stderrOffset int64) (commandSessionResult, error) {
	s.mu.Lock()
	result := commandSessionResult{
		SessionID: s.id, Status: s.status, Command: s.command, StartedAt: s.started,
		DurationMillis: time.Since(s.started).Milliseconds(), ExitCode: cloneInt(s.exitCode), Signal: s.signal, Error: s.err,
		StdoutOffset: stdoutOffset, StderrOffset: stderrOffset,
	}
	if s.cmd.Process != nil {
		result.PID = s.cmd.Process.Pid
	}
	if !s.finished.IsZero() {
		finished := s.finished
		result.FinishedAt = &finished
		result.DurationMillis = s.finished.Sub(s.started).Milliseconds()
	}
	s.mu.Unlock()

	var err error
	result.Stdout, result.NextStdout, result.StdoutMore, result.StdoutTruncated, err = s.stdout.read(stdoutOffset)
	if err != nil {
		return result, err
	}
	result.Stderr, result.NextStderr, result.StderrMore, result.StderrTruncated, err = s.stderr.read(stderrOffset)
	return result, err
}

func (m *commandManager) writeStdin(ctx context.Context, id, input string, appendNewline bool) (map[string]any, error) {
	session, err := m.get(id)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	if session.status != commandRunning {
		status := session.status
		session.mu.Unlock()
		return nil, fmt.Errorf("命令会话已是 %s,不能写入 stdin", status)
	}
	stdin := session.stdin
	session.mu.Unlock()
	if appendNewline {
		input += "\n"
	}
	session.stdinMu.Lock()
	defer session.stdinMu.Unlock()
	written, err := writeCommandInput(ctx, stdin, input)
	return map[string]any{"session_id": id, "bytes_written": written}, err
}

func writeCommandInput(ctx context.Context, stdin io.WriteCloser, input string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if writer, ok := stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		deadline, hasDeadline := ctx.Deadline()
		if hasDeadline {
			if err := writer.SetWriteDeadline(deadline); err != nil {
				return 0, err
			}
		}
		deadlineSet := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			_ = writer.SetWriteDeadline(time.Now())
			close(deadlineSet)
		})
		written, err := io.WriteString(stdin, input)
		if !stop() {
			<-deadlineSet
		}
		_ = writer.SetWriteDeadline(time.Time{})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return written, ctxErr
		}
		if hasDeadline && errors.Is(err, os.ErrDeadlineExceeded) && !time.Now().Before(deadline) {
			return written, context.DeadlineExceeded
		}
		return written, err
	}

	done := make(chan struct {
		written int
		err     error
	}, 1)
	go func() {
		written, err := io.WriteString(stdin, input)
		done <- struct {
			written int
			err     error
		}{written: written, err: err}
	}()
	select {
	case result := <-done:
		return result.written, result.err
	case <-ctx.Done():
		_ = stdin.Close()
		result := <-done
		return result.written, ctx.Err()
	}
}

func (m *commandManager) terminate(id string) (commandSessionResult, error) {
	session, err := m.get(id)
	if err != nil {
		return commandSessionResult{}, err
	}
	session.mu.Lock()
	if session.status == commandRunning {
		session.stopping = true
		session.cancel()
	}
	session.mu.Unlock()
	select {
	case <-session.done:
	case <-time.After(3 * time.Second):
		return session.result(0, 0)
	}
	return session.result(0, 0)
}

func (m *commandManager) close() {
	m.mu.Lock()
	m.closed = true
	sessions := make([]*commandSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*commandSession)
	m.mu.Unlock()
	for _, session := range sessions {
		session.mu.Lock()
		if session.status == commandRunning {
			session.stopping = true
			session.cancel()
		}
		session.mu.Unlock()
		select {
		case <-session.done:
		case <-time.After(3 * time.Second):
		}
	}
	closeCommandSessions(sessions)
}

func closeCommandSessions(sessions []*commandSession) {
	for _, session := range sessions {
		session.stdout.closeAndRemove()
		session.stderr.closeAndRemove()
	}
}

func (m *commandManager) tools() (Tool, Tool, Tool, Tool) {
	start := Tool{
		Name: "start_command", Description: "启动最长 1 小时的后台命令,返回 session_id;使用 poll_command 增量读取输出。",
		Capabilities: CapabilityRead | CapabilityWrite | CapabilityExecute, Effect: EffectOpaque,
		Parameters: map[string]any{
			"type": "object", "properties": map[string]any{
				"command":         map[string]any{"type": "string"},
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": int(maxCommandTimeout / time.Second)},
			}, "required": []string{"command"}, "additionalProperties": false,
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args startCommandArgs
			if err := ParseArguments(raw, &args); err != nil {
				return "", fmt.Errorf("参数解析失败: %w", err)
			}
			result, runErr := m.start(executionBoundary(ctx), args)
			return marshalCommandSessionResult(result, runErr)
		},
	}
	poll := Tool{
		Name: "poll_command", Description: "按 offset 增量读取后台命令输出并返回进程状态;wait_ms 最多等待 30 秒。",
		Capabilities: CapabilityRead, Effect: EffectPure, AutoApprove: func(json.RawMessage) bool { return true },
		Parameters: map[string]any{
			"type": "object", "properties": map[string]any{
				"session_id":    map[string]any{"type": "string"},
				"stdout_offset": map[string]any{"type": "integer", "minimum": 0},
				"stderr_offset": map[string]any{"type": "integer", "minimum": 0},
				"wait_ms":       map[string]any{"type": "integer", "minimum": 0, "maximum": 30000},
			}, "required": []string{"session_id"}, "additionalProperties": false,
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args pollCommandArgs
			if err := ParseArguments(raw, &args); err != nil {
				return "", fmt.Errorf("参数解析失败: %w", err)
			}
			result, runErr := m.poll(ctx, args)
			return marshalCommandSessionResult(result, runErr)
		},
	}
	write := Tool{
		Name: "write_stdin", Description: "向已批准的后台命令写入 stdin;默认追加换行。",
		Capabilities: CapabilityExecute, Effect: EffectOpaque, AutoApprove: func(json.RawMessage) bool { return true },
		Parameters: map[string]any{
			"type": "object", "properties": map[string]any{
				"session_id":     map[string]any{"type": "string"},
				"input":          map[string]any{"type": "string"},
				"append_newline": map[string]any{"type": "boolean"},
			}, "required": []string{"session_id", "input"}, "additionalProperties": false,
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				SessionID     string `json:"session_id"`
				Input         string `json:"input"`
				AppendNewline *bool  `json:"append_newline,omitempty"`
			}
			if err := ParseArguments(raw, &args); err != nil {
				return "", fmt.Errorf("参数解析失败: %w", err)
			}
			appendNewline := args.AppendNewline == nil || *args.AppendNewline
			result, runErr := m.writeStdin(ctx, args.SessionID, args.Input, appendNewline)
			encoded, err := json.Marshal(result)
			if err != nil {
				return "", err
			}
			return string(encoded), runErr
		},
	}
	terminate := Tool{
		Name: "terminate_command", Description: "终止后台命令会话并返回最终状态。",
		Capabilities: CapabilityExecute, Effect: EffectIdempotent, AutoApprove: func(json.RawMessage) bool { return true },
		Parameters: map[string]any{
			"type": "object", "properties": map[string]any{"session_id": map[string]any{"type": "string"}},
			"required": []string{"session_id"}, "additionalProperties": false,
		},
		Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				SessionID string `json:"session_id"`
			}
			if err := ParseArguments(raw, &args); err != nil {
				return "", fmt.Errorf("参数解析失败: %w", err)
			}
			result, runErr := m.terminate(args.SessionID)
			return marshalCommandSessionResult(result, runErr)
		},
	}
	return start, poll, write, terminate
}

func marshalCommandSessionResult(result commandSessionResult, runErr error) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(encoded), runErr
}

func newCommandSessionID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("生成命令会话 ID: %w", err)
	}
	return "cmd-" + hex.EncodeToString(random), nil
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
