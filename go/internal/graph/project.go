package graph

import (
	"context"
	sqldb "database/sql"
	"encoding/json"
	"errors"
	"strings"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

type Projection struct {
	ID                    string        `json:"id"`
	ProductID             string        `json:"product_id"`
	Title                 string        `json:"title"`
	SchemaVersion         int           `json:"schema_version"`
	Revision              int           `json:"revision"`
	SourceDraftRevisionID *string       `json:"source_draft_revision_id"`
	LastOperationGroupID  *string       `json:"last_operation_group_id"`
	CanUndo               bool          `json:"can_undo"`
	CanRedo               bool          `json:"can_redo"`
	Nodes                 []NodeView    `json:"nodes"`
	Edges                 []EdgeView    `json:"edges"`
	Groups                []GroupView   `json:"groups"`
	PendingProposal       *ProposalView `json:"pending_proposal"`
}

type NodeView struct {
	ID                     string           `json:"id"`
	NodeType               NodeType         `json:"node_type"`
	Title                  string           `json:"title"`
	PositionX              int              `json:"position_x"`
	PositionY              int              `json:"position_y"`
	Config                 map[string]any   `json:"config"`
	SourceProduct          *productSummary  `json:"source_product"`
	ProductFactSet         *factSetSnapshot `json:"product_fact_set"`
	BoundAssetID           *string          `json:"bound_asset_id"`
	GroupID                *string          `json:"group_id"`
	PreviewAssetID         *string          `json:"preview_asset_id"`
	ConfigStatus           ConfigStatus     `json:"config_status"`
	Unused                 bool             `json:"unused"`
	CurrentArtifactID      *string          `json:"current_artifact_id"`
	CurrentArtifactType    *string          `json:"current_artifact_type"`
	CurrentArtifactPayload map[string]any   `json:"current_artifact_payload"`
	Incoming               []EdgeSummary    `json:"incoming"`
	Outgoing               []EdgeSummary    `json:"outgoing"`
}

type EdgeSummary struct {
	ID       string       `json:"id"`
	NodeID   string       `json:"node_id"`
	DataType EdgeDataType `json:"data_type"`
	Role     EdgeRole     `json:"role"`
	Order    int          `json:"order"`
}

type EdgeView struct {
	ID           string       `json:"id"`
	SourceNodeID string       `json:"source_node_id"`
	TargetNodeID string       `json:"target_node_id"`
	DataType     EdgeDataType `json:"data_type"`
	Role         EdgeRole     `json:"role"`
	Order        int          `json:"order"`
}

type GroupView struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	MemberIDs []string `json:"member_ids"`
}

type ProposalView struct {
	ID                string             `json:"id"`
	Summary           string             `json:"summary"`
	BaseGraphRevision int                `json:"base_graph_revision"`
	Stale             bool               `json:"stale"`
	AddedNodes        []ProposalNodeView `json:"added_nodes"`
	AddedEdges        []ProposalEdgeView `json:"added_edges"`
	DeletedNodeIDs    []string           `json:"deleted_node_ids"`
	DeletedEdgeIDs    []string           `json:"deleted_edge_ids"`
	ChangedNodeIDs    []string           `json:"changed_node_ids"`
}

type ProposalNodeView struct {
	ID        string         `json:"id"`
	NodeType  NodeType       `json:"node_type"`
	Title     string         `json:"title"`
	PositionX int            `json:"position_x"`
	PositionY int            `json:"position_y"`
	GroupID   *string        `json:"group_id"`
	Config    map[string]any `json:"config"`
}

type ProposalEdgeView struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	Role         string `json:"role"`
	DataType     string `json:"data_type"`
	Order        int    `json:"order"`
}

func Project(ctx context.Context, tx *gorm.DB, row GraphRow) (Projection, error) {
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return Projection{}, err
	}
	last, err := lastOperationGroup(ctx, tx, row)
	if err != nil {
		return Projection{}, err
	}
	sources, previews, artifactDigests, err := loadGraphSources(ctx, tx, row, applied)
	if err != nil {
		return Projection{}, err
	}
	proposal, err := pendingProposalView(ctx, tx, row, applied)
	if err != nil {
		return Projection{}, err
	}
	var lastID *string
	canUndo, canRedo := false, false
	if last != nil {
		id := last.ID
		lastID = &id
		canUndo = last.HistoryKind != HistoryUndo
		canRedo = last.HistoryKind == HistoryUndo
	}
	return buildProjection(row, applied, lastID, canUndo, canRedo, previews, artifactDigests, sources, proposal), nil
}

