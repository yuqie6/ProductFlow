package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type ExecutionBoundary uint8

const (
	ExecutionHost ExecutionBoundary = iota
	ExecutionSandboxReadOnly
	ExecutionSandboxWorkspaceWrite
)

type executionBoundaryKey struct{}

// WithExecutionBoundary 为一次工具调用标记 subprocess 权限边界。
func WithExecutionBoundary(ctx context.Context, boundary ExecutionBoundary) context.Context {
	return context.WithValue(ctx, executionBoundaryKey{}, boundary)
}

func executionBoundary(ctx context.Context) ExecutionBoundary {
	boundary, _ := ctx.Value(executionBoundaryKey{}).(ExecutionBoundary)
	return boundary
}

type BuiltinOptions struct {
	// Sandbox 接受 auto、required、off。auto 不可用时显式退回 HOST 并关闭命令免审。
	Sandbox string
	// ExecTimeout controls synchronous exec_command. Zero uses the library
	// default; the CLI always supplies its explicit TOML value.
	ExecTimeout time.Duration
}

type SandboxInfo struct {
	Enabled bool
	Backend string
	Reason  string
}

type bashExecutor interface {
	Command(ctx context.Context, command string, boundary ExecutionBoundary) *exec.Cmd
}

type hostBashExecutor struct {
	workingDir string
}

func (e hostBashExecutor) Command(ctx context.Context, command string, _ ExecutionBoundary) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", command)
	cmd.Dir = e.workingDir
	cmd.Env = SafeCommandEnvironment(false)
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
	return cmd
}

type bwrapBashExecutor struct {
	path           string
	workingDir     string
	maskWSLInterop bool
	strongArgs     []string
	strongHome     string
}

func (e bwrapBashExecutor) Command(ctx context.Context, command string, boundary ExecutionBoundary) *exec.Cmd {
	if boundary == ExecutionHost {
		return hostBashExecutor{workingDir: e.workingDir}.Command(ctx, command, boundary)
	}
	args := []string{
		"--die-with-parent", "--new-session", "--unshare-pid", "--unshare-uts", "--unshare-ipc", "--unshare-cgroup-try",
		"--ro-bind", "/", "/",
	}
	if e.maskWSLInterop {
		// WSL 通过 /init 解释 Windows PE 文件；不遮蔽它会绕过 Linux 挂载边界。
		args = append(args, "--ro-bind", "/dev/null", "/init")
	}
	if len(e.strongArgs) > 0 {
		args = append(args, e.strongArgs...)
	} else if boundary == ExecutionSandboxReadOnly {
		args = append(args, "--unshare-net")
	} else {
		args = append(args, "--bind", e.workingDir, e.workingDir)
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
	)
	if e.strongHome != "" {
		args = append(args, "--dir", e.strongHome)
	}
	args = append(args, "--chdir", e.workingDir, "--", "bash", "--noprofile", "--norc", "-c", command)
	cmd := exec.CommandContext(ctx, e.path, args...)
	cmd.Env = SafeCommandEnvironment(true)
	if e.strongHome != "" {
		cmd.Env = replaceEnvironment(cmd.Env, "HOME", e.strongHome)
	}
	return cmd
}

func replaceEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

// NewBuiltinTools 构造内置工具并探测 bwrap。探测失败时是否报错由 Sandbox 策略决定。
func NewBuiltinTools(workingDir string, options BuiltinOptions) (*BuiltinToolSet, SandboxInfo, error) {
	canonical, err := canonicalWorkingDir(workingDir)
	if err != nil {
		return nil, SandboxInfo{}, err
	}
	policy := strings.ToLower(strings.TrimSpace(options.Sandbox))
	if policy == "" {
		policy = "auto"
	}
	if policy != "auto" && policy != "required" && policy != "off" {
		return nil, SandboxInfo{}, fmt.Errorf("不支持 workspace.sandbox=%q,可用值: auto, required, off", options.Sandbox)
	}
	host := hostBashExecutor{workingDir: canonical}
	execTimeout := options.ExecTimeout
	if execTimeout <= 0 {
		execTimeout = defaultExecCommandTimeout
	}
	if execTimeout > time.Hour {
		return nil, SandboxInfo{}, errors.New("exec_command timeout 不能超过 1 小时")
	}
	if policy == "off" {
		return builtinTools(canonical, host, false, execTimeout), SandboxInfo{Backend: "host", Reason: "workspace.sandbox=off"}, nil
	}

	maskWSLInterop := shouldMaskWSLInterop()
	bwrapPath, probeErr := probeBwrap(canonical, maskWSLInterop)
	if probeErr != nil {
		if policy == "required" {
			return nil, SandboxInfo{}, probeErr
		}
		return builtinTools(canonical, host, false, execTimeout), SandboxInfo{Backend: "host", Reason: probeErr.Error()}, nil
	}
	executor := bwrapBashExecutor{path: bwrapPath, workingDir: canonical, maskWSLInterop: maskWSLInterop}
	return builtinTools(canonical, executor, true, execTimeout), SandboxInfo{Enabled: true, Backend: "bwrap"}, nil
}

func probeBwrap(workingDir string, maskWSLInterop bool) (string, error) {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return "", errors.New("未找到 bwrap，命令子进程将无法进入 OS 沙箱")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := []string{
		"--die-with-parent", "--new-session", "--unshare-pid", "--unshare-uts", "--unshare-ipc", "--unshare-cgroup-try", "--unshare-net",
		"--ro-bind", "/", "/",
	}
	if maskWSLInterop {
		args = append(args, "--ro-bind", "/dev/null", "/init")
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--chdir", workingDir, "--", "true",
	)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = SafeCommandEnvironment(true)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("bwrap 探测失败: %w", err)
	}
	return path, nil
}

func canonicalWorkingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析工作区: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("读取工作区: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("工作区不是目录: %s", abs)
	}
	return filepath.Clean(abs), nil
}

// SafeCommandEnvironment returns the non-secret environment shared by harness
// subprocesses. Callers must add any tool-specific secret explicitly.
func SafeCommandEnvironment(sandboxed bool) []string {
	allowed := []string{
		"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE",
		"TERM", "COLORTERM", "NO_COLOR", "TZ", "GOPATH", "GOTOOLCHAIN", "GOFLAGS", "CGO_ENABLED",
		"CC", "CXX", "PKG_CONFIG_PATH",
	}
	environment := make([]string, 0, len(allowed)+2)
	for _, name := range allowed {
		if value, ok := os.LookupEnv(name); ok {
			if sandboxed && name == "PATH" {
				value = sandboxPath(value)
			}
			environment = append(environment, name+"="+value)
		}
	}
	environment = append(environment, "TMPDIR=/tmp", "GOCACHE=/tmp/harness-go-build")
	return environment
}

func sandboxPath(value string) string {
	entries := filepath.SplitList(value)
	filtered := entries[:0]
	for _, entry := range entries {
		clean := filepath.Clean(entry)
		if clean == "/mnt" || strings.HasPrefix(clean, "/mnt/") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return strings.Join(filtered, string(os.PathListSeparator))
}

func shouldMaskWSLInterop() bool {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil || !strings.Contains(strings.ToLower(string(data)), "microsoft") {
		return false
	}
	info, err := os.Stat("/init")
	return err == nil && !info.IsDir()
}
