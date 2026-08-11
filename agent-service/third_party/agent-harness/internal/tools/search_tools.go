package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultSearchLimit = 100
	maxSearchLimit     = 500
	maxSearchGlobs     = 20
	maxSearchLineBytes = 1 << 20
	maxMatchTextBytes  = 4 << 10
)

type pageArgs struct {
	Path   string `json:"path,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type listDirArgs struct {
	pageArgs
	Depth int `json:"depth,omitempty"`
}

type listDirEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

type listDirResult struct {
	Path       string         `json:"path"`
	Entries    []listDirEntry `json:"entries"`
	NextOffset int            `json:"next_offset,omitempty"`
}

type findFilesResult struct {
	Path       string   `json:"path"`
	Files      []string `json:"files"`
	NextOffset int      `json:"next_offset,omitempty"`
}

type searchTextResult struct {
	Path       string      `json:"path"`
	Matches    []textMatch `json:"matches"`
	NextOffset int         `json:"next_offset,omitempty"`
}

type findFilesArgs struct {
	pageArgs
	Glob string `json:"glob,omitempty"`
}

type searchTextArgs struct {
	pageArgs
	Pattern       string   `json:"pattern"`
	Globs         []string `json:"globs,omitempty"`
	Literal       bool     `json:"literal,omitempty"`
	CaseSensitive *bool    `json:"case_sensitive,omitempty"`
}

type textMatch struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

func structuredSearchTools(workingDir string) []Tool {
	list := listDirTool(workingDir)
	find := findFilesTool(workingDir)
	search := searchTextTool(workingDir)
	return []Tool{list, find, search}
}

func listDirTool(workingDir string) Tool {
	tool := Tool{
		Name:         "list_dir",
		Description:  "按路径和深度列出工作区目录，返回可分页的结构化条目；不会遍历符号链接、.git 或敏感文件。",
		Capabilities: CapabilityRead,
		Effect:       EffectPure,
		ParallelSafe: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string", "description": "工作区内目录，默认 ."},
				"depth":  map[string]any{"type": "integer", "minimum": 1, "maximum": 8, "description": "递归深度，默认 1"},
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "分页偏移"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxSearchLimit, "description": "返回条目数，默认 100"},
			},
			"additionalProperties": false,
		},
	}
	tool.AutoApprove = structuredPathAutoApprove(workingDir)
	tool.Handler = func(_ context.Context, raw json.RawMessage) (string, error) {
		var args listDirArgs
		if err := ParseArguments(raw, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
		path, display, err := resolveStructuredPath(workingDir, args.Path, true)
		if err != nil {
			return "", err
		}
		limit, err := normalizePage(&args.pageArgs)
		if err != nil {
			return "", err
		}
		depth := args.Depth
		if depth == 0 {
			depth = 1
		}
		if depth < 1 || depth > 8 {
			return "", fmt.Errorf("depth 必须在 1..8 之间")
		}
		items, more, err := collectDirectoryEntries(path, display, depth, args.Offset, limit)
		if err != nil {
			return "", err
		}
		result := listDirResult{Path: display, Entries: items}
		if more {
			result.NextOffset = args.Offset + len(items)
		}
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
	return tool
}

func findFilesTool(workingDir string) Tool {
	tool := Tool{
		Name:         "find_files",
		Description:  "使用 ripgrep 文件索引在工作区查找路径，支持 glob 和分页，返回结构化路径列表。",
		Capabilities: CapabilityRead,
		Effect:       EffectPure,
		ParallelSafe: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string", "description": "工作区内目录，默认 ."},
				"glob":   map[string]any{"type": "string", "description": "ripgrep glob，默认 *"},
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "分页偏移"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxSearchLimit, "description": "返回路径数，默认 100"},
			},
			"additionalProperties": false,
		},
	}
	tool.AutoApprove = structuredPathAutoApprove(workingDir)
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args findFilesArgs
		if err := ParseArguments(raw, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
		_, display, err := resolveStructuredPath(workingDir, args.Path, true)
		if err != nil {
			return "", err
		}
		limit, err := normalizePage(&args.pageArgs)
		if err != nil {
			return "", err
		}
		glob := strings.TrimSpace(args.Glob)
		if glob == "" {
			glob = "*"
		}
		if len(glob) > 4096 {
			return "", errors.New("glob 过长")
		}
		root, err := canonicalWorkspace(workingDir)
		if err != nil {
			return "", err
		}
		command := exec.CommandContext(ctx, "rg", "--files", "--color", "never", "--glob", glob, "--", display)
		command.Dir = root
		items, more, err := collectRGPaths(command, root, args.Offset, limit)
		if err != nil {
			return "", err
		}
		result := findFilesResult{Path: display, Files: items}
		if more {
			result.NextOffset = args.Offset + len(items)
		}
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
	return tool
}

func searchTextTool(workingDir string) Tool {
	tool := Tool{
		Name:         "search_text",
		Description:  "使用 ripgrep 在工作区搜索文本，支持 glob、字面匹配、大小写和分页，返回文件、行列与文本。",
		Capabilities: CapabilityRead,
		Effect:       EffectPure,
		ParallelSafe: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":        map[string]any{"type": "string", "description": "正则表达式或字面文本"},
				"path":           map[string]any{"type": "string", "description": "工作区内文件或目录，默认 ."},
				"globs":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": maxSearchGlobs},
				"literal":        map[string]any{"type": "boolean", "description": "按字面文本匹配"},
				"case_sensitive": map[string]any{"type": "boolean", "description": "是否区分大小写，默认 true"},
				"offset":         map[string]any{"type": "integer", "minimum": 0, "description": "分页偏移"},
				"limit":          map[string]any{"type": "integer", "minimum": 1, "maximum": maxSearchLimit, "description": "返回匹配数，默认 100"},
			},
			"required":             []string{"pattern"},
			"additionalProperties": false,
		},
	}
	tool.AutoApprove = structuredPathAutoApprove(workingDir)
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args searchTextArgs
		if err := ParseArguments(raw, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
		if args.Pattern == "" {
			return "", errors.New("缺少 pattern")
		}
		if len(args.Pattern) > 4096 {
			return "", errors.New("pattern 过长")
		}
		if len(args.Globs) > maxSearchGlobs {
			return "", fmt.Errorf("globs 最多 %d 项", maxSearchGlobs)
		}
		_, display, err := resolveStructuredPath(workingDir, args.Path, false)
		if err != nil {
			return "", err
		}
		limit, err := normalizePage(&args.pageArgs)
		if err != nil {
			return "", err
		}
		rgArgs := []string{"--json", "--color", "never"}
		if args.Literal {
			rgArgs = append(rgArgs, "--fixed-strings")
		}
		if args.CaseSensitive != nil && !*args.CaseSensitive {
			rgArgs = append(rgArgs, "--ignore-case")
		}
		for _, glob := range args.Globs {
			if strings.TrimSpace(glob) == "" || len(glob) > 4096 {
				return "", errors.New("globs 包含空值或过长值")
			}
			rgArgs = append(rgArgs, "--glob", glob)
		}
		rgArgs = append(rgArgs, "--", args.Pattern, display)
		root, err := canonicalWorkspace(workingDir)
		if err != nil {
			return "", err
		}
		command := exec.CommandContext(ctx, "rg", rgArgs...)
		command.Dir = root
		items, more, err := collectRGMatches(command, root, args.Offset, limit)
		if err != nil {
			return "", err
		}
		result := searchTextResult{Path: display, Matches: items}
		if more {
			result.NextOffset = args.Offset + len(items)
		}
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
	return tool
}

func structuredPathAutoApprove(workingDir string) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool {
		var args struct {
			Path string `json:"path,omitempty"`
		}
		if ParseArguments(raw, &args) != nil {
			return false
		}
		path := strings.TrimSpace(args.Path)
		if path == "" {
			path = "."
		}
		return safeStructuredReadPath(workingDir, path)
	}
}

func normalizePage(args *pageArgs) (int, error) {
	if args.Offset < 0 {
		return 0, errors.New("offset 不能为负数")
	}
	limit := args.Limit
	if limit == 0 {
		limit = defaultSearchLimit
	}
	if limit < 1 || limit > maxSearchLimit {
		return 0, fmt.Errorf("limit 必须在 1..%d 之间", maxSearchLimit)
	}
	return limit, nil
}

func resolveStructuredPath(workingDir, value string, directoryOnly bool) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "."
	}
	if !safeStructuredReadPath(workingDir, value) {
		return "", "", errors.New("path 必须是工作区内的非敏感路径")
	}
	root, err := canonicalWorkspace(workingDir)
	if err != nil {
		return "", "", err
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", "", err
	}
	if directoryOnly && !info.IsDir() {
		return "", "", errors.New("path 不是目录")
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", "", err
	}
	if relative == "" {
		relative = "."
	}
	return candidate, filepath.ToSlash(relative), nil
}

func safeStructuredReadPath(workingDir, value string) bool {
	return !pathContainsPart(value, ".git") && safeWorkspacePath(workingDir, value)
}

func canonicalWorkspace(workingDir string) (string, error) {
	root, err := filepath.Abs(workingDir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func collectDirectoryEntries(root, display string, maxDepth, offset, limit int) ([]listDirEntry, bool, error) {
	items := make([]listDirEntry, 0, limit+1)
	seen := 0
	var walk func(string, string, int) error
	walk = func(directory, relative string, depth int) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			childRelative := filepath.Join(relative, entry.Name())
			if sensitivePath(childRelative) || pathContainsPart(childRelative, ".git") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if seen >= offset {
				kind := "file"
				switch {
				case entry.Type()&os.ModeSymlink != 0:
					kind = "symlink"
				case info.IsDir():
					kind = "directory"
				}
				items = append(items, listDirEntry{Path: filepath.ToSlash(childRelative), Type: kind, Size: info.Size()})
				if len(items) > limit {
					return nil
				}
			}
			seen++
			if info.IsDir() && entry.Type()&os.ModeSymlink == 0 && depth < maxDepth {
				if err := walk(filepath.Join(directory, entry.Name()), childRelative, depth+1); err != nil {
					return err
				}
				if len(items) > limit {
					return nil
				}
			}
		}
		return nil
	}
	if err := walk(root, display, 1); err != nil {
		return nil, false, err
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	return items, more, nil
}

func collectRGPaths(command *exec.Cmd, workspace string, offset, limit int) ([]string, bool, error) {
	lines, more, err := scanRG(command, func(line []byte) (string, bool, error) {
		path := strings.TrimSpace(string(line))
		if path == "" || !safeRGResultPath(workspace, path) {
			return "", false, nil
		}
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(workspace, absolute)
		}
		relative, err := filepath.Rel(workspace, absolute)
		if err != nil {
			return "", false, err
		}
		return filepath.ToSlash(relative), true, nil
	}, offset, limit)
	return lines, more, err
}

func collectRGMatches(command *exec.Cmd, workspace string, offset, limit int) ([]textMatch, bool, error) {
	return scanRG(command, func(line []byte) (textMatch, bool, error) {
		var event rgEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return textMatch{}, false, fmt.Errorf("解析 rg JSON: %w", err)
		}
		if event.Type != "match" || !safeRGResultPath(workspace, event.Data.Path.Text) {
			return textMatch{}, false, nil
		}
		path := event.Data.Path.Text
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(workspace, absolute)
		}
		relative, err := filepath.Rel(workspace, absolute)
		if err != nil {
			return textMatch{}, false, err
		}
		column := 1
		if len(event.Data.Submatches) > 0 {
			column = event.Data.Submatches[0].Start + 1
		}
		text := strings.TrimRight(event.Data.Lines.Text, "\r\n")
		if len(text) > maxMatchTextBytes {
			text = text[:maxMatchTextBytes] + "..."
		}
		return textMatch{Path: filepath.ToSlash(relative), Line: event.Data.LineNumber, Column: column, Text: text}, true, nil
	}, offset, limit)
}

func scanRG[T any](command *exec.Cmd, decode func([]byte) (T, bool, error), offset, limit int) ([]T, bool, error) {
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, false, fmt.Errorf("启动 rg: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), maxSearchLineBytes)
	items := make([]T, 0, limit+1)
	seen := 0
	stopped := false
	for scanner.Scan() {
		item, include, decodeErr := decode(scanner.Bytes())
		if decodeErr != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return nil, false, decodeErr
		}
		if !include {
			continue
		}
		if seen >= offset {
			items = append(items, item)
			if len(items) > limit {
				stopped = true
				_ = command.Process.Kill()
				break
			}
		}
		seen++
	}
	if err := scanner.Err(); err != nil && !stopped {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, false, fmt.Errorf("读取 rg 输出: %w", err)
	}
	waitErr := command.Wait()
	if waitErr != nil && !stopped {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 1 {
			message := strings.TrimSpace(stderr.String())
			if message != "" {
				return nil, false, fmt.Errorf("rg: %s", message)
			}
			return nil, false, fmt.Errorf("rg: %w", waitErr)
		}
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	return items, more, nil
}

func safeRGResultPath(workspace, value string) bool {
	if value == "" || sensitivePath(value) || pathContainsPart(value, ".git") {
		return false
	}
	return safeWorkspacePath(workspace, value)
}
