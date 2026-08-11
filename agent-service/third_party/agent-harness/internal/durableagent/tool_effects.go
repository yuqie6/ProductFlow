package durableagent

import (
	"fmt"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
)

func legacyToolEffectsCompatible(catalog []llm.Tool, registered []durable.Tool) (bool, error) {
	available := make(map[string]durable.EffectClass, len(registered))
	for _, tool := range registered {
		if _, duplicate := available[tool.Name()]; duplicate {
			return false, fmt.Errorf("durable agent 工具重复注册: %s", tool.Name())
		}
		available[tool.Name()] = tool.Effect()
	}
	for _, schema := range catalog {
		name := schema.Function.Name
		effect, ok := available[name]
		if !ok {
			return false, fmt.Errorf("durable agent 可见工具 %s 缺少执行实现", name)
		}
		expected := durable.EffectPure
		if name == durable.FileEditToolName {
			expected = durable.EffectReconcilable
		}
		if effect != expected {
			return false, nil
		}
	}
	return true, nil
}
