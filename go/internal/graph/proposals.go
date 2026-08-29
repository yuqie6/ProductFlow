package graph

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

type proposalRow struct {
	ID                string
	Summary           string
	Status            string
	BaseGraphRevision int
	ChangeSetJSON     []byte
}

func ConfirmProposal(ctx context.Context, tx pgx.Tx, productID, graphID, proposalID string) (GraphRow, error) {
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
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_graph_proposals
		SET status = 'confirmed', resolved_at = NOW(), operation_group_id = $2
		WHERE id = $1
	`, proposal.ID, result.OperationGroupID); err != nil {
		return GraphRow{}, err
	}
	row.Revision = result.Revision
	return row, nil
}

func DiscardProposal(ctx context.Context, tx pgx.Tx, productID, graphID, proposalID string) error {
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
	_, err = tx.Exec(ctx, `
		UPDATE workflow_graph_proposals
		SET status = 'discarded', resolved_at = NOW()
		WHERE id = $1
	`, proposal.ID)
	return err
}

func pendingProposalView(ctx context.Context, tx pgx.Tx, row GraphRow, applied AppliedGraph) (*ProposalView, error) {
	var proposal proposalRow
	err := tx.QueryRow(ctx, `
		SELECT id, summary, status, base_graph_revision, change_set_json
		FROM workflow_graph_proposals
		WHERE graph_id = $1 AND status = 'pending'
	`, row.ID).Scan(&proposal.ID, &proposal.Summary, &proposal.Status, &proposal.BaseGraphRevision, &proposal.ChangeSetJSON)
	if errors.Is(err, pgx.ErrNoRows) {
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

func loadProposalForUpdate(ctx context.Context, tx pgx.Tx, graphID, proposalID string) (proposalRow, error) {
	var row proposalRow
	err := tx.QueryRow(ctx, `
		SELECT id, summary, status, base_graph_revision, change_set_json
		FROM workflow_graph_proposals
		WHERE id = $1 AND graph_id = $2
		FOR UPDATE
	`, proposalID, graphID).Scan(&row.ID, &row.Summary, &row.Status, &row.BaseGraphRevision, &row.ChangeSetJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return proposalRow{}, apperr.NotFound("图提案不存在")
	}
	return row, err
}
