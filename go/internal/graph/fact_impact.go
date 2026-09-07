package graph

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// FactImpactNode 是事实变更预览里的一个依赖图位（IQ-CF-03 / CF-B2）。
// 只经 RoleFacts 入边可达；未连边节点不会出现。
type FactImpactNode struct {
	NodeID                   string   `json:"node_id"`
	NodeType                 string   `json:"node_type"`
	Title                    string   `json:"title"`
	ImageTypeKey             string   `json:"image_type_key,omitempty"`
	DependsOnChangedKeys     []string `json:"depends_on_changed_keys"`
	DefaultSelected          bool     `json:"default_selected"`
	HasArtifact              bool     `json:"has_artifact"`
	CurrentArtifactID        *string  `json:"current_artifact_id,omitempty"`
	CurrentInputDigest       *string  `json:"current_input_digest,omitempty"`
	BoundFactSetVersionID    *string  `json:"bound_fact_set_version_id,omitempty"`
	ArtifactFactSetVersionID *string  `json:"artifact_fact_set_version_id,omitempty"`
	Reason                   string   `json:"reason"`
}

// FactImpactPreview 是保存前影响预览结果。
type FactImpactPreview struct {
	ProductID               string           `json:"product_id"`
	ChangedFactKeys         []string         `json:"changed_fact_keys"`
	CurrentFactSetVersionID *string          `json:"current_fact_set_version_id,omitempty"`
	Nodes                   []FactImpactNode `json:"nodes"`
	DefaultUpdateNodeIDs    []string         `json:"default_update_node_ids"`
	Explanation             string           `json:"explanation"`
}

