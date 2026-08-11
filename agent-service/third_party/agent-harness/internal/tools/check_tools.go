package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	RunCheckToolName                = "run_check"
	StrongCheckSandboxProfile       = "bwrap-readonly-offline-rlimit-v3"
	maxNamedChecks                  = 32
	strongCheckAddressSpaceKiB      = 2 * 1024 * 1024
	strongCheckProcessLimit         = 512
	strongCheckOpenFileLimit        = 1024
	strongCheckFileSizeBlocks       = 1024 * 1024
	strongCheckCPULimitGraceSeconds = 5
)

var (
	validCheckName        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	ErrCheckConfigChanged = errors.New("named check configuration changed")
)

type NamedCheck struct {
	Name           string
	Description    string
	Command        string
	TimeoutSeconds int
	Required       bool
}

type checkDefinition struct {
	NamedCheck
	digest string
}

type CheckSnapshot struct {
	Name         string `json:"name"`
	ConfigSHA256 string `json:"config_sha256"`
}

type NamedCheckSet struct {
	checks   map[string]checkDefinition
	ordered  []checkDefinition
	executor bashExecutor
	tool     Tool
}

type namedCheckResult struct {
	Name   string        `json:"name"`
	Result commandResult `json:"result"`
}

type NamedCheckResult struct {
	Name            string
	ConfigSHA256    string
	ExitCode        int
	Signal          string
	TimedOut        bool
	DurationMillis  int64
	Stdout          string
	Stderr          string
	StdoutSHA256    string
	StderrSHA256    string
	StdoutTruncated bool
	StderrTruncated bool
}

// NewNamedCheckSet validates operator-owned commands and requires a working
// bwrap boundary. Checks never fall back to host execution.
func NewNamedCheckSet(workingDir string, specs []NamedCheck) (*NamedCheckSet, SandboxInfo, error) {
	if len(specs) == 0 {
		return nil, SandboxInfo{}, nil
	}
	if len(specs) > maxNamedChecks {
		return nil, SandboxInfo{}, fmt.Errorf("named checks 最多配置 %d 个", maxNamedChecks)
	}
	workspace, err := canonicalWorkingDir(workingDir)
	if err != nil {
		return nil, SandboxInfo{}, err
	}
	maskWSLInterop := shouldMaskWSLInterop()
	bwrapPath, err := probeBwrap(workspace, maskWSLInterop)
	if err != nil {
		return nil, SandboxInfo{}, fmt.Errorf("named checks 需要强制 bwrap 沙箱: %w", err)
	}
	strongArgs, strongHome, err := strongCheckSandboxLayout(workspace, maskWSLInterop)
	if err != nil {
		return nil, SandboxInfo{}, fmt.Errorf("构造 named check 强沙箱: %w", err)
	}
	executor := bwrapBashExecutor{
		path: bwrapPath, workingDir: workspace, maskWSLInterop: maskWSLInterop,
		strongArgs: strongArgs, strongHome: strongHome,
	}
	if err := probeStrongCheckSandbox(executor); err != nil {
		return nil, SandboxInfo{}, fmt.Errorf("named checks 需要强制 bwrap 沙箱: %w", err)
	}
	set := &NamedCheckSet{
		checks:   make(map[string]checkDefinition, len(specs)),
		executor: executor,
	}
	for _, spec := range specs {
		spec.Name = strings.TrimSpace(spec.Name)
		spec.Description = strings.TrimSpace(spec.Description)
		spec.Command = strings.TrimSpace(spec.Command)
		if !validCheckName.MatchString(spec.Name) {
			return nil, SandboxInfo{}, fmt.Errorf("named check 名称 %q 无效", spec.Name)
		}
		if _, duplicate := set.checks[spec.Name]; duplicate {
			return nil, SandboxInfo{}, fmt.Errorf("named check 重名: %s", spec.Name)
		}
		if spec.Command == "" || strings.ContainsRune(spec.Command, 0) || len(spec.Command) > 32<<10 {
			return nil, SandboxInfo{}, fmt.Errorf("named check %s 缺少有效 command", spec.Name)
		}
		if spec.TimeoutSeconds == 0 {
			spec.TimeoutSeconds = 600
		}
		if spec.TimeoutSeconds < 1 || spec.TimeoutSeconds > 3600 {
			return nil, SandboxInfo{}, fmt.Errorf("named check %s timeout_seconds 必须在 1..3600", spec.Name)
		}
		if spec.Description == "" {
			spec.Description = "Run the trusted " + spec.Name + " validation."
		}
		digest, err := checkDigest(spec)
		if err != nil {
			return nil, SandboxInfo{}, err
		}
		definition := checkDefinition{NamedCheck: spec, digest: digest}
		set.checks[spec.Name] = definition
		set.ordered = append(set.ordered, definition)
	}
	sort.Slice(set.ordered, func(i, j int) bool { return set.ordered[i].Name < set.ordered[j].Name })
	set.tool = set.buildTool()
	return set, SandboxInfo{Enabled: true, Backend: "bwrap"}, nil
}

