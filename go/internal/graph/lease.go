package graph

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	// Business execution ownership survives queue redelivery. Recovery waits for
	// this lease to expire before taking over a crashed executor.
	graphRunExecutionLeaseDuration   = 35 * time.Minute
	graphRunExecutionLeaseRenewEvery = 5 * time.Minute
)

var errGraphRunLeaseLost = errors.New("graph run execution lease lost")

type graphRunLeaseContextKey struct{}

func withGraphRunLease(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, graphRunLeaseContextKey{}, token)
}

func graphRunLeaseToken(ctx context.Context) string {
	token, _ := ctx.Value(graphRunLeaseContextKey{}).(string)
	return token
}

func graphRunLeaseOwned(ctx context.Context, run graphRunRow) error {
	return graphRunLeaseValuesOwned(ctx, run.ExecutionLeaseToken, run.ExecutionLeaseExpiresAt)
}

func graphRunLeaseSchemaOwned(ctx context.Context, run schema.WorkflowGraphRuns) error {
	return graphRunLeaseValuesOwned(ctx, run.ExecutionLeaseToken, run.ExecutionLeaseExpiresAt)
}

func graphRunLeaseValuesOwned(ctx context.Context, token *string, expiresAt *time.Time) error {
	expected := graphRunLeaseToken(ctx)
	if expected == "" {
		return nil
	}
	if token == nil || *token != expected || expiresAt == nil || !expiresAt.After(time.Now().UTC()) {
		return errGraphRunLeaseLost
	}
	return nil
}

// acquireGraphRunLease claims a running row with a compare-and-set on its expiry.
// active distinguishes a missing/terminal/queued run from a running run owned by another worker.
func acquireGraphRunLease(ctx context.Context, db *gorm.DB, runID, token string) (active, acquired bool, err error) {
	now := time.Now().UTC()
	leaseUntil := now.Add(graphRunExecutionLeaseDuration)
	var updated int64
	err = tx.WithGorm(ctx, db, func(dbTx *gorm.DB) error {
		if err := queue.AssertExecution(ctx, dbTx, queue.ActorGraphRun, runID); err != nil {
			return err
		}
		result := dbTx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
			Where("id = ? AND status = ? AND (execution_lease_expires_at IS NULL OR execution_lease_expires_at <= ?)", runID, RunStatusRunning, now).
			Updates(map[string]any{
				"execution_lease_token":      token,
				"execution_lease_expires_at": leaseUntil,
			})
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected
		return nil
	})
	if err != nil {
		return false, false, err
	}
	if updated == 1 {
		return true, true, nil
	}

	var current schema.WorkflowGraphRuns
	if err := db.WithContext(ctx).Select("status", "execution_lease_token", "execution_lease_expires_at").
		Where("id = ?", runID).Take(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	return current.Status == RunStatusRunning, false, nil
}

func renewGraphRunLease(ctx context.Context, db *gorm.DB, runID, token string) (bool, error) {
	now := time.Now().UTC()
	leaseUntil := now.Add(graphRunExecutionLeaseDuration)
	var updated int64
	err := tx.WithGorm(ctx, db, func(dbTx *gorm.DB) error {
		result := dbTx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
			Where("id = ? AND status = ? AND execution_lease_token = ? AND execution_lease_expires_at > ?", runID, RunStatusRunning, token, now).
			Updates(map[string]any{"execution_lease_expires_at": leaseUntil})
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected
		return nil
	})
	return updated == 1, err
}

func releaseGraphRunLease(ctx context.Context, db *gorm.DB, runID, token string) (bool, error) {
	var updated int64
	err := tx.WithGorm(ctx, db, func(dbTx *gorm.DB) error {
		result := dbTx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
			Where("id = ? AND execution_lease_token = ?", runID, token).
			Updates(map[string]any{
				"execution_lease_token":      nil,
				"execution_lease_expires_at": nil,
			})
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected
		return nil
	})
	return updated == 1, err
}

// maintainGraphRunLease renews in the background and cancels the caller through onLost when fencing fails.
func maintainGraphRunLease(ctx context.Context, db *gorm.DB, runID, token string, onLost func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(graphRunExecutionLeaseRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				owned, err := renewGraphRunLease(ctx, db, runID, token)
				if ctx.Err() != nil {
					return
				}
				if err != nil || !owned {
					onLost()
					return
				}
			}
		}
	}()
	return done
}
