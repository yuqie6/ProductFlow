package graph

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	graphRunAdvisoryLockNamespace = 847261
)

var graphRunLocks sync.Map

type Executor struct {
	DB             *gorm.DB
	Deps           Dependencies
	Log            *zap.Logger
	AfterRunStatus func(ctx context.Context, tx *gorm.DB, runID string) error
}

// ExecuteRun 是 worker 入口：同一 run 只允许一个 worker；无法证明的 provider 结果标 unknown。
func (e Executor) ExecuteRun(ctx context.Context, runID string) error {
	e.logger().Info("graph run", zap.String("workflow_run_id", runID))
	unlock, ok := tryProcessLock(runID)
	if !ok {
		return queue.ErrBusy
	}
	defer unlock()

	sqlDB, err := e.DB.DB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	acquired, err := tryAdvisoryLock(ctx, conn, runID)
	if err != nil {
		return err
	}
	if !acquired {
		return queue.ErrBusy
	}
	defer func() { _ = releaseAdvisoryLock(context.Background(), conn, runID) }()

	if err := e.executeLoop(ctx, runID); err != nil {
		if errors.Is(err, queue.ErrBusy) || errors.Is(err, queue.ErrLater) {
			return err
		}
		if isMissingGraphRun(err) {
			return nil
		}
		if isProviderUnknown(err) {
			e.notifyRunStatus(ctx, runID)
			return nil
		}
		if failErr := failGraphRun(ctx, e.DB, runID, "工作流运行失败"); failErr != nil {
			if isMissingGraphRun(failErr) {
				return nil
			}
			return failErr
		}
		e.notifyRunStatus(ctx, runID)
		return nil
	}
	e.notifyRunStatus(ctx, runID)
	return nil
}

func (e Executor) notifyRunStatus(ctx context.Context, runID string) {
	if e.AfterRunStatus == nil {
		return
	}
	_ = tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		return e.AfterRunStatus(ctx, pgxTx, runID)
	})
}

func (e Executor) logger() *zap.Logger {
	if e.Log != nil {
		return e.Log
	}
	return zap.NewNop()
}

func (e Executor) executeLoop(ctx context.Context, runID string) error {
	const capacityWait = 250 * time.Millisecond
	type nodeOutcome struct{ err error }
	outcomes := make(chan nodeOutcome, 64)
	inflight := 0
	stopClaiming := false
	defer func() {
		for inflight > 0 {
			<-outcomes
			inflight--
		}
	}()

	waitOne := func(block bool) error {
		if inflight == 0 {
			return nil
		}
		if block {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case item := <-outcomes:
				inflight--
				return noteNodeOutcome(item.err, &stopClaiming)
			}
		}
		timer := time.NewTimer(capacityWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item := <-outcomes:
			inflight--
			return noteNodeOutcome(item.err, &stopClaiming)
		case <-timer.C:
			return nil
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var stop bool
		err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
			run, err := loadGraphRunByID(ctx, pgxTx, runID)
			if isMissingGraphRun(err) {
				stop = true
				return nil
			}
			if err != nil {
				return err
			}
			if run.Status != RunStatusRunning {
				stop = true
				return nil
			}
			applied, err := appliedGraphFromSnapshot(run.Snapshot)
			if err != nil {
				return err
			}
			if err := failBlockedQueuedNodes(ctx, pgxTx, applied, run.NodeRuns); err != nil {
				return err
			}
			done, err := completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
			if err != nil {
				return err
			}
			if done {
				stop = true
				return nil
			}
			return nil
		})
		if err != nil || stop {
			return err
		}

		run, err := e.loadRun(ctx, runID)
		if isMissingGraphRun(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if run.Status != RunStatusRunning {
			return nil
		}
		applied, err := appliedGraphFromSnapshot(run.Snapshot)
		if err != nil {
			return err
		}
		var ready []graphNodeRunRow
		for _, item := range run.NodeRuns {
			if item.Status == NodeRunQueued && processingUpstreamState(applied, run.NodeRuns, item) == "ready" {
				ready = append(ready, item)
			}
		}
		if (stopClaiming || len(ready) == 0) && inflight == 0 {
			return e.finishOrLater(ctx, runID)
		}

		claimed := 0
		waitingCapacity := false
		if !stopClaiming {
			for _, nodeRun := range ready {
				ok, err := claimQueuedNodeRun(ctx, e.DB, nodeRun.ID)
				if errors.Is(err, errWaitingCapacity) {
					waitingCapacity = true
					continue
				}
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				claimed++
				inflight++
				go func(nodeRunID string) {
					outcomes <- nodeOutcome{err: e.executeClaimedNode(ctx, runID, nodeRunID)}
				}(nodeRun.ID)
			}
		}
		if inflight > 0 {
			block := claimed > 0 || !waitingCapacity
			if err := waitOne(block); err != nil {
				return err
			}
			continue
		}
		if waitingCapacity {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(capacityWait):
			}
			continue
		}
		return queue.ErrBusy
	}
}

