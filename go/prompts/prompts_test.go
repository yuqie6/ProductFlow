package prompts

import (
	"strings"
	"testing"
)

func TestCatalogHasRequiredImageTypesAndNeedles(t *testing.T) {
	if AgentWorkflow() == "" || AgentGlobal() == "" || AgentGoalLoop() == "" {
		t.Fatal("agent prompts must not be empty")
	}
	if !strings.Contains(AgentWorkflow(), "不得提交第二份完整拓扑") {
		t.Fatal("workflow prompt missing topology rule")
	}
	if !strings.Contains(AgentGoalLoop(), "request_workflow_run_v1") || !strings.Contains(AgentGoalLoop(), "apply_graph_change_set_v1") {
		t.Fatal("goal loop must use manifest tool names")
	}
	if !strings.Contains(AgentGlobal(), "不能在全局会话上改某个商品的 live graph") {
		t.Fatal("global prompt missing live-graph rule")
	}
	if !strings.Contains(BriefInstructions(), "You write one ecommerce listing brief") {
		t.Fatal("brief instructions missing")
	}
	if !strings.Contains(OverlayInstructions(), "You write a compact visual overlay") {
		t.Fatal("overlay instructions missing")
	}
	if !strings.Contains(PromptInstructions(), "You write one ListingPromptPayload") {
		t.Fatal("prompt instructions missing")
	}
	look := ListingLook()
	if !strings.Contains(look.Rule, "做成能点击的商业套图") {
		t.Fatalf("look rule %q", look.Rule)
	}
	if look.ProductSharePercent != "55-75" || look.BenefitCount != "2-4" || !look.SourceNoteIsProductFact {
		t.Fatalf("look context %+v", look)
	}
	if len(look.BriefProhibitions) != 3 {
		t.Fatalf("brief prohibitions %+v", look.BriefProhibitions)
	}
	types := ImageTypes()
	if len(types) != 15 {
		t.Fatalf("image types %d", len(types))
	}
	selling, ok := ImageTypeByKey("selling_point")
	if !ok || !strings.Contains(selling.Job, "详情卖点图") || selling.Title != "核心卖点图" {
		t.Fatalf("selling_point %+v ok=%v", selling, ok)
	}
	hero, ok := ImageTypeByKey("hero")
	if !ok || hero.Title != "首屏海报图" || hero.Order != 0 {
		t.Fatalf("hero %+v ok=%v", hero, ok)
	}
	id := IdentityRules()
	if len(id.Shared) != 3 || id.NoOnImageText == "" || id.ContextDerived == "" {
		t.Fatalf("identity %+v", id)
	}
	compile := CompileImageTemplates()
	lead := compile.LeadFor("核心卖点图")
	if !strings.Contains(lead, "核心卖点图") || strings.Contains(lead, "{type_title}") {
		t.Fatalf("lead %q", lead)
	}
	if !strings.Contains(compile.FamilyLine("infographic"), "详情卖点图") {
		t.Fatalf("infographic %q", compile.Infographic)
	}
	required := compile.TextPolicyLine("required", "zh-CN")
	if !strings.Contains(required, "zh-CN") {
		t.Fatalf("text required %q", required)
	}
}
