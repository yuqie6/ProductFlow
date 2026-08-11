// Package tools 定义 agent 可调用的工具:注册表 + 内置工具。
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/yuqie6/agent-harness/internal/llm"
)

// Capability 描述工具可能触及的运行能力。一次具体调用是否只读仍由
// AutoApprove 判断;Capability 用于在调用前裁剪可见工具集合。
type Capability uint8

const (
	CapabilityRead Capability = 1 << iota
	CapabilityWrite
	CapabilityExecute
)

// Has 报告能力集合是否包含指定能力。
func (c Capability) Has(capability Capability) bool {
	return c&capability != 0
}

// EffectClass 描述工具副作用在进程失联后的恢复语义。
type EffectClass string

const (
	EffectPure         EffectClass = "pure"
	EffectIdempotent   EffectClass = "idempotent"
	EffectReconcilable EffectClass = "reconcilable"
	EffectOpaque       EffectClass = "opaque"
)

// Tool 一个可被 agent 调用的工具。Handler 保留字符串结果兼容性；
// ResultHandler 返回 Responses 原生的文本/图片 content parts。
type Tool struct {
	Name        string
	Description string
	// Strict requests provider-side JSON Schema conformance. A handler whose
	// correctness depends on the schema must still validate arguments locally.
	Strict       bool
	Capabilities Capability
	Effect       EffectClass
	// Parameters 是 JSON Schema 风格的参数声明,发给模型用于生成参数。
	Parameters    map[string]any
	Handler       func(ctx context.Context, args json.RawMessage) (string, error)
	ResultHandler func(ctx context.Context, args json.RawMessage) (llm.ToolResult, error)
	// AutoApprove 仅在能确定调用只读且局限于当前工作区时返回 true。
	// 未提供或无法判断时默认要求用户批准。
	AutoApprove func(args json.RawMessage) bool
	// AutoApproveEdit 仅用于 Auto Edit 模式。它必须把写入
	// 限制在工作区内,不能放行任意命令执行。
	AutoApproveEdit func(args json.RawMessage) bool
	// MaxCallsPerRun 对昂贵或有外部成本的工具设置单次 Runner 调用上限。
	// 零表示不单独限制,仍受 Runner.MaxIterations 约束。
	MaxCallsPerRun int
	// ParallelSafe 表示只读 Handler 可与同一模型响应中的其他显式安全 Handler 并发调用。
	// Effect 仍保留恢复语义;Runner 会按原 call 顺序写回结果并串行发送事件。
	ParallelSafe bool
}

func (t Tool) HasHandler() bool { return t.Handler != nil || t.ResultHandler != nil }

func (t Tool) Execute(ctx context.Context, args json.RawMessage) (llm.ToolResult, error) {
	if t.Handler != nil && t.ResultHandler != nil {
		return llm.ToolResult{}, errors.New("tool has both Handler and ResultHandler")
	}
	if t.ResultHandler != nil {
		return t.ResultHandler(ctx, args)
	}
	if t.Handler == nil {
		return llm.ToolResult{}, fmt.Errorf("tool %s has no handler", t.Name)
	}
	output, err := t.Handler(ctx, args)
	return llm.TextToolResult(output), err
}

// Schema 转成发给模型的工具声明。
func (t Tool) Schema() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			Strict:      t.Strict,
		},
	}
}

// CanAutoApprove 报告一次具体调用是否可以跳过交互批准。
func (t Tool) CanAutoApprove(args json.RawMessage) bool {
	return t.AutoApprove != nil && t.AutoApprove(args)
}

// CanAutoApproveEdit 报告一次写入是否可由 Auto Edit 模式放行。
func (t Tool) CanAutoApproveEdit(args json.RawMessage) bool {
	return t.AutoApproveEdit != nil && t.AutoApproveEdit(args)
}

// Registry 是线程安全的工具注册表。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry 用一批工具构造注册表。
func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range ts {
		r.Register(t)
	}
	return r
}

// Register 注册一个工具(重名覆盖)。
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name] = t
}

// Remove removes a tool from future catalog snapshots and returns whether it
// was registered. In-flight calls already holding a Tool value are unaffected.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return false
	}
	delete(r.tools, name)
	return true
}

// Names 返回已注册的工具名(用于报错与展示)。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Schemas 返回发给模型的全部工具声明。
func (r *Registry) Schemas() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]llm.Tool, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name].Schema())
	}
	return out
}

// Tools 返回按名称排序的工具副本,供运行时构造每次运行的工具目录。
func (r *Registry) Tools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name])
	}
	return out
}

// Lookup 按名字查工具。
func (r *Registry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// ParseArguments 解析模型返回的工具参数。
// 主流实现把 arguments 作为 JSON 字符串返回,少数直接给对象,这里两种都兼容。
func ParseArguments(raw json.RawMessage, out any) error {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return json.Unmarshal([]byte(s), out)
	}
	return json.Unmarshal(raw, out)
}
