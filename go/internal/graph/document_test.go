package graph

import "testing"

func TestDocumentOriginStampsSeedOnCreate(t *testing.T) {
	if got := stampDocumentOriginOnCreate(NodeCreativeBrief, nil); got != OriginSeed {
		t.Fatalf("origin %s", got)
	}
	requested := OriginGenerated
	if got := stampDocumentOriginOnCreate(NodeCreativeBrief, &requested); got != OriginGenerated {
		t.Fatalf("requested origin %s", got)
	}
}

func TestDocumentOriginDoesNotInferSeedWhenColumnIsMissing(t *testing.T) {
	node := AppliedNode{NodeType: NodeImagePrompt, Config: map[string]any{}}
	if got := DocumentOrigin(node); got != "" {
		t.Fatalf("missing origin %q", got)
	}
	if contentNodeShouldGenerate(node, false, DocumentActionComplete) {
		t.Fatal("missing origin must not be treated as a generation seed")
	}
	if got := documentOriginPtr(node); got != nil {
		t.Fatalf("missing origin pointer %+v", got)
	}
}

func TestApplyDocumentOriginMarksCollaborativeOnGeneratedDocumentEdit(t *testing.T) {
	prev := AppliedNode{NodeType: NodeCreativeBrief, Config: map[string]any{"goal": "卖"}, DocumentOrigin: OriginGenerated}
	next := map[string]any{"goal": "改过"}
	normalized, err := NormalizeNodeConfig(NodeCreativeBrief, next)
	if err != nil {
		t.Fatal(err)
	}
	if got := nextDocumentOrigin(NodeCreativeBrief, prev, normalized, nil); got != OriginCollaborative {
		t.Fatalf("origin %s", got)
	}
}

func TestApplyDocumentOriginKeepsPreviousWhenUnchanged(t *testing.T) {
	prev := AppliedNode{NodeType: NodeCreativeBrief, Config: map[string]any{"goal": "卖"}, DocumentOrigin: OriginGenerated}
	next := map[string]any{"goal": "卖"}
	normalized, err := NormalizeNodeConfig(NodeCreativeBrief, next)
	if err != nil {
		t.Fatal(err)
	}
	if got := nextDocumentOrigin(NodeCreativeBrief, prev, normalized, nil); got != OriginGenerated {
		t.Fatalf("origin %s", got)
	}
}

func TestBirthPromptDesignGoalRemainsSeed(t *testing.T) {
	key := "hero"
	node := AppliedNode{
		NodeType:       NodeImagePrompt,
		DocumentOrigin: OriginSeed,
		Config: map[string]any{
			"image_type_key": key,
			"prompt":         map[string]any{"design_goal": imageTypePromptGoal(key)},
		},
	}
	if DocumentOrigin(node) != OriginSeed {
		t.Fatalf("origin %s", DocumentOrigin(node))
	}
	if !contentNodeShouldGenerate(node, false, DocumentActionComplete) {
		t.Fatal("seed prompt must generate on fill")
	}
	if got := inferDocumentOriginFromConfig(NodeImagePrompt, node.Config); got != OriginSeed {
		t.Fatalf("infer %s", got)
	}
}

func TestInferDocumentOriginFromConfigMatchesSeedTemplates(t *testing.T) {
	if got := inferDocumentOriginFromConfig(NodeCreativeBrief, map[string]any{}); got != OriginSeed {
		t.Fatalf("empty brief %s", got)
	}
	note := "棉麻夏装，面向通勤"
	seedBrief := creativeBriefConfigFromSourceNote(&note)
	if got := inferDocumentOriginFromConfig(NodeCreativeBrief, seedBrief); got != OriginSeed {
		t.Fatalf("source-note brief %s", got)
	}
	if got := inferDocumentOriginFromConfig(NodeCreativeBrief, map[string]any{"goal": "手填卖点"}); got != OriginAuthored {
		t.Fatalf("authored brief %s", got)
	}
	if got := inferDocumentOriginFromConfig(NodeVisualSystem, map[string]any{}); got != OriginSeed {
		t.Fatalf("empty visual %s", got)
	}
	if got := inferDocumentOriginFromConfig(NodeVisualSystem, map[string]any{
		"visual_overlay": map[string]any{"style": []any{"干净白底"}},
	}); got != OriginAuthored {
		t.Fatalf("overlay visual %s", got)
	}
	hero := map[string]any{
		"image_type_key": "hero",
		"prompt":         map[string]any{"design_goal": imageTypePromptGoal("hero")},
	}
	if got := inferDocumentOriginFromConfig(NodeImagePrompt, hero); got != OriginSeed {
		t.Fatalf("birth prompt %s", got)
	}
	authoredPrompt := map[string]any{
		"image_type_key": "hero",
		"prompt": map[string]any{
			"design_goal": imageTypePromptGoal("hero"),
			"composition": map[string]any{"layout": "左侧留白"},
		},
	}
	if got := inferDocumentOriginFromConfig(NodeImagePrompt, authoredPrompt); got != OriginAuthored {
		t.Fatalf("composed prompt %s", got)
	}
}

