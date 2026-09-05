package graph

import "testing"

func TestImageStyleOverridesReplaceOnlyPresentFields(t *testing.T) {
	series := map[string]any{"style": []any{"冷色摄影"}, "colors": []any{map[string]any{"role": "background", "value": "#eeeeee"}}}
	local := map[string]any{"style": []any{}}
	got := mergeImageVisual(series, local)
	if !documentValueEqual(got["style"], []any{}) || !documentValueEqual(got["colors"], series["colors"]) {
		t.Fatalf("explicit clear must preserve unrelated series settings: %+v", got)
	}
	delete(local, "style")
	if !documentValueEqual(mergeImageVisual(series, local), series) {
		t.Fatal("restoring inheritance must recover series values")
	}
	if !documentValueEqual(series["style"], []any{"冷色摄影"}) {
		t.Fatal("local edit mutated the shared series")
	}
}

func TestImageProjectionShowsInheritedStyleWithoutLocalReplacement(t *testing.T) {
	g := imageDocumentGraph()
	g.Nodes = append(g.Nodes, AppliedNode{ID: "style", NodeType: NodeVisualSystem, Config: map[string]any{
		"visual_overlay": map[string]any{"style": []any{"系列冷色"}},
	}})
	g.Edges = append(g.Edges, AppliedEdge{ID: "style-one", SourceNodeID: "style", TargetNodeID: "one", Role: RoleVisualGuidance, DataType: DataVisualSystem})
	g.Nodes[1].Config["visual_overlay"] = map[string]any{"style": []any{"本图暖色"}}
	sources := map[string]SourceRecord{"style": {VisualPayload: map[string]any{"style": []any{"旧风格"}, "colors": []any{map[string]any{"role": "background", "value": "#eeeeee"}}}}}
	view := buildProjection(graphRow{}, g, nil, false, false, nil, nil, nil, sources, nil)
	for _, node := range view.Nodes {
		if node.ID != "one" {
			continue
		}
		if node.ImageInput == nil || !documentValueEqual(node.ImageInput.InheritedVisual["style"], []any{"系列冷色"}) {
			t.Fatalf("projection must show the series value being replaced: %+v", node.ImageInput)
		}
		if !documentValueEqual(node.ImageInput.InheritedVisual["colors"], sources["style"].VisualPayload["colors"]) {
			t.Fatal("inherited colors from published style missing")
		}
		return
	}
	t.Fatal("image projection missing")
}

func imageDocumentGraph() AppliedGraph {
	return AppliedGraph{Nodes: []AppliedNode{
		{ID: "plan", NodeType: NodeImagePrompt, Config: map[string]any{
			"text_settings": map[string]any{"policy": "required", "language": "zh-CN"},
			"prompt":        map[string]any{"design_goal": "卖点", "content": map[string]any{"background": "棚拍", "focus": []any{"商品"}}, "text": map[string]any{"headline": "基础标题"}},
		}},
		{ID: "one", NodeType: NodeImageGeneration, Config: FillDefaultNodeConfig(NodeImageGeneration, nil)},
		{ID: "two", NodeType: NodeImageGeneration, Config: FillDefaultNodeConfig(NodeImageGeneration, nil)},
	}, Edges: []AppliedEdge{
		{ID: "p1", SourceNodeID: "plan", TargetNodeID: "one", Role: RolePrompt, DataType: DataPrompt},
		{ID: "p2", SourceNodeID: "plan", TargetNodeID: "two", Role: RolePrompt, DataType: DataPrompt},
	}}
}

