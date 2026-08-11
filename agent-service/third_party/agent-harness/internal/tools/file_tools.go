package tools

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/fileedit"
)

const (
	maxReadBytes      = 100 << 10
	defaultReadLines  = 200
	maxReadLines      = 2000
	readTruncatedNote = "\n... (内容过长已截断,如需更多请说明)"
)

type readFileArgs struct {
	Path            string `json:"path"`
	StartLine       int    `json:"start_line,omitempty"`
	LineCount       int    `json:"line_count,omitempty"`
	IncludeMetadata bool   `json:"include_metadata,omitempty"`
}

type readFileResult struct {
	Path          string `json:"path"`
	SHA256        string `json:"sha256,omitempty"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	TotalLines    int    `json:"total_lines,omitempty"`
	NextStartLine int    `json:"next_start_line,omitempty"`
	Content       string `json:"content"`
}

func readTool() Tool {
	return Tool{
		Name:         "read_file",
		Description:  "读取文本文件。可用 start_line/line_count 分页;include_metadata 返回完整文件 SHA-256,供条件编辑使用。单页最多 100KB。",
		Capabilities: CapabilityRead,
		Effect:       EffectPure,
		ParallelSafe: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":             map[string]any{"type": "string", "description": "要读取的文件路径"},
				"start_line":       map[string]any{"type": "integer", "minimum": 1, "description": "起始行,从 1 开始"},
				"line_count":       map[string]any{"type": "integer", "minimum": 1, "maximum": maxReadLines, "description": "本页最多读取行数"},
				"include_metadata": map[string]any{"type": "boolean", "description": "返回行范围、总行数和完整文件 SHA-256"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args readFileArgs
			if err := ParseArguments(raw, &args); err != nil {
				return "", fmt.Errorf("参数解析失败: %w", err)
			}
			args.Path = strings.TrimSpace(args.Path)
			if args.Path == "" {
				return "", errors.New("缺少 path")
			}
			if args.StartLine < 0 || args.LineCount < 0 || args.LineCount > maxReadLines {
				return "", fmt.Errorf("读取范围无效: start_line=%d, line_count=%d", args.StartLine, args.LineCount)
			}
			if !args.IncludeMetadata && args.StartLine == 0 && args.LineCount == 0 {
				return legacyReadFile(ctx, args.Path)
			}
			result, err := readFilePage(ctx, args)
			if err != nil {
				return "", err
			}
			encoded, err := json.Marshal(result)
			return string(encoded), err
		},
	}
}

func legacyReadFile(ctx context.Context, path string) (string, error) {
	file, err := openRegularFile(ctx, path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := readWithContext(ctx, io.LimitReader(file, int64(maxReadBytes+1)))
	if err != nil {
		return "", err
	}
	if len(data) > maxReadBytes {
		return string(data[:maxReadBytes]) + readTruncatedNote, nil
	}
	return string(data), nil
}

func readFilePage(ctx context.Context, args readFileArgs) (readFileResult, error) {
	file, err := openRegularFile(ctx, args.Path)
	if err != nil {
		return readFileResult{}, err
	}
	defer file.Close()

	digest := ""
	totalLines := 0
	if args.IncludeMetadata {
		digest, totalLines, err = inspectFile(ctx, file)
		if err != nil {
			return readFileResult{}, err
		}
	}
	start := args.StartLine
	if start == 0 {
		start = 1
	}
	limit := args.LineCount
	if limit == 0 {
		limit = defaultReadLines
	}
	if args.IncludeMetadata && totalLines > 0 && start > totalLines {
		return readFileResult{}, fmt.Errorf("start_line=%d 超出文件总行数 %d", start, totalLines)
	}
	if args.IncludeMetadata {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return readFileResult{}, err
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxReadBytes+1)
	lines := make([]string, 0, min(limit, defaultReadLines))
	contentBytes := 0
	current := 0
	next := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return readFileResult{}, err
		}
		current++
		if current < start {
			continue
		}
		if len(lines) >= limit {
			next = current
			break
		}
		line := scanner.Text()
		needed := len(line)
		if len(lines) > 0 {
			needed++
		}
		if contentBytes+needed > maxReadBytes {
			if len(lines) == 0 {
				return readFileResult{}, fmt.Errorf("第 %d 行超过单页 %d 字节上限", current, maxReadBytes)
			}
			next = current
			break
		}
		lines = append(lines, line)
		contentBytes += needed
	}
	if err := scanner.Err(); err != nil {
		return readFileResult{}, fmt.Errorf("读取文件行: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return readFileResult{}, err
	}
	if !args.IncludeMetadata && len(lines) == 0 && start > 1 && current < start {
		return readFileResult{}, fmt.Errorf("start_line=%d 超出文件总行数 %d", start, current)
	}
	end := start + len(lines) - 1
	if len(lines) == 0 {
		end = 0
	}
	if args.IncludeMetadata && next == 0 && end > 0 && end < totalLines {
		next = end + 1
	}
	return readFileResult{
		Path: args.Path, SHA256: digest, StartLine: start, EndLine: end,
		TotalLines: totalLines, NextStartLine: next, Content: strings.Join(lines, "\n"),
	}, nil
}

type lineCounter struct {
	bytes    int64
	newlines int
	last     byte
}

func (c *lineCounter) Write(data []byte) (int, error) {
	c.bytes += int64(len(data))
	c.newlines += bytes.Count(data, []byte{'\n'})
	if len(data) > 0 {
		c.last = data[len(data)-1]
	}
	return len(data), nil
}

func inspectFile(ctx context.Context, file *os.File) (string, int, error) {
	digest := sha256.New()
	counter := &lineCounter{}
	if _, err := io.Copy(io.MultiWriter(digest, counter), contextReader{ctx: ctx, reader: file}); err != nil {
		return "", 0, err
	}
	lines := counter.newlines
	if counter.bytes > 0 && counter.last != '\n' {
		lines++
	}
	return hex.EncodeToString(digest.Sum(nil)), lines, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func openRegularFile(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("read_file 只支持普通文件: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, fmt.Errorf("read_file 目标在打开时发生变化: %s", path)
	}
	return file, nil
}

func readWithContext(ctx context.Context, reader io.Reader) ([]byte, error) {
	return io.ReadAll(contextReader{ctx: ctx, reader: reader})
}

type editFileArgs = durable.FileEditInput
type editFileResult = durable.FileEditResult

func editTool(workingDir string) Tool {
	editor, editorErr := fileedit.New(workingDir)
	return Tool{
		Name:         durable.FileEditToolName,
		Description:  durable.FileEditDescription,
		Capabilities: CapabilityWrite,
		Effect:       EffectReconcilable,
		Parameters:   durable.FileEditParameters(),
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args editFileArgs
			if err := ParseArguments(raw, &args); err != nil {
				return "", fmt.Errorf("参数解析失败: %w", err)
			}
			if editorErr != nil {
				return "", editorErr
			}
			result, runErr := applyConditionalEdit(ctx, editor, args)
			encoded, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				return "", marshalErr
			}
			return string(encoded), runErr
		},
	}
}

func applyConditionalEdit(ctx context.Context, editor *fileedit.Editor, args editFileArgs) (editFileResult, error) {
	result := editFileResult{Path: args.Path}
	prepared, err := editor.Prepare(ctx, fileedit.Input{
		Path: args.Path, ExpectedSHA256: args.ExpectedSHA256, OldText: args.OldText, NewText: args.NewText,
	})
	if err != nil {
		if errors.Is(err, fileedit.ErrConflict) {
			result.Status = "conflict"
		}
		return result, err
	}
	applied, err := editor.Apply(ctx, prepared)
	if err != nil {
		if errors.Is(err, fileedit.ErrConflict) {
			result.Status = "conflict"
		}
		return result, err
	}
	result = editFileResult{
		Status: "succeeded", Path: applied.Path, BeforeSHA256: applied.BeforeSHA256,
		AfterSHA256: applied.AfterSHA256, Bytes: applied.Bytes, AlreadyApplied: applied.AlreadyApplied,
	}
	return result, nil
}

func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