func noteNodeOutcome(err error, stopClaiming *bool) error {
	if err == nil || isProviderUnknown(err) {
		return nil
	}
	*stopClaiming = true
	return err
}

func isMissingGraphRun(err error) bool {
	return err != nil && (apperr.IsNotFound(err) || errors.Is(err, gorm.ErrRecordNotFound))
}

func (e Executor) loadRun(ctx context.Context, runID string) (graphRunRow, error) {
	var run graphRunRow
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadGraphRunByID(ctx, pgxTx, runID)
		if err != nil {
			return err
		}
		run = loaded
		return nil
	})
	return run, err
}

func failBlockedQueuedNodes(ctx context.Context, tx *gorm.DB, graph AppliedGraph, nodeRuns []graphNodeRunRow) error {
	now := time.Now().UTC()
	for {
		progressed := false
		for i := range nodeRuns {
			item := nodeRuns[i]
			if item.Status != NodeRunQueued {
				continue
			}
			if processingUpstreamState(graph, nodeRuns, item) != "blocked" {
				continue
			}
			if err := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
				Where("id = ? AND status = ?", item.ID, "queued").
				Updates(map[string]any{
					"status":              "failed",
					"failure_reason":      "上游处理节点未成功",
					"finished_at":         now,
					"active_attempt_id":   nil,
					"progress_updated_at": now,
				}).Error; err != nil {
				return err
			}
			nodeRuns[i].Status = NodeRunFailed
			progressed = true
		}
		if !progressed {
			return nil
		}
	}
}

func processingUpstreamState(graph AppliedGraph, nodeRuns []graphNodeRunRow, nodeRun graphNodeRunRow) string {
	if nodeRun.NodeID == nil {
		return "ready"
	}
	byNode := map[string]graphNodeRunRow{}
	for _, item := range nodeRuns {
		if item.NodeID != nil {
			byNode[*item.NodeID] = item
		}
	}
	waiting := false
	for _, edge := range graph.Incoming(*nodeRun.NodeID) {
		source, err := graph.Node(edge.SourceNodeID)
		if err != nil {
			continue
		}
		if !IsProcessingNode(source.NodeType) {
			continue
		}
		upstream, ok := byNode[source.ID]
		if !ok {
			continue
		}
		if upstream.Status == NodeRunSucceeded {
			continue
		}
		if upstream.Status == NodeRunQueued || upstream.Status == NodeRunRunning {
			waiting = true
			continue
		}
		return "blocked"
	}
	if waiting {
		return "wait"
	}
	return "ready"
}

func tryProcessLock(runID string) (func(), bool) {
	mu := &sync.Mutex{}
	actual, _ := graphRunLocks.LoadOrStore(runID, mu)
	lock := actual.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, false
	}
	return func() {
		lock.Unlock()
		graphRunLocks.CompareAndDelete(runID, lock)
	}, true
}

func graphRunAdvisoryKeys(runID string) (int32, int32) {
	hexID := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(runID)), "-", "")
	mod := uint64(1 << 31)
	if len(hexID) == 32 {
		var acc uint64
		for i := 0; i < 32; i += 2 {
			var b byte
			hi := hexNibble(hexID[i])
			lo := hexNibble(hexID[i+1])
			b = hi<<4 | lo
			acc = (acc*256 + uint64(b)) % mod
		}
		return graphRunAdvisoryLockNamespace, int32(acc)
	}
	sum := sha256.Sum256([]byte(runID))
	ident := binary.BigEndian.Uint64(sum[:8]) % mod
	return graphRunAdvisoryLockNamespace, int32(ident)
}

func hexNibble(ch byte) byte {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0'
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10
	default:
		return 0
	}
}

func tryAdvisoryLock(ctx context.Context, conn *sqldb.Conn, runID string) (bool, error) {
	ns, key := graphRunAdvisoryKeys(runID)
	var acquired bool
	err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`, ns, key).Scan(&acquired)
	return acquired, err
}

func releaseAdvisoryLock(ctx context.Context, conn *sqldb.Conn, runID string) error {
	ns, key := graphRunAdvisoryKeys(runID)
	_, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1, $2)`, ns, key)
	return err
}

func (e Executor) finishOrLater(ctx context.Context, runID string) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		run, err := loadGraphRunByID(ctx, pgxTx, runID)
		if isMissingGraphRun(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if run.Status != RunStatusRunning {
			return nil
		}
		applied, err := appliedGraphFromSnapshot(run.Snapshot)
		if err != nil {
			return err
		}
		if err := failBlockedQueuedNodes(ctx, pgxTx, applied, run.NodeRuns); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
		return err
	})
}

func loadProductIDForGraph(ctx context.Context, tx *gorm.DB, graphID string) (string, error) {
	var rec schema.WorkflowGraphs
	err := tx.WithContext(ctx).Select("product_id").Where("id = ?", graphID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", apperr.NotFound("商品工作流不存在")
	}
	if err != nil {
		return "", err
	}
	return rec.ProductID, nil
}
