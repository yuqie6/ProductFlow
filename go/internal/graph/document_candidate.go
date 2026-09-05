package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// DocumentSectionDefinition 描述文稿候选里一个可独立应用的 section 及其字段。
type DocumentSectionDefinition struct {
	Key    string   `json:"key"`    // section 闭集，如 objective / copy
	Fields []string `json:"fields"` // 该 section 可独立合并的可见字段
}

// DocumentCandidateSection 对比当前文稿与候选在一个 section 上的差异。
type DocumentCandidateSection struct {
	Key       string         `json:"key"`
	Changed   bool           `json:"changed"`   // 当前与候选该 section 是否不同
	Current   map[string]any `json:"current"`   // live 文稿该 section；无值为空 map
	Candidate map[string]any `json:"candidate"` // 建议文稿该 section；无值为空 map
}

// DocumentCandidate 是内容节点上挂起的 AI 文稿建议。Status 为 outdated 时不可 Apply。
type DocumentCandidate struct {
	ArtifactID string `json:"artifact_id"`
	NodeID     string `json:"node_id"`
	// DocumentAction 是生成该候选时的 complete|rewrite|replace。
	DocumentAction   string `json:"document_action"`
	Status           string `json:"status"`
	BaseDocumentHash string `json:"base_document_hash"` // 生成时节点可见文稿哈希；偏离则 outdated
	// InputDigest 是生成时的编译 input digest；偏离则 Status=outdated，不可 Apply。
	InputDigest       string                     `json:"input_digest"`
	CurrentDocument   map[string]any             `json:"current_document"`   // 节点当前可见文稿
	CandidateDocument map[string]any             `json:"candidate_document"` // 候选可见文稿
	Sections          []DocumentCandidateSection `json:"sections"`           // 可独立应用的差异块
	CreatedAt         time.Time                  `json:"created_at"`
}

// ApplyDocumentCandidateInput 指定 artifact 与要合并的 section_keys；空 keys 表示整份候选。
type ApplyDocumentCandidateInput struct {
	ArtifactID        string   `json:"artifact_id"`
	BaseGraphRevision int      `json:"base_graph_revision"` // 必须等于当前 live revision，否则 Conflict
	SectionKeys       []string `json:"section_keys"`        // 空表示整份候选；未知 key 为 Validation
}

// DiscardDocumentCandidateInput 用 artifact_id 做乐观校验。
type DiscardDocumentCandidateInput struct {
	ArtifactID string `json:"artifact_id"`
}

// documentSections 定义候选可独立应用的 section。未知类型返回 nil，Apply 会 Validation。
func documentSections(nodeType NodeType) []DocumentSectionDefinition {
	switch nodeType {
	case NodeCreativeBrief:
		return []DocumentSectionDefinition{
			{Key: "objective", Fields: []string{"goal", "key_messages"}},
			{Key: "requirements", Fields: []string{"required_elements"}},
			{Key: "gaps", Fields: []string{"fact_gaps"}},
			{Key: "guardrails", Fields: []string{"prohibitions"}},
		}
	case NodeVisualSystem:
		return []DocumentSectionDefinition{
			{Key: "style", Fields: []string{"style"}},
			{Key: "palette", Fields: []string{"colors"}},
		}
	case NodeImagePrompt:
		return []DocumentSectionDefinition{
			{Key: "objective", Fields: []string{"design_goal"}},
			{Key: "subject", Fields: []string{"product_fidelity"}},
			{Key: "composition", Fields: []string{"composition.layout", "composition.viewpoint", "composition.product_share_percent", "content.background"}},
			{Key: "visual_style", Fields: []string{"content.focus", "content.selling_points", "content.decorations", "atmosphere"}},
			{Key: "copy", Fields: []string{"text", "composition.copy_regions"}},
			{Key: "constraints", Fields: []string{"shared_rules", "creative_boundary"}},
		}
	default:
		return nil
	}
}

