package graph

import (
	"context"
	"io"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type Service struct {
	DB             *gorm.DB
	AfterRunStatus func(ctx context.Context, tx *gorm.DB, runID string) error
	Products       ProductGuard
}

func (s Service) guardCtx(ctx context.Context) context.Context {
	return WithProductGuard(ctx, s.Products)
}

func (s Service) CreateEmpty(ctx context.Context, productID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := CreateEmpty(ctx, pgxTx, productID, DefaultGraphTitle)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

func (s Service) Current(ctx context.Context, productID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

func (s Service) Get(ctx context.Context, productID, graphID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadGraph(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

func (s Service) ApplyChangeSet(ctx context.Context, productID, graphID string, changeSet ChangeSet) (Projection, error) {
	return s.applyChangeSet(ctx, productID, graphID, changeSet, ActorUser)
}

// ApplyAgentChangeSet 立即写入一条 Agent 可逆命令；actor 固定为 agent。
func (s Service) ApplyAgentChangeSet(ctx context.Context, productID, graphID string, changeSet ChangeSet) (Projection, error) {
	if len(changeSet.Operations) != 1 {
		return Projection{}, apperr.Validation("立即写入只接受一条可逆改图命令；多步改图请提交提案")
	}
	return s.applyChangeSet(ctx, productID, graphID, changeSet, ActorAgent)
}

func (s Service) applyChangeSet(ctx context.Context, productID, graphID string, changeSet ChangeSet, actor ActorType) (Projection, error) {
	ctx = s.guardCtx(ctx)
	changeSet.ActorType = actor
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := Mutate(ctx, pgxTx, productID, graphID, changeSet, HistoryEdit)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

type AgentProposalResult struct {
	ID                string
	GraphID           string
	BaseGraphRevision int
	Summary           string
}

// CreateAgentProposal 只存 PENDING 提案，不改 live 图。
func (s Service) CreateAgentProposal(ctx context.Context, productID, conversationID string, changeSet ChangeSet) (AgentProposalResult, error) {
	var out AgentProposalResult
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := CreateProposal(ctx, pgxTx, productID, conversationID, changeSet)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	return out, err
}

// TryCurrent 没有 active 图时返回 nil。
func (s Service) TryCurrent(ctx context.Context, productID string) (*Projection, error) {
	ctx = s.guardCtx(ctx)
	var out *Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := TryLoadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		proj, err := Project(ctx, pgxTx, *row)
		if err != nil {
			return err
		}
		out = &proj
		return nil
	})
	return out, err
}

func (s Service) Undo(ctx context.Context, productID, graphID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := Undo(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

func (s Service) Redo(ctx context.Context, productID, graphID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := Redo(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

func (s Service) ConfirmProposal(ctx context.Context, productID, graphID, proposalID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := ConfirmProposal(ctx, pgxTx, productID, graphID, proposalID)
		if err != nil {
			return err
		}
		active, err := loadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			active = row
		}
		out, err = Project(ctx, pgxTx, active.Identity)
		return err
	})
	return out, err
}

func (s Service) DiscardProposal(ctx context.Context, productID, graphID, proposalID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := DiscardProposal(ctx, pgxTx, productID, graphID, proposalID); err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

func ParseChangeSetReader(r io.Reader) (ChangeSet, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return ChangeSet{}, err
	}
	return ParseChangeSet(raw)
}

func (s Service) SubmitRun(ctx context.Context, productID, graphID, scope string, nodeID *string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		out, err = s.SubmitRunTx(ctx, pgxTx, productID, graphID, scope, nodeID)
		return err
	})
	return out, err
}

// SubmitRunTx 在调用方已有的事务里提交 GraphRun。
func (s Service) SubmitRunTx(ctx context.Context, pgxTx *gorm.DB, productID, graphID, scope string, nodeID *string) (GraphRunResponse, error) {
	ctx = s.guardCtx(ctx)
	submission, err := submitGraphRun(ctx, pgxTx, productID, graphID, scope, nodeID)
	if err != nil {
		return GraphRunResponse{}, err
	}
	return serializeGraphRun(submission.Run), nil
}

func (s Service) ListRuns(ctx context.Context, productID, graphID string) (GraphRunListResponse, error) {
	var out GraphRunListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		runs, err := listGraphRuns(ctx, pgxTx, productID, graphID, 20)
		if err != nil {
			return err
		}
		items := make([]GraphRunResponse, 0, len(runs))
		for _, run := range runs {
			items = append(items, serializeGraphRun(run))
		}
		out = GraphRunListResponse{Items: items}
		return nil
	})
	return out, err
}

func (s Service) GetRun(ctx context.Context, productID, graphID, runID string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		run, err := loadGraphRun(ctx, pgxTx, productID, graphID, runID)
		if err != nil {
			return err
		}
		out = serializeGraphRun(run)
		return nil
	})
	return out, err
}

func (s Service) CancelRun(ctx context.Context, productID, graphID, runID string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		out, err = s.CancelRunTx(ctx, pgxTx, productID, graphID, runID)
		return err
	})
	return out, err
}

// CancelRunTx 在调用方已有的事务里取消 GraphRun。
func (s Service) CancelRunTx(ctx context.Context, pgxTx *gorm.DB, productID, graphID, runID string) (GraphRunResponse, error) {
	run, err := cancelGraphRun(ctx, pgxTx, productID, graphID, runID)
	if err != nil {
		return GraphRunResponse{}, err
	}
	if s.AfterRunStatus != nil {
		if err := s.AfterRunStatus(ctx, pgxTx, run.ID); err != nil {
			return GraphRunResponse{}, err
		}
	}
	return serializeGraphRun(run), nil
}

func (s Service) RetryRun(ctx context.Context, productID, graphID, runID string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		out, err = s.RetryRunTx(ctx, pgxTx, productID, graphID, runID)
		return err
	})
	return out, err
}

// RetryRunTx 在调用方已有的事务里重试 GraphRun。
func (s Service) RetryRunTx(ctx context.Context, pgxTx *gorm.DB, productID, graphID, runID string) (GraphRunResponse, error) {
	ctx = s.guardCtx(ctx)
	submission, err := retryGraphRun(ctx, pgxTx, productID, graphID, runID)
	if err != nil {
		return GraphRunResponse{}, err
	}
	return serializeGraphRun(submission.Run), nil
}
