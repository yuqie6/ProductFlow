// Package fileedit implements the shared conditional file edit primitive used
// by interactive and durable tools.
package fileedit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const MaxBytes = 10 << 20

var ErrConflict = errors.New("file edit conflict")

type Input struct {
	Path           string
	ExpectedSHA256 string
	OldText        string
	NewText        string
}

type Prepared struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path,omitempty"`
	BeforeState  string `json:"before_state"`
	AfterSHA256  string `json:"after_sha256"`
	Content      []byte `json:"content"`
	Mode         uint32 `json:"mode"`
}

type Result struct {
	Path           string
	BeforeSHA256   string
	AfterSHA256    string
	Bytes          int
	AlreadyApplied bool
}

type State struct {
	Name    string
	Content []byte
	SHA256  string
	Mode    os.FileMode
	Exists  bool
}

type Editor struct {
	rootPath     string
	rootInfo     os.FileInfo
	beforeCommit func()
}

var editLocks [64]sync.Mutex

func New(root string) (*Editor, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("解析 edit root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("edit root 不是目录: %s", resolved)
	}
	return &Editor{rootPath: filepath.Clean(resolved), rootInfo: info}, nil
}

func (e *Editor) RootPath() string { return e.rootPath }

func (e *Editor) Prepare(ctx context.Context, input Input) (Prepared, error) {
	relative, display, err := e.normalize(input.Path)
	if err != nil {
		return Prepared{}, err
	}
	expected := strings.ToLower(strings.TrimSpace(input.ExpectedSHA256))
	if expected != "absent" {
		decoded, decodeErr := hex.DecodeString(expected)
		if decodeErr != nil || len(decoded) != sha256.Size {
			return Prepared{}, errors.New("expected_sha256 必须是 64 位 SHA-256 或 absent")
		}
	}
	state, err := e.State(ctx, relative)
	prepared := Prepared{Path: display, RelativePath: relative, Mode: 0o644}
	if errors.Is(err, os.ErrNotExist) {
		if expected != "absent" {
			return Prepared{}, fmt.Errorf("%w: %s 不存在", ErrConflict, display)
		}
		if input.OldText != "" {
			return Prepared{}, errors.New("创建新文件时 old_text 必须为空")
		}
		prepared.BeforeState = "absent"
		prepared.Content = []byte(input.NewText)
	} else {
		if err != nil {
			return Prepared{}, err
		}
		if expected == "absent" {
			if input.OldText == "" && state.SHA256 == Digest([]byte(input.NewText)) {
				prepared.BeforeState = "absent"
				prepared.Content = []byte(input.NewText)
				prepared.AfterSHA256 = state.SHA256
				return prepared, nil
			}
			return Prepared{}, fmt.Errorf("%w: %s 已存在", ErrConflict, display)
		}
		if state.SHA256 != expected {
			return Prepared{}, fmt.Errorf("%w: expected_sha256=%s, actual_sha256=%s", ErrConflict, expected, state.SHA256)
		}
		if input.OldText == "" {
			return Prepared{}, errors.New("修改现有文件时 old_text 不能为空")
		}
		if count := bytes.Count(state.Content, []byte(input.OldText)); count != 1 {
			return Prepared{}, fmt.Errorf("%w: old_text 必须恰好出现一次,实际出现 %d 次", ErrConflict, count)
		}
		prepared.BeforeState = state.SHA256
		prepared.Mode = uint32(state.Mode.Perm())
		prepared.Content = bytes.Replace(state.Content, []byte(input.OldText), []byte(input.NewText), 1)
	}
	if len(prepared.Content) > MaxBytes {
		return Prepared{}, fmt.Errorf("edit_file 结果超过 %d 字节上限", MaxBytes)
	}
	prepared.AfterSHA256 = Digest(prepared.Content)
	return prepared, nil
}

