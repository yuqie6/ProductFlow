package graph

import "testing"

func TestApplyDocumentSectionsOnlyReplacesSelectedBusinessSection(t *testing.T) {
	node := AppliedNode{
		ID: "brief", NodeType: NodeCreativeBrief, DocumentOrigin: OriginAuthored,
		Config: map[string]any{
			"goal": "人工目标", "key_messages": []any{"保留目标"},
			"required_elements": []any{"人工文案"}, "prohibitions": []any{"禁止虚构"},
		},
	}
	proposed := proposedDocumentConfig(node, map[string]any{
		"goal": "AI 目标", "key_messages": []any{"AI 目标 2"},
		"required_elements": []any{"AI 文案"}, "prohibitions": []any{"AI 限制"},
	}, DocumentActionRewrite)
	applied, err := applyDocumentSections(node, proposed, []string{"requirements"})
	if err != nil {
		t.Fatal(err)
	}
	if applied["goal"] != "人工目标" || !documentValueEqual(applied["key_messages"], []any{"保留目标"}) {
		t.Fatalf("unselected objective changed: %+v", applied)
	}
	if !documentValueEqual(applied["required_elements"], []any{"AI 文案"}) {
		t.Fatalf("selected copy not applied: %+v", applied)
	}
	if !documentValueEqual(applied["prohibitions"], []any{"禁止虚构"}) {
		t.Fatalf("unselected guardrails changed: %+v", applied)
	}
}

func TestApplyDocumentSectionsDeletesMissingSelectedBriefField(t *testing.T) {
	node := AppliedNode{
		ID: "brief", NodeType: NodeCreativeBrief, DocumentOrigin: OriginAuthored,
		Config: map[string]any{
			"goal": "人工目标", "required_elements": []any{"必须保留的旧文案"},
		},
	}
	candidate := map[string]any{"goal": "AI 目标"}

	applied, err := applyDocumentSections(node, candidate, []string{"requirements"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := applied["required_elements"]; exists {
		t.Fatalf("selected copy section retained a field removed by the candidate: %+v", applied)
	}
	if applied["goal"] != "人工目标" {
		t.Fatalf("unselected objective changed: %+v", applied)
	}
}

func TestMergeGeneratedBriefRewriteRemovesOmittedDocumentField(t *testing.T) {
	current := map[string]any{
		"goal": "人工目标", "required_elements": []any{"旧文案"}, "metadata": "保留",
	}
	generated := map[string]any{"goal": "AI 目标"}

	merged := mergeGeneratedBrief(current, generated, DocumentActionRewrite, OriginAuthored)
	if _, exists := merged["required_elements"]; exists {
		t.Fatalf("rewrite retained an omitted document field: %+v", merged)
	}
	if merged["goal"] != "AI 目标" || merged["metadata"] != "保留" {
		t.Fatalf("rewrite produced unexpected config: %+v", merged)
	}
}

func TestDocumentBaseHashChangesAfterManualEdit(t *testing.T) {
	node := AppliedNode{
		ID: "prompt", NodeType: NodeImagePrompt, DocumentOrigin: OriginAuthored,
		Config: map[string]any{"image_type_key": "hero", "prompt": map[string]any{"design_goal": "原目标"}},
	}
	before := documentBaseHash(node)
	node.Config["prompt"] = map[string]any{"design_goal": "人工修改"}
	after := documentBaseHash(node)
	if before == after {
		t.Fatal("manual edit must invalidate the candidate base hash")
	}
}

func TestDocumentSectionsExposeStableBusinessKeys(t *testing.T) {
	got := documentSections(NodeImagePrompt)
	want := []string{"objective", "subject", "composition", "visual_style", "copy", "constraints"}
	if len(got) != len(want) {
		t.Fatalf("sections %+v", got)
	}
	for index, key := range want {
		if got[index].Key != key {
			t.Fatalf("section %d = %s, want %s", index, got[index].Key, key)
		}
	}
}

func TestDocumentSectionsCoverLivePromptFields(t *testing.T) {
	covered := map[string]struct{}{}
	for _, section := range documentSections(NodeImagePrompt) {
		for _, field := range section.Fields {
			covered[field] = struct{}{}
		}
	}
	for _, field := range []string{
		"design_goal", "shared_rules", "creative_boundary", "product_fidelity",
		"composition", "content", "text", "atmosphere",
	} {
		if _, ok := covered[field]; !ok {
			t.Fatalf("prompt field %s is not in any document section", field)
		}
	}
	for _, field := range []string{"subject", "visual_style", "copy_overlay", "constraints"} {
		if _, ok := covered[field]; ok {
			t.Fatalf("retired prompt field %s should not be a document section field", field)
		}
	}
}

func TestApplyDocumentSectionsPromptCompositionKeepsContent(t *testing.T) {
	node := AppliedNode{
		ID: "prompt", NodeType: NodeImagePrompt, DocumentOrigin: OriginAuthored,
		Config: map[string]any{
			"prompt": map[string]any{
				"design_goal": "人工目标",
				"composition": map[string]any{"layout": "居中", "product_share_percent": 70.0},
				"content":     map[string]any{"background": "白底"},
			},
		},
	}
	proposed := proposedDocumentConfig(node, map[string]any{
		"design_goal": "AI 目标",
		"composition": map[string]any{"layout": "左侧主体", "product_share_percent": 58.0, "viewpoint": "平视"},
		"content":     map[string]any{"background": "木桌"},
	}, DocumentActionRewrite)
	applied, err := applyDocumentSections(node, proposed, []string{"composition"})
	if err != nil {
		t.Fatal(err)
	}
	prompt, _ := applied["prompt"].(map[string]any)
	if prompt["design_goal"] != "人工目标" {
		t.Fatalf("unselected objective changed: %+v", prompt)
	}
	content, _ := prompt["content"].(map[string]any)
	if content["background"] != "白底" {
		t.Fatalf("unselected content changed: %+v", prompt)
	}
	composition, _ := prompt["composition"].(map[string]any)
	if composition["layout"] != "左侧主体" {
		t.Fatalf("selected composition not applied: %+v", prompt)
	}
}