func (s *NamedCheckSet) Tool() Tool {
	if s == nil {
		return Tool{}
	}
	return s.tool
}

func (s *NamedCheckSet) RequiredNames() []string {
	if s == nil {
		return nil
	}
	var names []string
	for _, check := range s.ordered {
		if check.Required {
			names = append(names, check.Name)
		}
	}
	return names
}

// ContractSHA256 binds recovery to operator-owned execution configuration
// without persisting command text in the agent journal.
func (s *NamedCheckSet) ContractSHA256() (string, error) {
	if s == nil {
		return "", nil
	}
	type checkContract struct {
		Name         string `json:"name"`
		ConfigSHA256 string `json:"config_sha256"`
		Required     bool   `json:"required"`
	}
	checks := make([]checkContract, 0, len(s.ordered))
	for _, check := range s.ordered {
		checks = append(checks, checkContract{
			Name: check.Name, ConfigSHA256: check.digest, Required: check.Required,
		})
	}
	encoded, err := json.Marshal(struct {
		Version int             `json:"version"`
		Checks  []checkContract `json:"checks"`
	}{Version: 1, Checks: checks})
	if err != nil {
		return "", fmt.Errorf("编码 named check 恢复契约: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Run executes one operator-owned check in the mandatory strong sandbox. The
// returned hashes cover the complete streams even when their previews truncate.
func (s *NamedCheckSet) Run(ctx context.Context, name string) (NamedCheckResult, error) {
	if s == nil {
		return NamedCheckResult{}, errors.New("named checks 未配置")
	}
	definition, ok := s.checks[strings.TrimSpace(name)]
	if !ok {
		return NamedCheckResult{}, fmt.Errorf("未知 named check %q", name)
	}
	result, runErr := executeStructuredCommand(
		WithExecutionBoundary(ctx, ExecutionSandboxReadOnly), s.executor,
		resourceLimitedCheckCommand(definition.Command, definition.TimeoutSeconds),
		time.Duration(definition.TimeoutSeconds)*time.Second,
	)
	return NamedCheckResult{
		Name: definition.Name, ConfigSHA256: definition.digest, ExitCode: result.ExitCode, Signal: result.Signal,
		TimedOut: result.TimedOut, DurationMillis: result.DurationMillis,
		Stdout: result.Stdout, Stderr: result.Stderr,
		StdoutSHA256: result.StdoutSHA256, StderrSHA256: result.StderrSHA256,
		StdoutTruncated: result.StdoutTruncated, StderrTruncated: result.StderrTruncated,
	}, runErr
}

func (s *NamedCheckSet) Snapshot(raw json.RawMessage) (json.RawMessage, error) {
	snapshot, err := s.DecodeSnapshot(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(snapshot)
}

func (s *NamedCheckSet) DecodeSnapshot(raw json.RawMessage) (CheckSnapshot, error) {
	if s == nil {
		return CheckSnapshot{}, errors.New("named checks 未配置")
	}
	var snapshot CheckSnapshot
	if err := ParseArguments(raw, &snapshot); err != nil {
		return snapshot, fmt.Errorf("解析 run_check 参数: %w", err)
	}
	snapshot.Name = strings.TrimSpace(snapshot.Name)
	definition, ok := s.checks[snapshot.Name]
	if !ok {
		return snapshot, fmt.Errorf("未知 named check %q", snapshot.Name)
	}
	if snapshot.ConfigSHA256 != "" && snapshot.ConfigSHA256 != definition.digest {
		return snapshot, fmt.Errorf("%w: %s", ErrCheckConfigChanged, snapshot.Name)
	}
	snapshot.ConfigSHA256 = definition.digest
	return snapshot, nil
}

func (s *NamedCheckSet) buildTool() Tool {
	names := make([]string, len(s.ordered))
	details := make([]string, len(s.ordered))
	for index, check := range s.ordered {
		names[index] = check.Name
		required := ""
		if check.Required {
			required = " [required gate]"
		}
		details[index] = check.Name + required + ": " + check.Description
	}
	tool := Tool{
		Name:         RunCheckToolName,
		Description:  "运行操作者预先声明的隔离检查。模型只能选择名称，不能提供命令。可用检查: " + strings.Join(details, "; "),
		Capabilities: CapabilityRead | CapabilityExecute,
		Effect:       EffectPure,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "enum": names, "description": "检查名称"},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
	}
	tool.AutoApprove = func(raw json.RawMessage) bool {
		_, err := s.DecodeSnapshot(raw)
		return err == nil
	}
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (string, error) {
		snapshot, err := s.DecodeSnapshot(raw)
		if err != nil {
			return "", err
		}
		definition := s.checks[snapshot.Name]
		check, runErr := s.Run(ctx, snapshot.Name)
		result := commandResult{
			Command: definition.Command, ExitCode: check.ExitCode, Signal: check.Signal, TimedOut: check.TimedOut,
			DurationMillis: check.DurationMillis, Stdout: check.Stdout, Stderr: check.Stderr,
			StdoutSHA256: check.StdoutSHA256, StderrSHA256: check.StderrSHA256,
			StdoutTruncated: check.StdoutTruncated, StderrTruncated: check.StderrTruncated,
		}
		encoded, err := json.Marshal(namedCheckResult{Name: check.Name, Result: result})
		if err != nil {
			return "", err
		}
		return string(encoded), runErr
	}
	return tool
}

func checkDigest(check NamedCheck) (string, error) {
	encoded, err := json.Marshal(struct {
		Name           string `json:"name"`
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
		SandboxProfile string `json:"sandbox_profile"`
	}{
		Name: check.Name, Command: check.Command, TimeoutSeconds: check.TimeoutSeconds,
		SandboxProfile: StrongCheckSandboxProfile,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func resourceLimitedCheckCommand(command string, timeoutSeconds int) string {
	cpuSeconds := timeoutSeconds + strongCheckCPULimitGraceSeconds
	return fmt.Sprintf(
		"ulimit -t %d || exit $?\nulimit -v %d || exit $?\nulimit -u %d || exit $?\nulimit -n %d || exit $?\nulimit -f %d || exit $?\n%s",
		cpuSeconds, strongCheckAddressSpaceKiB, strongCheckProcessLimit,
		strongCheckOpenFileLimit, strongCheckFileSizeBlocks, command,
	)
}

func strongCheckSandboxLayout(workspace string, maskWSLHostMounts bool) ([]string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("读取用户 HOME: %w", err)
	}
	home, err = canonicalWorkingDir(home)
	if err != nil {
		return nil, "", fmt.Errorf("读取用户 HOME: %w", err)
	}
	if workspace == home {
		return nil, "", errors.New("工作区不能是整个用户 HOME")
	}
	if maskWSLHostMounts && filepath.Clean(workspace) == "/mnt" {
		return nil, "", errors.New("工作区不能是整个 WSL /mnt 挂载根")
	}

	var args []string
	createdDirs := make(map[string]bool)
	ensureDirectories := func(base, path string) {
		for _, directory := range sandboxDirectoryArgs(base, path) {
			if createdDirs[directory] {
				continue
			}
			args = append(args, "--dir", directory)
			createdDirs[directory] = true
		}
	}
	maskedWSLRoot := false
	const wslMountRoot = "/mnt"
	if maskWSLHostMounts {
		if info, statErr := os.Stat(wslMountRoot); statErr == nil && info.IsDir() {
			args = append(args, "--tmpfs", wslMountRoot)
			createdDirs[wslMountRoot] = true
			maskedWSLRoot = true
		}
	}
	if maskedWSLRoot && withinPath(wslMountRoot, home) {
		ensureDirectories(wslMountRoot, home)
	}
	args = append(args, "--tmpfs", home)
	createdDirs[home] = true
	appendReadOnlyBind := func(base, path string) {
		ensureDirectories(base, path)
		args = append(args, "--ro-bind", path, path)
	}

	if withinPath(home, workspace) {
		appendReadOnlyBind(home, workspace)
	} else if maskedWSLRoot && withinPath(wslMountRoot, workspace) {
		appendReadOnlyBind(wslMountRoot, workspace)
	}
	for _, cache := range strongCheckReadOnlyCaches(home) {
		if withinPath(workspace, cache) || withinPath(cache, workspace) {
			continue
		}
		appendReadOnlyBind(home, cache)
	}
	masks, err := sensitiveWorkspaceMasks(workspace)
	if err != nil {
		return nil, "", err
	}
	args = append(args, masks...)
	args = append(args, "--unshare-net")
	return args, "/tmp/harness-home", nil
}

func strongCheckReadOnlyCaches(home string) []string {
	goPath := strings.TrimSpace(os.Getenv("GOPATH"))
	if goPath == "" {
		goPath = filepath.Join(home, "go")
	}
	var caches []string
	for _, root := range filepath.SplitList(goPath) {
		for _, suffix := range []string{filepath.Join("pkg", "mod"), "bin"} {
			candidate := filepath.Clean(filepath.Join(root, suffix))
			info, err := os.Lstat(candidate)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !withinPath(home, candidate) {
				continue
			}
			caches = append(caches, candidate)
		}
	}
	sort.Strings(caches)
	return caches
}

func sandboxDirectoryArgs(base, target string) []string {
	relative, err := filepath.Rel(base, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	parts := strings.Split(relative, string(filepath.Separator))
	directories := make([]string, 0, len(parts))
	current := base
	for _, part := range parts {
		current = filepath.Join(current, part)
		directories = append(directories, current)
	}
	return directories
}

func sensitiveWorkspaceMasks(workspace string) ([]string, error) {
	const maxMasks = 256
	var args []string
	masks := 0
	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if pathContainsPart(relative, ".git") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !sensitivePath(relative) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("敏感路径不能是符号链接: %s", relative)
		}
		masks++
		if masks > maxMasks {
			return fmt.Errorf("工作区敏感路径超过 %d 个", maxMasks)
		}
		if entry.IsDir() {
			args = append(args, "--tmpfs", path)
			return filepath.SkipDir
		}
		args = append(args, "--ro-bind", "/dev/null", path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描工作区敏感路径: %w", err)
	}
	return args, nil
}

func withinPath(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func probeStrongCheckSandbox(executor bwrapBashExecutor) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := executor.Command(ctx, `test "$HOME" = /tmp/harness-home && test ! -e "$HOME/.ssh"`, ExecutionSandboxReadOnly)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("强沙箱探测失败: %w", err)
	}
	return nil
}
