package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/agent-harness/internal/artifact"
)

const ReadArtifactToolName = "read_artifact"

type readArtifactArgs struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// NewArtifactTool exposes bounded, read-only paging for artifact references
// already returned to the model by a prior tool call.
func NewArtifactTool(store *artifact.FileStore, pageBytes int) (Tool, error) {
	if store == nil {
		return Tool{}, errors.New("artifact store 未配置")
	}
	if pageBytes < 1 || pageBytes > artifact.MaxPageBytes {
		return Tool{}, fmt.Errorf("artifact page bytes 必须在 1..%d", artifact.MaxPageBytes)
	}
	tool := Tool{
		Name:         ReadArtifactToolName,
		Description:  "分页读取已保存的大型工具输出。使用工具结果中的 artifact.id；按 next_offset 继续读取。",
		Capabilities: CapabilityRead,
		Effect:       EffectPure,
		ParallelSafe: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "工具结果给出的 sha256 artifact ID"},
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "起始字节偏移"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": pageBytes, "description": "本页最大字节数"},
			},
			"required":             []string{"id"},
			"additionalProperties": false,
		},
	}
	decode := func(raw json.RawMessage) (readArtifactArgs, error) {
		var args readArtifactArgs
		if err := ParseArguments(raw, &args); err != nil {
			return args, err
		}
		args.ID = strings.TrimSpace(args.ID)
		if args.Limit == 0 {
			args.Limit = pageBytes
		}
		if args.Limit < 1 || args.Limit > pageBytes {
			return args, fmt.Errorf("artifact limit 必须在 1..%d", pageBytes)
		}
		if _, err := store.ReadPage(args.ID, args.Offset, args.Limit); err != nil {
			return args, err
		}
		return args, nil
	}
	tool.AutoApprove = func(raw json.RawMessage) bool {
		_, err := decode(raw)
		return err == nil
	}
	tool.Handler = func(_ context.Context, raw json.RawMessage) (string, error) {
		var args readArtifactArgs
		if err := ParseArguments(raw, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
		if args.Limit == 0 {
			args.Limit = pageBytes
		}
		if args.Limit < 1 || args.Limit > pageBytes {
			return "", fmt.Errorf("artifact limit 必须在 1..%d", pageBytes)
		}
		page, err := store.ReadPage(strings.TrimSpace(args.ID), args.Offset, args.Limit)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(page)
		return string(encoded), err
	}
	return tool, nil
}