func TestImageOverridesInheritLeavesAndRestoreWithoutMutatingPlan(t *testing.T) {
	g := imageDocumentGraph()
	digest := func(id string) string {
		t.Helper()
		d, err := compileImageRuntime(g, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	first := digest("one")
	base := cloneMap(g.Nodes[0].Config)
	g.Nodes[2].Config["prompt_overrides"] = map[string]any{"content": map[string]any{"background": "厨房"}}
	g.Nodes[2].Config["text_override"] = map[string]any{"policy": "required", "language": "en-US", "headline": "Local title"}
	payload, spec, _, err := resolveImageDocument(g, "two", nil)
	if err != nil {
		t.Fatal(err)
	}
	if asMapOrNil(payload["content"])["background"] != "厨房" || asMapOrNil(payload["text"])["headline"] != "Local title" || spec["text_language"] != "en-US" {
		t.Fatalf("resolved %+v %+v", payload, spec)
	}
	if !documentValueEqual(base, g.Nodes[0].Config) || digest("one") != first {
		t.Fatal("local override changed shared plan or sibling")
	}
	plan := asMapOrNil(g.Nodes[0].Config["prompt"])
	plan["content"] = map[string]any{"background": "白底", "focus": []any{"杯盖"}}
	g.Nodes[0].Config["prompt"] = plan
	payload, _, _, _ = resolveImageDocument(g, "two", nil)
	content := asMapOrNil(payload["content"])
	if content["background"] != "厨房" || !documentValueEqual(content["focus"], []any{"杯盖"}) {
		t.Fatalf("inheritance %+v", content)
	}
	delete(g.Nodes[2].Config, "prompt_overrides")
	delete(g.Nodes[2].Config, "text_override")
	payload, spec, _, _ = resolveImageDocument(g, "two", nil)
	if asMapOrNil(payload["content"])["background"] != "白底" || asMapOrNil(payload["text"])["headline"] != "基础标题" || spec["text_language"] != "zh-CN" {
		t.Fatalf("restore %+v %+v", payload, spec)
	}
}

func TestImageTextOverrideNoneAndEmptyLeavesAreExplicit(t *testing.T) {
	g := imageDocumentGraph()
	g.Nodes[1].Config["prompt_overrides"] = map[string]any{"content": map[string]any{"background": "", "focus": []any{}}}
	g.Nodes[1].Config["text_override"] = map[string]any{"policy": "none", "language": nil}
	payload, spec, _, err := resolveImageDocument(g, "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	if asMapOrNil(payload["content"])["background"] != "" || spec["text_policy"] != "none" || anyTextField(asMapOrNil(payload["text"])) {
		t.Fatalf("explicit clear %+v %+v", payload, spec)
	}
	if !anyTextField(asMapOrNil(asMapOrNil(g.Nodes[0].Config["prompt"])["text"])) {
		t.Fatal("cleared source copy")
	}
}

func TestPlanTextSettingsAffectDigestButDoNotRewriteDocumentOrigin(t *testing.T) {
	g := imageDocumentGraph()
	before, _ := compileImageRuntime(g, "one", nil)
	previous := cloneMap(g.Nodes[0].Config)
	g.Nodes[0].Config["text_settings"] = map[string]any{"policy": "none", "language": nil}
	after, _ := compileImageRuntime(g, "one", nil)
	if before == after {
		t.Fatal("text setting ignored by image digest")
	}
	if documentVisibleChanged(NodeImagePrompt, previous, g.Nodes[0].Config) {
		t.Fatal("settings edit claimed authored document")
	}
	generated := proposedDocumentConfig(g.Nodes[0], map[string]any{"design_goal": "新方案"}, DocumentActionReplace)
	if !documentValueEqual(generated["text_settings"], g.Nodes[0].Config["text_settings"]) {
		t.Fatal("candidate replaced user text settings")
	}
}

func TestRetiredTextParametersAndConstraintOverridesAreRejected(t *testing.T) {
	for _, key := range []string{"text_policy", "text_language"} {
		spec := cloneMap(defaultGenerationSpec)
		spec[key] = "none"
		if _, err := NormalizeNodeConfig(NodeImageGeneration, map[string]any{"generation_spec": spec}); err == nil {
			t.Fatalf("accepted retired %s", key)
		}
	}
	if _, err := NormalizeNodeConfig(NodeImageGeneration, map[string]any{"prompt_overrides": map[string]any{"product_fidelity": map[string]any{}}}); err == nil {
		t.Fatal("allowed overriding product constraints")
	}
	if _, err := NormalizeNodeConfig(NodeImagePrompt, map[string]any{"text_settings": map[string]any{"policy": "required", "language": ""}}); err == nil {
		t.Fatal("accepted missing text language")
	}
}

func TestImageDigestUsesEffectiveOverrides(t *testing.T) {
	g := imageDocumentGraph()
	before, err := compileImageRuntime(g, "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes[1].Config["prompt_overrides"] = map[string]any{"content": map[string]any{}}
	after, err := compileImageRuntime(g, "one", nil)
	if err != nil || before != after {
		t.Fatalf("empty override changed input: %v", err)
	}
	g.Nodes[1].Config["prompt_overrides"] = map[string]any{"content": map[string]any{"background": "厨房"}}
	before, _ = compileImageRuntime(g, "one", nil)
	g.Nodes[0].Config["prompt"].(map[string]any)["content"].(map[string]any)["background"] = "户外"
	after, _ = compileImageRuntime(g, "one", nil)
	if before != after {
		t.Fatal("overridden upstream leaf changed effective input")
	}
}

func TestBriefRequirementsSurviveLocalOverridesAndNoText(t *testing.T) {
	g := imageDocumentGraph()
	g.Nodes = append(g.Nodes, AppliedNode{ID: "brief", NodeType: NodeCreativeBrief, Config: map[string]any{
		"required_elements": []any{"展示杯盖"}, "prohibitions": []any{"不得宣称医疗功效"},
	}})
	g.Edges = append(g.Edges, AppliedEdge{ID: "brief-plan", SourceNodeID: "brief", TargetNodeID: "plan", Role: RoleBrief, DataType: DataCreativeBrief})
	g.Nodes[1].Config["text_override"] = map[string]any{"policy": "none", "language": nil}
	g.Nodes[1].Config["prompt_overrides"] = map[string]any{"content": map[string]any{"background": "厨房"}}
	payload, _, _, err := resolveImageDocument(g, "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !documentValueEqual(payload["shared_rules"], []any{"展示杯盖"}) || !documentValueEqual(payload["creative_boundary"], []any{"不得宣称医疗功效"}) {
		t.Fatalf("shared requirements lost: %+v", payload)
	}
	before, _ := compileImageRuntime(g, "one", nil)
	g.Nodes[3].Config["required_elements"] = []any{"展示杯底"}
	after, _ := compileImageRuntime(g, "one", nil)
	if before == after {
		t.Fatal("connected brief requirement missing from image digest")
	}
}