func (e *Editor) Apply(ctx context.Context, prepared Prepared) (Result, error) {
	if prepared.Path == "" || prepared.BeforeState == "" || prepared.AfterSHA256 == "" {
		return Result{}, errors.New("prepared edit 缺少必要字段")
	}
	if Digest(prepared.Content) != prepared.AfterSHA256 {
		return Result{}, errors.New("prepared edit 内容摘要不匹配")
	}
	path := prepared.RelativePath
	if path == "" {
		path = prepared.Path
	}
	relative, display, err := e.normalize(path)
	if err != nil {
		return Result{}, err
	}
	prepared.RelativePath = relative
	prepared.Path = display
	lock := &editLocks[lockIndex(e.rootPath+"\x00"+relative)]
	lock.Lock()
	defer lock.Unlock()

	root, err := e.openRoot()
	if err != nil {
		return Result{}, err
	}
	defer root.Close()
	parent, base, err := openParent(root, relative)
	if err != nil {
		return Result{}, err
	}
	defer parent.Close()
	state, err := readStateAt(ctx, parent, base)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	current := stateName(state, err)
	if current == prepared.AfterSHA256 {
		return resultOf(prepared, state.Content, true), nil
	}
	if current != prepared.BeforeState {
		return Result{}, conflict(current, prepared)
	}

	temporary, err := writeTemporary(ctx, parent, prepared.Content, os.FileMode(prepared.Mode))
	if err != nil {
		return Result{}, err
	}
	defer parent.Remove(temporary)
	if e.beforeCommit != nil {
		e.beforeCommit()
	}
	state, err = readStateAt(ctx, parent, base)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	current = stateName(state, err)
	if current == prepared.AfterSHA256 {
		return resultOf(prepared, state.Content, true), nil
	}
	if current != prepared.BeforeState {
		return Result{}, conflict(current, prepared)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if prepared.BeforeState == "absent" {
		if err := parent.Link(temporary, base); err != nil {
			state, readErr := readStateAt(ctx, parent, base)
			if readErr == nil && state.SHA256 == prepared.AfterSHA256 {
				return resultOf(prepared, state.Content, true), nil
			}
			if readErr == nil {
				return Result{}, conflict(stateName(state, readErr), prepared)
			}
			return Result{}, err
		}
		if err := parent.Remove(temporary); err != nil {
			return Result{}, err
		}
	} else if err := parent.Rename(temporary, base); err != nil {
		return Result{}, err
	}
	if err := syncRoot(parent); err != nil {
		return Result{}, err
	}
	return resultOf(prepared, prepared.Content, false), nil
}

func (e *Editor) State(ctx context.Context, path string) (State, error) {
	relative, _, err := e.normalize(path)
	if err != nil {
		return State{}, err
	}
	root, err := e.openRoot()
	if err != nil {
		return State{}, err
	}
	defer root.Close()
	parent, base, err := openParent(root, relative)
	if err != nil {
		return State{}, err
	}
	defer parent.Close()
	return readStateAt(ctx, parent, base)
}

func (e *Editor) normalize(path string) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", errors.New("缺少 path")
	}
	relative := path
	if filepath.IsAbs(path) {
		var err error
		relative, err = filepath.Rel(e.rootPath, filepath.Clean(path))
		if err != nil {
			return "", "", err
		}
	}
	relative = filepath.Clean(relative)
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("edit_file 路径逃出 root: %s", path)
	}
	if sensitivePath(relative) {
		return "", "", fmt.Errorf("edit_file 拒绝敏感路径: %s", path)
	}
	return relative, filepath.Join(e.rootPath, relative), nil
}

func (e *Editor) openRoot() (*os.Root, error) {
	root, err := os.OpenRoot(e.rootPath)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(e.rootInfo, info) {
		root.Close()
		return nil, errors.New("edit root 在初始化后已被替换")
	}
	return root, nil
}