func buildProjection(
	row GraphRow,
	applied AppliedGraph,
	lastID *string,
	canUndo, canRedo bool,
	previews map[string]string,
	artifactDigests map[string]string,
	sources map[string]SourceRecord,
	proposal *ProposalView,
) Projection {
	outgoingIDs := map[string]struct{}{}
	for _, edge := range applied.Edges {
		outgoingIDs[edge.SourceNodeID] = struct{}{}
	}
	memberIDs := map[string][]string{}
	for _, group := range applied.Groups {
		memberIDs[group.ID] = []string{}
	}
	nodes := make([]NodeView, 0, len(applied.Nodes))
	for _, node := range applied.Nodes {
		if node.GroupID != nil {
			memberIDs[*node.GroupID] = append(memberIDs[*node.GroupID], node.ID)
		}
		unused := false
		if node.NodeType == NodeImageAsset {
			_, connected := outgoingIDs[node.ID]
			unused = !connected
		}
		var preview *string
		if id, ok := previews[node.ID]; ok {
			preview = &id
		}
		record := sources[node.ID]
		var sourceProduct *productSummary
		var factSet *factSetSnapshot
		if node.NodeType == NodeProductSource && record.ProductSource != nil {
			sourceProduct = record.ProductSource.SourceProduct
			factSet = record.ProductSource.FactSetVersion
		}
		incoming := []EdgeSummary{}
		outgoing := []EdgeSummary{}
		for _, edge := range applied.Edges {
			if edge.TargetNodeID == node.ID {
				incoming = append(incoming, EdgeSummary{edge.ID, edge.SourceNodeID, edge.DataType, edge.Role, edge.Order})
			}
			if edge.SourceNodeID == node.ID {
				outgoing = append(outgoing, EdgeSummary{edge.ID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
			}
		}
		status, err := applied.ConfigStatus(node.ID)
		if err != nil {
			status = ConfigIncomplete
		}
		status = configStatusWithStale(applied, node, artifactDigests[node.ID], sources, status)
		nodes = append(nodes, NodeView{
			ID:                     node.ID,
			NodeType:               node.NodeType,
			Title:                  node.Title,
			PositionX:              node.PositionX,
			PositionY:              node.PositionY,
			Config:                 nonemptyMap(node.Config),
			SourceProduct:          sourceProduct,
			ProductFactSet:         factSet,
			BoundAssetID:           node.BoundAssetID,
			GroupID:                node.GroupID,
			PreviewAssetID:         preview,
			ConfigStatus:           status,
			Unused:                 unused,
			CurrentArtifactID:      record.CurrentArtifactID,
			CurrentArtifactType:    record.CurrentArtifactType,
			CurrentArtifactPayload: record.CurrentArtifactPayload,
			Incoming:               incoming,
			Outgoing:               outgoing,
		})
	}
	edges := make([]EdgeView, 0, len(applied.Edges))
	for _, edge := range applied.Edges {
		edges = append(edges, EdgeView{edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
	}
	groups := make([]GroupView, 0, len(applied.Groups))
	for _, group := range applied.Groups {
		members := memberIDs[group.ID]
		if members == nil {
			members = []string{}
		}
		groups = append(groups, GroupView{group.ID, group.Title, members})
	}
	return Projection{
		ID:                    row.ID,
		ProductID:             row.ProductID,
		Title:                 row.Title,
		SchemaVersion:         row.SchemaVersion,
		Revision:              applied.Revision,
		SourceDraftRevisionID: nil,
		LastOperationGroupID:  lastID,
		CanUndo:               canUndo,
		CanRedo:               canRedo,
		Nodes:                 nodes,
		Edges:                 edges,
		Groups:                groups,
		PendingProposal:       proposal,
	}
}

func configStatusWithStale(applied AppliedGraph, node AppliedNode, artifactDigest string, sources map[string]SourceRecord, status ConfigStatus) ConfigStatus {
	if status != ConfigReady || !IsProcessingNode(node.NodeType) || artifactDigest == "" || sources == nil {
		return status
	}
	digest, err := compileInputDigest(applied, node.ID, sources)
	if err != nil {
		return status
	}
	if digest != artifactDigest {
		return ConfigStale
	}
	return status
}

func loadGraphSources(ctx context.Context, tx *gorm.DB, row GraphRow, applied AppliedGraph) (map[string]SourceRecord, map[string]string, map[string]string, error) {
	nodeRows, err := pfdb.Query(ctx, tx, `
		SELECT n.id, n.bound_image_asset_id, n.current_artifact_id,
		       a.artifact_type, a.payload_json, a.input_digest, a.product_image_asset_id
		FROM workflow_graph_nodes n
		LEFT JOIN workflow_graph_artifacts a ON a.id = n.current_artifact_id
		WHERE n.graph_id = $1
	`, row.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	defer nodeRows.Close()

	type nodeArtifact struct {
		boundAssetID  *string
		artifactID    *string
		artifactType  *string
		payload       []byte
		digest        *string
		outputAssetID *string
	}
	artifacts := map[string]nodeArtifact{}
	for nodeRows.Next() {
		var rec nodeArtifact
		var nodeID string
		if err := nodeRows.Scan(&nodeID, &rec.boundAssetID, &rec.artifactID, &rec.artifactType, &rec.payload, &rec.digest, &rec.outputAssetID); err != nil {
			return nil, nil, nil, err
		}
		artifacts[nodeID] = rec
	}
	if err := nodeRows.Err(); err != nil {
		return nil, nil, nil, err
	}

	previews := map[string]string{}
	digests := map[string]string{}
	sources := map[string]SourceRecord{}
	for _, node := range applied.Nodes {
		rec := artifacts[node.ID]
		if rec.boundAssetID != nil && *rec.boundAssetID != "" {
			previews[node.ID] = *rec.boundAssetID
		} else if rec.outputAssetID != nil && *rec.outputAssetID != "" {
			previews[node.ID] = *rec.outputAssetID
		}
		if rec.digest != nil && *rec.digest != "" {
			digests[node.ID] = *rec.digest
		}
		record := SourceRecord{}
		if rec.artifactID != nil {
			record.CurrentArtifactID = rec.artifactID
			record.CurrentArtifactType = rec.artifactType
			record.CurrentOutputAssetID = rec.outputAssetID
			record.CurrentInputDigest = rec.digest
			if len(rec.payload) > 0 {
				payload := map[string]any{}
				if err := json.Unmarshal(rec.payload, &payload); err == nil {
					record.CurrentArtifactPayload = payload
				}
			}
		}
		switch node.NodeType {
		case NodeProductSource:
			snap, err := loadProductSourceSnapshot(ctx, tx, row.ProductID, node.Config)
			if err != nil {
				return nil, nil, nil, err
			}
			record.ProductSource = &snap
			record.Facts = snap.Facts
		case NodeCreativeBrief:
			record.Brief = cloneMap(node.Config)
		case NodeVisualSystem:
			versionID, _ := node.Config["visual_system_version_id"].(string)
			if strings.TrimSpace(versionID) != "" {
				vid := strings.TrimSpace(versionID)
				record.VisualSystemVersionID = &vid
				payload, err := loadVisualSystemPayload(ctx, tx, vid)
				if err != nil {
					return nil, nil, nil, err
				}
				record.VisualPayload = payload
			}
			if record.VisualPayload == nil {
				if overrides, ok := node.Config["visual_overrides"].([]any); ok {
					merged := mergeVisualOverrideItems(overrides)
					if len(merged) > 0 {
						record.VisualPayload = CatalogVisualOverlay(merged)
					}
				}
			}
			if record.VisualPayload == nil {
				record.VisualPayload = visualOverlayFromConfig(node.Config)
			}
		case NodeImageAsset:
			record.BoundAssetID = node.BoundAssetID
			label := node.Title
			record.BoundAssetLabel = &label
			if node.BoundAssetID != nil {
				display, mime, err := loadBoundAssetMeta(ctx, tx, row.ProductID, *node.BoundAssetID)
				if err != nil {
					return nil, nil, nil, err
				}
				if display != "" {
					record.BoundAssetLabel = &display
				}
				if mime != "" {
					record.BoundAssetMIME = &mime
				}
			}
		}
		sources[node.ID] = record
	}
	return sources, previews, digests, nil
}

func loadVisualSystemPayload(ctx context.Context, tx *gorm.DB, versionID string) (map[string]any, error) {
	var payload []byte
	err := pfdb.QueryRow(ctx, tx, `SELECT payload_json FROM visual_system_versions WHERE id = $1`, versionID).Scan(&payload)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func loadBoundAssetMeta(ctx context.Context, tx *gorm.DB, productID, assetID string) (string, string, error) {
	var display, mime string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT a.display_name, COALESCE(m.mime_type, '')
		FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.product_id = $1 AND a.id = $2
	`, productID, assetID).Scan(&display, &mime)
	if errors.Is(err, sqldb.ErrNoRows) {
		return "", "", nil
	}
	return display, mime, err
}
