package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/yuqie6/agent-harness/internal/fileedit"
)

const maxFileEditBytes = fileedit.MaxBytes

const (
	FileEditToolName    = "edit_file"
	FileEditDescription = "在工作区内按 SHA-256 前置条件修改文件，并在原子替换前再次检查。先用 read_file(include_metadata=true) 获取 expected_sha256;新文件使用 expected_sha256=absent。old_text 必须在现有文件中恰好出现一次。"
)

type FileEditInput struct {
	Path           string `json:"path"`
	ExpectedSHA256 string `json:"expected_sha256"`
	OldText        string `json:"old_text"`
	NewText        string `json:"new_text"`
}

type FileEditResult struct {
	Status         string `json:"status"`
	Path           string `json:"path"`
	BeforeSHA256   string `json:"before_sha256,omitempty"`
	AfterSHA256    string `json:"after_sha256"`
	Bytes          int    `json:"bytes"`
	AlreadyApplied bool   `json:"already_applied,omitempty"`
}

// FileEditParameters returns a fresh JSON schema for the shared interactive
// and durable edit_file contract.
func FileEditParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":            map[string]any{"type": "string", "description": "目标文件路径"},
			"expected_sha256": map[string]any{"type": "string", "description": "读取时的完整文件 SHA-256;新文件使用 absent"},
			"old_text":        map[string]any{"type": "string", "description": "要精确替换且只能出现一次的原文;新文件必须为空"},
			"new_text":        map[string]any{"type": "string", "description": "替换后的文本或新文件完整内容"},
		},
		"required":             []string{"path", "expected_sha256", "old_text", "new_text"},
		"additionalProperties": false,
	}
}

type fileEditPrepared = fileedit.Prepared

type FileEditTool struct {
	editor *fileedit.Editor
}

func NewFileEditTool(root string) (*FileEditTool, error) {
	editor, err := fileedit.New(root)
	if err != nil {
		return nil, fmt.Errorf("初始化 durable edit root: %w", err)
	}
	return &FileEditTool{editor: editor}, nil
}

func (t *FileEditTool) Name() string        { return FileEditToolName }
func (t *FileEditTool) Effect() EffectClass { return EffectReconcilable }

func (t *FileEditTool) Prepare(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input FileEditInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, fmt.Errorf("edit_file input: %w", err)
	}
	prepared, err := t.editor.Prepare(ctx, fileedit.Input{
		Path: input.Path, ExpectedSHA256: input.ExpectedSHA256, OldText: input.OldText, NewText: input.NewText,
	})
	if err != nil {
		return nil, durableEditError(err)
	}
	return json.Marshal(prepared)
}

func (t *FileEditTool) Execute(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
	prepared, err := decodePreparedEdit(invocation.Prepared)
	if err != nil {
		return nil, err
	}
	result, err := t.editor.Apply(ctx, prepared)
	if err != nil {
		return nil, durableEditError(err)
	}
	return fileEditResultJSON(result)
}

func (t *FileEditTool) Reconcile(ctx context.Context, invocation Invocation) (ReconcileResult, error) {
	prepared, err := decodePreparedEdit(invocation.Prepared)
	if err != nil {
		return ReconcileResult{}, err
	}
	path := prepared.RelativePath
	if path == "" {
		path = prepared.Path
	}
	current, err := t.editor.State(ctx, path)
	state := "absent"
	if err == nil {
		state = current.SHA256
	} else if !errors.Is(err, os.ErrNotExist) {
		return ReconcileResult{}, err
	}
	switch state {
	case prepared.AfterSHA256:
		result, err := fileEditResultJSON(fileedit.Result{
			Path: prepared.Path, BeforeSHA256: beforeDigest(prepared), AfterSHA256: current.SHA256,
			Bytes: len(current.Content), AlreadyApplied: true,
		})
		return ReconcileResult{State: ReconcileApplied, Result: result, Detail: "目标内容已经存在"}, err
	case prepared.BeforeState:
		return ReconcileResult{State: ReconcileNotApplied, Detail: "仍处于副作用前状态"}, nil
	default:
		return ReconcileResult{State: ReconcileConflict, Detail: fmt.Sprintf("文件状态为 %s,既非 %s 也非 %s", state, prepared.BeforeState, prepared.AfterSHA256)}, nil
	}
}

func decodePreparedEdit(raw json.RawMessage) (fileEditPrepared, error) {
	var prepared fileEditPrepared
	if err := decodeStrict(raw, &prepared); err != nil {
		return prepared, fmt.Errorf("prepared edit: %w", err)
	}
	if prepared.Path == "" || prepared.BeforeState == "" || prepared.AfterSHA256 == "" {
		return prepared, errors.New("prepared edit 缺少必要字段")
	}
	if digestBytes(prepared.Content) != prepared.AfterSHA256 {
		return prepared, errors.New("prepared edit 内容摘要不匹配")
	}
	return prepared, nil
}

func fileEditResultJSON(editResult fileedit.Result) (json.RawMessage, error) {
	result := FileEditResult{
		Status: "succeeded", Path: editResult.Path, BeforeSHA256: editResult.BeforeSHA256,
		AfterSHA256: editResult.AfterSHA256, Bytes: editResult.Bytes, AlreadyApplied: editResult.AlreadyApplied,
	}
	return json.Marshal(result)
}

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 包含多个值")
		}
		return err
	}
	return nil
}

func digestBytes(content []byte) string {
	return fileedit.Digest(content)
}

func durableEditError(err error) error {
	if errors.Is(err, fileedit.ErrConflict) {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

func beforeDigest(prepared fileEditPrepared) string {
	if prepared.BeforeState == "absent" {
		return ""
	}
	return prepared.BeforeState
}
