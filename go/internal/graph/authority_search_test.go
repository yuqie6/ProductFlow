package graph_test

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

const (
	mockAuthorityBriefGoal = "MOCK-AUTHORITY-BRIEF-GOAL"
	mockAuthorityLayout    = "MOCK-AUTHORITY-LAYOUT"
	userAuthorityLayout    = "USER-AUTHORITY-LAYOUT"
)

type authorityKind string

const (
	actAuthorBrief       authorityKind = "author_brief"
	actAuthorPrompt      authorityKind = "author_prompt"
	actRunGraph          authorityKind = "run_graph"
	actCookRewriteBrief  authorityKind = "cook_rewrite_brief"
	actCookReplacePrompt authorityKind = "cook_replace_prompt"
	actApplyAll          authorityKind = "apply_all"
	actApplyObjective    authorityKind = "apply_objective"
	actDiscard           authorityKind = "discard"
	actUndo              authorityKind = "undo"
	actStale             authorityKind = "stale"
)

var defaultSearchKinds = []authorityKind{
	actAuthorBrief,
	actRunGraph,
	actCookRewriteBrief,
	actApplyAll,
	actUndo,
	actStale,
}

type authorityAction struct {
	Kind authorityKind
}

func (a authorityAction) String() string { return string(a.Kind) }

type searchEnv struct {
	t            *testing.T
	gs           *graphServer
	productID    string
	graphID      string
	prompts      *countingPrompt
	images       *countingImage
	seq          int
	dirtyPending map[string]bool
}

func canvasSearchWalks() int {
	raw := strings.TrimSpace(os.Getenv("PRODUCTFLOW_CANVAS_SEARCH_WALKS"))
	if raw == "" {
		return 8
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 8
	}
	return n
}

func TestDocumentAuthoritySearch(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	kinds := defaultSearchKinds
	for _, first := range kinds {
		for _, second := range kinds {
			trace := []authorityAction{{Kind: first}, {Kind: second}}
			if hits := replayAuthorityTrace(t, gs, trace); len(hits) > 0 {
				shrunk := shrinkAuthorityTrace(t, gs, trace)
				t.Fatalf("depth-2 %s then %s: %s; shrunk %v", first, second, strings.Join(hits, "; "), shrunk)
			}
		}
	}
	walks := canvasSearchWalks()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pool := []authorityKind{
		actAuthorBrief, actAuthorPrompt, actRunGraph, actCookRewriteBrief, actCookReplacePrompt,
		actApplyAll, actApplyObjective, actDiscard, actUndo, actStale,
	}
	for i := 0; i < walks; i++ {
		depth := 1 + rng.Intn(3)
		trace := make([]authorityAction, depth)
		for j := range trace {
			trace[j] = authorityAction{Kind: pool[rng.Intn(len(pool))]}
		}
		if hits := replayAuthorityTrace(t, gs, trace); len(hits) > 0 {
			shrunk := shrinkAuthorityTrace(t, gs, trace)
			t.Fatalf("walk %d %v: %s; shrunk %v", i, trace, strings.Join(hits, "; "), shrunk)
		}
	}
}

func shrinkAuthorityTrace(t *testing.T, gs *graphServer, trace []authorityAction) []authorityAction {
	t.Helper()
	best := append([]authorityAction(nil), trace...)
	changed := true
	for changed && len(best) > 1 {
		changed = false
		for i := range best {
			candidate := append(append([]authorityAction(nil), best[:i]...), best[i+1:]...)
			if next := replayAuthorityTrace(t, gs, candidate); len(next) > 0 {
				best = candidate
				changed = true
				break
			}
		}
	}
	return best
}

func replayAuthorityTrace(t *testing.T, gs *graphServer, trace []authorityAction) []string {
	t.Helper()
	productID, graphID := gs.createDirectGraph(t)
	env := newSearchEnv(t, gs, productID, graphID)
	for _, step := range trace {
		if hits := env.apply(step); len(hits) > 0 {
			return append([]string{string(step.Kind)}, hits...)
		}
	}
	return nil
}

