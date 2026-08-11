package durableagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/internal/tools"
)

type preparedTool struct {
	Arguments json.RawMessage `json:"arguments"`
	Rejected  string          `json:"rejected,omitempty"`
}

type pureToolSnapshot func(json.RawMessage) (preparedTool, error)

type durablePureTool struct {
	source   tools.Tool
	snapshot pureToolSnapshot
}

func newDurableReadTool(workspace string, source tools.Tool) (*durablePureTool, error) {
	if source.Name != "read_file" || !source.HasHandler() || source.Effect != tools.EffectPure {
		return nil, errors.New("durable agent 需要 pure read_file 工具")
	}
	return newDurablePureTool(source, readFileSnapshot(workspace, source))
}

func newDurablePureTool(source tools.Tool, snapshot pureToolSnapshot) (*durablePureTool, error) {
	if source.Name == "" || !source.HasHandler() || source.Effect != tools.EffectPure {
		return nil, errors.New("durable agent 需要带 handler 的 pure 工具")
	}
	if snapshot == nil {
		snapshot = defaultPureToolSnapshot(source)
	}
	return &durablePureTool{source: source, snapshot: snapshot}, nil
}

func (t *durablePureTool) Name() string                { return t.source.Name }
func (t *durablePureTool) Effect() durable.EffectClass { return durable.EffectPure }
func (t *durablePureTool) ReturnsContent() bool        { return t.source.ResultHandler != nil }

func (t *durablePureTool) Prepare(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	prepared, err := t.snapshot(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(prepared)
}

func (t *durablePureTool) Execute(ctx context.Context, invocation durable.Invocation) (json.RawMessage, error) {
	var prepared preparedTool
	if err := json.Unmarshal(invocation.Prepared, &prepared); err != nil {
		return nil, fmt.Errorf("prepared %s: %w", t.source.Name, err)
	}
	result := toolExecutionResult{}
	if prepared.Rejected != "" {
		result.Error = prepared.Rejected
		return json.Marshal(result)
	}
	if !t.source.CanAutoApprove(prepared.Arguments) {
		result.Error = t.source.Name + " 调用在 Prepare 后离开了只读边界"
		return json.Marshal(result)
	}
	output, err := t.source.Execute(ctx, prepared.Arguments)
	if output.Text != nil {
		result.Output = *output.Text
	} else {
		result.ContentParts = append([]llm.ContentPart(nil), output.ContentParts...)
		for index := range result.ContentParts {
			result.ContentParts[index].EmbeddedData = append([]byte(nil), output.ContentParts[index].EmbeddedData...)
		}
	}
	if err != nil {
		result.Error = err.Error()
	}
	return json.Marshal(result)
}

func (t *durablePureTool) Reconcile(context.Context, durable.Invocation) (durable.ReconcileResult, error) {
	return durable.ReconcileResult{}, fmt.Errorf("pure %s 不需要对账", t.source.Name)
}

func defaultPureToolSnapshot(source tools.Tool) pureToolSnapshot {
	return func(raw json.RawMessage) (preparedTool, error) {
		prepared := preparedTool{Arguments: append(json.RawMessage(nil), raw...)}
		if !source.CanAutoApprove(raw) {
			prepared.Rejected = source.Name + " 调用未通过只读边界校验"
		}
		return prepared, nil
	}
}

func readFileSnapshot(workspace string, source tools.Tool) pureToolSnapshot {
	return func(raw json.RawMessage) (preparedTool, error) {
		prepared := preparedTool{Arguments: append(json.RawMessage(nil), raw...)}
		var args struct {
			Path            string `json:"path"`
			StartLine       int    `json:"start_line,omitempty"`
			LineCount       int    `json:"line_count,omitempty"`
			IncludeMetadata bool   `json:"include_metadata,omitempty"`
		}
		if err := tools.ParseArguments(raw, &args); err != nil {
			prepared.Rejected = "参数解析失败: " + err.Error()
			return prepared, nil
		}
		if !source.CanAutoApprove(raw) {
			prepared.Rejected = "read_file 只允许读取当前 workspace 内的非敏感文件"
			return prepared, nil
		}
		candidate := args.Path
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(workspace, candidate)
		}
		resolved, err := filepath.EvalSymlinks(filepath.Clean(candidate))
		if err != nil {
			prepared.Rejected = err.Error()
			return prepared, nil
		}
		args.Path = resolved
		prepared.Arguments, err = json.Marshal(args)
		return prepared, err
	}
}

func rejectionTool() durable.Tool {
	return durable.FuncTool{
		ToolName: rejectionToolName, EffectClass: durable.EffectPure,
		ExecuteFunc: func(_ context.Context, invocation durable.Invocation) (json.RawMessage, error) {
			return append(json.RawMessage(nil), invocation.Input...), nil
		},
	}
}

func toolResultValue(step durable.Step, codec toolResultCodec) (llm.ToolResult, error) {
	switch codec {
	case toolResultRawJSON:
		return llm.TextToolResult(string(step.Result)), nil
	case toolResultQuestion:
		output, err := questionResultText(step)
		return llm.TextToolResult(output), err
	case toolResultEnvelope, toolResultContentEnvelope:
	default:
		return llm.ToolResult{}, fmt.Errorf("工具 %s 缺少已验证的结果编码", step.Tool)
	}
	var result toolExecutionResult
	if err := json.Unmarshal(step.Result, &result); err != nil {
		return llm.ToolResult{}, fmt.Errorf("解析 %s 结果: %w", step.Tool, err)
	}
	if codec == toolResultEnvelope && len(result.ContentParts) != 0 {
		return llm.ToolResult{}, fmt.Errorf("工具 %s 的字符串结果包含未声明 content parts", step.Tool)
	}
	if codec == toolResultContentEnvelope && result.Output != "" {
		return llm.ToolResult{}, fmt.Errorf("工具 %s 的多模态结果包含字符串 output", step.Tool)
	}
	if codec == toolResultContentEnvelope && len(result.ContentParts) == 0 {
		if result.Error == "" {
			return llm.ToolResult{}, fmt.Errorf("工具 %s 的多模态结果缺少 content parts", step.Tool)
		}
		return llm.ContentToolResult([]llm.ContentPart{{
			Type: "input_text", Text: "工具执行失败: " + result.Error,
		}}), nil
	}
	if len(result.ContentParts) != 0 {
		output := llm.ContentToolResult(result.ContentParts)
		if result.Error != "" {
			output.ContentParts = append(output.ContentParts, llm.ContentPart{Type: "input_text", Text: "工具执行失败: " + result.Error})
		}
		return output, nil
	}
	output := strings.TrimSpace(result.Output)
	if result.Error != "" {
		if output == "" {
			return llm.TextToolResult("工具执行失败: " + result.Error), nil
		}
		return llm.TextToolResult(result.Output + "\n工具执行失败: " + result.Error), nil
	}
	if output == "" {
		return llm.TextToolResult("(工具返回空输出)"), nil
	}
	return llm.TextToolResult(result.Output), nil
}
