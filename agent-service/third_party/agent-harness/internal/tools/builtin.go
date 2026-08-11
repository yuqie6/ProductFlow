package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// BuiltinToolSet 持有内置工具及其后台命令资源。
type BuiltinToolSet struct {
	Tools            []Tool
	commands         *commandManager
	close            sync.Once
	execTimeoutNanos atomic.Int64
}

// Close 终止仍在运行的后台命令并关闭输出日志。
func (set *BuiltinToolSet) Close() {
	if set == nil {
		return
	}
	set.close.Do(set.commands.close)
}

// SetExecTimeout updates the timeout used by exec_command calls started after
// this method returns. Running commands keep their existing context deadline.
func (set *BuiltinToolSet) SetExecTimeout(timeout time.Duration) error {
	if set == nil || timeout <= 0 || timeout > time.Hour {
		return fmt.Errorf("exec_command timeout 必须在 1ns..1h")
	}
	set.execTimeoutNanos.Store(int64(timeout))
	return nil
}

func (set *BuiltinToolSet) execTimeout() time.Duration {
	return time.Duration(set.execTimeoutNanos.Load())
}

// BuiltinTools 返回文件与命令执行原语;调用方必须关闭返回值。
// workingDir 用于判断只读调用能否在不询问用户的情况下运行。
func BuiltinTools(workingDir string) *BuiltinToolSet {
	return builtinTools(workingDir, hostBashExecutor{workingDir: workingDir}, true, defaultExecCommandTimeout)
}

func builtinTools(workingDir string, executor bashExecutor, autoApproveCommands bool, execTimeout time.Duration) *BuiltinToolSet {
	set := &BuiltinToolSet{}
	set.execTimeoutNanos.Store(int64(execTimeout))
	read := readTool()
	read.AutoApprove = func(args json.RawMessage) bool {
		var a struct {
			Path string `json:"path"`
		}
		return ParseArguments(args, &a) == nil && safeWorkspacePath(workingDir, a.Path)
	}
	edit := editTool(workingDir)
	edit.AutoApproveEdit = func(args json.RawMessage) bool {
		var a struct {
			Path string `json:"path"`
		}
		return ParseArguments(args, &a) == nil && safeWorkspaceWritePath(workingDir, a.Path)
	}
	execCommand := execCommandTool(executor, set.execTimeout)
	commands := newCommandManager(executor)
	set.commands = commands
	startCommand, pollCommand, writeStdin, terminateCommand := commands.tools()
	if autoApproveCommands {
		autoApprove := func(args json.RawMessage) bool {
			var a struct {
				Command string `json:"command"`
			}
			return ParseArguments(args, &a) == nil && safeReadCommand(workingDir, a.Command)
		}
		execCommand.AutoApprove = autoApprove
		startCommand.AutoApprove = autoApprove
	}
	result := []Tool{
		read,
	}
	result = append(result, structuredSearchTools(workingDir)...)
	result = append(result,
		edit,
		execCommand,
		startCommand,
		pollCommand,
		writeStdin,
		terminateCommand,
	)
	set.Tools = result
	return set
}

func safeReadCommand(workingDir, command string) bool {
	fields, ok := simpleCommandFields(command)
	if !ok {
		return false
	}

	switch fields[0] {
	case "pwd":
		if len(fields) != 1 {
			return false
		}
	case "ls", "rg", "grep", "head", "tail", "wc", "stat":
		if hasUnsafeReadFlag(fields[0], fields[1:]) {
			return false
		}
	case "git":
		if len(fields) < 2 || !safeGitRead(fields[1], fields[2:]) {
			return false
		}
	default:
		return false
	}

	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") {
			continue
		}
		if sensitivePath(field) || sensitiveGitObjectPath(fields[0], field) || pathEscapesWorkspace(workingDir, field) {
			return false
		}
	}
	return true
}

func sensitiveGitObjectPath(command, value string) bool {
	if command != "git" {
		return false
	}
	_, path, found := strings.Cut(value, ":")
	return found && sensitivePath(path)
}

func simpleCommandFields(command string) ([]string, bool) {
	for _, r := range command {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_./:=,@%+- \t", r) {
			continue
		}
		return nil, false
	}
	fields := strings.Fields(command)
	return fields, len(fields) > 0
}

func hasUnsafeReadFlag(command string, args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if command == "rg" && (strings.HasPrefix(lower, "--pre") ||
			strings.HasPrefix(lower, "--hidden") || strings.HasPrefix(lower, "--no-ignore") ||
			lower == "-u" || lower == "-uu" || lower == "-uuu") {
			return true
		}
	}
	return false
}

func safeGitRead(subcommand string, args []string) bool {
	switch subcommand {
	case "status", "diff", "log", "show", "rev-parse", "ls-files", "grep":
	case "branch":
		return len(args) == 1 && args[0] == "--show-current"
	default:
		return false
	}
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if strings.HasPrefix(lower, "--output") || lower == "--ext-diff" ||
			lower == "--textconv" || strings.HasPrefix(lower, "--open-files-in-pager") {
			return false
		}
	}
	return true
}

func safeWorkspacePath(workingDir, path string) bool {
	if path == "" || sensitivePath(path) {
		return false
	}
	root, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		return false
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func safeWorkspaceWritePath(workingDir, path string) bool {
	if path == "" || sensitivePath(path) || pathContainsPart(path, ".git") {
		return false
	}
	root, err := filepath.Abs(workingDir)
	if err != nil {
		return false
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	if !safeWorkspaceWriteTarget(root, candidate) {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		return safeWorkspaceWriteTarget(root, resolved)
	} else if !os.IsNotExist(err) {
		return false
	}

	for parent := filepath.Dir(candidate); ; parent = filepath.Dir(parent) {
		if _, err := os.Lstat(parent); err == nil {
			resolved, err := filepath.EvalSymlinks(parent)
			return err == nil && safeWorkspaceWriteTarget(root, resolved)
		} else if !os.IsNotExist(err) {
			return false
		}
		if next := filepath.Dir(parent); next == parent {
			return false
		}
	}
}

func safeWorkspaceWriteTarget(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return !sensitivePath(relative) && !pathContainsPart(relative, ".git")
}

func pathContainsPart(path, want string) bool {
	for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(path)), "/") {
		if part == want {
			return true
		}
	}
	return false
}

func pathEscapesWorkspace(workingDir, value string) bool {
	if filepath.IsAbs(value) {
		return !safeWorkspacePath(workingDir, value)
	}
	if value == ".." || strings.HasPrefix(value, ".."+string(filepath.Separator)) {
		return true
	}
	path := filepath.Join(workingDir, value)
	if _, err := os.Lstat(path); err != nil {
		return false
	}
	return !safeWorkspacePath(workingDir, value)
}

func sensitivePath(path string) bool {
	for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(path)), "/") {
		if part == "auth.json" || part == ".env" ||
			(strings.HasPrefix(part, ".env.") && part != ".env.example") {
			return true
		}
	}
	return false
}