func authorityCountingPrompt() countingPrompt {
	return countingPrompt{MockPromptProvider: graph.MockPromptProvider{
		Brief: map[string]any{
			"goal": mockAuthorityBriefGoal, "design_goals": []any{"mock"},
			"required_copy": []any{}, "prohibitions": []any{}, "fact_gaps": []any{},
		},
		Prompt: map[string]any{
			"design_goal": "MOCK-AUTHORITY-PROMPT-GOAL",
			"composition": map[string]any{"layout": mockAuthorityLayout, "product_share_percent": 70, "copy_regions": []any{}},
		},
	}}
}

func newSearchEnv(t *testing.T, gs *graphServer, productID, graphID string) *searchEnv {
	t.Helper()
	prompt := authorityCountingPrompt()
	return &searchEnv{
		t:            t,
		gs:           gs,
		productID:    productID,
		graphID:      graphID,
		prompts:      &prompt,
		images:       &countingImage{},
		dirtyPending: map[string]bool{},
	}
}

func (e *searchEnv) apply(step authorityAction) []string {
	before := loadProjection(e.t, e.gs, e.productID, e.graphID)
	switch step.Kind {
	case actAuthorBrief:
		return e.author(before, graph.NodeCreativeBrief)
	case actAuthorPrompt:
		return e.author(before, graph.NodeImagePrompt)
	case actRunGraph:
		if !hasContentNodes(before) {
			return nil
		}
		return e.submitAndCheck(before, map[string]any{"scope": "graph"})
	case actCookRewriteBrief:
		node, ok := findContent(before, graph.NodeCreativeBrief)
		if !ok {
			return nil
		}
		return e.submitAndCheck(before, map[string]any{
			"scope": "node", "node_id": node.ID, "force": true, "document_action": "rewrite",
		})
	case actCookReplacePrompt:
		node, ok := findContent(before, graph.NodeImagePrompt)
		if !ok {
			return nil
		}
		return e.submitAndCheck(before, map[string]any{
			"scope": "node", "node_id": node.ID, "force": true, "document_action": "replace",
		})
	case actApplyAll:
		return e.applyCandidate(before, nil)
	case actApplyObjective:
		return e.applyCandidate(before, []string{"objective"})
	case actDiscard:
		return e.discardCandidate(before)
	case actUndo:
		return e.undo(before)
	case actStale:
		return e.staleWrite(before)
	default:
		return []string{"unknown action"}
	}
}

func (e *searchEnv) author(before graph.Projection, nodeType graph.NodeType) []string {
	node, ok := findContent(before, nodeType)
	if !ok {
		return nil
	}
	cfg := cloneConfig(e.t, node.Config)
	delete(cfg, "document_origin")
	e.seq++
	switch nodeType {
	case graph.NodeCreativeBrief:
		cfg["goal"] = fmt.Sprintf("USER-BRIEF-GOAL-%d", e.seq)
	case graph.NodeImagePrompt:
		prompt, _ := cfg["prompt"].(map[string]any)
		if prompt == nil {
			prompt = map[string]any{}
		}
		composition, _ := prompt["composition"].(map[string]any)
		if composition == nil {
			composition = map[string]any{}
		}
		composition["layout"] = userAuthorityLayout
		prompt["composition"] = composition
		prompt["design_goal"] = fmt.Sprintf("USER-PROMPT-GOAL-%d", e.seq)
		cfg["prompt"] = prompt
	}
	resp := e.gs.doJSON(e.t, "POST", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/changesets", map[string]any{
		"base_graph_revision": before.Revision,
		"summary":             "搜索器手填文稿",
		"operations": []map[string]any{
			{"op": "update_node_config", "node_ref": node.ID, "config": cfg},
		},
	})
	if resp.StatusCode != 200 {
		return []string{fmt.Sprintf("author %s status %d", nodeType, e.gs.readStatus(resp))}
	}
	e.gs.decode(e.t, resp, &graph.Projection{})
	if node.PendingCandidateArtifactID != nil {
		e.dirtyPending[node.ID] = true
	}
	return nil
}

