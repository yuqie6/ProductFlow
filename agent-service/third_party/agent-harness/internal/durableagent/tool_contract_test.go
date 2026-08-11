package durableagent

import (
	"testing"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/internal/tools"
)

func TestToolExecutionContractBindsEffectAndResultCodec(t *testing.T) {
	catalog := contractTestCatalog("business_state")
	registered := contractTestTools("business_state", durable.EffectReconcilable)
	baseline, codecs, err := buildToolExecutionContract(catalog, registered, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if codecs["business_state"] != toolResultEnvelope || codecs[rejectionToolName] != toolResultEnvelope {
		t.Fatalf("result codecs = %#v", codecs)
	}

	changedEffect, _, err := buildToolExecutionContract(
		catalog, contractTestTools("business_state", durable.EffectOpaque), nil, false, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	changedCodec, rawCodecs, err := buildToolExecutionContract(
		catalog, registered, map[string]bool{"business_state": true}, false, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if baseline == changedEffect {
		t.Fatal("tool execution contract ignored effect drift")
	}
	if baseline == changedCodec || rawCodecs["business_state"] != toolResultRawJSON {
		t.Fatal("tool execution contract ignored result codec drift")
	}
}

func TestToolExecutionContractBindsReviewSteps(t *testing.T) {
	catalog := contractTestCatalog(durable.FileEditToolName)
	registered := append(contractTestTools(durable.FileEditToolName, durable.EffectReconcilable), durable.FuncTool{
		ToolName: editReviewToolName, EffectClass: durable.EffectPure,
	})
	direct, directCodecs, err := buildToolExecutionContract(catalog, registered, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewed, reviewedCodecs, err := buildToolExecutionContract(catalog, registered, nil, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if direct == reviewed {
		t.Fatal("tool execution contract ignored review step expansion")
	}
	if directCodecs[durable.FileEditToolName] != toolResultRawJSON || reviewedCodecs[durable.FileEditToolName] != toolResultRawJSON {
		t.Fatalf("edit result codecs = direct:%q reviewed:%q", directCodecs[durable.FileEditToolName], reviewedCodecs[durable.FileEditToolName])
	}
}

func TestToolExecutionContractBindsNamedCheckConfiguration(t *testing.T) {
	catalog := contractTestCatalog(tools.RunCheckToolName)
	registered := contractTestTools(tools.RunCheckToolName, durable.EffectPure)
	first, _, err := buildToolExecutionContract(catalog, registered, nil, false, "check-a")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := buildToolExecutionContract(catalog, registered, nil, false, "check-b")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("tool execution contract ignored named check configuration drift")
	}
	if _, _, err := buildToolExecutionContract(catalog, registered, nil, false, ""); err == nil {
		t.Fatal("run_check contract accepted a missing configuration digest")
	}
}

func contractTestCatalog(name string) []llm.Tool {
	return []llm.Tool{{
		Type: "function",
		Function: llm.ToolFunction{
			Name: name, Description: "test tool",
			Parameters: map[string]any{"type": "object", "additionalProperties": false},
		},
	}}
}

func contractTestTools(name string, effect durable.EffectClass) []durable.Tool {
	return []durable.Tool{
		durable.FuncTool{ToolName: name, EffectClass: effect},
		rejectionTool(),
	}
}
