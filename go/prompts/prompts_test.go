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
	if !strings.Contains(SourceNoteInstructions(), "merchant-editable product source note") {
		t.Fatal("source-note instructions missing")
	}
	if !strings.Contains(SourceNoteInstructions(), "可重点强调") || !strings.Contains(SourceNoteInstructions(), "Do not add photography or listing direction") {
		t.Fatal("source-note must forbid listing-direction tails")
	}
	if !strings.Contains(SourceNoteInstructions(), "2–4 selling points") || !strings.Contains(SourceNoteInstructions(), "Do not write a dry anatomy list") {
		t.Fatal("source-note must extract buyer-facing selling points")
	}
	if !strings.Contains(SourceNoteInstructions(), "only a parts inventory") {
		t.Fatal("source-note must rewrite a dry current note into selling points")
	}
	if strings.Contains(SourceNoteInstructions(), "and what to emphasize") {
		t.Fatal("visible must not ask the model what to emphasize")
	}
	if !strings.Contains(BriefInstructions(), "You write one ecommerce listing brief") {
		t.Fatal("brief instructions missing")
	}
	if !strings.Contains(OverlayInstructions(), "You write a compact visual overlay") {
		t.Fatal("overlay instructions missing")
	}
	if !strings.Contains(PromptInstructions(), "You art-direct one high-converting ecommerce image") {
		t.Fatal("prompt instructions missing")
	}
	look := ListingLook()
	if !strings.Contains(look.Rule, "做成能点击的商业套图") {
		t.Fatalf("look rule %q", look.Rule)
	}
	if look.ProductSharePercent != "photography 55-75; infographic 40-55" || look.BenefitCount != "photography 0-3; infographic 1-5" || !look.SourceNoteIsProductFact {
		t.Fatalf("look context %+v", look)
	}
	if !strings.Contains(look.Rule, "卖点图") {
		t.Fatalf("look rule must name selling-point division: %q", look.Rule)
	}
	if len(look.BriefProhibitions) != 1 {
		t.Fatalf("brief prohibitions %+v", look.BriefProhibitions)
	}
	types := ImageTypes()
	if len(types) != 15 {
		t.Fatalf("image types %d", len(types))
	}
	selling, ok := ImageTypeByKey("selling_point")
	if !ok || !strings.Contains(selling.Job, "详情转化") || selling.Title != "核心卖点图" {
		t.Fatalf("selling_point %+v ok=%v", selling, ok)
	}
	hero, ok := ImageTypeByKey("hero")
	if !ok || hero.Title != "封面主图" || hero.Order != 0 {
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
	heroLine := compile.TypeLine("hero")
	if !strings.Contains(heroLine, "封面") || strings.Contains(heroLine, "双列") || strings.Contains(heroLine, "信息流") {
		t.Fatalf("hero type line %q", heroLine)
	}
	sellingLine := compile.TypeLine("selling_point")
	if !strings.Contains(sellingLine, "一页") || !strings.Contains(sellingLine, "小图标") || !strings.Contains(sellingLine, "信任") {
		t.Fatalf("selling_point type line %q", sellingLine)
	}
	sceneLine := compile.TypeLine("scene")
	if !strings.Contains(sceneLine, "真实环境") || !strings.Contains(sceneLine, "暖白台面") {
		t.Fatalf("scene type line %q", sceneLine)
	}
	detailLine := compile.TypeLine("detail")
	if !strings.Contains(detailLine, "特写") || !strings.Contains(detailLine, "裁切") {
		t.Fatalf("detail type line %q", detailLine)
	}
	faqLine := compile.TypeLine("faq")
	if !strings.Contains(faqLine, "问答") || strings.Contains(faqLine, "详情转化卖点") {
		t.Fatalf("faq must stay a Q&A module: %q", faqLine)
	}
	if !strings.Contains(compile.FamilyLine("infographic"), "资料模块") {
		t.Fatalf("infographic family fallback %q", compile.Infographic)
	}
	for _, needle := range []string{"3 到 5 条", "小图标"} {
		if !strings.Contains(selling.Job, needle) {
			t.Fatalf("selling point job missing %q: %q", needle, selling.Job)
		}
	}
	if !strings.Contains(selling.Job, "完整设计") || !strings.Contains(selling.Job, "买家问题") {
		t.Fatalf("selling point quality contract incomplete: %q", selling.Job)
	}
	if !strings.Contains(hero.Job, "买家问题") || !strings.Contains(hero.Job, "失败") || !strings.Contains(hero.Job, "工艺") {
		t.Fatalf("hero quality contract incomplete: %q", hero.Job)
	}
	if strings.Contains(compile.FamilyLine("photography"), "主光从左上") || strings.Contains(compile.FamilyLine("photography"), "暖白或类目色底") {
		t.Fatalf("photography must not force one lighting and palette: %q", compile.Photography)
	}
	if !strings.Contains(OverlayInstructions(), "incidental tabletops") {
		t.Fatal("overlay instructions must reject incidental source-photo colors")
	}
	if !strings.Contains(OverlayInstructions(), "same background color") {
		t.Fatal("overlay instructions must not lock every shot to one background")
	}
	if !strings.Contains(PromptInstructions(), "Do not add generic rendering-defect lists") || !strings.Contains(PromptInstructions(), "one strong visual concept") {
		t.Fatal("prompt instructions must prioritize art direction over guardrail lists")
	}
	if !strings.Contains(PromptInstructions(), "complete ecommerce conversion page") || !strings.Contains(PromptInstructions(), "compact trust strip") {
		t.Fatal("prompt instructions must preserve infographic conversion hierarchy")
	}
	if !strings.Contains(PromptInstructions(), "hero: a commercial product cover") || !strings.Contains(PromptInstructions(), "scene: sell ownership") || !strings.Contains(PromptInstructions(), "detail: prove craft") {
		t.Fatal("prompt instructions must branch by image type")
	}
	if strings.Contains(PromptInstructions(), "two-column") || strings.Contains(PromptInstructions(), "search-grid") {
		t.Fatal("hero must not describe shopping-app layout as the image job")
	}
	if !strings.Contains(BriefInstructions(), "Preserve explicit user prohibitions") || !strings.Contains(OverlayInstructions(), "Shared restrictions belong to the creative brief") {
		t.Fatal("brief must own supplied restrictions without duplicating them in visual style")
	}
	if !strings.Contains(BriefInstructions(), "fact_gaps") {
		t.Fatal("brief instructions must collect fact gaps")
	}
	required := compile.TextPolicyLine("required", "zh-CN", "photography")
	if !strings.Contains(required, "zh-CN") || !strings.Contains(required, "逐字") || !strings.Contains(required, "简短标题") {
		t.Fatalf("photography text required %q", required)
	}
	infoRequired := compile.TextPolicyLine("required", "zh-CN", "infographic")
	if !strings.Contains(infoRequired, "详情模块") || strings.Contains(infoRequired, "像封面上的一句利益") {
		t.Fatalf("infographic text required %q", infoRequired)
	}
}
