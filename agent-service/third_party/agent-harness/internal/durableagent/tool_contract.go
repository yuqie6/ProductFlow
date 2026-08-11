package durableagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/internal/tools"
)

const toolExecutionContractVersion = 1

type toolResultCodec string

const (
	toolResultEnvelope        toolResultCodec = "execution_envelope"
	toolResultContentEnvelope toolResultCodec = "content_execution_envelope"
	toolResultRawJSON         toolResultCodec = "raw_json"
	toolResultQuestion        toolResultCodec = "question_answer"
)

type toolStepContract struct {
	Name   string              `json:"name"`
	Effect durable.EffectClass `json:"effect"`
}

type modelToolExecutionContract struct {
	Name                string             `json:"name"`
	Steps               []toolStepContract `json:"steps"`
	ResultCodec         toolResultCodec    `json:"result_codec"`
	ConfigurationSHA256 string             `json:"configuration_sha256,omitempty"`
}

type toolExecutionContractSnapshot struct {
	Version       int                          `json:"version"`
	CatalogSHA256 string                       `json:"catalog_sha256"`
	Tools         []modelToolExecutionContract `json:"tools"`
	Fallback      modelToolExecutionContract   `json:"fallback"`
}

func buildToolExecutionContract(
	catalog []llm.Tool,
	registered []durable.Tool,
	rawResults map[string]bool,
	reviewEdit bool,
	checkContractSHA256 string,
) (string, map[string]toolResultCodec, error) {
	implementations := make(map[string]durable.Tool, len(registered))
	for _, implementation := range registered {
		if implementation == nil || implementation.Name() == "" {
			return "", nil, errors.New("durable agent 工具执行契约包含无效实现")
		}
		if _, duplicate := implementations[implementation.Name()]; duplicate {
			return "", nil, fmt.Errorf("durable agent 工具执行契约包含重名实现: %s", implementation.Name())
		}
		implementations[implementation.Name()] = implementation
	}

	contracts := make([]modelToolExecutionContract, 0, len(catalog))
	resultCodecs := make(map[string]toolResultCodec, len(catalog)+1)
	for _, schema := range catalog {
		name := schema.Function.Name
		implementation := implementations[name]
		if implementation == nil {
			return "", nil, fmt.Errorf("durable agent 模型工具 %s 缺少执行契约", name)
		}
		steps := []toolStepContract{{Name: name, Effect: implementation.Effect()}}
		if name == durable.FileEditToolName && reviewEdit {
			review := implementations[editReviewToolName]
			if review == nil {
				return "", nil, errors.New("durable agent 审后编辑缺少 review 执行契约")
			}
			steps = append([]toolStepContract{{Name: editReviewToolName, Effect: review.Effect()}}, steps...)
		}
		codec := resultCodecFor(name, rawResults)
		if content, ok := implementation.(interface{ ReturnsContent() bool }); ok && content.ReturnsContent() {
			codec = toolResultContentEnvelope
		}
		contract := modelToolExecutionContract{Name: name, Steps: steps, ResultCodec: codec}
		if name == tools.RunCheckToolName {
			if checkContractSHA256 == "" {
				return "", nil, errors.New("durable agent run_check 缺少配置契约")
			}
			contract.ConfigurationSHA256 = checkContractSHA256
		}
		contracts = append(contracts, contract)
		resultCodecs[name] = codec
	}
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].Name < contracts[j].Name })

	rejection := implementations[rejectionToolName]
	if rejection == nil {
		return "", nil, errors.New("durable agent 缺少工具拒绝执行契约")
	}
	fallback := modelToolExecutionContract{
		Name:        rejectionToolName,
		Steps:       []toolStepContract{{Name: rejectionToolName, Effect: rejection.Effect()}},
		ResultCodec: toolResultEnvelope,
	}
	resultCodecs[rejectionToolName] = toolResultEnvelope
	catalogSHA256, err := catalogDigest(catalog)
	if err != nil {
		return "", nil, err
	}
	encoded, err := json.Marshal(toolExecutionContractSnapshot{
		Version: toolExecutionContractVersion, CatalogSHA256: catalogSHA256, Tools: contracts, Fallback: fallback,
	})
	if err != nil {
		return "", nil, fmt.Errorf("编码 durable agent 工具执行契约: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), resultCodecs, nil
}

func resultCodecFor(name string, rawResults map[string]bool) toolResultCodec {
	switch {
	case name == (durableQuestionTool{}).Name():
		return toolResultQuestion
	case name == durable.FileEditToolName || rawResults[name]:
		return toolResultRawJSON
	default:
		return toolResultEnvelope
	}
}