func openParent(root *os.Root, name string) (*os.Root, string, error) {
	parentName := filepath.Dir(name)
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	if parentName == "." {
		return current, filepath.Base(name), nil
	}
	for _, component := range strings.Split(parentName, string(filepath.Separator)) {
		info, err := current.Lstat(component)
		if err != nil {
			current.Close()
			return nil, "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			current.Close()
			return nil, "", fmt.Errorf("edit_file 父路径不是普通目录: %s", parentName)
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			current.Close()
			return nil, "", err
		}
		openedInfo, err := next.Stat(".")
		if err != nil || !os.SameFile(info, openedInfo) {
			next.Close()
			current.Close()
			if err != nil {
				return nil, "", err
			}
			return nil, "", fmt.Errorf("edit_file 父目录在打开时发生变化: %s", parentName)
		}
		current.Close()
		current = next
	}
	return current, filepath.Base(name), nil
}

func readStateAt(ctx context.Context, root *os.Root, name string) (State, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return State{Name: name}, err
	}
	if !info.Mode().IsRegular() {
		return State{Name: name}, fmt.Errorf("edit_file 只支持普通文件: %s", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return State{Name: name}, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return State{Name: name}, err
	}
	if !openedInfo.Mode().IsRegular() {
		return State{Name: name}, fmt.Errorf("edit_file 只支持普通文件: %s", name)
	}
	content, err := readLimited(ctx, file, MaxBytes+1)
	if err != nil {
		return State{Name: name}, err
	}
	if len(content) > MaxBytes {
		return State{Name: name}, fmt.Errorf("edit_file 超过 %d 字节上限", MaxBytes)
	}
	return State{Name: name, Content: content, SHA256: Digest(content), Mode: openedInfo.Mode(), Exists: true}, nil
}

func readLimited(ctx context.Context, reader io.Reader, limit int) ([]byte, error) {
	var output bytes.Buffer
	buffer := make([]byte, 32<<10)
	for output.Len() < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := min(len(buffer), limit-output.Len())
		read, err := reader.Read(buffer[:remaining])
		if read > 0 {
			_, _ = output.Write(buffer[:read])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return output.Bytes(), nil
}

func writeTemporary(ctx context.Context, root *os.Root, content []byte, mode os.FileMode) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		suffix := make([]byte, 8)
		if _, err := rand.Read(suffix); err != nil {
			return "", err
		}
		name := ".harness-edit-" + hex.EncodeToString(suffix)
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := writeAll(ctx, file, content); err != nil {
			file.Close()
			root.Remove(name)
			return "", err
		}
		if err := file.Chmod(mode.Perm()); err != nil {
			file.Close()
			root.Remove(name)
			return "", err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			root.Remove(name)
			return "", err
		}
		if err := file.Close(); err != nil {
			root.Remove(name)
			return "", err
		}
		return name, nil
	}
	return "", errors.New("无法创建 edit_file 临时文件")
}

func writeAll(ctx context.Context, writer io.Writer, content []byte) error {
	for len(content) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := content[:min(len(content), 32<<10)]
		written, err := writer.Write(chunk)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func stateName(state State, err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "absent"
	}
	return state.SHA256
}

func resultOf(prepared Prepared, content []byte, already bool) Result {
	result := Result{
		Path: prepared.Path, AfterSHA256: Digest(content), Bytes: len(content), AlreadyApplied: already,
	}
	if prepared.BeforeState != "absent" {
		result.BeforeSHA256 = prepared.BeforeState
	}
	return result
}

func conflict(current string, prepared Prepared) error {
	return fmt.Errorf("%w: 当前状态 %s,预期 %s 或 %s", ErrConflict, current, prepared.BeforeState, prepared.AfterSHA256)
}

func Digest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func lockIndex(value string) byte {
	digest := sha256.Sum256([]byte(value))
	return digest[0] & 63
}

func sensitivePath(path string) bool {
	for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(path)), "/") {
		if part == ".git" || part == ".env" || part == "auth.json" ||
			(strings.HasPrefix(part, ".env.") && part != ".env.example") {
			return true
		}
	}
	return false
}