func (e *searchEnv) submitAndCheck(before graph.Projection, body map[string]any) []string {
	resp := e.gs.doJSON(e.t, "POST", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/runs", body)
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil
		}
		return []string{fmt.Sprintf("submit run %d %s", resp.StatusCode, raw)}
	}
	var run graph.GraphRunResponse
	e.gs.decode(e.t, resp, &run)
	executor := graph.Executor{
		DB: e.gs.db,
		Deps: graph.Dependencies{
			Prompt: e.prompts,
			Image:  e.images,
			Assets: product.Service{DB: e.gs.db, Media: e.gs.media},
		},
	}
	if err := e.gs.tryExecuteLocally(e.t, run.ID, executor); err != nil {
		return []string{fmt.Sprintf("execute run: %v", err)}
	}
	got := e.gs.do(e.t, "GET", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/runs/"+run.ID, nil, "")
	e.gs.mustStatus(e.t, got, 200)
	var finished graph.GraphRunResponse
	e.gs.decode(e.t, got, &finished)
	after := loadProjection(e.t, e.gs, e.productID, e.graphID)
	return checkAuthorityAfterRun(before, after, finished, body)
}

func (e *searchEnv) applyCandidate(before graph.Projection, sections []string) []string {
	node, ok := firstPending(before)
	if !ok {
		return nil
	}
	resp := e.gs.doJSON(e.t, "POST", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/nodes/"+node.ID+"/candidate/apply", map[string]any{
		"artifact_id":         *node.PendingCandidateArtifactID,
		"base_graph_revision": before.Revision,
		"section_keys":        sections,
	})
	if e.dirtyPending[node.ID] {
		code := e.gs.readStatus(resp)
		if code == 200 {
			return []string{"O5 apply succeeded after live document changed"}
		}
		if code == 409 || code == 400 {
			return nil
		}
		return []string{fmt.Sprintf("O5 apply status %d", code)}
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 400 || resp.StatusCode == 409 {
			return nil
		}
		return []string{fmt.Sprintf("apply status %d %s", resp.StatusCode, raw)}
	}
	var after graph.Projection
	e.gs.decode(e.t, resp, &after)
	published := nodeByID(after, node.ID)
	if published.PendingCandidateArtifactID != nil {
		return []string{"apply left pending candidate"}
	}
	if len(sections) == 1 && sections[0] == "objective" {
		if hits := checkObjectiveOnly(node, published); len(hits) > 0 {
			return hits
		}
	}
	delete(e.dirtyPending, node.ID)
	return nil
}

func (e *searchEnv) discardCandidate(before graph.Projection) []string {
	node, ok := firstPending(before)
	if !ok {
		return nil
	}
	resp := e.gs.doJSON(e.t, "POST", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/nodes/"+node.ID+"/candidate/discard", map[string]any{
		"artifact_id": *node.PendingCandidateArtifactID,
	})
	if resp.StatusCode != 200 {
		return []string{fmt.Sprintf("discard status %d", e.gs.readStatus(resp))}
	}
	e.gs.decode(e.t, resp, &graph.Projection{})
	delete(e.dirtyPending, node.ID)
	after := loadProjection(e.t, e.gs, e.productID, e.graphID)
	got := nodeByID(after, node.ID)
	if got.PendingCandidateArtifactID != nil {
		return []string{"discard left pending candidate"}
	}
	if !jsonEqual(node.Config, got.Config) {
		return []string{"discard mutated live config"}
	}
	return nil
}