func TestAuthoredPromptDoesNotGenerateWithoutForce(t *testing.T) {
	node := AppliedNode{
		NodeType:       NodeImagePrompt,
		DocumentOrigin: OriginAuthored,
		Config: map[string]any{
			"image_type_key": "hero",
			"prompt": map[string]any{
				"design_goal": "自定义",
				"composition": map[string]any{"layout": "左侧留白"},
			},
		},
	}
	if contentNodeShouldGenerate(node, false, DocumentActionComplete) {
		t.Fatal("authored prompt must not generate")
	}
	if !contentNodeShouldGenerate(node, true, DocumentActionReplace) {
		t.Fatal("force replace must generate")
	}
	if !contentNodeShouldGenerate(node, true, DocumentActionRewrite) {
		t.Fatal("force refine must generate")
	}
}

func TestMergeGeneratedPromptRewriteReplacesDocumentFields(t *testing.T) {
	current := map[string]any{
		"prompt": map[string]any{
			"design_goal": "手填目标",
			"composition": map[string]any{"layout": "左侧留白", "product_share_percent": 40},
			"text":        map[string]any{"headline": "夏日", "subtitle": nil, "body": nil},
		},
	}
	generated := map[string]any{
		"design_goal": "模型目标",
		"composition": map[string]any{"layout": "居中", "viewpoint": "正面", "product_share_percent": 90},
		"text":        map[string]any{"headline": "模型标题", "subtitle": "副标题", "body": nil},
		"content":     map[string]any{"background": "干净背景"},
	}
	got := mergeGeneratedPrompt(current, generated, DocumentActionRewrite, OriginAuthored)
	prompt, _ := got["prompt"].(map[string]any)
	if prompt["design_goal"] != "模型目标" {
		t.Fatalf("design_goal %+v", prompt["design_goal"])
	}
	composition, _ := prompt["composition"].(map[string]any)
	if composition["layout"] != "居中" {
		t.Fatalf("layout %+v", composition["layout"])
	}
	if composition["viewpoint"] != "正面" {
		t.Fatalf("viewpoint %+v", composition["viewpoint"])
	}
	if composition["product_share_percent"] != 90 {
		t.Fatalf("share %+v", composition["product_share_percent"])
	}
	text, _ := prompt["text"].(map[string]any)
	if text["headline"] != "模型标题" {
		t.Fatalf("headline %+v", text["headline"])
	}
	if text["subtitle"] != "副标题" {
		t.Fatalf("subtitle %+v", text["subtitle"])
	}
	content, _ := prompt["content"].(map[string]any)
	if content["background"] != "干净背景" {
		t.Fatalf("background %+v", content["background"])
	}
}

func TestNormalizeRejectsRetiredDocumentKeys(t *testing.T) {
	_, err := NormalizeNodeConfig(NodeCreativeBrief, map[string]any{
		"goal":            "卖",
		"document_origin": OriginGenerated,
	})
	if err == nil {
		t.Fatal("retired document_origin must be rejected")
	}
}

func TestLiveDocumentDivergedFromSnapshot(t *testing.T) {
	snapshot := AppliedNode{NodeType: NodeCreativeBrief, Config: map[string]any{"goal": "卖"}}
	live := AppliedNode{NodeType: NodeCreativeBrief, Config: map[string]any{"goal": "用户中途改过"}}
	if !liveDocumentDivergedFromSnapshot(live, snapshot) {
		t.Fatal("changed goal must diverge")
	}
	if liveDocumentDivergedFromSnapshot(snapshot, snapshot) {
		t.Fatal("identical documents must not diverge")
	}
}

func TestMergeGeneratedPromptRewriteReplacesAuthoredSeedLookingGoal(t *testing.T) {
	current := map[string]any{
		"image_type_key": "hero",
		"prompt":         map[string]any{"design_goal": "手填目标"},
	}
	generated := map[string]any{
		"design_goal": "模型目标",
		"composition": map[string]any{"layout": "居中"},
	}
	got := mergeGeneratedPrompt(current, generated, DocumentActionRewrite, OriginAuthored)
	prompt, _ := got["prompt"].(map[string]any)
	if prompt["design_goal"] != "模型目标" {
		t.Fatalf("design_goal %+v", prompt["design_goal"])
	}
	composition, _ := prompt["composition"].(map[string]any)
	if composition["layout"] != "居中" {
		t.Fatalf("layout %+v", composition["layout"])
	}
}
