package graph

import "testing"

func TestApplyDocumentSectionsOnlyReplacesSelectedBusinessSection(t *testing.T) {
	node := AppliedNode{
		ID: "brief", NodeType: NodeCreativeBrief, DocumentOrigin: OriginAuthored,
		Config: map[string]any{
			"goal": "人工目标", "design_goals": []any{"保留目标"},
			"required_copy": []any{"人工文案"}, "prohibitions": []any{"禁止虚构"},
		},
	}
	proposed := proposedDocumentConfig(node, map[string]any{
		"goal": "AI 目标", "design_goals": []any{"AI 目标 2"},
		"required_copy": []any{"AI 文案"}, "prohibitions": []any{"AI 限制"},
	}, DocumentActionRewrite)
	applied, err := applyDocumentSections(node, proposed, []string{"copy"})
	if err != nil {
		t.Fatal(err)
	}
	if applied["goal"] != "人工目标" || !documentValueEqual(applied["design_goals"], []any{"保留目标"}) {
		t.Fatalf("unselected objective changed: %+v", applied)
	}
	if !documentValueEqual(applied["required_copy"], []any{"AI 文案"}) {
		t.Fatalf("selected copy not applied: %+v", applied)
	}
	if !documentValueEqual(applied["prohibitions"], []any{"禁止虚构"}) {
		t.Fatalf("unselected guardrails changed: %+v", applied)
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