func (e *searchEnv) undo(before graph.Projection) []string {
	if !before.CanUndo {
		return nil
	}
	resp := e.gs.do(e.t, "POST", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/undo", nil, "")
	if resp.StatusCode != 200 {
		return []string{fmt.Sprintf("undo status %d", e.gs.readStatus(resp))}
	}
	e.gs.decode(e.t, resp, &graph.Projection{})
	return nil
}

func (e *searchEnv) staleWrite(before graph.Projection) []string {
	node, ok := findContent(before, graph.NodeCreativeBrief)
	if !ok {
		return nil
	}
	freshCfg := cloneConfig(e.t, node.Config)
	delete(freshCfg, "document_origin")
	freshCfg["goal"] = "fresh-same-node"
	first := e.gs.doJSON(e.t, "POST", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/changesets", map[string]any{
		"base_graph_revision": before.Revision,
		"summary":             "同节点先写",
		"operations": []map[string]any{
			{"op": "update_node_config", "node_ref": node.ID, "config": freshCfg},
		},
	})
	if first.StatusCode != 200 {
		return []string{fmt.Sprintf("O7 same-node first write status %d", e.gs.readStatus(first))}
	}
	e.gs.decode(e.t, first, &graph.Projection{})

	lostCfg := cloneConfig(e.t, node.Config)
	delete(lostCfg, "document_origin")
	lostCfg["goal"] = "stale-revision-write"
	resp := e.gs.doJSON(e.t, "POST", "/api/v3/products/"+e.productID+"/workflows/"+e.graphID+"/changesets", map[string]any{
		"base_graph_revision": before.Revision,
		"summary":             "过期 revision",
		"operations": []map[string]any{
			{"op": "update_node_config", "node_ref": node.ID, "config": lostCfg},
		},
	})
	code := e.gs.readStatus(resp)
	if code != 409 && code != 400 {
		return []string{fmt.Sprintf("O7 stale changeset status %d want 409", code)}
	}
	after := loadProjection(e.t, e.gs, e.productID, e.graphID)
	got := nodeByID(after, node.ID)
	if jsonEqual(got.Config["goal"], "stale-revision-write") {
		return []string{"O7 stale changeset wrote live config"}
	}
	if !jsonEqual(got.Config["goal"], "fresh-same-node") {
		return []string{fmt.Sprintf("O7 first write not kept: %+v", got.Config["goal"])}
	}
	return nil
}

func checkAuthorityAfterRun(before, after graph.Projection, run graph.GraphRunResponse, body map[string]any) []string {
	var hits []string
	action, _ := body["document_action"].(string)
	force, _ := body["force"].(bool)
	for _, node := range before.Nodes {
		if !isContent(node) {
			continue
		}
		live := nodeByID(after, node.ID)
		orig := nodeOrigin(node)
		if orig == graph.OriginAuthored || orig == graph.OriginGenerated || orig == graph.OriginCollaborative {
			if !force && !jsonEqual(visibleDocumentConfig(node), visibleDocumentConfig(live)) {
				hits = append(hits, fmt.Sprintf("O3 %s %s live config overwritten", node.NodeType, orig))
			}
			if !force && nodeOrigin(live) != orig && nodeOrigin(live) == graph.OriginGenerated && orig != graph.OriginGenerated {
				hits = append(hits, fmt.Sprintf("O3 %s origin %s -> %s", node.NodeType, orig, nodeOrigin(live)))
			}
		}
		if force && action != "" && bodyNodeID(body) == node.ID {
			if live.PendingCandidateArtifactID == nil {
				hits = append(hits, fmt.Sprintf("O4 %s missing pending candidate", node.NodeType))
			}
			if !jsonEqual(visibleDocumentConfig(node), visibleDocumentConfig(live)) {
				hits = append(hits, fmt.Sprintf("O4 %s live config changed before apply", node.NodeType))
			}
		}
	}
	for _, nodeRun := range run.NodeRuns {
		if nodeRun.NodeID == nil {
			continue
		}
		disp, _ := nodeRun.Output["disposition"].(string)
		if disp != "candidate" {
			continue
		}
		beforeNode := nodeByID(before, *nodeRun.NodeID)
		afterNode := nodeByID(after, *nodeRun.NodeID)
		if !jsonEqual(visibleDocumentConfig(beforeNode), visibleDocumentConfig(afterNode)) {
			hits = append(hits, fmt.Sprintf("O2 %s candidate disposition overwrote live", beforeNode.NodeType))
		}
		if afterNode.PendingCandidateArtifactID == nil {
			hits = append(hits, fmt.Sprintf("O2 %s missing pending after candidate disposition", beforeNode.NodeType))
		}
	}
	return hits
}

