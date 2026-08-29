package graph

import (
	"context"
	"errors"
	"net/http"
	"strings"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

type proposalRow struct {
	ID                string
	Summary           string
	Status            string
	BaseGraphRevision int
	ChangeSetJSON     []byte
}

// CreateProposal 校验后写入 PENDING 提案，不改 live 图。
func CreateProposal(ctx context.Context, tx *gorm.DB, productID, conversationID string, changeSet ChangeSet) (AgentProposalResult, error) {
	row, err := loadActiveGraphForUpdate(ctx, tx, productID)
	if err != nil {
		return AgentProposalResult{}, err
	}
	if row == nil {
		return AgentProposalResult{}, apperr.Conflict("当前商品没有可编辑的工作流")
	}
	changeSet.ActorType = ActorAgent
	if changeSet.BaseGraphRevision != row.Revision {
		return AgentProposalResult{}, apperr.Conflict("图 revision 已变化，请刷新后重试")
	}
	var pendingID *string
	err = pfdb.QueryRow(ctx, tx, `
		SELECT id FROM workflow_graph_proposals WHERE graph_id = $1 AND status = 'pending' LIMIT 1
	`, row.ID).Scan(&pendingID)
	if err != nil && !errors.Is(err, sqldb.ErrNoRows) {
		return AgentProposalResult{}, err
	}
	if pendingID != nil {
		return AgentProposalResult{}, apperr.Conflict("已有未应用的图提案，请先确认或取消")
	}
	applied, err := loadAppliedGraph(ctx, tx, *row)
	if err != nil {
		return AgentProposalResult{}, err
	}
	if _, err := Apply(applied, changeSet); err != nil {
		var e apperr.Error
		if errors.As(err, &e) && e.Status == http.StatusBadRequest {
			return AgentProposalResult{}, apperr.Conflict("图提案无法应用到当前工作流: " + e.Detail)
		}
		return AgentProposalResult{}, err
	}
	raw, err := MarshalChangeSet(changeSet)
	if err != nil {
		return AgentProposalResult{}, err
	}
	id := clockid.New()
	var conversation any
	if strings.TrimSpace(conversationID) == "" {
		conversation = nil
	} else {
		conversation = conversationID
	}
	_, err = pfdb.Exec(ctx, tx, `
		INSERT INTO workflow_graph_proposals (
			id, graph_id, conversation_id, status, summary, base_graph_revision, change_set_json, created_at
		) VALUES ($1, $2, $3, 'pending', $4, $5, $6, NOW())
	`, id, row.ID, conversation, changeSet.Summary, row.Revision, raw)
	if err != nil {
		return AgentProposalResult{}, err
	}
	return AgentProposalResult{
		ID:                id,
		GraphID:           row.ID,
		BaseGraphRevision: row.Revision,
		Summary:           changeSet.Summary,
	}, nil
}

func ConfirmProposal(ctx context.Context, tx *gorm.DB, productID, graphID, proposalID string) (GraphRow, error) {
	row, err := loadGraphForUpdate(ctx, tx, productID, graphID)
	if err != nil {
		return GraphRow{}, err
	}
	proposal, err := loadProposalForUpdate(ctx, tx, row.ID, proposalID)
	if err != nil {
		return GraphRow{}, err
	}
	if proposal.Status != "pending" {
		return GraphRow{}, apperr.Conflict("图提案已经结束")
	}
	if proposal.BaseGraphRevision != row.Revision {
		return GraphRow{}, apperr.Conflict("图 revision 已变化，请刷新后重试")
	}
	parsed, err := ParseChangeSet(proposal.ChangeSetJSON)
	if err != nil {
		return GraphRow{}, err
	}
	parsed.BaseGraphRevision = row.Revision
	parsed.ActorType = ActorAgent
	result, err := Mutate(ctx, tx, productID, row.ID, parsed, HistoryEdit)
	if err != nil {
		return GraphRow{}, err
	}
	if _, err := pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_proposals
		SET status = 'confirmed', resolved_at = NOW(), operation_group_id = $2
		WHERE id = $1
	`, proposal.ID, result.OperationGroupID); err != nil {
		return GraphRow{}, err
	}
	row.Revision = result.Revision
	return row, nil
}

func DiscardProposal(ctx context.Context, tx *gorm.DB, productID, graphID, proposalID string) error {
	row, err := loadGraph(ctx, tx, productID, graphID)
	if err != nil {
		return err
	}
	proposal, err := loadProposalForUpdate(ctx, tx, row.ID, proposalID)
	if err != nil {
		return err
	}
	if proposal.Status != "pending" {
		return apperr.Conflict("图提案已经结束")
	}
	_, err = pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_proposals
		SET status = 'discarded', resolved_at = NOW()
		WHERE id = $1
	`, proposal.ID)
	return err
}

