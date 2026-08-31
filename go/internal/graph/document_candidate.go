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

type DocumentSectionDefinition struct {
	Key    string   `json:"key"`
	Fields []string `json:"fields"`
}

type DocumentCandidateSection struct {
	Key       string         `json:"key"`
	Changed   bool           `json:"changed"`
	Current   map[string]any `json:"current"`
	Candidate map[string]any `json:"candidate"`
}

type DocumentCandidate struct {
	ArtifactID        string                     `json:"artifact_id"`
	NodeID            string                     `json:"node_id"`
	DocumentAction    string                     `json:"document_action"`
	Status            string                     `json:"status"`
	BaseDocumentHash  string                     `json:"base_document_hash"`
	InputDigest       string                     `json:"input_digest"`
	CurrentDocument   map[string]any             `json:"current_document"`
	CandidateDocument map[string]any             `json:"candidate_document"`
	Sections          []DocumentCandidateSection `json:"sections"`
	CreatedAt         time.Time                  `json:"created_at"`
}

type ApplyDocumentCandidateInput struct {
	ArtifactID        string   `json:"artifact_id"`
	BaseGraphRevision int      `json:"base_graph_revision"`
	SectionKeys       []string `json:"section_keys"`
}

type DiscardDocumentCandidateInput struct {
	ArtifactID string `json:"artifact_id"`
}

func documentSections(nodeType NodeType) []DocumentSectionDefinition {
	switch nodeType {
	case NodeCreativeBrief:
		return []DocumentSectionDefinition{
			{Key: "objective", Fields: []string{"goal", "design_goals"}},
			{Key: "copy", Fields: []string{"required_copy"}},
			{Key: "gaps", Fields: []string{"fact_gaps"}},
			{Key: "guardrails", Fields: []string{"prohibitions"}},
		}
	case NodeVisualSystem:
		return []DocumentSectionDefinition{
			{Key: "style", Fields: []string{"style"}},
			{Key: "palette", Fields: []string{"colors"}},
			{Key: "guardrails", Fields: []string{"prohibitions"}},
		}
	case NodeImagePrompt:
		return []DocumentSectionDefinition{
			{Key: "objective", Fields: []string{"design_goal"}},
			{Key: "subject", Fields: []string{"subject"}},
			{Key: "composition", Fields: []string{"composition"}},
			{Key: "visual_style", Fields: []string{"visual_style"}},
			{Key: "copy", Fields: []string{"copy_overlay"}},
			{Key: "constraints", Fields: []string{"constraints"}},
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
		return pickDocumentFields(config, []string{"goal", "design_goals", "required_copy", "prohibitions", "fact_gaps"})
	case NodeVisualSystem:
		overlay, _ := config["visual_overlay"].(map[string]any)
		return pickDocumentFields(overlay, []string{"style", "colors", "prohibitions"})
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
		if value, ok := source[field]; ok {
			out[field] = cloneValue(value)
		}
	}
	return out
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
			if value, ok := candidateVisible[field]; ok {
				currentVisible[field] = cloneValue(value)
			} else {
				delete(currentVisible, field)
			}
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