func checkObjectiveOnly(before, after graph.NodeView) []string {
	switch before.NodeType {
	case graph.NodeCreativeBrief:
		if !jsonEqual(before.Config["required_copy"], after.Config["required_copy"]) {
			return []string{"O6 brief copy changed on objective apply"}
		}
		if !jsonEqual(before.Config["prohibitions"], after.Config["prohibitions"]) {
			return []string{"O6 brief guardrails changed on objective apply"}
		}
	case graph.NodeImagePrompt:
		beforePrompt, _ := before.Config["prompt"].(map[string]any)
		afterPrompt, _ := after.Config["prompt"].(map[string]any)
		if !jsonEqual(beforePrompt["composition"], afterPrompt["composition"]) {
			return []string{"O6 prompt composition changed on objective apply"}
		}
	}
	return nil
}

func findContent(view graph.Projection, nodeType graph.NodeType) (graph.NodeView, bool) {
	for _, node := range view.Nodes {
		if node.NodeType == nodeType {
			return node, true
		}
	}
	return graph.NodeView{}, false
}

func hasContentNodes(view graph.Projection) bool {
	for _, node := range view.Nodes {
		if isContent(node) {
			return true
		}
	}
	return false
}

func visibleDocumentConfig(node graph.NodeView) map[string]any {
	cfg := node.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	switch node.NodeType {
	case graph.NodeCreativeBrief:
		return map[string]any{
			"goal":          cfg["goal"],
			"design_goals":  cfg["design_goals"],
			"required_copy": cfg["required_copy"],
			"prohibitions":  cfg["prohibitions"],
			"fact_gaps":     cfg["fact_gaps"],
		}
	case graph.NodeVisualSystem:
		return map[string]any{
			"style":        cfg["style"],
			"colors":       cfg["colors"],
			"prohibitions": cfg["prohibitions"],
		}
	case graph.NodeImagePrompt:
		prompt, _ := cfg["prompt"].(map[string]any)
		if prompt == nil {
			prompt = map[string]any{}
		}
		return map[string]any{
			"design_goal": prompt["design_goal"],
			"composition": prompt["composition"],
		}
	default:
		return map[string]any{}
	}
}

func firstPending(view graph.Projection) (graph.NodeView, bool) {
	for _, node := range view.Nodes {
		if node.PendingCandidateArtifactID != nil {
			return node, true
		}
	}
	return graph.NodeView{}, false
}

func nodeByID(view graph.Projection, id string) graph.NodeView {
	for _, node := range view.Nodes {
		if node.ID == id {
			return node
		}
	}
	return graph.NodeView{}
}

func nodeOrigin(node graph.NodeView) string {
	if node.DocumentOrigin == nil {
		return ""
	}
	return *node.DocumentOrigin
}

func isContent(node graph.NodeView) bool {
	switch node.NodeType {
	case graph.NodeCreativeBrief, graph.NodeVisualSystem, graph.NodeImagePrompt:
		return true
	default:
		return false
	}
}

func bodyNodeID(body map[string]any) string {
	raw, _ := body["node_id"].(string)
	return raw
}

func jsonEqual(a, b any) bool {
	ra, err := json.Marshal(a)
	if err != nil {
		return false
	}
	rb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var xa, xb any
	if err := json.Unmarshal(ra, &xa); err != nil {
		return false
	}
	if err := json.Unmarshal(rb, &xb); err != nil {
		return false
	}
	return reflect.DeepEqual(xa, xb)
}