func documentBaseHash(node AppliedNode) string {
	payload, _ := json.Marshal(map[string]any{
		"node_type":       node.NodeType,
		"config":          nonemptyMap(node.Config),
		"document_origin": DocumentOrigin(node),
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validateDocumentSection(nodeType NodeType, section string) error {
	if section == "" {
		return nil
	}
	if nodeType == NodeImagePrompt {
		for _, definition := range documentSections(nodeType) {
			if definition.Key == section {
				return nil
			}
		}
	}
	return apperr.Validation("document_section 只支持画面方案的有效章节")
}

func proposedDocumentConfig(node AppliedNode, payload map[string]any, action string) map[string]any {
	action = validDocumentAction(action)
	if action == "" {
		action = DocumentActionComplete
	}
	switch node.NodeType {
	case NodeCreativeBrief:
		return mergeGeneratedBrief(node.Config, payload, action, DocumentOrigin(node))
	case NodeVisualSystem:
		return mergeGeneratedOverlay(node.Config, payload, action, DocumentOrigin(node))
	case NodeImagePrompt:
		return mergeGeneratedPrompt(node.Config, payload, action, DocumentOrigin(node))
	default:
		return cloneMap(node.Config)
	}
}

func visibleDocument(nodeType NodeType, config map[string]any) map[string]any {
	switch nodeType {
	case NodeCreativeBrief:
		return pickDocumentFields(config, []string{"goal", "key_messages", "required_elements", "prohibitions", "fact_gaps"})
	case NodeVisualSystem:
		overlay, _ := config["visual_overlay"].(map[string]any)
		return pickDocumentFields(overlay, []string{"style", "colors"})
	case NodeImagePrompt:
		prompt, _ := config["prompt"].(map[string]any)
		return cloneMap(prompt)
	default:
		return map[string]any{}
	}
}

func pickDocumentFields(source map[string]any, fields []string) map[string]any {
	out := map[string]any{}
	for _, field := range fields {
		copyDocumentField(out, source, strings.Split(field, "."))
	}
	return out
}

func copyDocumentField(target, source map[string]any, path []string) {
	key := path[0]
	if len(path) == 1 {
		if value, ok := source[key]; ok {
			target[key] = cloneValue(value)
		} else {
			delete(target, key)
		}
		return
	}
	sourceChild, _ := source[key].(map[string]any)
	targetChild, _ := target[key].(map[string]any)
	if targetChild == nil {
		targetChild = map[string]any{}
	}
	copyDocumentField(targetChild, sourceChild, path[1:])
	if len(targetChild) > 0 {
		target[key] = targetChild
	} else {
		delete(target, key)
	}
}

func candidateSections(nodeType NodeType, current, candidate map[string]any) []DocumentCandidateSection {
	out := make([]DocumentCandidateSection, 0, len(documentSections(nodeType)))
	for _, definition := range documentSections(nodeType) {
		currentPart := pickDocumentFields(current, definition.Fields)
		candidatePart := pickDocumentFields(candidate, definition.Fields)
		out = append(out, DocumentCandidateSection{
			Key: definition.Key, Changed: !documentValueEqual(currentPart, candidatePart),
			Current: currentPart, Candidate: candidatePart,
		})
	}
	return out
}

// applyDocumentSections 按 section_keys 把候选可见字段合并进节点 config。空 keys 表示整份候选。
// 未知 section 返回 Validation。只改可见文稿字段，不碰 image_type_key 等 hidden。
func applyDocumentSections(node AppliedNode, candidateConfig map[string]any, requested []string) (map[string]any, error) {
	definitions := documentSections(node.NodeType)
	allowed := map[string]DocumentSectionDefinition{}
	for _, definition := range definitions {
		allowed[definition.Key] = definition
	}
	selected := map[string]struct{}{}
	for _, key := range requested {
		key = strings.TrimSpace(key)
		if _, ok := allowed[key]; !ok {
			return nil, apperr.Validation("候选文稿包含未知 section_key")
		}
		selected[key] = struct{}{}
	}
	if len(selected) == 0 {
		return cloneMap(candidateConfig), nil
	}
	out := cloneMap(node.Config)
	currentVisible := visibleDocument(node.NodeType, out)
	candidateVisible := visibleDocument(node.NodeType, candidateConfig)
	for key := range selected {
		for _, field := range allowed[key].Fields {
			copyDocumentField(currentVisible, candidateVisible, strings.Split(field, "."))
		}
	}
	switch node.NodeType {
	case NodeCreativeBrief:
		for key := range selected {
			for _, field := range allowed[key].Fields {
				delete(out, field)
			}
		}
		for key, value := range currentVisible {
			out[key] = value
		}
	case NodeVisualSystem:
		out["visual_overlay"] = currentVisible
	case NodeImagePrompt:
		out["prompt"] = currentVisible
	}
	return out, nil
}

// loadDocumentCandidate 读节点上挂起的候选 artifact。lock=true 时 FOR UPDATE 图。
// 无 pending_candidate_artifact_id 返回 NotFound。outdated 仍返回，但 Apply 会拒。
func loadDocumentCandidate(ctx context.Context, tx *gorm.DB, productID, graphID, nodeID string, lock bool) (graphRow, AppliedGraph, AppliedNode, schema.WorkflowGraphNodes, schema.WorkflowGraphArtifacts, DocumentCandidate, error) {
	row, err := loadGraph(ctx, tx, productID, graphID)
	if err != nil {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, schema.WorkflowGraphNodes{}, schema.WorkflowGraphArtifacts{}, DocumentCandidate{}, err
	}
	if lock {
		row, err = loadGraphForUpdate(ctx, tx, productID, graphID)
		if err != nil {
			return graphRow{}, AppliedGraph{}, AppliedNode{}, schema.WorkflowGraphNodes{}, schema.WorkflowGraphArtifacts{}, DocumentCandidate{}, err
		}
	}
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, schema.WorkflowGraphNodes{}, schema.WorkflowGraphArtifacts{}, DocumentCandidate{}, err
	}
	node, err := applied.Node(nodeID)
	if err != nil || !isContentNodeType(node.NodeType) {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, schema.WorkflowGraphNodes{}, schema.WorkflowGraphArtifacts{}, DocumentCandidate{}, apperr.NotFound("文稿候选不存在")
	}
	query := tx.WithContext(ctx)
	if lock {
		query = query.Clauses(pfdb.ForUpdate())
	}
	var nodeRec schema.WorkflowGraphNodes
	if err := query.Where("id = ? AND graph_id = ?", nodeID, graphID).Take(&nodeRec).Error; err != nil {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, schema.WorkflowGraphNodes{}, schema.WorkflowGraphArtifacts{}, DocumentCandidate{}, err
	}
	if nodeRec.PendingCandidateArtifactID == nil {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, nodeRec, schema.WorkflowGraphArtifacts{}, DocumentCandidate{}, apperr.NotFound("文稿候选不存在")
	}
	var artifact schema.WorkflowGraphArtifacts
	if err := tx.WithContext(ctx).Where("id = ?", *nodeRec.PendingCandidateArtifactID).Take(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return graphRow{}, AppliedGraph{}, AppliedNode{}, nodeRec, artifact, DocumentCandidate{}, apperr.NotFound("文稿候选不存在")
		}
		return graphRow{}, AppliedGraph{}, AppliedNode{}, nodeRec, artifact, DocumentCandidate{}, err
	}
	if artifact.NodeID == nil || *artifact.NodeID != nodeID || artifact.DocumentAction == nil || artifact.BaseDocumentHash == nil {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, nodeRec, artifact, DocumentCandidate{}, apperr.Conflict("文稿候选记录不完整")
	}
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(artifact.PayloadJSON), &payload); err != nil {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, nodeRec, artifact, DocumentCandidate{}, err
	}
	proposedConfig := proposedDocumentConfig(node, payload, *artifact.DocumentAction)
	current := visibleDocument(node.NodeType, node.Config)
	candidate := visibleDocument(node.NodeType, proposedConfig)
	status := "ready"
	sources, _, _, _, err := loadGraphSources(ctx, tx, row, applied)
	if err != nil {
		return graphRow{}, AppliedGraph{}, AppliedNode{}, nodeRec, artifact, DocumentCandidate{}, err
	}
	inputDigest, err := compileInputDigest(applied, nodeID, sources)
	if err != nil || documentBaseHash(node) != *artifact.BaseDocumentHash || inputDigest != artifact.InputDigest {
		status = "outdated"
	}
	view := DocumentCandidate{
		ArtifactID: artifact.ID, NodeID: nodeID, DocumentAction: *artifact.DocumentAction,
		Status: status, BaseDocumentHash: *artifact.BaseDocumentHash, InputDigest: artifact.InputDigest,
		CurrentDocument: current, CandidateDocument: candidate,
		Sections: candidateSections(node.NodeType, current, candidate), CreatedAt: artifact.CreatedAt,
	}
	return row, applied, node, nodeRec, artifact, view, nil
}

func normalizeSectionKeys(keys []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