func pendingProposalView(ctx context.Context, tx *gorm.DB, row GraphRow, applied AppliedGraph) (*ProposalView, error) {
	var proposal proposalRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, summary, status, base_graph_revision, change_set_json
		FROM workflow_graph_proposals
		WHERE graph_id = $1 AND status = 'pending'
	`, row.ID).Scan(&proposal.ID, &proposal.Summary, &proposal.Status, &proposal.BaseGraphRevision, &proposal.ChangeSetJSON)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	view := &ProposalView{
		ID:                proposal.ID,
		Summary:           proposal.Summary,
		BaseGraphRevision: proposal.BaseGraphRevision,
		Stale:             proposal.BaseGraphRevision != applied.Revision,
		AddedNodes:        []ProposalNodeView{},
		AddedEdges:        []ProposalEdgeView{},
		DeletedNodeIDs:    []string{},
		DeletedEdgeIDs:    []string{},
		ChangedNodeIDs:    []string{},
	}
	if view.Stale {
		return view, nil
	}
	parsed, err := ParseChangeSet(proposal.ChangeSetJSON)
	if err != nil {
		view.Stale = true
		return view, nil
	}
	after, err := Apply(applied, parsed)
	if err != nil {
		view.Stale = true
		return view, nil
	}
	beforeNodes := map[string]AppliedNode{}
	for _, node := range applied.Nodes {
		beforeNodes[node.ID] = node
	}
	afterNodes := map[string]AppliedNode{}
	for _, node := range after.Nodes {
		afterNodes[node.ID] = node
		if _, ok := beforeNodes[node.ID]; !ok {
			view.AddedNodes = append(view.AddedNodes, ProposalNodeView{
				ID: node.ID, NodeType: node.NodeType, Title: node.Title,
				PositionX: node.PositionX, PositionY: node.PositionY,
				GroupID: node.GroupID, Config: nonemptyMap(node.Config),
			})
		}
	}
	beforeEdges := map[string]AppliedEdge{}
	for _, edge := range applied.Edges {
		beforeEdges[edge.ID] = edge
	}
	afterEdges := map[string]AppliedEdge{}
	for _, edge := range after.Edges {
		afterEdges[edge.ID] = edge
		if _, ok := beforeEdges[edge.ID]; !ok {
			view.AddedEdges = append(view.AddedEdges, ProposalEdgeView{
				ID: edge.ID, SourceNodeID: edge.SourceNodeID, TargetNodeID: edge.TargetNodeID,
				Role: string(edge.Role), DataType: string(edge.DataType), Order: edge.Order,
			})
		}
	}
	for id := range beforeNodes {
		if _, ok := afterNodes[id]; !ok {
			view.DeletedNodeIDs = append(view.DeletedNodeIDs, id)
		}
	}
	for id := range beforeEdges {
		if _, ok := afterEdges[id]; !ok {
			view.DeletedEdgeIDs = append(view.DeletedEdgeIDs, id)
		}
	}
	for _, node := range after.Nodes {
		prev, ok := beforeNodes[node.ID]
		if !ok {
			continue
		}
		if node.Title != prev.Title || !mapsEqual(node.Config, prev.Config) ||
			node.PositionX != prev.PositionX || node.PositionY != prev.PositionY ||
			!samePtr(node.GroupID, prev.GroupID) || !samePtr(node.BoundAssetID, prev.BoundAssetID) {
			view.ChangedNodeIDs = append(view.ChangedNodeIDs, node.ID)
		}
	}
	return view, nil
}

func loadProposalForUpdate(ctx context.Context, tx *gorm.DB, graphID, proposalID string) (proposalRow, error) {
	var row proposalRow
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, summary, status, base_graph_revision, change_set_json
		FROM workflow_graph_proposals
		WHERE id = $1 AND graph_id = $2
		FOR UPDATE
	`, proposalID, graphID).Scan(&row.ID, &row.Summary, &row.Status, &row.BaseGraphRevision, &row.ChangeSetJSON)
	if errors.Is(err, sqldb.ErrNoRows) {
		return proposalRow{}, apperr.NotFound("图提案不存在")
	}
	return row, err
}
