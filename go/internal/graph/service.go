package graph

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

type Service struct {
	Pool *pgxpool.Pool
}

func (s Service) CreateEmpty(ctx context.Context, productID string) (Projection, error) {
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		row, err := CreateEmpty(ctx, pgxTx, productID, DefaultGraphTitle)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row)
		return err
	})
	return out, err
}

func (s Service) Current(ctx context.Context, productID string) (Projection, error) {
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		row, err := loadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row)
		return err
	})
	return out, err
}

func (s Service) Get(ctx context.Context, productID, graphID string) (Projection, error) {
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		row, err := loadGraph(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row)
		return err
	})
	return out, err
}

func (s Service) ApplyChangeSet(ctx context.Context, productID, graphID string, changeSet ChangeSet) (Projection, error) {
	changeSet.ActorType = ActorUser
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		result, err := Mutate(ctx, pgxTx, productID, graphID, changeSet, HistoryEdit)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row)
		return err
	})
	return out, err
}

func (s Service) Undo(ctx context.Context, productID, graphID string) (Projection, error) {
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		result, err := Undo(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row)
		return err
	})
	return out, err
}

func (s Service) Redo(ctx context.Context, productID, graphID string) (Projection, error) {
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		result, err := Redo(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row)
		return err
	})
	return out, err
}

func (s Service) ConfirmProposal(ctx context.Context, productID, graphID, proposalID string) (Projection, error) {
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		row, err := ConfirmProposal(ctx, pgxTx, productID, graphID, proposalID)
		if err != nil {
			return err
		}
		active, err := loadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			active = row
		}
		out, err = Project(ctx, pgxTx, active)
		return err
	})
	return out, err
}

func (s Service) DiscardProposal(ctx context.Context, productID, graphID, proposalID string) (Projection, error) {
	var out Projection
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if err := DiscardProposal(ctx, pgxTx, productID, graphID, proposalID); err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row)
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
