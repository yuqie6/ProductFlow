package graph

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

type proposalRow struct {
	ID                string
	Summary           string
	Status            string
	BaseGraphRevision int
	ChangeSetJSON     []byte
}

func proposalFromSchema(rec schema.WorkflowGraphProposals) proposalRow {
	return proposalRow{
		ID:                rec.ID,
		Summary:           rec.Summary,
		Status:            rec.Status,
		BaseGraphRevision: rec.BaseGraphRevision,
		ChangeSetJSON:     []byte(rec.ChangeSetJSON),
	}
}

// CreateProposal 校验后写入 PENDING 提案，不改 live 图。
// 无 active 图、revision 已变或已有 pending 提案返回 Conflict；Apply 的 Validation 也升成 Conflict。
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
	var pending schema.WorkflowGraphProposals
	err = tx.WithContext(ctx).Where("graph_id = ? AND status = ?", row.ID, "pending").Take(&pending).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentProposalResult{}, err
	}
	if err == nil {
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
	var conversation *string
	if trimmed := strings.TrimSpace(conversationID); trimmed != "" {
		conversation = &trimmed
	}
	err = tx.WithContext(ctx).Create(&schema.WorkflowGraphProposals{
		ID:                id,
		GraphID:           row.ID,
		ConversationID:    conversation,
		Status:            "pending",
		Summary:           changeSet.Summary,
		BaseGraphRevision: row.Revision,
		ChangeSetJSON:     string(raw),
		CreatedAt:         time.Now().UTC(),
	}).Error
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

// ConfirmProposal 把 PENDING 提案 Mutate 进 live 图。非 pending 返回 NotPending。
func ConfirmProposal(ctx context.Context, tx *gorm.DB, productID, graphID, proposalID string) (graphRow, error) {
	row, err := loadGraphForUpdate(ctx, tx, productID, graphID)
	if err != nil {
		return graphRow{}, err
	}
	proposal, err := loadProposalForUpdate(ctx, tx, row.ID, proposalID)
	if err != nil {
		return graphRow{}, err
	}
	if proposal.Status != "pending" {
		return graphRow{}, apperr.NotPending("图提案已经结束")
	}
	if proposal.BaseGraphRevision != row.Revision {
		return graphRow{}, apperr.Conflict("图 revision 已变化，请刷新后重试")
	}
	parsed, err := ParseChangeSet(proposal.ChangeSetJSON)
	if err != nil {
		return graphRow{}, err
	}
	parsed.BaseGraphRevision = row.Revision
	parsed.ActorType = ActorAgent
	result, err := Mutate(ctx, tx, productID, row.ID, parsed, HistoryEdit)
	if err != nil {
		return graphRow{}, err
	}
	if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphProposals{}).Where("id = ?", proposal.ID).Updates(map[string]any{
		"status":             "confirmed",
		"resolved_at":        time.Now().UTC(),
		"operation_group_id": result.OperationGroupID,
	}).Error; err != nil {
		return graphRow{}, err
	}
	row.Revision = result.Revision
	return row, nil
}

// DiscardProposal 把提案标 discarded，不改 live 图。
// 缺图或缺提案 NotFound；非 pending 返回 NotPending。
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
		return apperr.NotPending("图提案已经结束")
	}
	return tx.WithContext(ctx).Model(&schema.WorkflowGraphProposals{}).Where("id = ?", proposal.ID).Updates(map[string]any{
		"status":      "discarded",
		"resolved_at": time.Now().UTC(),
	}).Error
}

// pendingProposalView 读该图唯一 PENDING 提案并相对当前 revision 投影。没有返回 nil,nil。
// Stale 表示 base 已落后，确认前须先处理冲突。不改 live 图。
func pendingProposalView(ctx context.Context, tx *gorm.DB, row graphRow, applied AppliedGraph) (*ProposalView, error) {
	var rec schema.WorkflowGraphProposals
	err := tx.WithContext(ctx).Where("graph_id = ? AND status = ?", row.ID, "pending").Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	proposal := proposalFromSchema(rec)
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
	var rec schema.WorkflowGraphProposals
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND graph_id = ?", proposalID, graphID).
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return proposalRow{}, apperr.NotFound("图提案不存在")
	}
	if err != nil {
		return proposalRow{}, err
	}
	return proposalFromSchema(rec), nil
}