// DiffFactKeys 比较当前与拟议 facts，返回值变化或增删的 key（大小写折叠后用当前 key 原文）。
func DiffFactKeys(current, proposed []map[string]any) []string {
	cur := factValueByKey(current)
	prop := factValueByKey(proposed)
	changed := map[string]string{}
	for folded, item := range cur {
		next, ok := prop[folded]
		if !ok || factValueText(item.value) != factValueText(next.value) {
			changed[folded] = item.key
		}
	}
	for folded, item := range prop {
		if _, ok := cur[folded]; !ok {
			changed[folded] = item.key
		}
	}
	out := make([]string, 0, len(changed))
	for _, key := range changed {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

type factKeyValue struct {
	key   string
	value any
}

func factValueByKey(facts []map[string]any) map[string]factKeyValue {
	out := map[string]factKeyValue{}
	for _, fact := range facts {
		key := strings.TrimSpace(factKeyString(fact["key"]))
		if key == "" {
			continue
		}
		out[strings.ToLower(key)] = factKeyValue{key: key, value: fact["value"]}
	}
	return out
}

func factKeyString(v any) string {
	s, _ := v.(string)
	return s
}

// PreviewFactImpact 只扫 RoleFacts 入边可达的文稿/生图节点，按 text_trace / 文案匹配判定默认更新集。
// 禁止全图扫描注入未连边资料。changedKeys 为空时返回空 nodes。
func PreviewFactImpact(g AppliedGraph, sources map[string]SourceRecord, sourceProductID string, changedKeys []string) FactImpactPreview {
	out := FactImpactPreview{
		ProductID:            sourceProductID,
		ChangedFactKeys:      append([]string(nil), changedKeys...),
		Nodes:                []FactImpactNode{},
		DefaultUpdateNodeIDs: []string{},
		Explanation:          "旧运行仍可用当时的 fact_set_version_id 与 input_digest 解释；未选中的已完成节点在采用新版本后保持 artifact，不会自动全图重跑。",
	}
	sort.Strings(out.ChangedFactKeys)
	if len(changedKeys) == 0 || strings.TrimSpace(sourceProductID) == "" {
		return out
	}
	changedSet := map[string]struct{}{}
	for _, key := range changedKeys {
		changedSet[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	sourceNodeIDs := productSourceNodesFor(g, sources, sourceProductID)
	if len(sourceNodeIDs) == 0 {
		return out
	}
	for _, id := range sourceNodeIDs {
		if record := sources[id]; record.ProductSource != nil && record.ProductSource.FactSetVersionID != nil {
			out.CurrentFactSetVersionID = record.ProductSource.FactSetVersionID
			break
		}
	}
	reachable := roleFactsReachable(g, sourceNodeIDs)
	promptDeps := map[string][]string{}
	for _, node := range g.Nodes {
		if !reachable[node.ID] {
			continue
		}
		switch node.NodeType {
		case NodeImagePrompt, NodeCreativeBrief, NodeVisualSystem:
			keys := usedFactKeysForNode(node, sources)
			deps := intersectChanged(keys, changedSet, changedKeys)
			promptDeps[node.ID] = deps
			out.Nodes = append(out.Nodes, buildImpactNode(node, sources, deps, sourceFactSetID(sources, sourceNodeIDs)))
		}
	}
	for _, node := range g.Nodes {
		if node.NodeType != NodeImageGeneration || !reachable[node.ID] {
			continue
		}
		deps := imageDependsOnChanged(g, node.ID, promptDeps, changedSet, changedKeys, sources)
		out.Nodes = append(out.Nodes, buildImpactNode(node, sources, deps, sourceFactSetID(sources, sourceNodeIDs)))
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].DefaultSelected != out.Nodes[j].DefaultSelected {
			return out.Nodes[i].DefaultSelected
		}
		if out.Nodes[i].NodeType != out.Nodes[j].NodeType {
			return out.Nodes[i].NodeType < out.Nodes[j].NodeType
		}
		return out.Nodes[i].Title < out.Nodes[j].Title || (out.Nodes[i].Title == out.Nodes[j].Title && out.Nodes[i].NodeID < out.Nodes[j].NodeID)
	})
	for _, node := range out.Nodes {
		if node.DefaultSelected {
			out.DefaultUpdateNodeIDs = append(out.DefaultUpdateNodeIDs, node.NodeID)
		}
	}
	return out
}

func productSourceNodesFor(g AppliedGraph, sources map[string]SourceRecord, sourceProductID string) []string {
	var ids []string
	for _, node := range g.Nodes {
		if node.NodeType != NodeProductSource {
			continue
		}
		record := sources[node.ID]
		if record.ProductSource == nil || record.ProductSource.SourceProductID == nil {
			continue
		}
		if *record.ProductSource.SourceProductID == sourceProductID {
			ids = append(ids, node.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func roleFactsReachable(g AppliedGraph, sourceNodeIDs []string) map[string]bool {
	out := map[string]bool{}
	queue := append([]string(nil), sourceNodeIDs...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, edge := range g.Edges {
			if edge.SourceNodeID != id || edge.Role != RoleFacts {
				continue
			}
			if out[edge.TargetNodeID] {
				continue
			}
			out[edge.TargetNodeID] = true
			queue = append(queue, edge.TargetNodeID)
		}
	}
	// 生图经 RolePrompt 挂在已可达的 prompt 上时，也视为受事实边影响的图位。
	for _, edge := range g.Edges {
		if edge.Role != RolePrompt {
			continue
		}
		if out[edge.SourceNodeID] {
			out[edge.TargetNodeID] = true
		}
	}
	return out
}

func usedFactKeysForNode(node AppliedNode, sources map[string]SourceRecord) []string {
	record := sources[node.ID]
	if keys := factKeysFromPayload(record.CurrentArtifactPayload); len(keys) > 0 {
		return keys
	}
	prompt := record.PromptDocument
	if prompt == nil {
		if raw, ok := node.Config["prompt"].(map[string]any); ok {
			prompt = raw
		}
	}
	if prompt == nil && (node.NodeType == NodeCreativeBrief || node.NodeType == NodeVisualSystem) {
		prompt = cloneMap(node.Config)
	}
	if prompt == nil {
		return nil
	}
	imageType, _ := node.Config["image_type_key"].(string)
	override := false
	if node.NodeType == NodeImagePrompt {
		if _, ok := node.Config["text_override"]; ok {
			override = true
		}
	}
	trace := BuildTextTrace(TextTraceInput{
		Prompt:            prompt,
		Facts:             record.Facts,
		ImageTypeKey:      imageType,
		UserImageOverride: override,
	})
	return trace.FactKeys
}

func factKeysFromPayload(payload map[string]any) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload["text_trace"].(map[string]any)
	if !ok {
		return nil
	}
	return stringListFromAny(raw["fact_keys"])
}

func stringListFromAny(v any) []string {
	switch typed := v.(type) {
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, _ := item.(string)
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func intersectChanged(used []string, changedSet map[string]struct{}, changedKeys []string) []string {
	if len(used) == 0 {
		return nil
	}
	hit := map[string]struct{}{}
	var out []string
	for _, key := range used {
		folded := strings.ToLower(strings.TrimSpace(key))
		if _, ok := changedSet[folded]; !ok {
			continue
		}
		if _, seen := hit[folded]; seen {
			continue
		}
		hit[folded] = struct{}{}
		// 保留调用方 changedKeys 里的原文大小写。
		matched := key
		for _, changed := range changedKeys {
			if strings.EqualFold(changed, key) {
				matched = changed
				break
			}
		}
		out = append(out, matched)
	}
	sort.Strings(out)
	return out
}

func imageDependsOnChanged(
	g AppliedGraph,
	imageNodeID string,
	promptDeps map[string][]string,
	changedSet map[string]struct{},
	changedKeys []string,
	sources map[string]SourceRecord,
) []string {
	for _, edge := range incomingSorted(g, imageNodeID) {
		if edge.Role != RolePrompt {
			continue
		}
		if deps, ok := promptDeps[edge.SourceNodeID]; ok {
			return deps
		}
		prompt, err := g.Node(edge.SourceNodeID)
		if err != nil {
			continue
		}
		keys := usedFactKeysForNode(prompt, sources)
		return intersectChanged(keys, changedSet, changedKeys)
	}
	return nil
}

func sourceFactSetID(sources map[string]SourceRecord, sourceNodeIDs []string) *string {
	for _, id := range sourceNodeIDs {
		if record := sources[id]; record.ProductSource != nil && record.ProductSource.FactSetVersionID != nil {
			return record.ProductSource.FactSetVersionID
		}
	}
	return nil
}

func buildImpactNode(node AppliedNode, sources map[string]SourceRecord, deps []string, boundFactSet *string) FactImpactNode {
	record := sources[node.ID]
	imageType, _ := node.Config["image_type_key"].(string)
	item := FactImpactNode{
		NodeID:                node.ID,
		NodeType:              string(node.NodeType),
		Title:                 node.Title,
		ImageTypeKey:          strings.TrimSpace(imageType),
		DependsOnChangedKeys:  deps,
		DefaultSelected:       len(deps) > 0,
		HasArtifact:           record.CurrentArtifactID != nil && *record.CurrentArtifactID != "",
		CurrentArtifactID:     record.CurrentArtifactID,
		CurrentInputDigest:    record.CurrentInputDigest,
		BoundFactSetVersionID: boundFactSet,
		Reason:                impactReason(deps, record),
	}
	if ver := artifactFactSetVersionID(record); ver != nil {
		item.ArtifactFactSetVersionID = ver
	} else if record.CurrentInputDigest != nil && boundFactSet != nil {
		// 产物 digest 未单独存 version 时，说明用编译时绑定的 fact_set_version_id 解释。
		item.ArtifactFactSetVersionID = boundFactSet
	}
	if item.DependsOnChangedKeys == nil {
		item.DependsOnChangedKeys = []string{}
	}
	return item
}

func impactReason(deps []string, record SourceRecord) string {
	if len(deps) > 0 {
		return "图位文案依赖变更事实，默认纳入更新"
	}
	if record.CurrentArtifactID != nil {
		return "已连边但文案未使用变更事实；默认不更新并保持现有产物"
	}
	return "已连边但文案未使用变更事实；默认不更新"
}

func artifactFactSetVersionID(record SourceRecord) *string {
	payload := record.CurrentArtifactPayload
	if payload == nil {
		return nil
	}
	if raw, ok := payload["fact_set_version_id"].(string); ok && strings.TrimSpace(raw) != "" {
		v := strings.TrimSpace(raw)
		return &v
	}
	if ctx, ok := payload["compiled_context"].(map[string]any); ok {
		if versions, ok := ctx["fact_set_versions"].([]any); ok {
			for _, item := range versions {
				entry, _ := item.(map[string]any)
				if entry == nil {
					continue
				}
				if raw, ok := entry["fact_set_version_id"].(string); ok && strings.TrimSpace(raw) != "" {
					v := strings.TrimSpace(raw)
					return &v
				}
			}
		}
	}
	return nil
}

// LoadLiveGraphSources 读取商品 active 图与编译用 SourceRecord；无图返回 nil,nil,nil,nil。
func LoadLiveGraphSources(ctx context.Context, tx *gorm.DB, productID string) (*Identity, AppliedGraph, map[string]SourceRecord, error) {
	live, err := TryLive(ctx, tx, productID)
	if err != nil {
		return nil, AppliedGraph{}, nil, err
	}
	if live == nil {
		return nil, AppliedGraph{}, nil, nil
	}
	applied, err := loadAppliedGraph(ctx, tx, graphRow{Identity: live.Identity})
	if err != nil {
		return nil, AppliedGraph{}, nil, err
	}
	sources, _, _, _, err := loadGraphSources(ctx, tx, graphRow{Identity: live.Identity}, applied)
	if err != nil {
		return nil, AppliedGraph{}, nil, err
	}
	id := live.Identity
	return &id, applied, sources, nil
}

// AdoptFactSetOnProductSources 把绑定该商品的 product_source 钉到新 fact_set_version_id。
// 不入队跑图。返回是否改写了配置。
func AdoptFactSetOnProductSources(ctx context.Context, tx *gorm.DB, productID, factSetVersionID string) (bool, error) {
	live, err := TryLiveForUpdate(ctx, tx, productID)
	if err != nil {
		return false, err
	}
	if live == nil {
		return false, nil
	}
	ops := make([]Operation, 0)
	for _, node := range live.Applied.Nodes {
		if node.NodeType != NodeProductSource {
			continue
		}
		binding, err := parseProductSourceBinding(productID, node.Config)
		if err != nil {
			return false, err
		}
		if binding.sourceProductID == nil || *binding.sourceProductID != productID {
			continue
		}
		cfg := cloneMap(node.Config)
		if cfg == nil {
			cfg = map[string]any{}
		}
		current, _ := cfg["fact_set_version_id"].(string)
		if strings.TrimSpace(current) == factSetVersionID {
			continue
		}
		cfg["fact_set_version_id"] = factSetVersionID
		ops = append(ops, UpdateNodeConfigOp{NodeRef: node.ID, Config: cfg})
	}
	if len(ops) == 0 {
		return false, nil
	}
	graphID := live.Identity.ID
	_, err = WriteTx(ctx, tx, Command{
		ProductID: productID,
		GraphID:   &graphID,
		ChangeSet: ChangeSet{
			BaseGraphRevision: live.Identity.Revision,
			Summary:           "采用新事实版本",
			ActorType:         ActorUser,
			Operations:        ops,
		},
	})
	return err == nil, err
}

// PreserveUnselectedFactArtifacts 在采用新 fact 版本后，为未选中且已有产物的处理节点重盖 input_digest，
// 使 skipUnchanged 保持 artifact、避免全图自动重跑。选中节点及其需重算的祖先保持 stale。
func PreserveUnselectedFactArtifacts(ctx context.Context, tx *gorm.DB, productID string, updateNodeIDs []string) ([]string, error) {
	identity, applied, sources, err := LoadLiveGraphSources(ctx, tx, productID)
	if err != nil {
		return nil, err
	}
	if identity == nil {
		return nil, nil
	}
	selected := map[string]struct{}{}
	for _, id := range updateNodeIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		selected[id] = struct{}{}
		for _, ancestor := range processingAncestors(applied, id) {
			selected[ancestor] = struct{}{}
		}
	}
	sourceNodeIDs := productSourceNodesFor(applied, sources, productID)
	reachable := roleFactsReachable(applied, sourceNodeIDs)
	preserved := make([]string, 0)
	for _, node := range applied.Nodes {
		if !reachable[node.ID] {
			continue
		}
		if _, keepStale := selected[node.ID]; keepStale {
			continue
		}
		switch node.NodeType {
		case NodeImagePrompt, NodeCreativeBrief, NodeVisualSystem, NodeImageGeneration:
		default:
			continue
		}
		record := sources[node.ID]
		if record.CurrentArtifactID == nil || *record.CurrentArtifactID == "" {
			continue
		}
		digest, err := compileInputDigest(applied, node.ID, sources)
		if err != nil {
			// 缺边等无法编译：跳过，不清产物。
			continue
		}
		if record.CurrentInputDigest != nil && *record.CurrentInputDigest == digest {
			preserved = append(preserved, node.ID)
			continue
		}
		if err := restampNodeArtifactDigest(ctx, tx, identity.ID, identity.Revision, node, record, digest); err != nil {
			return preserved, err
		}
		preserved = append(preserved, node.ID)
	}
	sort.Strings(preserved)
	return preserved, nil
}

func restampNodeArtifactDigest(
	ctx context.Context,
	tx *gorm.DB,
	graphID string,
	revision int,
	node AppliedNode,
	record SourceRecord,
	digest string,
) error {
	if record.CurrentArtifactID == nil {
		return nil
	}
	var existing schema.WorkflowGraphArtifacts
	if err := tx.WithContext(ctx).Where("id = ?", *record.CurrentArtifactID).Take(&existing).Error; err != nil {
		return err
	}
	payload := existing.PayloadJSON
	hash := existing.PayloadHash
	if payload == "" {
		payload = "{}"
		hash = sha256Hex([]byte(payload))
	}
	id := clockid.New()
	nodeID := node.ID
	rec := schema.WorkflowGraphArtifacts{
		ID:                  id,
		GraphID:             graphID,
		NodeID:              &nodeID,
		ArtifactType:        existing.ArtifactType,
		SchemaVersion:       existing.SchemaVersion,
		GraphRevision:       revision,
		PayloadJSON:         payload,
		PayloadHash:         hash,
		InputDigest:         digest,
		DocumentAction:      existing.DocumentAction,
		BaseDocumentHash:    existing.BaseDocumentHash,
		ProductImageAssetID: existing.ProductImageAssetID,
		ProviderName:        existing.ProviderName,
		ProviderModel:       existing.ProviderModel,
		CreatedAt:           time.Now().UTC(),
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).
		Where("id = ?", node.ID).
		Updates(map[string]any{"current_artifact_id": id}).Error
}

// MergeRoleFactsIntoSources 把 RoleFacts 源节点的 facts 填进目标节点 SourceRecord，供 text_trace 匹配。
func MergeRoleFactsIntoSources(g AppliedGraph, sources map[string]SourceRecord) map[string]SourceRecord {
	out := map[string]SourceRecord{}
	for id, record := range sources {
		out[id] = record
	}
	for _, node := range g.Nodes {
		record := out[node.ID]
		var facts []map[string]any
		for _, edge := range incomingSorted(g, node.ID) {
			if edge.Role != RoleFacts {
				continue
			}
			src := out[edge.SourceNodeID]
			facts = append(facts, mergeRuntimeFacts(src.Facts, src.ProductSource)...)
		}
		if len(facts) > 0 {
			record.Facts = facts
			out[node.ID] = record
		}
	}
	return out
}

// ValidateUpdateNodeIDs 校验更新范围均在影响预览可达集内。
func ValidateUpdateNodeIDs(preview FactImpactPreview, updateNodeIDs []string) error {
	allowed := map[string]struct{}{}
	for _, node := range preview.Nodes {
		allowed[node.NodeID] = struct{}{}
	}
	for _, id := range updateNodeIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return apperr.Validation("更新范围节点无效")
		}
		if _, ok := allowed[id]; !ok {
			return apperr.Validation("更新范围只能选择事实影响预览中的图位")
		}
	}
	return nil
}

// Ensure factValueText available — defined in product package; duplicate small helper for graph.
func factValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64, float32, int, int64, int32, bool:
		return strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(jsonNumber(typed)), " ", ""))
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func jsonNumber(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}
